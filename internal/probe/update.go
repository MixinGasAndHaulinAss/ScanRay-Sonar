package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/NCLGISA/ScanRay-Sonar/internal/probesign"
	"github.com/NCLGISA/ScanRay-Sonar/internal/version"
)

const updateCheckInterval = 6 * time.Hour

// runUpdateLoop periodically checks the API for a newer signed probe
// binary and applies it in-place (then exits so the supervisor restarts).
func runUpdateLoop(ctx context.Context, log *slog.Logger, cfg *Config) {
	// Stagger first check so a mass restart does not stampede the API.
	select {
	case <-ctx.Done():
		return
	case <-time.After(2 * time.Minute):
	}
	if err := checkAndMaybeUpdate(ctx, log, cfg); err != nil {
		log.Debug("probe update check", "err", err)
	}
	t := time.NewTicker(updateCheckInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := checkAndMaybeUpdate(ctx, log, cfg); err != nil {
				log.Debug("probe update check", "err", err)
			}
		}
	}
}

func checkAndMaybeUpdate(ctx context.Context, log *slog.Logger, cfg *Config) error {
	if cfg.BaseURL == "" {
		return fmt.Errorf("missing baseUrl")
	}
	osName, arch := runtime.GOOS, runtime.GOARCH
	url := fmt.Sprintf("%s/api/v1/probe/latest?os=%s&arch=%s",
		trimSlash(cfg.BaseURL), osName, arch)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	cli := &http.Client{Timeout: 30 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("latest: %s %s", resp.Status, string(body))
	}
	var man probesign.Manifest
	if err := json.NewDecoder(resp.Body).Decode(&man); err != nil {
		return err
	}
	local := version.Get().Version
	if cfg.AgentVersion != "" {
		local = cfg.AgentVersion
	}
	if !probesign.CompareCalVer(local, man.Version) {
		log.Debug("probe up to date", "local", local, "remote", man.Version)
		return nil
	}
	if err := probesign.Verify(man.SHA256, man.Sig); err != nil {
		return fmt.Errorf("verify manifest: %w", err)
	}

	dl := man.URL
	if dl == "" {
		dl = fmt.Sprintf("%s/api/v1/probe/download/%s/%s", trimSlash(cfg.BaseURL), osName, arch)
	} else if dl[0] == '/' {
		dl = trimSlash(cfg.BaseURL) + dl
	}

	log.Info("downloading probe update", "version", man.Version, "url", dl)
	bin, err := downloadBytes(ctx, cli, dl)
	if err != nil {
		return err
	}
	got := probesign.HashSHA256Hex(bin)
	if got != man.SHA256 {
		return fmt.Errorf("sha256 mismatch: got %s want %s", got, man.SHA256)
	}
	if err := probesign.Verify(got, man.Sig); err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	staging := exe + ".new"
	if err := os.WriteFile(staging, bin, 0o755); err != nil {
		return fmt.Errorf("write staging: %w", err)
	}
	log.Info("applying probe update — process will exit for supervisor restart",
		"version", man.Version, "path", exe)
	if err := applyUpdate(exe, staging); err != nil {
		_ = os.Remove(staging)
		return err
	}
	// Exit so SCM/systemd restarts us on the new binary.
	os.Exit(0)
	return nil
}

func downloadBytes(ctx context.Context, cli *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download: %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 128<<20))
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
