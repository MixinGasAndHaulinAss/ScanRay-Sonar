// process_tracker.go — bookkeeping needed to turn cumulative
// per-process counters (read/write/sent/recv bytes) into the bps
// rates the UI wants.
//
// gopsutil only exposes cumulative byte counters; rate information
// has to be computed by the caller across two observations. We keep
// the previous snapshot's counters in a map keyed by (pid, started)
// — including the start time defends against pid reuse, where a
// new short-lived process inherits the pid of one we sampled
// earlier and would otherwise produce a wildly negative delta.
//
// The cache is bounded only by "live process count": old PIDs that
// no longer appear are evicted at the end of every collection pass.
//
// All fields are uint64 because gopsutil exposes them that way; we
// use saturating subtraction (max(0, new-old)) to handle the rare
// case of a counter rollover without leaking negative deltas.

package probe

import (
	"sync"
	"time"
)

type procCounters struct {
	readBytes  uint64
	writeBytes uint64
	netSent    uint64
	netRecv    uint64
	at         time.Time
}

type procTracker struct {
	mu   sync.Mutex
	prev map[procKey]procCounters
}

// procKey defends against PID reuse by including the create time.
type procKey struct {
	pid     int32
	started int64 // unix-ms; 0 if unavailable
}

var procDelta = &procTracker{prev: map[procKey]procCounters{}}

// recordAndDelta stores `now` for `key` and returns the per-second
// rate vs. the previously stored sample. Returns zeros if no prior
// sample exists (the very first snapshot can't compute rates).
func (t *procTracker) recordAndDelta(key procKey, now procCounters) (readBps, writeBps, netSentBps, netRecvBps uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	prev, ok := t.prev[key]
	t.prev[key] = now
	if !ok {
		return 0, 0, 0, 0
	}
	dt := now.at.Sub(prev.at).Seconds()
	if dt <= 0.5 {
		// Anything under half a second is too noisy to report.
		return 0, 0, 0, 0
	}
	rate := func(a, b uint64) uint64 {
		if a < b {
			return 0
		}
		return uint64(float64(a-b) / dt)
	}
	return rate(now.readBytes, prev.readBytes),
		rate(now.writeBytes, prev.writeBytes),
		rate(now.netSent, prev.netSent),
		rate(now.netRecv, prev.netRecv)
}

// reapMissing evicts entries for PIDs that were not seen in the most
// recent sweep, keeping memory usage bounded over weeks of uptime.
func (t *procTracker) reapMissing(seen map[procKey]struct{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for k := range t.prev {
		if _, ok := seen[k]; !ok {
			delete(t.prev, k)
		}
	}
}
