// Package probe holds the implementation of the sonar-probe agent:
// hardware fingerprinting, enrollment, websocket transport, and the
// long-lived run loop. cmd/sonar-probe is just a thin entry point.
package probe

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// Fingerprint returns a stable per-host identifier suitable for use as
// the agent fingerprint. The goal is "the same host always produces the
// same value, two hosts virtually never collide" — not a cryptographic
// secret. It is sent in the clear at enrollment time and recorded in
// the agents table.
//
// Per-OS specifics live in fingerprint_<os>.go via machineID():
//
//   - Linux:   /etc/machine-id, /sys/class/dmi/id/product_uuid, or
//     /var/lib/dbus/machine-id (whichever is readable).
//   - Windows: HKLM\Software\Microsoft\Cryptography!MachineGuid.
//   - other:   no source available; the operator must set
//     SONAR_FINGERPRINT explicitly.
//
// The chosen source plus the hostname is sha256'd so a leaked
// machine-id can't be trivially correlated across systems.
func Fingerprint(hostname string) (string, error) {
	raw := machineID()
	if raw == "" {
		return "", errors.New("probe: no machine identifier available; set SONAR_FINGERPRINT")
	}
	h := sha256.Sum256([]byte(raw + "|" + hostname))
	return "sonar1:" + hex.EncodeToString(h[:16]), nil
}
