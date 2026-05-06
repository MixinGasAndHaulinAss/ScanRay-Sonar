package collector

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"

	scrypto "github.com/NCLGISA/ScanRay-Sonar/internal/crypto"
	"github.com/NCLGISA/ScanRay-Sonar/internal/snmp"
)

type snmpTargetRow struct {
	ID                  uuid.UUID `json:"id"`
	MgmtIP              string    `json:"mgmtIp"`
	SNMPVersion         string    `json:"snmpVersion"`
	Vendor              string    `json:"vendor"`
	PollIntervalSeconds int       `json:"pollIntervalSeconds"`
	EncSNMPCreds        string    `json:"encSnmpCreds"` // base64
}

// RunSNMPPoller polls SNMP for appliances assigned to this collector and pushes snapshots over HTTPS.
func RunSNMPPoller(ctx context.Context, log *slog.Logger, cfg *Config) error {
	mkb := os.Getenv("SONAR_MASTER_KEY")
	if mkb == "" {
		log.Info("SONAR_MASTER_KEY unset — skipping SNMP polling (websocket-only collector)")
		<-ctx.Done()
		return nil
	}
	sealer, err := scrypto.NewSealer(mkb)
	if err != nil {
		return fmt.Errorf("collector sealer: %w", err)
	}

	rt := &rateTracker{}
	cli := &http.Client{Timeout: 120 * time.Second}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			targets, err := fetchTargets(ctx, cli, cfg)
			if err != nil {
				log.Warn("fetch snmp targets failed", "err", err)
				continue
			}
			for _, t := range targets {
				t := t
				go func() {
					cctx, cc := context.WithTimeout(ctx, 45*time.Second)
					defer cc()
					if err := pollOne(cctx, cli, cfg, sealer, rt, t); err != nil {
						log.Warn("collector snmp poll failed", "appliance_id", t.ID.String(), "err", err)
						_ = postPollError(cctx, cli, cfg, t.ID, err.Error())
					}
				}()
			}
		}
	}
}

func fetchTargets(ctx context.Context, cli *http.Client, cfg *Config) ([]snmpTargetRow, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		cfg.BaseURL+"/api/v1/collectors/me/snmp-targets", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.JWT)
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("targets %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		Targets []snmpTargetRow `json:"targets"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out.Targets, nil
}

func pollOne(ctx context.Context, cli *http.Client, cfg *Config, sealer *scrypto.Sealer, rt *rateTracker, t snmpTargetRow) error {
	raw, err := base64.StdEncoding.DecodeString(t.EncSNMPCreds)
	if err != nil {
		return fmt.Errorf("decode creds: %w", err)
	}
	plain, err := sealer.Open(raw, []byte("appliance:"+t.ID.String()))
	if err != nil {
		return fmt.Errorf("open creds: %w", err)
	}
	var creds snmp.Creds
	if err := json.Unmarshal(plain, &creds); err != nil {
		return fmt.Errorf("creds json: %w", err)
	}

	c, err := snmp.Dial(ctx, snmp.Target{Host: t.MgmtIP}, creds)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer c.Close()

	snap := snmp.CollectAll(ctx, c)
	switch t.Vendor {
	case "cisco":
		snap.Chassis = snmp.CollectCiscoChassis(ctx, c)
	}

	rt.applyRates(t.ID, &snap)

	body, err := json.Marshal(map[string]any{
		"applianceId": t.ID.String(),
		"snapshot":    snap,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		cfg.BaseURL+"/api/v1/collectors/me/snmp-result", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.JWT)
	req.Header.Set("Content-Type", "application/json")
	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("post snapshot %d: %s", resp.StatusCode, string(rb))
	}
	return nil
}

func postPollError(ctx context.Context, cli *http.Client, cfg *Config, applianceID uuid.UUID, msg string) error {
	body, _ := json.Marshal(map[string]any{
		"applianceId": applianceID.String(),
		"error":       msg,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		cfg.BaseURL+"/api/v1/collectors/me/snmp-error", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.JWT)
	req.Header.Set("Content-Type", "application/json")
	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

type rateKey struct {
	apID uuid.UUID
	idx  int32
}

type rateState struct {
	t           time.Time
	inOctets    uint64
	outOctets   uint64
	inErrors    uint64
	outErrors   uint64
	inDiscards  uint64
	outDiscards uint64
}

type rateTracker struct {
	mu   sync.Mutex
	rate map[rateKey]rateState
}

func (rt *rateTracker) applyRates(id uuid.UUID, snap *snmp.Snapshot) {
	if rt.rate == nil {
		rt.rate = map[rateKey]rateState{}
	}
	now := snap.CapturedAt

	rt.mu.Lock()
	defer rt.mu.Unlock()

	for i := range snap.Interfaces {
		ifc := &snap.Interfaces[i]
		k := rateKey{apID: id, idx: ifc.Index}
		prev, hadPrev := rt.rate[k]

		rt.rate[k] = rateState{
			t:           now,
			inOctets:    ifc.InOctets,
			outOctets:   ifc.OutOctets,
			inErrors:    ifc.InErrors,
			outErrors:   ifc.OutErrors,
			inDiscards:  ifc.InDiscards,
			outDiscards: ifc.OutDiscards,
		}

		if !hadPrev || prev.t.IsZero() {
			continue
		}
		dt := now.Sub(prev.t).Seconds()
		if dt <= 0 {
			continue
		}
		inBps := bpsDelta(prev.inOctets, ifc.InOctets, dt)
		outBps := bpsDelta(prev.outOctets, ifc.OutOctets, dt)
		if inBps != nil {
			ifc.InBps = inBps
		}
		if outBps != nil {
			ifc.OutBps = outBps
		}
	}
}

func bpsDelta(prev, cur uint64, dtSec float64) *uint64 {
	delta := cur - prev
	bps := uint64(float64(delta) * 8 / dtSec)
	const sanityMaxBps = 10_000_000_000_000 // 10 Tbps
	if bps > sanityMaxBps {
		return nil
	}
	return &bps
}
