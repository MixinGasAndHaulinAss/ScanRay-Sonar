//go:build windows

package main

import (
	"context"
	"fmt"
	"log/slog"

	"golang.org/x/sys/windows/svc"

	"github.com/NCLGISA/ScanRay-Sonar/internal/probe"
)

// ServiceName is the Windows SCM identifier the install.ps1 script
// uses with `sc.exe create`. Keep in sync with handlers_probe.go.
const ServiceName = "SonarProbe"

// runProbe wraps probe.Run for Windows. When invoked under the
// Service Control Manager it negotiates start/stop/shutdown signals
// over the SCM channel; when invoked from an interactive shell (e.g.
// a sysadmin testing the binary) it runs in the foreground so logs
// stream to the console.
func runProbe(ctx context.Context, logger *slog.Logger, cfg *probe.Config) error {
	inService, err := svc.IsWindowsService()
	if err != nil {
		logger.Warn("svc.IsWindowsService failed; assuming foreground", "err", err)
		inService = false
	}
	if !inService {
		return probe.Run(ctx, logger, cfg)
	}

	h := &svcHandler{cfg: cfg, logger: logger}
	if err := svc.Run(ServiceName, h); err != nil {
		return fmt.Errorf("svc.Run: %w", err)
	}
	return h.runErr
}

// svcHandler bridges the SCM control channel to a context-cancelled
// probe.Run goroutine. svc.Run blocks until Execute returns.
type svcHandler struct {
	cfg    *probe.Config
	logger *slog.Logger
	runErr error
}

func (h *svcHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const accepts = svc.AcceptStop | svc.AcceptShutdown
	status <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- probe.Run(ctx, h.logger, h.cfg) }()

	status <- svc.Status{State: svc.Running, Accepts: accepts}

	for {
		select {
		case c := <-requests:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				h.logger.Info("service stop requested", "cmd", c.Cmd)
				cancel()
				status <- svc.Status{State: svc.StopPending}
				h.runErr = <-done
				return false, 0
			default:
				h.logger.Warn("unexpected service control", "cmd", c.Cmd)
			}
		case err := <-done:
			h.runErr = err
			if err != nil {
				h.logger.Error("probe.Run exited unexpectedly", "err", err)
				status <- svc.Status{State: svc.StopPending}
				return true, 1
			}
			status <- svc.Status{State: svc.StopPending}
			return false, 0
		}
	}
}
