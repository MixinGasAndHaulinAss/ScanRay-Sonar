//go:build !windows

package main

import (
	"context"
	"log/slog"

	"github.com/NCLGISA/ScanRay-Sonar/internal/probe"
)

// runProbe is the cross-platform run-loop dispatch. On Unix-y systems
// systemd (or a comparable supervisor) keeps the process alive, so we
// just call probe.Run directly and let it block on ctx.
func runProbe(ctx context.Context, logger *slog.Logger, cfg *probe.Config) error {
	return probe.Run(ctx, logger, cfg)
}
