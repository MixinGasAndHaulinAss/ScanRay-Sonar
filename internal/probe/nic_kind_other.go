//go:build !linux && !windows

package probe

// classifyNICOS is a no-op on platforms where we don't have a cheap
// authoritative source. The cross-platform name heuristic in
// classifyNICName takes over.
func classifyNICOS(_ string) string { return "" }
