//go:build windows

package probe

import (
	"bufio"
	"context"
	"net"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// DefaultGatewayIP returns the IPv4 address of the host's default
// route. Windows has no convenient userspace file for this — the
// modern Win32 API is GetIpForwardTable2 — but `route print -4 0.0.0.0`
// is a built-in command on every supported Windows version and the
// output is trivially parseable.
//
// We hide the cmd.exe console window with SysProcAttr.HideWindow so
// the probe doesn't flicker a black box on the user's screen when
// running interactively (it normally runs as the SonarProbe SYSTEM
// service where this is irrelevant, but the dev workstation case
// matters for engineers iterating locally).
func DefaultGatewayIP() string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "route", "print", "-4", "0.0.0.0")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return parseWindowsRoutePrint(string(out))
}

// parseWindowsRoutePrint extracts the lowest-metric default-route
// gateway from `route print -4 0.0.0.0` output. The relevant section
// looks like:
//
//	===========================================================================
//	Active Routes:
//	Network Destination        Netmask          Gateway       Interface  Metric
//	          0.0.0.0          0.0.0.0     192.168.1.1    192.168.1.10     35
//	          0.0.0.0          0.0.0.0  192.168.10.254   192.168.10.42    25
//	===========================================================================
//
// We pick the row with the smallest Metric (most preferred). Hosts
// connected to two networks (Wi-Fi + tethered Ethernet) commonly have
// two default routes and Windows uses the lowest metric.
func parseWindowsRoutePrint(text string) string {
	bestMetric := -1
	var bestIP string
	scan := bufio.NewScanner(strings.NewReader(text))
	for scan.Scan() {
		fields := strings.Fields(scan.Text())
		if len(fields) < 5 {
			continue
		}
		// We only care about IPv4 default routes.
		if fields[0] != "0.0.0.0" || fields[1] != "0.0.0.0" {
			continue
		}
		ip := net.ParseIP(fields[2])
		if ip == nil || ip.To4() == nil {
			continue
		}
		// Metric is the last column. Parse defensively.
		var metric int
		if _, err := readInt(fields[len(fields)-1], &metric); err != nil {
			continue
		}
		if bestMetric < 0 || metric < bestMetric {
			bestMetric = metric
			bestIP = ip.String()
		}
	}
	return bestIP
}

// readInt is a tiny strconv shim to keep parseWindowsRoutePrint
// allocation-free for the common case.
func readInt(s string, out *int) (int, error) {
	n := 0
	negative := false
	for i, c := range s {
		if i == 0 && c == '-' {
			negative = true
			continue
		}
		if c < '0' || c > '9' {
			return 0, &parseIntError{s: s}
		}
		n = n*10 + int(c-'0')
	}
	if negative {
		n = -n
	}
	*out = n
	return n, nil
}

type parseIntError struct{ s string }

func (e *parseIntError) Error() string { return "not an integer: " + e.s }
