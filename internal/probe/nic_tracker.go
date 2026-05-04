// nic_tracker.go — bookkeeping needed to turn cumulative per-NIC byte
// counters into the bps rates the UI wants. Mirrors the (smaller)
// procTracker pattern in process_tracker.go.
//
// gopsutil's net.IOCounters returns cumulative BytesSent/BytesRecv per
// interface; rate information has to be computed across two
// observations. We keep the previous snapshot's counters in a map
// keyed on interface name and compute saturating-subtraction deltas.

package probe

import (
	"sync"
	"time"
)

type nicCounters struct {
	bytesSent uint64
	bytesRecv uint64
	at        time.Time
}

type nicTracker struct {
	mu   sync.Mutex
	prev map[string]nicCounters
}

var nicDelta = &nicTracker{prev: map[string]nicCounters{}}

// recordAndDelta stores `now` for `name` and returns per-second rates
// vs. the previously stored sample. Returns zeros if no prior sample
// exists (the first snapshot of a probe lifetime can't compute rates).
func (t *nicTracker) recordAndDelta(name string, now nicCounters) (sentBps, recvBps uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	prev, ok := t.prev[name]
	t.prev[name] = now
	if !ok {
		return 0, 0
	}
	dt := now.at.Sub(prev.at).Seconds()
	if dt <= 0.5 {
		// Anything under half a second is too noisy to report.
		return 0, 0
	}
	rate := func(a, b uint64) uint64 {
		if a < b {
			return 0
		}
		return uint64(float64(a-b) / dt)
	}
	return rate(now.bytesSent, prev.bytesSent), rate(now.bytesRecv, prev.bytesRecv)
}

// reapMissing evicts entries for interfaces that were not seen in the
// most recent sweep. NICs come and go (USB tethers, VPN tunnels) so
// keeping every name we ever saw would slowly leak memory.
func (t *nicTracker) reapMissing(seen map[string]struct{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for k := range t.prev {
		if _, ok := seen[k]; !ok {
			delete(t.prev, k)
		}
	}
}
