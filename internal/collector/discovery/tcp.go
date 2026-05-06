// Package discovery contains lightweight network recon helpers used by
// sonar-collector (Phase 2). Full vendor CLI / VMware integrations land here
// incrementally.
package discovery

import (
	"context"
	"net"
	"strconv"
	"time"
)

// ProbeTCP tries common management ports and returns true if any accept a TCP connection.
func ProbeTCP(ctx context.Context, ip string, ports []int, perDial time.Duration) bool {
	d := net.Dialer{Timeout: perDial}
	for _, p := range ports {
		addr := net.JoinHostPort(ip, strconv.Itoa(p))
		c, err := d.DialContext(ctx, "tcp", addr)
		if err == nil {
			_ = c.Close()
			return true
		}
	}
	return false
}
