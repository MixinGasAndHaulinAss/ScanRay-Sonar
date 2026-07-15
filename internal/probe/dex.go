// dex.go — Digital Employee Experience telemetry that backs the
// ControlUp-style device tabs (Installed Apps, Missing Patches,
// Top Apps, Event Log, Power Events, Stopped Processes, Storage I/O).
//
// Slow inventory (apps/patches/events) is refreshed on the health
// cadence (5 min) and cached on `extras`. Fast signals (app focus,
// process stops, storage I/O samples) update every snapshot tick.

package probe

import (
	"context"
	"sync"
	"time"
)

// InstalledApp is one row of the Installed Applications tab.
type InstalledApp struct {
	Name            string `json:"name"`
	Version         string `json:"version,omitempty"`
	Publisher       string `json:"publisher,omitempty"`
	InstallDate     string `json:"installDate,omitempty"`
	InstallLocation string `json:"installLocation,omitempty"`
}

// BrowserExtension is one Chrome/Edge extension from the profile Extensions path.
type BrowserExtension struct {
	Browser string `json:"browser"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	ID      string `json:"id,omitempty"`
}

// MissingPatch is one outstanding Windows Update / patch.
type MissingPatch struct {
	Title    string   `json:"title"`
	KB       string   `json:"kb,omitempty"`
	Severity string   `json:"severity,omitempty"`
	SizeMB   *float64 `json:"sizeMb,omitempty"`
}

// Win11Readiness summarises upgrade readiness signals.
type Win11Readiness struct {
	Eligible   *bool  `json:"eligible,omitempty"`
	Reason     string `json:"reason,omitempty"`
	TPMReady   *bool  `json:"tpmReady,omitempty"`
	SecureBoot *bool  `json:"secureBoot,omitempty"`
	CPUOK      *bool  `json:"cpuOk,omitempty"`
	RAMOK      *bool  `json:"ramOk,omitempty"`
	StorageOK  *bool  `json:"storageOk,omitempty"`
}

// AppFocusRow is an aggregated foreground-app sample for Top Apps.
type AppFocusRow struct {
	Name         string  `json:"name"`
	PID          int32   `json:"pid,omitempty"`
	FocusSeconds float64 `json:"focusSeconds"`
	FocusPct     float64 `json:"focusPct,omitempty"`
	LastSeen     string  `json:"lastSeen,omitempty"`
}

// ProcessStopEvent is a process that exited since a prior sample.
type ProcessStopEvent struct {
	Time     string  `json:"time"`
	Name     string  `json:"name"`
	PID      int32   `json:"pid"`
	User     string  `json:"user,omitempty"`
	CPUPct   float64 `json:"cpuPct,omitempty"`
	MemPct   float64 `json:"memPct,omitempty"`
	Duration string  `json:"duration,omitempty"`
}

// EventLogRow is a recent Windows event-log entry.
type EventLogRow struct {
	Time     string `json:"time"`
	Log      string `json:"log,omitempty"`
	Level    string `json:"level,omitempty"`
	Provider string `json:"provider,omitempty"`
	EventID  int    `json:"eventId,omitempty"`
	Message  string `json:"message,omitempty"`
}

// PowerEvent is a sleep/wake/shutdown/reboot related system event.
type PowerEvent struct {
	Time    string `json:"time"`
	Kind    string `json:"kind"` // sleep, wake, shutdown, reboot, other
	EventID int    `json:"eventId,omitempty"`
	Message string `json:"message,omitempty"`
}

// StorageIOEvent is a recent high disk-I/O sample. Full per-file ETW
// paths are not always available; File may be empty and Bytes is the
// combined read+write rate for the process over the sample interval.
type StorageIOEvent struct {
	Time     string `json:"time"`
	Process  string `json:"process"`
	PID      int32  `json:"pid"`
	File     string `json:"file,omitempty"`
	Bytes    uint64 `json:"bytes"`
	ReadBps  uint64 `json:"readBps,omitempty"`
	WriteBps uint64 `json:"writeBps,omitempty"`
}

// DexInventory is the slow-cadence DEX cache.
type DexInventory struct {
	InstalledApps       []InstalledApp     `json:"installedApps,omitempty"`
	InstalledExtensions []BrowserExtension `json:"installedExtensions,omitempty"`
	MissingPatches      []MissingPatch     `json:"missingPatches,omitempty"`
	Win11Readiness      *Win11Readiness    `json:"win11Readiness,omitempty"`
	EventLog            []EventLogRow      `json:"eventLog,omitempty"`
	PowerEvents         []PowerEvent       `json:"powerEvents,omitempty"`
	CollectedAt         string             `json:"collectedAt,omitempty"`
	// Warnings from the Windows inventory script (e.g. WUA timeout).
	// Merged into Snapshot.CollectionWarnings; not shown as its own tab.
	Warnings []string `json:"warnings,omitempty"`
}

type dexState struct {
	mu sync.RWMutex

	inventory *DexInventory

	// focusCounts accumulates seconds of foreground time by process name.
	focusCounts  map[string]float64
	focusPID     map[string]int32
	focusLast    map[string]time.Time
	focusSamples int

	// stopped / storage rings (newest last).
	stopped []ProcessStopEvent
	storage []StorageIOEvent

	// priorProcs for stop detection keyed by pid+createMs.
	priorProcs map[procKey]ProcessRow
}

var dex = &dexState{
	focusCounts: map[string]float64{},
	focusPID:    map[string]int32{},
	focusLast:   map[string]time.Time{},
	priorProcs:  map[procKey]ProcessRow{},
}

func (d *dexState) setInventory(inv *DexInventory) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.inventory = inv
}

func (d *dexState) latestInventory() *DexInventory {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.inventory == nil {
		return nil
	}
	cp := *d.inventory
	cp.InstalledApps = append([]InstalledApp(nil), d.inventory.InstalledApps...)
	cp.InstalledExtensions = append([]BrowserExtension(nil), d.inventory.InstalledExtensions...)
	cp.MissingPatches = append([]MissingPatch(nil), d.inventory.MissingPatches...)
	cp.EventLog = append([]EventLogRow(nil), d.inventory.EventLog...)
	cp.PowerEvents = append([]PowerEvent(nil), d.inventory.PowerEvents...)
	cp.Warnings = append([]string(nil), d.inventory.Warnings...)
	if d.inventory.Win11Readiness != nil {
		w := *d.inventory.Win11Readiness
		cp.Win11Readiness = &w
	}
	return &cp
}

func (d *dexState) recordFocus(name string, pid int32, seconds float64) {
	if name == "" || seconds <= 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.focusCounts[name] += seconds
	d.focusPID[name] = pid
	d.focusLast[name] = time.Now()
	d.focusSamples++
}

func (d *dexState) topApps(limit int) []AppFocusRow {
	d.mu.RLock()
	defer d.mu.RUnlock()
	type pair struct {
		name string
		sec  float64
	}
	var total float64
	pairs := make([]pair, 0, len(d.focusCounts))
	for n, s := range d.focusCounts {
		pairs = append(pairs, pair{n, s})
		total += s
	}
	// insertion sort by sec desc (small N)
	for i := 1; i < len(pairs); i++ {
		j := i
		for j > 0 && pairs[j].sec > pairs[j-1].sec {
			pairs[j], pairs[j-1] = pairs[j-1], pairs[j]
			j--
		}
	}
	if limit > 0 && len(pairs) > limit {
		pairs = pairs[:limit]
	}
	out := make([]AppFocusRow, 0, len(pairs))
	for _, p := range pairs {
		row := AppFocusRow{
			Name:         p.name,
			PID:          d.focusPID[p.name],
			FocusSeconds: roundTo(p.sec, 1),
		}
		if total > 0 {
			row.FocusPct = roundTo(p.sec/total*100, 1)
		}
		if t, ok := d.focusLast[p.name]; ok {
			row.LastSeen = t.UTC().Format(time.RFC3339)
		}
		out = append(out, row)
	}
	return out
}

func (d *dexState) pushStopped(evs []ProcessStopEvent) {
	if len(evs) == 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopped = append(d.stopped, evs...)
	const capN = 200
	if len(d.stopped) > capN {
		d.stopped = append([]ProcessStopEvent(nil), d.stopped[len(d.stopped)-capN:]...)
	}
}

func (d *dexState) recentStopped() []ProcessStopEvent {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if len(d.stopped) == 0 {
		return nil
	}
	out := make([]ProcessStopEvent, len(d.stopped))
	copy(out, d.stopped)
	return out
}

func (d *dexState) pushStorage(evs []StorageIOEvent) {
	if len(evs) == 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.storage = append(d.storage, evs...)
	const capN = 200
	if len(d.storage) > capN {
		d.storage = append([]StorageIOEvent(nil), d.storage[len(d.storage)-capN:]...)
	}
}

func (d *dexState) recentStorage() []StorageIOEvent {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if len(d.storage) == 0 {
		return nil
	}
	out := make([]StorageIOEvent, len(d.storage))
	copy(out, d.storage)
	return out
}

// detectProcessStops compares the current process set to the prior
// snapshot and records exits. Call once per CollectSnapshot after
// collectProcesses has built the full process list (we pass all rows
// before Top-N truncation via noteAllProcesses).
func (d *dexState) noteProcesses(rows []ProcessRow, keys map[procKey]struct{}) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	d.mu.Lock()
	defer d.mu.Unlock()

	var stops []ProcessStopEvent
	for k, prev := range d.priorProcs {
		if _, ok := keys[k]; ok {
			continue
		}
		dur := ""
		if prev.StartedAt != "" {
			if t, err := time.Parse(time.RFC3339, prev.StartedAt); err == nil {
				dur = formatDurShort(time.Since(t))
			}
		}
		stops = append(stops, ProcessStopEvent{
			Time:     now,
			Name:     prev.Name,
			PID:      prev.PID,
			User:     prev.User,
			CPUPct:   prev.CPUPct,
			MemPct:   prev.MemPct,
			Duration: dur,
		})
	}
	next := make(map[procKey]ProcessRow, len(rows))
	for _, r := range rows {
		// Rebuild key from startedAt when possible.
		var started int64
		if r.StartedAt != "" {
			if t, err := time.Parse(time.RFC3339, r.StartedAt); err == nil {
				started = t.UnixMilli()
			}
		}
		k := procKey{pid: r.PID, started: started}
		next[k] = r
	}
	d.priorProcs = next
	if len(stops) > 0 {
		d.stopped = append(d.stopped, stops...)
		const capN = 200
		if len(d.stopped) > capN {
			d.stopped = append([]ProcessStopEvent(nil), d.stopped[len(d.stopped)-capN:]...)
		}
	}
}

func formatDurShort(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	sec := int(d.Seconds())
	if sec < 60 {
		return itoa(sec) + "s"
	}
	if sec < 3600 {
		return itoa(sec/60) + "m " + itoa(sec%60) + "s"
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	return itoa(h) + "h " + itoa(m) + "m"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// CollectDEXInventory is implemented per-OS.
func CollectDEXInventory(ctx context.Context) *DexInventory {
	return collectDEXInventoryOS(ctx)
}

// SampleAppFocus is implemented per-OS; no-op elsewhere.
func SampleAppFocus() (name string, pid int32, ok bool) {
	return sampleAppFocusOS()
}

// runFocusLoop samples the foreground window every 15s.
func runFocusLoop(ctx context.Context) {
	prev := time.Now()
	tick := func() {
		now := time.Now()
		dt := now.Sub(prev).Seconds()
		prev = now
		if dt <= 0 || dt > 60 {
			dt = 15
		}
		name, pid, ok := SampleAppFocus()
		if ok {
			dex.recordFocus(name, pid, dt)
		}
	}
	tick()
	t := time.NewTicker(15 * time.Second)
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
