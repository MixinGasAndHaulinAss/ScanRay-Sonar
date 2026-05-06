package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Config is persisted after enrollment (JSON file inside the container).
type Config struct {
	BaseURL          string `json:"baseUrl"`
	IngestWS         string `json:"ingestWs"`
	JWT              string `json:"jwt"`
	CollectorID      string `json:"collectorId"`
	SiteID           string `json:"siteId"`
	Name             string `json:"name"`
	Hostname         string `json:"hostname"`
	Fingerprint      string `json:"fingerprint"`
	CollectorVersion string `json:"collectorVersion"`
	EnrolledAt       string `json:"enrolledAt"`
}

type enrollReqBody struct {
	Token             string `json:"token"`
	Name              string `json:"name"`
	Hostname          string `json:"hostname"`
	Fingerprint       string `json:"fingerprint"`
	CollectorVersion  string `json:"collectorVersion"`
}

type enrollRespBody struct {
	CollectorID string `json:"collectorId"`
	SiteID      string `json:"siteId"`
	JWT         string `json:"jwt"`
	IngestWS    string `json:"ingestWs"`
}

// Enroll exchanges a single-use token for a collector JWT.
func Enroll(ctx context.Context, base, token, name, hostname, fingerprint, ver string) (*Config, error) {
	body, _ := json.Marshal(enrollReqBody{
		Token:            token,
		Name:             name,
		Hostname:         hostname,
		Fingerprint:      fingerprint,
		CollectorVersion: ver,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		base+"/api/v1/collectors/enroll", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("collector: build enroll request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	cli := &http.Client{Timeout: 30 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("collector: enroll POST: %w", err)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("collector: enroll rejected (%d): %s", resp.StatusCode, string(rb))
	}
	var er enrollRespBody
	if err := json.Unmarshal(rb, &er); err != nil {
		return nil, fmt.Errorf("collector: decode enroll resp: %w", err)
	}
	return &Config{
		BaseURL:          base,
		IngestWS:         er.IngestWS,
		JWT:              er.JWT,
		CollectorID:      er.CollectorID,
		SiteID:           er.SiteID,
		Name:             name,
		Hostname:         hostname,
		Fingerprint:      fingerprint,
		CollectorVersion: ver,
		EnrolledAt:       time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// SaveConfig writes cfg as JSON.
func SaveConfig(path string, cfg *Config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadConfig reads JSON config from disk.
func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}
