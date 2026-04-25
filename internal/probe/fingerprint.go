// Package probe holds the implementation of the sonar-probe agent:
// hardware fingerprinting, enrollment, websocket transport, and the
// long-lived run loop. cmd/sonar-probe is just a thin entry point.
package probe

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
)

// Fingerprint returns a stable per-host identifier suitable for use as
// the agent fingerprint. The goal is "the same host always produces the
// same value, two hosts virtually never collide" — not a cryptographic
// secret. It is sent in the clear at enrollment time and recorded in
// the agents table.
//
// Sources, in priority order:
//
//  1. /etc/machine-id  (systemd; written once per install)
//  2. /sys/class/dmi/id/product_uuid (BIOS; needs root on most hosts)
//  3. /var/lib/dbus/machine-id (legacy fallback)
//
// The chosen source plus the hostname is sha256'd so a leaked machine-id
// can't be trivially correlated across systems.
func Fingerprint(hostname string) (string, error) {
	for _, p := range []string{
		"/etc/machine-id",
		"/sys/class/dmi/id/product_uuid",
		"/var/lib/dbus/machine-id",
	} {
		b, err := os.ReadFile(p)
		if err != nil || len(b) == 0 {
			continue
		}
		raw := strings.TrimSpace(string(b))
		h := sha256.Sum256([]byte(raw + "|" + hostname))
		return "sonar1:" + hex.EncodeToString(h[:16]), nil
	}
	return "", errors.New("probe: no machine identifier available; run as root or set SONAR_FINGERPRINT")
}
