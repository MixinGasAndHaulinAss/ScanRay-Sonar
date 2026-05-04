//go:build windows

package probe

import (
	"context"
)

// CollectHealthSignals is the Windows entry point. The expensive
// data sources (CIM/WMI for battery, COM for Microsoft.Update,
// Get-WinEvent for the event-log roll-ups) are batched into a single
// PowerShell invocation in winRunPSBatch — one ~1 s child process
// every 5 minutes, not eight.
//
// CPU + disk queue lengths are read separately via typeperf because
// PowerShell's Get-Counter has a hard 1-second sample window that
// would balloon the batch's wall time.
//
// Anything that fails individually (no battery on a desktop, no
// Microsoft.Update agent on a domain-controlled WSUS host, etc.)
// leaves the corresponding HealthSignals field nil; the JSON
// omitempty drops it from the wire and the dashboard renders "—".
func CollectHealthSignals(ctx context.Context) *HealthSignals {
	h := &HealthSignals{}
	winRunPSBatch(ctx, h)
	winRunTypeperf(ctx, h)
	return h
}
