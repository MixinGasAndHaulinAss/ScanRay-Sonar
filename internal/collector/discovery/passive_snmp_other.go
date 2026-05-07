//go:build !linux

// Non-Linux stub for passive SNMP discovery. The collector binary
// builds on Windows / macOS for development convenience, but the
// AF_PACKET-based capture in passive_snmp_linux.go only works on
// Linux. On other platforms CapturePassiveSNMP returns an error
// immediately so the discovery_poller logs it and moves on.
package discovery

import (
	"context"
)

// CapturePassiveSNMP — non-Linux stub.
func CapturePassiveSNMP(_ context.Context, _ PassiveCaptureOpts, _ SNMPClassifier) ([]PassiveDevice, error) {
	return nil, ErrPassiveCaptureUnsupported
}
