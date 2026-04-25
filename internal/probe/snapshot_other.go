//go:build !windows && !linux

package probe

import "context"

// collectOSExtras is a no-op on platforms we don't actively target.
// The snapshot still ships the cross-platform fields collected by
// snapshot.go.
func collectOSExtras(_ context.Context, _ *Snapshot) {}
