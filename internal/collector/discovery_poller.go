package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/NCLGISA/ScanRay-Sonar/internal/collector/discovery"
)

type collectorJobWire struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Description string          `json:"description,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

type siteCredWire struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Secret string `json:"secret"`
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
	jobs, ok := fetchCollectorJobs(ctx, log, cli, cfg)
	if !ok {
		return
	}
	creds := fetchSiteCredentials(ctx, log, cli, cfg)
	for _, j := range jobs {
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
		ports := []int{443, 22, 80, 161, 23}
		devices := []map[string]any{}
		for _, cidr := range payload.Subnets {
			hosts, err := discovery.IPv4Hosts(cidr, 512)
			if err != nil {
				log.Warn("discovery cidr parse failed", "cidr", cidr, "err", err)
				continue
			}
			for _, h := range hosts {
				pctx, cancel := context.WithTimeout(ctx, dto+500*time.Millisecond)
				reachable, rtt := discovery.ProbeReachability(pctx, h, ports, dto)
				cancel()
				if !reachable {
					continue
				}
				dev := buildDevice(ctx, log, h, rtt, creds, dto)
				devices = append(devices, dev)
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

func buildDevice(ctx context.Context, log *slog.Logger, ip string, rtt float64, creds []siteCredWire, perStep time.Duration) map[string]any {
	protocols := []string{"tcp"}
	meta := map[string]any{
		"rttMs":       rtt,
		"portsProbed": []int{443, 22, 80, 161, 23},
	}
	dev := map[string]any{
		"ip":         ip,
		"identified": false,
		"protocols":  protocols,
		"metadata":   meta,
	}
	// Try every SSH cred until one identifies a vendor.
	for _, c := range creds {
		select {
		case <-ctx.Done():
			return dev
		default:
		}
		switch strings.ToLower(c.Kind) {
		case "ssh":
			user, pass, key := parseCredSecret(c.Secret)
			out, err := discovery.SSHRun(ctx, ip, discovery.SSHCred{Username: user, Password: pass, PrivateKeyPEM: key}, "show version", perStep)
			if err != nil {
				continue
			}
			if profile, ok := discovery.IdentifyVendor(out); ok {
				meta["identifiedBy"] = "ssh"
				meta["identifyOutputBytes"] = len(out)
				dev["identified"] = true
				dev["vendor"] = profile.Name
				protocols = appendUniq(protocols, "ssh")
				dev["protocols"] = protocols
				return dev
			}
		case "telnet":
			user, pass, _ := parseCredSecret(c.Secret)
			out, err := discovery.TelnetRun(ctx, ip, discovery.TelnetCred{Username: user, Password: pass}, "show version", perStep)
			if err != nil {
				continue
			}
			if profile, ok := discovery.IdentifyVendor(out); ok {
				meta["identifiedBy"] = "telnet"
				dev["identified"] = true
				dev["vendor"] = profile.Name
				protocols = appendUniq(protocols, "telnet")
				dev["protocols"] = protocols
				return dev
			}
		case "vmware":
			// govmomi not yet wired in. The vmware.go module returns ErrVMwareNotImplemented;
			// we log once per device so an operator knows credentials are present but unused.
			log.Debug("vmware credential present but govmomi not yet wired", "ip", ip)
		}
	}
	return dev
}

// parseCredSecret accepts either a plain `password` or a JSON blob
// `{"username":"…","password":"…","key":"-----BEGIN…-----"}`.
func parseCredSecret(s string) (user, pass, key string) {
	t := strings.TrimSpace(s)
	if strings.HasPrefix(t, "{") {
		var m map[string]string
		if json.Unmarshal([]byte(t), &m) == nil {
			return m["username"], m["password"], m["key"]
		}
	}
	return "", t, ""
}

func appendUniq(in []string, v string) []string {
	for _, x := range in {
		if x == v {
			return in
		}
	}
	return append(in, v)
}

func fetchCollectorJobs(ctx context.Context, log *slog.Logger, cli *http.Client, cfg *Config) ([]collectorJobWire, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BaseURL+"/api/v1/collectors/me/jobs", nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("Authorization", "Bearer "+cfg.JWT)
	resp, err := cli.Do(req)
	if err != nil {
		log.Warn("discovery jobs fetch failed", "err", err)
		return nil, false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Warn("discovery jobs bad status", "code", resp.StatusCode, "body", string(body))
		return nil, false
	}
	var wrap struct {
		Jobs []collectorJobWire `json:"jobs"`
	}
	if json.Unmarshal(body, &wrap) != nil {
		return nil, false
	}
	return wrap.Jobs, true
}

func fetchSiteCredentials(ctx context.Context, log *slog.Logger, cli *http.Client, cfg *Config) []siteCredWire {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BaseURL+"/api/v1/collectors/me/site-credentials", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+cfg.JWT)
	resp, err := cli.Do(req)
	if err != nil {
		log.Warn("site-credentials fetch failed", "err", err)
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Warn("site-credentials bad status", "code", resp.StatusCode, "body", string(body))
		return nil
	}
	var wrap struct {
		Credentials []siteCredWire `json:"credentials"`
	}
	if json.Unmarshal(body, &wrap) != nil {
		return nil
	}
	return wrap.Credentials
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
