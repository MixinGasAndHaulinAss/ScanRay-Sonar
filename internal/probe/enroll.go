package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Config is the on-disk per-agent state written by Enroll and read by
// Run. JSON-serialised to /etc/sonar-probe/agent.json by default.
type Config struct {
	BaseURL      string `json:"baseUrl"`
	IngestWS     string `json:"ingestWs"`
	JWT          string `json:"jwt"`
	AgentID      string `json:"agentId"`
	SiteID       string `json:"siteId"`
	Hostname     string `json:"hostname"`
	Fingerprint  string `json:"fingerprint"`
	AgentVersion string `json:"agentVersion"`
	EnrolledAt   string `json:"enrolledAt"`
	// LatencyTarget overrides the default ICMP probe target
	// ("8.8.8.8"). Honored by extras.LatencyTargets; an empty
	// string falls back to the default. Useful for environments
	// where outbound ICMP to public DNS is firewalled (e.g.
	// air-gapped sites with an internal anycast resolver).
	LatencyTarget string `json:"latencyTarget,omitempty"`
}

type enrollReq struct {
	Token        string `json:"token"`
	Hostname     string `json:"hostname"`
	Fingerprint  string `json:"fingerprint"`
	OS           string `json:"os"`
	OSVersion    string `json:"osVersion"`
	AgentVersion string `json:"agentVersion"`
}

type enrollResp struct {
	AgentID  string `json:"agentId"`
	SiteID   string `json:"siteId"`
	JWT      string `json:"jwt"`
	IngestWS string `json:"ingestWs"`
}

// Enroll exchanges a single-use enrollment token for a long-lived agent
// JWT. The result is returned and is suitable for persisting to disk.
func Enroll(ctx context.Context, base, token, hostname, fingerprint, agentVersion string) (*Config, error) {
	body, _ := json.Marshal(enrollReq{
		Token:        token,
		Hostname:     hostname,
		Fingerprint:  fingerprint,
		OS:           runtime.GOOS,
		OSVersion:    osVersion(),
		AgentVersion: agentVersion,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		base+"/api/v1/agents/enroll", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("probe: build enroll request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	cli := &http.Client{Timeout: 30 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("probe: enroll POST: %w", err)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("probe: enroll rejected (%d): %s", resp.StatusCode, string(rb))
	}
	var er enrollResp
	if err := json.Unmarshal(rb, &er); err != nil {
		return nil, fmt.Errorf("probe: decode enroll resp: %w", err)
	}

	return &Config{
		BaseURL:      base,
		IngestWS:     er.IngestWS,
		JWT:          er.JWT,
		AgentID:      er.AgentID,
		SiteID:       er.SiteID,
		Hostname:     hostname,
		Fingerprint:  fingerprint,
		AgentVersion: agentVersion,
		EnrolledAt:   time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// osVersion returns a best-effort OS version string. Linux reads
// PRETTY_NAME from /etc/os-release; Windows shells out to `cmd /c ver`
// (which prints e.g. "Microsoft Windows [Version 10.0.19045.4291]");
// other platforms just return runtime.GOOS so we don't pretend.
func osVersion() string {
	switch runtime.GOOS {
	case "linux":
		b, err := readSmall("/etc/os-release")
		if err != nil {
			return "linux"
		}
		for _, line := range splitLines(string(b)) {
			if v, ok := stripPrefix(line, "PRETTY_NAME="); ok {
				return unquote(v)
			}
		}
		return "linux"
	case "windows":
		out, err := exec.Command("cmd", "/c", "ver").Output()
		if err != nil {
			return "windows"
		}
		return strings.TrimSpace(string(out))
	default:
		return runtime.GOOS
	}
}
