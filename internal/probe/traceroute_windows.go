//go:build windows

package probe

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

// hopLineRE matches the leading two columns of a tracert output line:
// optional whitespace, the hop number, then either an RTT or "*"
// markers. We only care about whether a router responded at all, so
// any line starting with a hop integer that is followed by something
// non-empty (an IP, hostname, or RTT) counts as a successful hop.
//
// Examples that match (and the captured hop number):
//
//	"  1     1 ms    <1 ms    <1 ms  10.4.9.1"     -> 1
//	" 10    19 ms    18 ms    23 ms  8.8.8.8"      -> 10
//
// Examples that intentionally don't count:
//
//	"  4     *        *        *     Request timed out."
//
// Anything that contains "Request timed out" is filtered out below
// before we ask the regex.
var hopLineRE = regexp.MustCompile(`^\s*(\d+)\s+`)

// TraceHopCount returns the count of routers along the path to addr
// using Windows' built-in tracert.exe utility. Set maxTTL to -1 for
// the default (30). The function returns the number of hops up to
// and including the target — e.g. a path "router1, router2, target"
// reports 3.
//
// We use tracert because raw-ICMP TTL ramping doesn't work reliably
// on Windows (see traceICMPRamp comment in latency.go).
func TraceHopCount(ctx context.Context, addr string, maxTTL int) (int, error) {
	if maxTTL <= 0 || maxTTL > 64 {
		maxTTL = 30
	}
	cmd := exec.CommandContext(ctx,
		"tracert.exe",
		"-d",                          // skip reverse DNS for speed
		"-h", strconv.Itoa(maxTTL),    // max hops
		"-w", "1000",                  // 1 s wait per hop
		addr,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	hops := 0
	for _, line := range strings.Split(string(out), "\n") {
		l := strings.TrimRight(line, "\r ")
		if l == "" {
			continue
		}
		// "Request timed out." lines may be the entire RTT slot for a
		// hop; we still want to count the hop number itself if it's
		// listed, but only if at least one of the three RTT slots
		// got a response. Simpler heuristic: count a hop only when
		// the line ends with an IP address (router responded with
		// either TimeExceeded or EchoReply on at least one try).
		if !hopLineRE.MatchString(l) {
			continue
		}
		// The last whitespace-separated token should be the IP/host
		// when at least one round-trip succeeded; tracert emits
		// "Request timed out." or "*" tokens otherwise.
		fields := strings.Fields(l)
		if len(fields) == 0 {
			continue
		}
		last := fields[len(fields)-1]
		if last == "out." || last == "*" {
			continue
		}
		hops++
	}
	return hops, nil
}
