package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/NCLGISA/ScanRay-Sonar/internal/collector"
	"github.com/NCLGISA/ScanRay-Sonar/internal/logging"
	"github.com/NCLGISA/ScanRay-Sonar/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "enroll":
		must(cmdEnroll(os.Args[2:]))
	case "run":
		must(cmdRun(os.Args[2:]))
	case "version", "--version", "-v":
		v := version.Get()
		fmt.Printf("sonar-collector %s (%s, built %s)\n", v.Version, v.Commit, v.BuildTime)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `sonar-collector — ScanRay Sonar remote site collector

Usage:
  sonar-collector enroll --token=<TOKEN> --base=<URL> --name=<NAME> [--config=PATH]
  sonar-collector run [--config=PATH] [--log-level=info]
  sonar-collector version`)
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func cmdEnroll(args []string) error {
	fs := flag.NewFlagSet("enroll", flag.ExitOnError)
	token := fs.String("token", "", "single-use enrollment token")
	base := fs.String("base", "", "Sonar API base URL")
	name := fs.String("name", "", "collector display name (unique per site)")
	cfgPath := fs.String("config", envDefault("SONAR_COLLECTOR_CONFIG", "/etc/sonar/collector.json"), "config file path")
	hostOverride := fs.String("hostname", "", "override hostname")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *token == "" || *base == "" || *name == "" {
		return errors.New("--token, --base, and --name are required")
	}
	*base = strings.TrimRight(*base, "/")

	host := *hostOverride
	if host == "" {
		h, err := os.Hostname()
		if err != nil {
			return err
		}
		host = h
	}
	fp := envDefault("SONAR_COLLECTOR_FINGERPRINT", host+"-"+*name)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg, err := collector.Enroll(ctx, *base, *token, *name, host, fp, version.Get().Version)
	if err != nil {
		return err
	}
	if err := collector.SaveConfig(*cfgPath, cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Printf("enrolled collector %s (site %s)\n", cfg.CollectorID, cfg.SiteID)
	return nil
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	cfgPath := fs.String("config", envDefault("SONAR_COLLECTOR_CONFIG", "/etc/sonar/collector.json"), "config file path")
	logLevel := fs.String("log-level", envDefault("SONAR_COLLECTOR_LOG_LEVEL", "info"), "log level")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := collector.LoadConfig(*cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	log := logging.Setup(*logLevel, "sonar-collector", version.Get().Version)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		collector.RunDiscoveryPoller(ctx, log, cfg)
	}()

	go func() {
		collectCtx := ctx // shared cancellation with Run below for simplicity
		if err := collector.RunSNMPPoller(collectCtx, log, cfg); err != nil && ctx.Err() == nil {
			log.Warn("snmp poller exited", "err", err)
		}
	}()

	go func() {
		collector.RunPassiveSNMPDiscovery(ctx, log, cfg)
	}()

	return collector.Run(ctx, log, cfg)
}

func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
