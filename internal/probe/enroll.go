package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
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

// osVersion returns a best-effort OS version string. Linux: contents
// of /etc/os-release PRETTY_NAME, falling back to runtime.GOOS.
func osVersion() string {
	b, err := readSmall("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	for _, line := range splitLines(string(b)) {
		if v, ok := stripPrefix(line, "PRETTY_NAME="); ok {
			return unquote(v)
		}
	}
	return runtime.GOOS
}
