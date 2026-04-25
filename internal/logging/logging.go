// Package logging configures the process-wide structured logger (slog).
// All services in this monorepo (api, poller, probe) use this so logs share
// a single shape and can be parsed by any downstream pipeline.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// Setup installs a JSON slog handler at the given level as the default
// logger and returns it. Levels: debug, info, warn, error.
func Setup(level, service, version string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: lvl,
	})
	logger := slog.New(h).With(
		slog.String("service", service),
		slog.String("version", version),
	)
	slog.SetDefault(logger)
	return logger
}
