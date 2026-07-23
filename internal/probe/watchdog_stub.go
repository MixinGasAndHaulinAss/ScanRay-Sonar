//go:build !windows

package probe

import "log/slog"

// EnsureServiceWatchdog is a no-op off Windows; systemd Restart= handles
// probe recovery on Linux.
func EnsureServiceWatchdog(log *slog.Logger) {}
