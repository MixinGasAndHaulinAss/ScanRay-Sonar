// publicip.go — discover the host's apparent public (NAT'd) IP.
//
// We hit https://icanhazip.com once and cache the answer for an hour.
// The probe runs on potentially flaky networks (laptops, hotel WiFi,
// airgapped gov LANs that route nothing outbound), so the lookup is
// best-effort: failures don't prevent the snapshot from being sent.
package probe

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// publicIPProbeURL is the discovery endpoint. icanhazip.com returns a
// single line containing only the dotted-quad IP, no JSON, no banner.
// Cloudflare runs it; same outage profile as the rest of the
// internet, which is fine for this purpose.
const publicIPProbeURL = "https://icanhazip.com"

// publicIPTTL is how long a successful lookup is cached. An hour
// strikes a balance between catching IP changes from CGNAT pool
// rotations and not hammering icanhazip every snapshot cycle.
const publicIPTTL = time.Hour

var (
	pubIPMu    sync.Mutex
	pubIPCache string
	pubIPAt    time.Time
)

// PublicIP returns the host's apparent public IP as a string. Returns
// "" when discovery failed (no network, blocked egress, etc.). The
// caller treats "" as "don't update the column", which is the right
// fallback — we'd rather keep a stale value the operator already
// vetted than overwrite it with NULL.
func PublicIP(ctx context.Context) string {
	pubIPMu.Lock()
	if time.Since(pubIPAt) < publicIPTTL && pubIPCache != "" {
		ip := pubIPCache
		pubIPMu.Unlock()
		return ip
	}
	pubIPMu.Unlock()

	ip := fetchPublicIP(ctx)
	if ip == "" {
		return ""
	}
	pubIPMu.Lock()
	pubIPCache = ip
	pubIPAt = time.Now()
	pubIPMu.Unlock()
	return ip
}

func fetchPublicIP(ctx context.Context) string {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(cctx, http.MethodGet, publicIPProbeURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "scanraysonar-probe")
	req.Header.Set("Accept", "text/plain")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return ""
	}
	candidate := strings.TrimSpace(string(body))
	addr, err := netip.ParseAddr(candidate)
	if err != nil {
		return ""
	}
	if !addr.IsValid() || addr.IsUnspecified() ||
		addr.IsLoopback() || addr.IsLinkLocalUnicast() ||
		addr.IsPrivate() {
		// icanhazip is a public service; if it returned a private
		// address something is wrong (split-horizon DNS, captive
		// portal, etc.) — discard so we don't claim the box is
		// "exposed" at 192.168.x.x.
		return ""
	}
	return addr.String()
}
