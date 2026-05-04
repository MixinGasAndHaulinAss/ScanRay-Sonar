//go:build darwin

package probe

import (
	"context"
	"net"
	"os/exec"
	"strings"
	"time"
)

// DefaultGatewayIP returns the IPv4 address of the host's default
// route by parsing `route -n get default`. The command exists on
// every macOS install; the output looks like:
//
//	   route to: default
//	destination: default
//	       mask: default
//	    gateway: 192.168.1.1
//	  interface: en0
//
// We grep the "gateway: " line, validate the IP, and return.
func DefaultGatewayIP() string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "route", "-n", "get", "default").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "gateway:") {
			continue
		}
		val := strings.TrimSpace(strings.TrimPrefix(line, "gateway:"))
		ip := net.ParseIP(val)
		if ip != nil && ip.To4() != nil {
			return ip.String()
		}
	}
	return ""
}
