// Command sonar-poller polls network appliances over SNMP v1/v2c/v3
// (LLDP topology and vendor APIs land in later phases) and persists
// snapshots + time-series samples directly to Postgres/TimescaleDB,
// where the API surfaces them to the UI.
//
// Phase 3a: SNMP polling is live. NATS is still wired in for
// future fan-out of poll events but the Phase 3a write path is
// purely DB-backed.
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
	scrypto "github.com/NCLGISA/ScanRay-Sonar/internal/crypto"
	"github.com/NCLGISA/ScanRay-Sonar/internal/db"
	"github.com/NCLGISA/ScanRay-Sonar/internal/logging"
	"github.com/NCLGISA/ScanRay-Sonar/internal/poller"
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

	sealer, err := scrypto.NewSealer(cfg.MasterKeyB64)
	if err != nil {
		return err
	}

	// NATS is optional in Phase 3a — the poller writes directly to
	// Postgres. Connection failure is logged and we proceed; later
	// phases that publish events will need to handle a nil conn.
	nc, err := nats.Connect(cfg.NATSURL,
		nats.Name("sonar-poller"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		log.Warn("NATS unavailable; continuing without event publishing", "err", err)
	}
	defer func() {
		if nc != nil {
			nc.Drain()
		}
	}()

	log.Info("ScanRay Sonar Poller starting (Phase 3a: SNMP polling)",
		"version", version.Get().Version,
		"nats", cfg.NATSURL,
	)

	sched := poller.New(pool, sealer, log)
	sched.Run(ctx)

	log.Info("shutdown")
	return nil
}
