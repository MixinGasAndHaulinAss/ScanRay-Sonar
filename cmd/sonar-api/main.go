// Command sonar-api is the HTTP/WebSocket front-end of ScanRay Sonar.
// A single binary serves the OpenAPI surface, the embedded React UI,
// and both websocket hubs (agent ingest + UI live updates).
package main

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/NCLGISA/ScanRay-Sonar/internal/api"
	"github.com/NCLGISA/ScanRay-Sonar/internal/auth"
	"github.com/NCLGISA/ScanRay-Sonar/internal/config"
	scrypto "github.com/NCLGISA/ScanRay-Sonar/internal/crypto"
	"github.com/NCLGISA/ScanRay-Sonar/internal/db"
	"github.com/NCLGISA/ScanRay-Sonar/internal/logging"
	"github.com/NCLGISA/ScanRay-Sonar/internal/probebins"
	"github.com/NCLGISA/ScanRay-Sonar/internal/version"
	"github.com/NCLGISA/ScanRay-Sonar/web"
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
	log := logging.Setup(cfg.LogLevel, "sonar-api", version.Get().Version)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := db.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := db.Migrate(cfg); err != nil {
		return err
	}

	if created, err := db.BootstrapAdmin(ctx, pool, cfg.BootstrapAdminEmail, cfg.BootstrapAdminPassword); err != nil {
		log.Warn("bootstrap admin failed", "err", err)
	} else if created {
		log.Info("bootstrap admin created", "email", cfg.BootstrapAdminEmail)
	}

	store := db.NewStore(pool)

	iss, err := auth.NewIssuer(cfg.JWTSecretB64, cfg.JWTAccessTTL, cfg.JWTRefreshTTL)
	if err != nil {
		return err
	}

	sealer, err := scrypto.NewSealer(cfg.MasterKeyB64)
	if err != nil {
		return err
	}

	// NATS is non-fatal at startup — many compose stacks may bring it up
	// after the API. The api.Server checks IsConnected() before publishing.
	nc, err := nats.Connect(cfg.NATSURL,
		nats.Name("sonar-api"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		log.Warn("NATS unavailable; continuing without it", "err", err, "url", cfg.NATSURL)
	}

	webRoot, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		webRoot = nil
	}

	probeRoot, err := fs.Sub(probebins.FS, "bin")
	if err != nil {
		probeRoot = nil
	}

	srv := api.New(api.Deps{
		Config:      cfg,
		Logger:      log,
		Pool:        pool,
		Store:       store,
		Issuer:      iss,
		Sealer:      sealer,
		NATS:        nc,
		OpenAPISpec: api.Spec(),
		WebFS:       webRoot,
		ProbeFS:     probeRoot,
	})

	log.Info("ScanRay Sonar API starting",
		"version", version.Get().Version,
		"bind", cfg.BindAddr,
		"public_url", cfg.PublicURL,
	)
	return srv.ListenAndServe(ctx)
}
