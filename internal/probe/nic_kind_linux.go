//go:build linux

package probe

import "os"

// classifyNICOS uses Linux's authoritative sources for NIC type:
//   - /sys/class/net/<if>/wireless exists for any 802.11 interface,
//     regardless of the actual interface name (modern systemd-udev
//     names like wlp3s0 follow no fixed prefix convention).
//   - /sys/class/net/<if>/type contains the ARPHRD_* numeric type;
//     772 == ARPHRD_LOOPBACK.
//
// Returns "" when no authoritative answer exists; the cross-platform
// name heuristic in classifyNICName takes over.
func classifyNICOS(name string) string {
	if name == "" {
		return ""
	}
	base := "/sys/class/net/" + name
	if _, err := os.Stat(base + "/wireless"); err == nil {
		return "wireless"
	}
	if data, err := os.ReadFile(base + "/type"); err == nil {
		s := string(data)
		// Trim trailing newline; raw text is "1\n", "772\n", etc.
		if len(s) > 0 && s[len(s)-1] == '\n' {
			s = s[:len(s)-1]
		}
		if s == "772" {
			return "loopback"
		}
	}
	return ""
}
