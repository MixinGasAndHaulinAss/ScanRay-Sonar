package discovery

import (
	"context"
	"net"
	"strconv"
	"time"
)

// ProbeReachability returns reachable + RTT in ms by attempting a TCP connect to
// a small set of common ports. We deliberately avoid raw ICMP because the
// collector container runs unprivileged in distroless and many deploys block
// outbound ICMP at the firewall but allow management ports. Reachability is the
// boolean OR across ports; RTT is the fastest successful dial.
func ProbeReachability(ctx context.Context, ip string, ports []int, perDial time.Duration) (bool, float64) {
	if len(ports) == 0 {
		ports = []int{443, 22, 80, 161, 23}
	}
	d := net.Dialer{Timeout: perDial}
	bestRTT := -1.0
	for _, p := range ports {
		select {
		case <-ctx.Done():
			break
		default:
		}
		start := time.Now()
		c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(p)))
		if err != nil {
			continue
		}
		_ = c.Close()
		rtt := float64(time.Since(start).Microseconds()) / 1000.0
		if bestRTT < 0 || rtt < bestRTT {
			bestRTT = rtt
		}
	}
	if bestRTT < 0 {
		return false, 0
	}
	return true, bestRTT
}
