// Command sonar-probe is the cross-platform Sonar endpoint agent. It
// runs as a service on Linux (Windows/macOS support follow), collects
// host telemetry, and pushes it to sonar-api over a persistent
// websocket.
//
// Subcommands:
//
//	sonar-probe enroll --token=... --base=... [--config=...]
//	sonar-probe run    [--config=...]
//	sonar-probe version
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/NCLGISA/ScanRay-Sonar/internal/probe"
	"github.com/NCLGISA/ScanRay-Sonar/internal/version"
)

const defaultConfigPath = "/etc/sonar-probe/agent.json"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "enroll":
		mustNoErr(cmdEnroll(os.Args[2:]))
	case "run":
		mustNoErr(cmdRun(os.Args[2:]))
	case "version", "--version", "-v":
		v := version.Get()
		fmt.Printf("sonar-probe %s (%s, built %s, %s/%s)\n",
			v.Version, v.Commit, v.BuildTime, runtime.GOOS, runtime.GOARCH)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `sonar-probe — ScanRay Sonar endpoint agent

Usage:
  sonar-probe enroll --token=<TOKEN> --base=<URL> [--config=<PATH>] [--hostname=<NAME>]
  sonar-probe run    [--config=<PATH>]
  sonar-probe version`)
}

func mustNoErr(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

func cmdEnroll(args []string) error {
	fs := flag.NewFlagSet("enroll", flag.ExitOnError)
	token := fs.String("token", "", "single-use enrollment token from the Sonar UI")
	base := fs.String("base", "", "sonar-api base URL (e.g. https://sonar.example.com)")
	cfgPath := fs.String("config", defaultConfigPath, "path to write the agent config")
	hostnameOverride := fs.String("hostname", "", "override hostname (defaults to os.Hostname())")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *token == "" || *base == "" {
		return errors.New("--token and --base are required")
	}
	*base = strings.TrimRight(*base, "/")

	hostname := *hostnameOverride
	if hostname == "" {
		h, err := os.Hostname()
		if err != nil || h == "" {
			return fmt.Errorf("unable to determine hostname: %w", err)
		}
		hostname = h
	}

	fp := os.Getenv("SONAR_FINGERPRINT")
	if fp == "" {
		var err error
		fp, err = probe.Fingerprint(hostname)
		if err != nil {
			return err
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg, err := probe.Enroll(ctx, *base, *token, hostname, fp, version.Get().Version)
	if err != nil {
		return err
	}
	if err := probe.SaveConfig(*cfgPath, cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Printf("enrolled as agent %s (site %s) — config at %s\n",
		cfg.AgentID, cfg.SiteID, *cfgPath)
	return nil
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	cfgPath := fs.String("config", defaultConfigPath, "path to the agent config")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := probe.LoadConfig(*cfgPath)
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Info("sonar-probe starting",
		"version", version.Get().Version,
		"agent_id", cfg.AgentID,
		"site_id", cfg.SiteID,
		"ingest", cfg.IngestWS,
	)
	return probe.Run(ctx, logger, cfg)
}
