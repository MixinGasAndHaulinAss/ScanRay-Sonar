package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/NCLGISA/ScanRay-Sonar/internal/checks"
)

// runChecksLoop pulls due synthetic checks from the API and posts results.
// Additive to DEX/metrics — does not replace host telemetry.
func runChecksLoop(ctx context.Context, log *slog.Logger, cfg *Config) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := pullAndRunChecks(ctx, cfg); err != nil {
				log.Debug("checks pull failed", "err", err)
			}
		}
	}
}

func pullAndRunChecks(ctx context.Context, cfg *Config) error {
	if cfg.BaseURL == "" || cfg.JWT == "" {
		return fmt.Errorf("missing baseUrl/jwt")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BaseURL+"/api/v1/agent/checks", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.JWT)
	cli := &http.Client{Timeout: 20 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("checks list %d: %s", resp.StatusCode, string(body))
	}
	var jobs []struct {
		ID     string         `json:"id"`
		TypeID string         `json:"typeId"`
		Name   string         `json:"name"`
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal(body, &jobs); err != nil {
		return err
	}
	if len(jobs) == 0 {
		return nil
	}
	type sampleOut struct {
		Key   string  `json:"key"`
		Value float64 `json:"value,omitempty"`
		Text  string  `json:"text,omitempty"`
	}
	type resultOut struct {
		CheckID string      `json:"checkId"`
		OK      bool        `json:"ok"`
		Error   string      `json:"error,omitempty"`
		Samples []sampleOut `json:"samples"`
	}
	var results []resultOut
	for _, j := range jobs {
		rctx, cancel := context.WithTimeout(ctx, 25*time.Second)
		res := checks.Run(rctx, j.TypeID, j.Params)
		cancel()
		out := resultOut{CheckID: j.ID, OK: res.OK, Error: res.Error}
		for _, sm := range res.Samples {
			so := sampleOut{Key: sm.Key, Text: sm.Text}
			if sm.HasNum {
				so.Value = sm.Value
			}
			out.Samples = append(out.Samples, so)
		}
		results = append(results, out)
	}
	payload, _ := json.Marshal(map[string]any{"results": results})
	preq, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/api/v1/agent/checks/results", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	preq.Header.Set("Authorization", "Bearer "+cfg.JWT)
	preq.Header.Set("Content-Type", "application/json")
	presp, err := cli.Do(preq)
	if err != nil {
		return err
	}
	defer presp.Body.Close()
	if presp.StatusCode >= 300 {
		b, _ := io.ReadAll(presp.Body)
		return fmt.Errorf("checks results %d: %s", presp.StatusCode, string(b))
	}
	return nil
}
