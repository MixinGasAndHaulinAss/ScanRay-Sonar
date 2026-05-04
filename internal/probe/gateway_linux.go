//go:build linux

package probe

import (
	"bufio"
	"encoding/hex"
	"net"
	"os"
	"strings"
)

// DefaultGatewayIP returns the IPv4 address of the host's default
// route ("0.0.0.0/0"). It parses /proc/net/route directly to avoid
// shelling out — `ip route show default` would also work but we
// don't want a runtime dependency on iproute2.
//
// /proc/net/route layout (tab-separated; first row is the header):
//
//	Iface  Destination  Gateway   Flags  RefCnt  Use  Metric  Mask  ...
//	wlp3s0 00000000     0103A8C0  0003   0       0    600     ...
//
// The Destination is "00000000" for the default route. The Gateway
// column is little-endian hex, so 0103A8C0 == 192.168.3.1.
//
// Returns "" if no default route is present.
func DefaultGatewayIP() string {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return ""
	}
	defer f.Close()

	scan := bufio.NewScanner(f)
	first := true
	for scan.Scan() {
		if first {
			first = false
			continue
		}
		fields := strings.Fields(scan.Text())
		if len(fields) < 3 {
			continue
		}
		if fields[1] != "00000000" {
			continue
		}
		if ip := decodeProcRouteHex(fields[2]); ip != "" {
			return ip
		}
	}
	return ""
}

// decodeProcRouteHex turns a /proc/net/route hex address ("0103A8C0")
// into the dotted-quad ("192.168.3.1"). Returns "" on a parse error.
func decodeProcRouteHex(s string) string {
	if len(s) != 8 {
		return ""
	}
	raw, err := hex.DecodeString(s)
	if err != nil {
		return ""
	}
	// Little-endian byte order in the proc file.
	ip := net.IPv4(raw[3], raw[2], raw[1], raw[0])
	if ip.IsUnspecified() {
		return ""
	}
	return ip.String()
}
