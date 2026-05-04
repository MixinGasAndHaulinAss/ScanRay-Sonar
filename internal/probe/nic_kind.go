// nic_kind.go — classify a network interface as "wired", "wireless",
// "virtual", or "loopback" so the UI can split traffic by adapter type
// (the dashboards in pages/agents/NetworkPerformance.tsx and friends
// rely on this for their WiFi-vs-Wired charts).
//
// classifyNIC uses two passes:
//  1. classifyNICOS — per-OS authoritative source (Linux:
//     /sys/class/net/<if>/wireless presence; Windows: registry-derived
//     adapter type). Returns "" if it can't decide.
//  2. classifyNICName — last-resort name heuristic. Always returns one
//     of the four labels.
//
// The split keeps the snapshot file cross-platform; OS-specific logic
// lives in nic_kind_{linux,windows,other}.go.

package probe

import "strings"

// classifyNIC returns one of "wired", "wireless", "virtual", "loopback".
func classifyNIC(name string, addrs []string) string {
	if k := classifyNICOS(name); k != "" {
		return k
	}
	return classifyNICName(name)
}

// classifyNICName is the cross-platform name-based fallback. It is
// intentionally permissive — when the OS-specific path can't decide,
// we'd rather mis-bucket a NIC than refuse to classify it.
func classifyNICName(name string) string {
	n := strings.ToLower(name)
	// Loopback first — most specific.
	if n == "lo" || strings.HasPrefix(n, "lo0") || strings.HasPrefix(n, "loopback") {
		return "loopback"
	}
	// Wireless name patterns (Linux: wlan0, wlp3s0; Windows: "Wi-Fi",
	// "Wireless"; macOS: en0 *can* be wireless but we can't tell from
	// the name alone, so the OS path handles macOS).
	if strings.HasPrefix(n, "wlan") ||
		strings.HasPrefix(n, "wlp") ||
		strings.HasPrefix(n, "wlx") ||
		strings.Contains(n, "wi-fi") ||
		strings.Contains(n, "wifi") ||
		strings.Contains(n, "wireless") ||
		strings.HasPrefix(n, "ath") ||
		strings.HasPrefix(n, "ra0") {
		return "wireless"
	}
	// Virtual / container / VPN. Order matters: docker/podman bridges
	// before the generic "virtual ethernet" check.
	if strings.HasPrefix(n, "docker") ||
		strings.HasPrefix(n, "br-") ||
		strings.HasPrefix(n, "virbr") ||
		strings.HasPrefix(n, "veth") ||
		strings.HasPrefix(n, "tap") ||
		strings.HasPrefix(n, "tun") ||
		strings.HasPrefix(n, "vmnet") ||
		strings.HasPrefix(n, "vboxnet") ||
		strings.HasPrefix(n, "utun") ||
		strings.HasPrefix(n, "ppp") ||
		strings.Contains(n, "vethernet") ||
		strings.Contains(n, "hyper-v") ||
		strings.Contains(n, "vpn") ||
		strings.Contains(n, "tailscale") ||
		strings.Contains(n, "wireguard") ||
		strings.Contains(n, "zerotier") {
		return "virtual"
	}
	return "wired"
}
