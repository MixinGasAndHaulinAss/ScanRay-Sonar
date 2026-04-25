//go:build linux

package probe

import (
	"os"
	"strings"
)

// machineID returns a raw, stable host identifier on Linux, trying the
// usual sources in priority order. Empty string means "nothing
// readable" — the caller surfaces a clearer error message.
func machineID() string {
	for _, p := range []string{
		"/etc/machine-id",
		"/sys/class/dmi/id/product_uuid",
		"/var/lib/dbus/machine-id",
	} {
		b, err := os.ReadFile(p)
		if err != nil || len(b) == 0 {
			continue
		}
		if s := strings.TrimSpace(string(b)); s != "" {
			return s
		}
	}
	return ""
}
