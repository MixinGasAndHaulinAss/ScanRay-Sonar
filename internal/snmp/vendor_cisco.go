// Package snmp — Cisco-specific chassis collectors.
//
// Two MIB families are useful here:
//   - CISCO-PROCESS-MIB — CPU averages, per-CPU
//     entry. cpmCPUTotal5secRev exposes the most recent reading.
//   - CISCO-MEMORY-POOL-MIB / CISCO-ENHANCED-MEMPOOL-MIB — heap
//     used/free in bytes. We try the enhanced pool first (newer IOS-XE)
//     and fall back to the legacy table.
//
// All values are best-effort; missing OIDs simply leave the
// corresponding Chassis field nil so the UI knows to render "—".
package snmp

import "context"

const (
	// CISCO-PROCESS-MIB
	oidCpmCPUTotal5secRev = "1.3.6.1.4.1.9.9.109.1.1.1.1.6"
	// CISCO-MEMORY-POOL-MIB::ciscoMemoryPoolUsed
	oidCmpMemUsed = "1.3.6.1.4.1.9.9.48.1.1.1.5"
	// CISCO-MEMORY-POOL-MIB::ciscoMemoryPoolFree
	oidCmpMemFree = "1.3.6.1.4.1.9.9.48.1.1.1.6"
	// CISCO-ENHANCED-MEMPOOL-MIB::cempMemPoolUsed
	oidCempMemUsed = "1.3.6.1.4.1.9.9.221.1.1.1.1.18"
	// CISCO-ENHANCED-MEMPOOL-MIB::cempMemPoolFree
	oidCempMemFree = "1.3.6.1.4.1.9.9.221.1.1.1.1.20"
)

// CollectCiscoChassis populates Chassis CPU/memory by reading the
// Cisco-specific MIBs. Returns an empty Chassis (no error) for hosts
// that don't expose them — the universal fallback is "show CPU/memory
// as unknown" rather than failing the whole snapshot.
func CollectCiscoChassis(_ context.Context, c *Client) Chassis {
	var ch Chassis

	// CPU: average across CPUs, weighted equally. Devices with one
	// CPU report a single row; chassis with route processors + line
	// cards may report many.
	if cpuVars, err := c.BulkWalk(oidCpmCPUTotal5secRev); err == nil && len(cpuVars) > 0 {
		var total float64
		var n float64
		for _, v := range cpuVars {
			if u, ok := v.Value.Uint64(); ok && u <= 100 {
				total += float64(u)
				n++
			}
		}
		if n > 0 {
			avg := total / n
			ch.CPUPct = &avg
		}
	}

	// Memory: prefer the enhanced pool. Sum across all entries (a
	// switch with multiple supervisors reports a "Processor" pool per
	// engine — combining them gives the operator the chassis total).
	used, free, ok := walkMemPair(c, oidCempMemUsed, oidCempMemFree)
	if !ok {
		used, free, ok = walkMemPair(c, oidCmpMemUsed, oidCmpMemFree)
	}
	if ok {
		usedB := used
		totalB := used + free
		ch.MemUsedBytes = &usedB
		ch.MemTotalBytes = &totalB
	}

	return ch
}

// walkMemPair sums a (used, free) MIB pair, returning whether either
// walk produced any rows. The two roots are aligned by OID-suffix
// (per-pool index) but we don't bother joining — we just sum, since
// the operator dashboard shows chassis totals.
func walkMemPair(c *Client, usedOID, freeOID string) (used, free uint64, ok bool) {
	usedVars, err1 := c.BulkWalk(usedOID)
	freeVars, err2 := c.BulkWalk(freeOID)
	if err1 != nil && err2 != nil {
		return 0, 0, false
	}
	if len(usedVars) == 0 && len(freeVars) == 0 {
		return 0, 0, false
	}
	for _, v := range usedVars {
		if u, ok := v.Value.Uint64(); ok {
			used += u
		}
	}
	for _, v := range freeVars {
		if u, ok := v.Value.Uint64(); ok {
			free += u
		}
	}
	return used, free, true
}
