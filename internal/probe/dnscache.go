// Package probe — process-local reverse-DNS cache.
//
// Reverse DNS is best-effort enrichment for the "Talking to" view: we
// want a hostname for each remote peer when one exists, but we never
// want a slow PTR lookup to delay or block snapshot delivery.
//
// Design:
//   - Single in-process map keyed by IP. Positive answers cached for
//     1h; misses cached for 5m (so a host that has no PTR doesn't get
//     re-queried every minute).
//   - resolveBatch resolves a batch in parallel with a per-lookup
//     deadline AND an overall deadline, so a hostile resolver can't
//     stall the snapshot loop.
//   - Default resolver is the OS resolver, which on Windows respects
//     the connected NIC's DNS suffix list and on Linux reads
//     /etc/resolv.conf. That gives the best chance of resolving
//     internal hostnames against the local AD/DNS server.
package probe

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	dnsPositiveTTL = time.Hour
	dnsNegativeTTL = 5 * time.Minute
	dnsLookupTO    = 500 * time.Millisecond
	dnsTotalTO     = 4 * time.Second
	dnsParallel    = 8
)

type dnsRec struct {
	name  string
	until time.Time
}

var (
	dnsCacheMu sync.Mutex
	dnsCache   = map[string]dnsRec{}
)

// dnsCachedName returns a cached hostname for ip if we have a fresh
// entry. The bool is true on cache hit (positive or negative).
func dnsCachedName(ip string) (string, bool) {
	dnsCacheMu.Lock()
	defer dnsCacheMu.Unlock()
	rec, ok := dnsCache[ip]
	if !ok || time.Now().After(rec.until) {
		return "", false
	}
	return rec.name, true
}

func dnsStore(ip, name string) {
	ttl := dnsPositiveTTL
	if name == "" {
		ttl = dnsNegativeTTL
	}
	dnsCacheMu.Lock()
	dnsCache[ip] = dnsRec{name: name, until: time.Now().Add(ttl)}
	dnsCacheMu.Unlock()
}

// resolveBatch returns ip→hostname for the input set, using cache first
// and a bounded worker pool for the remainder. Misses leave the map
// unset; callers should treat absence as "no PTR".
func resolveBatch(ctx context.Context, ips []string) map[string]string {
	out := make(map[string]string, len(ips))
	var pending []string
	for _, ip := range ips {
		if name, ok := dnsCachedName(ip); ok {
			if name != "" {
				out[ip] = name
			}
			continue
		}
		pending = append(pending, ip)
	}
	if len(pending) == 0 {
		return out
	}
	totalCtx, cancel := context.WithTimeout(ctx, dnsTotalTO)
	defer cancel()

	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, dnsParallel)
	)
	for _, ip := range pending {
		select {
		case <-totalCtx.Done():
			return out
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			defer func() { <-sem }()
			lc, lcCancel := context.WithTimeout(totalCtx, dnsLookupTO)
			defer lcCancel()
			names, err := net.DefaultResolver.LookupAddr(lc, ip)
			var name string
			if err == nil && len(names) > 0 {
				name = strings.TrimSuffix(names[0], ".")
			}
			dnsStore(ip, name)
			if name != "" {
				mu.Lock()
				out[ip] = name
				mu.Unlock()
			}
		}(ip)
	}
	wg.Wait()
	return out
}
