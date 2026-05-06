//go:build windows

package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/NCLGISA/ScanRay-Sonar/internal/winagent"
)

// sonar-collector-winagent runs on a Windows endpoint and serves
// https://0.0.0.0:8443 with a single endpoint, /v1/inventory, that returns
// WMI-derived inventory. The bearer token is the shared secret the
// collector handed us at enrollment; CertFile/KeyFile are the mTLS chain
// pinned by the collector.
func main() {
	addr := flag.String("addr", "0.0.0.0:8443", "listen address")
	cert := flag.String("cert", os.Getenv("WINAGENT_CERT"), "TLS cert PEM")
	key := flag.String("key", os.Getenv("WINAGENT_KEY"), "TLS key PEM")
	tok := flag.String("token", os.Getenv("WINAGENT_TOKEN"), "shared bearer token")
	flag.Parse()

	if *tok == "" {
		log.Fatalf("WINAGENT_TOKEN (or -token) required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	cfg := winagent.Config{
		SharedToken: *tok,
		CertFile:    *cert,
		KeyFile:     *key,
		ListenAddr:  *addr,
	}
	log.Printf("sonar-collector-winagent listening on %s", *addr)
	if err := winagent.ServeHTTPS(ctx, cfg); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
