// extras.go — cached "slow" telemetry that doesn't change every 30 s.
//
// The snapshot loop ticks every 60 s. Two classes of data are slower
// than that and shouldn't be re-collected on every snapshot:
//
//   * Latency probes — each probe takes ~1.6 s end to end. We probe
//     the configured target (default 8.8.8.8) plus the local default
//     gateway every 60 s on a separate goroutine.
//   * Health signals — battery, BSOD count, missing patches, WiFi
//     RSSI, etc. Some of these (especially Get-WinEvent + COM Update
//     queries on Windows) are expensive enough that we cache them at
//     a 5-minute cadence.
//
// CollectSnapshot reads from the shared `extras` singleton; the
// timer goroutines write to it. A tiny mutex keeps reads/writes
// race-free without meaningful contention.

package probe

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// LatencyTarget is one logical target for the latency probe.
type LatencyTarget struct {
	Name    string // "8.8.8.8", "gateway"
	Address string // resolved IP
}

type extrasState struct {
	mu      sync.RWMutex
	latency []LatencyRow
	health  *HealthSignals

	// slowWarnings are non-fatal health/DEX collection issues from the
	// 5-minute loop (e.g. Windows Update COM timeout). Merged into
	// Snapshot.CollectionWarnings on each CollectSnapshot.
	slowWarnings []string

	// icmpBroken is set on the first ICMP listen failure so the
	// "ICMP unavailable" warning lands in CollectionWarnings exactly
	// once instead of every minute.
	icmpBroken bool
}

var extras = &extrasState{}

// LatestLatency returns a copy of the most recent latency rows.
func (e *extrasState) LatestLatency() []LatencyRow {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if len(e.latency) == 0 {
		return nil
	}
	out := make([]LatencyRow, len(e.latency))
	copy(out, e.latency)
	return out
}

// LatestHealth returns a copy of the cached HealthSignals or nil.
func (e *extrasState) LatestHealth() *HealthSignals {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.health == nil {
		return nil
	}
	cp := *e.health
	return &cp
}

// ICMPBroken is consulted by CollectSnapshot to add a one-time
// warning to the snapshot's CollectionWarnings.
func (e *extrasState) ICMPBroken() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.icmpBroken
}

// setLatency overwrites the cached latency rows.
func (e *extrasState) setLatency(rows []LatencyRow, broken bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.latency = rows
	if broken {
		e.icmpBroken = true
	}
}

// setHealth overwrites the cached health signals.
func (e *extrasState) setHealth(h *HealthSignals) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.health = h
}

// setSlowWarnings replaces the cached health/DEX collection warnings.
func (e *extrasState) setSlowWarnings(w []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(w) == 0 {
		e.slowWarnings = nil
		return
	}
	e.slowWarnings = append([]string(nil), w...)
}

// LatestSlowWarnings returns a copy of health/DEX collection warnings.
func (e *extrasState) LatestSlowWarnings() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if len(e.slowWarnings) == 0 {
		return nil
	}
	return append([]string(nil), e.slowWarnings...)
}

// LatencyTargets resolves the targets we want to probe right now.
// The 8.8.8.8 target is a fixed literal address (DNS resolution can't
// affect it). The gateway lookup may return "" — in that case we
// return only the 8.8.8.8 row so the UI still has something to chart.
func LatencyTargets(target string) []LatencyTarget {
	if target == "" {
		target = "8.8.8.8"
	}
	out := []LatencyTarget{{Name: target, Address: target}}
	if gw := DefaultGatewayIP(); gw != "" {
		out = append(out, LatencyTarget{Name: "gateway", Address: gw})
	}
	return out
}

// runLatencyLoop is started by the run.go ingest loop. It probes once
// immediately (so the first snapshot has data) and then every 60 s.
// Cancellation cuts in between iterations.
func runLatencyLoop(ctx context.Context, log *slog.Logger, target string) {
	tick := func() {
		probeCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		var rows []LatencyRow
		broken := false
		for _, t := range LatencyTargets(target) {
			row, err := ProbeICMP(probeCtx, t.Address)
			if err != nil {
				if !extras.ICMPBroken() {
					log.Warn("icmp probe failed", "target", t.Address, "err", err)
				}
				broken = true
				continue
			}
			row.Target = t.Name
			row.Address = t.Address
			rows = append(rows, row)
		}
		extras.setLatency(rows, broken)
	}

	tick()
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}

// runHealthLoop refreshes HealthSignals on a 5-minute cadence.
// CollectHealthSignals is implemented per-OS in
// health_{windows,linux,other}.go. After collecting the per-OS signals
// we also do one TTL-ramp traceroute to the primary latency target so
// the Network · Latency dashboard can rank hosts by path length.
//
// Health, traceroute, and DEX each get their own timeout budget so a
// hung Windows Update COM search (common on AVD/WSUS-broken hosts)
// cannot starve installed-apps / event-log inventory.
func runHealthLoop(ctx context.Context, log *slog.Logger) {
	tick := func() {
		var warns []string

		hCtx, hCancel := context.WithTimeout(ctx, 45*time.Second)
		h := CollectHealthSignals(hCtx)
		hErr := hCtx.Err()
		hCancel()
		if h == nil {
			h = &HealthSignals{}
		}
		warns = append(warns, h.SlowCollectionWarnings...)
		h.SlowCollectionWarnings = nil
		if hErr == context.DeadlineExceeded {
			warns = append(warns, "health: collection timed out")
		}

		// Traceroute is comparatively expensive (~10-30 s for a path
		// of typical length). Independent budget so it never blocks DEX.
		tCtx, tCancel := context.WithTimeout(ctx, 25*time.Second)
		if hops, err := TraceHopCount(tCtx, "8.8.8.8", 30); err == nil && hops > 0 {
			h.TracerouteHops = &hops
		} else if err != nil {
			log.Debug("traceroute failed", "err", err)
		}
		tCancel()
		extras.setHealth(h)

		// DEX inventory shares the health cadence — COM Update +
		// registry Uninstall + WinEvent are too heavy for the 30s loop.
		dCtx, dCancel := context.WithTimeout(ctx, 60*time.Second)
		inv := CollectDEXInventory(dCtx)
		dErr := dCtx.Err()
		dCancel()
		if inv != nil {
			warns = append(warns, inv.Warnings...)
			inv.Warnings = nil
			dex.setInventory(inv)
			if dErr == context.DeadlineExceeded &&
				len(inv.InstalledApps) == 0 && len(inv.EventLog) == 0 {
				warns = append(warns, "dex: inventory timed out")
			}
		} else if dErr == context.DeadlineExceeded {
			warns = append(warns, "dex: inventory timed out")
		}
		extras.setSlowWarnings(warns)
	}
	tick()
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}

// suppress unused-warn for log import on platforms that don't log
// anything from the loops.
var _ = slog.Default
