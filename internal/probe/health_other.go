//go:build !windows && !linux

package probe

import "context"

// CollectHealthSignals is a no-op on platforms we don't actively
// target. Returning nil from extras.runHealthLoop leaves
// extras.health unchanged, which keeps the snapshot's `health` field
// nil — the dashboards then render the same "no data" placeholder
// they show for older probes.
func CollectHealthSignals(_ context.Context) *HealthSignals { return nil }
