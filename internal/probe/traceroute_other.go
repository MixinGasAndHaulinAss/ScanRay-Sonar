//go:build !windows

package probe

import "context"

// TraceHopCount is the cross-platform entry point for the runHealthLoop
// traceroute. On Linux/macOS we use the same raw-ICMP TTL-ramp as
// ProbeICMP (which works correctly on those platforms); on Windows
// the implementation shells out to tracert.exe — see
// traceroute_windows.go.
func TraceHopCount(ctx context.Context, addr string, maxTTL int) (int, error) {
	return traceICMPRamp(ctx, addr, maxTTL)
}
