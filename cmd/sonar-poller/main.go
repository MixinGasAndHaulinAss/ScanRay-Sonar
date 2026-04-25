// Command sonar-poller polls network appliances over SNMP v1/v2c/v3,
// LLDP, and vendor APIs (Meraki, etc.), publishing observations to NATS
// for the API to fan out to the UI and persist into TimescaleDB.
//
// Phase 1: skeleton only — verifies environment + connectivity, then
// idles. Real collection lands in Phase 3.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/NCLGISA/ScanRay-Sonar/internal/config"
	"github.com/NCLGISA/ScanRay-Sonar/internal/db"
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
	log := logging.Setup(cfg.LogLevel, "sonar-poller", version.Get().Version)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := db.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	nc, err := nats.Connect(cfg.NATSURL,
		nats.Name("sonar-poller"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		log.Warn("NATS unavailable; will retry on next cycle", "err", err)
	}
	defer func() {
		if nc != nil {
			nc.Drain()
		}
	}()

	log.Info("ScanRay Sonar Poller starting (Phase 1 skeleton)",
		"version", version.Get().Version,
		"nats", cfg.NATSURL,
	)

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("shutdown")
			return nil
		case <-ticker.C:
			log.Debug("poll tick (no work registered yet)")
		}
	}
}
