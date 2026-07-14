// Command sonar-flowd listens for NetFlow/IPFIX datagrams and persists
// flow summaries to TimescaleDB. Set SONAR_FLOW_LISTEN=:2055.
//
// The same listener also starts from sonar-poller when the env var is set;
// this binary exists for deployments that want flow ingest isolated.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/NCLGISA/ScanRay-Sonar/internal/config"
	"github.com/NCLGISA/ScanRay-Sonar/internal/db"
	"github.com/NCLGISA/ScanRay-Sonar/internal/flows"
	"github.com/NCLGISA/ScanRay-Sonar/internal/logging"
	"github.com/NCLGISA/ScanRay-Sonar/internal/version"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logging.Setup(cfg.LogLevel, "sonar-flowd", version.Get().Version)
	addr := strings.TrimSpace(os.Getenv("SONAR_FLOW_LISTEN"))
	if addr == "" {
		addr = ":2055"
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := db.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	log.Info("sonar-flowd starting", "version", version.Get().Version, "listen", addr)
	return flows.NewListener(addr, pool, log).Run(ctx)
}
