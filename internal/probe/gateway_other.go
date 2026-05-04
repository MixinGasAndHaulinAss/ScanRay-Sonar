//go:build !linux && !windows && !darwin

package probe

// DefaultGatewayIP is a no-op for platforms we don't actively target.
// Returning "" causes the latency module to skip the gateway probe;
// the 8.8.8.8 latency row is unaffected.
func DefaultGatewayIP() string { return "" }
