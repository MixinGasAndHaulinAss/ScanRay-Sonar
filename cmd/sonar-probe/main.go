// Command sonar-probe is the cross-platform Sonar endpoint agent. It
// runs as a service on Windows/Linux/macOS, collects host telemetry,
// and pushes it to sonar-api over a persistent websocket.
//
// Phase 1: skeleton — emits its version, parses environment, and
// idles. Enrollment + telemetry land in Phase 2.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/NCLGISA/ScanRay-Sonar/internal/version"
)

func main() {
	var (
		showVersion = flag.Bool("version", false, "print version and exit")
		ingestURL   = flag.String("ingest", "", "wss:// ingest URL (overrides SONAR_INGEST_URL)")
	)
	flag.Parse()

	if *showVersion {
		v := version.Get()
		fmt.Printf("sonar-probe %s (%s, built %s, %s/%s)\n",
			v.Version, v.Commit, v.BuildTime, runtime.GOOS, runtime.GOARCH)
		return
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if *ingestURL == "" {
		*ingestURL = os.Getenv("SONAR_INGEST_URL")
	}
	if *ingestURL == "" {
		logger.Error("no ingest URL configured (set --ingest or SONAR_INGEST_URL)")
		os.Exit(2)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Info("sonar-probe starting (Phase 1 skeleton)",
		"version", version.Get().Version,
		"ingest", *ingestURL,
		"os", runtime.GOOS,
		"arch", runtime.GOARCH,
	)

	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("shutdown")
			return
		case <-t.C:
			logger.Debug("probe heartbeat (no collectors registered yet)")
		}
	}
}
