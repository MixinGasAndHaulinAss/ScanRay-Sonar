package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/NCLGISA/ScanRay-Sonar/internal/collector/discovery"
)

type collectorJobWire struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Description string          `json:"description,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

// RunDiscoveryPoller periodically executes discovery_scan jobs delegated by central Sonar.
func RunDiscoveryPoller(ctx context.Context, log *slog.Logger, cfg *Config) {
	cli := &http.Client{Timeout: 120 * time.Second}
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runDiscoveryOnce(ctx, log, cli, cfg)
		}
	}
}

func runDiscoveryOnce(ctx context.Context, log *slog.Logger, cli *http.Client, cfg *Config) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		cfg.BaseURL+"/api/v1/collectors/me/jobs", nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+cfg.JWT)
	resp, err := cli.Do(req)
	if err != nil {
		log.Warn("discovery jobs fetch failed", "err", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Warn("discovery jobs bad status", "code", resp.StatusCode, "body", string(body))
		return
	}
	var wrap struct {
		Jobs []collectorJobWire `json:"jobs"`
	}
	if json.Unmarshal(body, &wrap) != nil {
		return
	}
	for _, j := range wrap.Jobs {
		if j.Kind != "discovery_scan" {
			continue
		}
		var payload struct {
			Subnets       []string `json:"subnets"`
			IcmpTimeoutMs int      `json:"icmpTimeoutMs"`
		}
		if json.Unmarshal(j.Payload, &payload) != nil || len(payload.Subnets) == 0 {
			continue
		}
		if payload.IcmpTimeoutMs <= 0 {
			payload.IcmpTimeoutMs = 2000
		}
		dto := time.Duration(payload.IcmpTimeoutMs) * time.Millisecond
		ports := []int{443, 22, 80}
		devices := []map[string]any{}
		for _, cidr := range payload.Subnets {
			hosts, err := discovery.IPv4Hosts(cidr, 512)
			if err != nil {
				log.Warn("discovery cidr parse failed", "cidr", cidr, "err", err)
				continue
			}
			for _, h := range hosts {
				pctx, cancel := context.WithTimeout(ctx, dto+500*time.Millisecond)
				ok := discovery.ProbeTCP(pctx, h, ports, dto)
				cancel()
				if !ok {
					continue
				}
				devices = append(devices, map[string]any{
					"ip": h, "identified": false, "protocols": []string{"tcp"},
					"metadata": map[string]any{"portsProbed": ports},
				})
				if len(devices) >= 200 {
					break
				}
			}
			if len(devices) >= 200 {
				break
			}
		}
		if len(devices) == 0 {
			continue
		}
		postDiscoveryResults(ctx, log, cli, cfg, devices)
		return
	}
}

func postDiscoveryResults(ctx context.Context, log *slog.Logger, cli *http.Client, cfg *Config, devices []map[string]any) {
	raw, _ := json.Marshal(map[string]any{"devices": devices})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		cfg.BaseURL+"/api/v1/collectors/me/discovery-results", bytes.NewReader(raw))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+cfg.JWT)
	req.Header.Set("Content-Type", "application/json")
	resp, err := cli.Do(req)
	if err != nil {
		log.Warn("discovery post failed", "err", err)
		return
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		log.Warn("discovery post rejected", "code", resp.StatusCode, "body", string(rb))
		return
	}
	log.Info("discovery batch uploaded", "count", len(devices))
}
