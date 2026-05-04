//go:build !windows

package probe

import "context"

// collectSessionsOS returns ok=false on platforms where gopsutil's
// host.Users() works fine; the cross-platform fallback in
// snapshot.go!collectUsers takes over.
func collectSessionsOS(_ context.Context) ([]SessionRow, bool) {
	return nil, false
}
