// Package collector — passive SNMP discovery scheduler on the agent
// side. Pulls site-discovery settings from central, runs a capture +
// classification pass on the configured interface, and POSTs the
// resulting batch back to /api/v1/collectors/me/passive-snmp.
//
// The runner ticks slowly by design (default 6h) — passive discovery
// is a low-frequency inventory rebuild, not a real-time stream. The
// "useful" rate of new device additions is days, not seconds.
package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/NCLGISA/ScanRay-Sonar/internal/collector/discovery"
)

type passiveSNMPSettings struct {
	Enabled         bool   `json:"enabled"`
	Interface       string `json:"interface"`
	CaptureSeconds  int    `json:"captureSeconds"`
	RetireAfter     int    `json:"retireAfter"`
	RunIntervalSecs int    `json:"runIntervalSeconds"`
	// SNMPCommunity is the v2c community we use to probe each
	// captured IP. Empty falls back to "public".
	SNMPCommunity string `json:"snmpCommunity"`
}

// RunPassiveSNMPDiscovery is the long-lived runner. It polls the
// settings every few minutes (cheap GET) so an operator turning the
// feature on doesn't have to wait a whole capture cycle.
func RunPassiveSNMPDiscovery(ctx context.Context, log *slog.Logger, cfg *Config) {
	cli := &http.Client{Timeout: 30 * time.Second}
	settingsCheck := time.NewTicker(5 * time.Minute)
	defer settingsCheck.Stop()

	var (
		nextRun  time.Time
		settings passiveSNMPSettings
	)

	for {
		select {
		case <-ctx.Done():
			return
		case <-settingsCheck.C:
		case <-time.After(time.Until(nextRun)):
		}

		s, ok := fetchPassiveSNMPSettings(ctx, log, cli, cfg)
		if !ok {
			nextRun = time.Now().Add(15 * time.Minute)
			continue
		}
		settings = s
		if !settings.Enabled {
			nextRun = time.Now().Add(15 * time.Minute)
			continue
		}
		if settings.RunIntervalSecs < 600 {
			settings.RunIntervalSecs = 600
		}
		if !nextRun.IsZero() && time.Now().Before(nextRun) {
			continue
		}

		log.Info("passive snmp discovery starting",
			"interface", settings.Interface,
			"capture_s", settings.CaptureSeconds,
			"retire_after", settings.RetireAfter)

		runOnePassiveSNMPCapture(ctx, log, cli, cfg, settings)
		nextRun = time.Now().Add(time.Duration(settings.RunIntervalSecs) * time.Second)
	}
}

func runOnePassiveSNMPCapture(ctx context.Context, log *slog.Logger, cli *http.Client, cfg *Config, s passiveSNMPSettings) {
	community := s.SNMPCommunity
	if community == "" {
		community = "public"
	}
	classifier := discovery.NewSNMPProbe(community, 3*time.Second)

	cctx, cancel := context.WithTimeout(ctx, time.Duration(s.CaptureSeconds+30)*time.Second)
	defer cancel()

	devices, err := discovery.CapturePassiveSNMP(cctx, discovery.PassiveCaptureOpts{
		Interface:      s.Interface,
		CaptureSeconds: s.CaptureSeconds,
	}, classifier)
	if err != nil {
		// Non-Linux builds will hit ErrPassiveCaptureUnsupported on
		// every cycle — log once at info instead of warn.
		if errors.Is(err, discovery.ErrPassiveCaptureUnsupported) {
			log.Info("passive snmp discovery unavailable on this OS")
			return
		}
		log.Warn("passive snmp capture failed", "err", err)
		return
	}
	if len(devices) == 0 {
		log.Info("passive snmp capture finished, no destinations seen")
		return
	}

	wire := make([]map[string]any, 0, len(devices))
	for _, d := range devices {
		wire = append(wire, map[string]any{
			"ip":          d.IP,
			"vendor":      d.Vendor,
			"type":        d.Type,
			"subType":     d.SubType,
			"sysDescr":    d.SysDescr,
			"sysObjectId": d.SysObjectID,
			"sysName":     d.SysName,
			"sysLocation": d.SysLocation,
		})
	}
	body, _ := json.Marshal(map[string]any{
		"capturedAt":  time.Now().UTC(),
		"devices":     wire,
		"retireAfter": s.RetireAfter,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		cfg.BaseURL+"/api/v1/collectors/me/passive-snmp", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+cfg.JWT)
	req.Header.Set("Content-Type", "application/json")
	resp, err := cli.Do(req)
	if err != nil {
		log.Warn("passive snmp post failed", "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(resp.Body)
		log.Warn("passive snmp post rejected", "code", resp.StatusCode, "body", string(rb))
		return
	}
	log.Info("passive snmp discovery uploaded", "device_count", len(devices))
}

func fetchPassiveSNMPSettings(ctx context.Context, log *slog.Logger, cli *http.Client, cfg *Config) (passiveSNMPSettings, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		cfg.BaseURL+"/api/v1/collectors/me/passive-snmp-settings", nil)
	if err != nil {
		return passiveSNMPSettings{}, false
	}
	req.Header.Set("Authorization", "Bearer "+cfg.JWT)
	resp, err := cli.Do(req)
	if err != nil {
		log.Warn("passive snmp settings fetch failed", "err", err)
		return passiveSNMPSettings{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return passiveSNMPSettings{}, false
	}
	var s passiveSNMPSettings
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return passiveSNMPSettings{}, false
	}
	return s, true
}
