//go:build !linux && !windows

package probe

// machineID has no portable source on macOS/BSD yet — operators must
// set SONAR_FINGERPRINT explicitly. Returning "" funnels the caller
// into the friendly "no machine identifier available" error.
func machineID() string { return "" }
