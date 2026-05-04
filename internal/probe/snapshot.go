// Package probe — system snapshot collector.
//
// snapshot.go assembles a single Snapshot value summarising "what is
// happening on this host right now". It is cross-platform; OS-specific
// bits (pending-reboot detection, Windows service inventory, Linux
// failed-unit enumeration) live in snapshot_<os>.go.
//
// Every field is JSON-tagged so the snapshot can be sent verbatim over
// the agent websocket and stored as JSONB on the API side without
// translation.
package probe

import (
	"context"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	psnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

// Snapshot is a point-in-time view of a host. Stable-shape JSON: the
// API stores it as JSONB and the UI binds to it directly, so adding new
// fields is fine but renaming breaks the dashboard.
type Snapshot struct {
	SchemaVersion int    `json:"schemaVersion"` // bump on breaking shape changes
	CapturedAt    string `json:"capturedAt"`
	CaptureMs     int64  `json:"captureMs"` // wall-clock cost of building this snapshot

	Host                Host         `json:"host"`
	PublicIP            string       `json:"publicIp,omitempty"` // discovered via icanhazip; cached 1h
	CPU                 CPU          `json:"cpu"`
	Memory              Memory       `json:"memory"`
	LoadAvg             *LoadAvg     `json:"loadAvg,omitempty"` // linux/macos only
	Disks               []Disk       `json:"disks"`
	NICs                []NIC        `json:"nics"`
	// Static hardware inventory: collected once per probe lifetime
	// (refreshed every 6h) and inlined into every snapshot. Optional
	// — older probe builds and platforms without a collector simply
	// omit the field. See internal/probe/hardware.go.
	Hardware *Hardware `json:"hardware,omitempty"`
	TopByCPU            []ProcessRow `json:"topByCpu"`
	TopByMem            []ProcessRow `json:"topByMem"`
	Listeners           []Listener   `json:"listeners"`
	// Conversations are aggregated active TCP/UDP peer pairs (one row
	// per (proto, direction, remote, process)). Schema v2+.
	Conversations       []Conversation `json:"conversations,omitempty"`
	// Latency is the per-target ICMP RTT report cached by
	// extras.runLatencyLoop. Empty on platforms without raw-socket
	// access; see CollectionWarnings for the reason. Schema v4+.
	Latency             []LatencyRow `json:"latency,omitempty"`
	// HealthSignals are slow-cadence host metrics (battery, BSOD,
	// missing patches, WiFi RSSI, queue lengths, ...). Optional —
	// older probes and unsupported platforms simply omit the field.
	// Schema v4+.
	Health              *HealthSignals `json:"health,omitempty"`
	LoggedInUsers       []SessionRow `json:"loggedInUsers"`
	PendingReboot       bool         `json:"pendingReboot"`
	PendingRebootReason string       `json:"pendingRebootReason,omitempty"`

	// Windows-only.
	StoppedAutoServices []ServiceRow `json:"stoppedAutoServices,omitempty"`
	// Linux-only.
	FailedUnits []string `json:"failedUnits,omitempty"`

	// CollectionWarnings collects non-fatal errors (e.g. one disk
	// counter unavailable, ports enumeration denied) so the UI can
	// surface partial-data conditions instead of pretending the host
	// is silent.
	CollectionWarnings []string `json:"collectionWarnings,omitempty"`
}

type Host struct {
	Hostname        string `json:"hostname"`
	OS              string `json:"os"`              // runtime.GOOS
	Platform        string `json:"platform"`        // ubuntu, debian, windows, ...
	PlatformFamily  string `json:"platformFamily"`  // debian, rhel, ...
	PlatformVersion string `json:"platformVersion"` // 22.04, 10.0.19045, ...
	KernelVersion   string `json:"kernelVersion"`
	KernelArch      string `json:"kernelArch"`
	Virtualization  string `json:"virtualization,omitempty"`
	BootTime        string `json:"bootTime"` // RFC3339
	UptimeSeconds   uint64 `json:"uptimeSeconds"`
	Procs           uint64 `json:"procs"`
}

type CPU struct {
	Model       string  `json:"model"`
	Cores       int     `json:"cores"`        // physical
	LogicalCPUs int     `json:"logicalCpus"`  // hyperthreads
	MHz         float64 `json:"mhz"`          // nominal
	UsagePct    float64 `json:"usagePct"`     // overall, last 1s
	PerCorePct  []int   `json:"perCorePct"`   // rounded to int per core
}

type Memory struct {
	TotalBytes     uint64  `json:"totalBytes"`
	UsedBytes      uint64  `json:"usedBytes"`
	AvailableBytes uint64  `json:"availableBytes"`
	UsedPct        float64 `json:"usedPct"`
	SwapTotalBytes uint64  `json:"swapTotalBytes"`
	SwapUsedBytes  uint64  `json:"swapUsedBytes"`
	SwapUsedPct    float64 `json:"swapUsedPct"`
}

type LoadAvg struct {
	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`
}

type Disk struct {
	Device     string  `json:"device"`
	Mountpoint string  `json:"mountpoint"`
	FSType     string  `json:"fsType"`
	TotalBytes uint64  `json:"totalBytes"`
	UsedBytes  uint64  `json:"usedBytes"`
	FreeBytes  uint64  `json:"freeBytes"`
	UsedPct    float64 `json:"usedPct"`
}

type NIC struct {
	Name      string   `json:"name"`
	MAC       string   `json:"mac,omitempty"`
	MTU       int      `json:"mtu,omitempty"`
	Up        bool     `json:"up"`
	// Kind is one of "wired", "wireless", "virtual", "loopback".
	// Powers the WiFi-vs-Wired charts on the Network - Performance
	// dashboard. Always populated; classifier in nic_kind.go.
	Kind      string   `json:"kind,omitempty"`
	Addresses []string `json:"addresses,omitempty"`
	BytesSent uint64   `json:"bytesSent"`
	BytesRecv uint64   `json:"bytesRecv"`
	// BytesSentBps / BytesRecvBps are deltas of the cumulative
	// counters across the previous and current snapshot, divided by
	// the elapsed time. Zero on the first snapshot of a probe
	// lifetime — see nic_tracker.go.
	BytesSentBps uint64 `json:"bytesSentBps"`
	BytesRecvBps uint64 `json:"bytesRecvBps"`
	PktsSent     uint64 `json:"pktsSent"`
	PktsRecv     uint64 `json:"pktsRecv"`
	ErrIn        uint64 `json:"errIn"`
	ErrOut       uint64 `json:"errOut"`
	DropIn       uint64 `json:"dropIn"`
	DropOut      uint64 `json:"dropOut"`
}

// ProcessRow is one row of the "top processes" tables plus enough
// stats to make those tables genuinely useful for triage: not just
// "what's eating CPU" but "is it eating disk too? is it actually
// talking to the network?". The newer fields (memPct, diskRead/Write,
// netSent/Recv, openConns) require the deltaTracker (see processes.go)
// to compare two consecutive snapshots, so they're zero on the first
// snapshot of a probe lifetime.
type ProcessRow struct {
	PID         int32   `json:"pid"`
	Name        string  `json:"name"`
	User        string  `json:"user,omitempty"`
	Cmdline     string  `json:"cmdline,omitempty"`
	CPUPct      float64 `json:"cpuPct"`
	RSSBytes    uint64  `json:"rssBytes"`
	MemPct      float64 `json:"memPct,omitempty"`
	DiskReadBps uint64  `json:"diskReadBps,omitempty"`
	DiskWriteBps uint64 `json:"diskWriteBps,omitempty"`
	NetSentBps  uint64  `json:"netSentBps,omitempty"`
	NetRecvBps  uint64  `json:"netRecvBps,omitempty"`
	OpenConns   int     `json:"openConns,omitempty"`
}

type Listener struct {
	Proto       string `json:"proto"` // tcp, udp
	Address     string `json:"address"`
	Port        uint32 `json:"port"`
	PID         int32  `json:"pid,omitempty"`
	ProcessName string `json:"processName,omitempty"`
}

// Conversation is an aggregated active connection between this host
// and a remote peer. Direction is inferred from whether the local
// port matches one of our listeners (inbound) vs an ephemeral port
// connecting out (outbound). Local peers (loopback) are excluded —
// they're noise for the "what is this host talking to" view.
type Conversation struct {
	Proto       string `json:"proto"`              // tcp, udp
	Direction   string `json:"direction"`          // inbound, outbound, local
	RemoteIP    string `json:"remoteIp"`
	RemoteHost  string `json:"remoteHost,omitempty"` // reverse-DNS, best-effort
	RemotePort  uint32 `json:"remotePort"`
	LocalPort   uint32 `json:"localPort,omitempty"`  // populated for inbound (the listener port)
	State       string `json:"state,omitempty"`      // ESTABLISHED, CLOSE_WAIT, ...
	PID         int32  `json:"pid,omitempty"`
	ProcessName string `json:"processName,omitempty"`
	Count       int    `json:"count"`                // number of socket rows aggregated
}

type SessionRow struct {
	User    string `json:"user"`
	Tty     string `json:"tty,omitempty"`
	Host    string `json:"host,omitempty"`
	Started string `json:"started,omitempty"` // RFC3339
	// State is meaningful on Windows ("Active", "Disconnected",
	// "Idle", "Listen", ...). On *nix it stays empty.
	State string `json:"state,omitempty"`
	// Source identifies the connection origin: on Windows this is
	// the RDP client name when set, or "console" for the local
	// session; on *nix gopsutil already populates Host so we leave
	// Source empty.
	Source string `json:"source,omitempty"`
}

type ServiceRow struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	StartType   string `json:"startType,omitempty"` // auto, manual, ...
	Status      string `json:"status,omitempty"`    // running, stopped, ...
}

// CollectSnapshot builds a Snapshot. It tolerates partial failures —
// any sub-collector that errors records the failure in
// CollectionWarnings and the rest of the snapshot is still returned.
//
// The 1-second CPU sampling window dominates wall time; the rest is
// effectively free.
func CollectSnapshot(ctx context.Context, hostname string) Snapshot {
	start := time.Now()
	s := Snapshot{
		SchemaVersion: 4, // v4 adds NIC.Kind/bps, Latency, HealthSignals, richer SessionRow
		CapturedAt:    start.UTC().Format(time.RFC3339Nano),
		// Pre-allocate every collection-typed field so JSON always
		// emits `[]` instead of `null` even when a collector is a
		// no-op for this OS (e.g. host.Users() is "not implemented"
		// on Windows). The UI depends on these being arrays.
		Disks:         []Disk{},
		NICs:          []NIC{},
		TopByCPU:      []ProcessRow{},
		TopByMem:      []ProcessRow{},
		Listeners:     []Listener{},
		Conversations: []Conversation{},
		LoggedInUsers: []SessionRow{},
	}

	if hi, err := host.InfoWithContext(ctx); err == nil {
		s.Host = Host{
			Hostname:        hostname,
			OS:              runtime.GOOS,
			Platform:        hi.Platform,
			PlatformFamily:  hi.PlatformFamily,
			PlatformVersion: hi.PlatformVersion,
			KernelVersion:   hi.KernelVersion,
			KernelArch:      hi.KernelArch,
			Virtualization:  strings.TrimSpace(hi.VirtualizationSystem + " " + hi.VirtualizationRole),
			BootTime:        time.Unix(int64(hi.BootTime), 0).UTC().Format(time.RFC3339),
			UptimeSeconds:   hi.Uptime,
			Procs:           hi.Procs,
		}
	} else {
		s.Host.Hostname = hostname
		s.Host.OS = runtime.GOOS
		s.warn("host.Info: " + err.Error())
	}

	if ip := PublicIP(ctx); ip != "" {
		s.PublicIP = ip
	}

	collectCPU(ctx, &s)
	collectMemory(ctx, &s)
	collectLoad(ctx, &s)
	collectDisks(ctx, &s)
	collectNICs(ctx, &s)
	collectProcesses(ctx, &s)
	collectSockets(ctx, &s)
	collectUsers(ctx, &s)
	collectOSExtras(ctx, &s) // pending reboot + per-OS service/unit lists

	// Hardware inventory: cached once per refreshHardwareEvery, so
	// after the first snapshot this is just a map read.
	s.Hardware = snapshotHardware(ctx)

	// Latency + HealthSignals are populated by separate goroutines
	// at slower cadences (60 s and 5 min, see extras.go). We just
	// copy out of the cache here so the snapshot stays a single
	// fast-path read.
	if rows := extras.LatestLatency(); len(rows) > 0 {
		s.Latency = rows
	}
	if extras.ICMPBroken() {
		s.warn("latency: ICMP probe unavailable on this host (no raw-socket privilege)")
	}
	s.Health = extras.LatestHealth()

	s.CaptureMs = time.Since(start).Milliseconds()
	return s
}

func (s *Snapshot) warn(msg string) {
	s.CollectionWarnings = append(s.CollectionWarnings, msg)
}

func collectCPU(ctx context.Context, s *Snapshot) {
	if infos, err := cpu.InfoWithContext(ctx); err == nil && len(infos) > 0 {
		s.CPU.Model = strings.TrimSpace(infos[0].ModelName)
		s.CPU.MHz = infos[0].Mhz
	}
	if cores, err := cpu.CountsWithContext(ctx, false); err == nil {
		s.CPU.Cores = cores
	}
	if logical, err := cpu.CountsWithContext(ctx, true); err == nil {
		s.CPU.LogicalCPUs = logical
	}
	// 1s sample, all cores. Blocking but bounded; ctx still cuts in.
	if pct, err := cpu.PercentWithContext(ctx, time.Second, false); err == nil && len(pct) > 0 {
		s.CPU.UsagePct = roundTo(pct[0], 1)
	} else if err != nil {
		s.warn("cpu.Percent: " + err.Error())
	}
	if perCore, err := cpu.PercentWithContext(ctx, 0, true); err == nil {
		out := make([]int, 0, len(perCore))
		for _, v := range perCore {
			out = append(out, int(v+0.5))
		}
		s.CPU.PerCorePct = out
	}
}

func collectMemory(ctx context.Context, s *Snapshot) {
	if vm, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		s.Memory.TotalBytes = vm.Total
		s.Memory.UsedBytes = vm.Used
		s.Memory.AvailableBytes = vm.Available
		s.Memory.UsedPct = roundTo(vm.UsedPercent, 1)
	} else {
		s.warn("mem.VirtualMemory: " + err.Error())
	}
	if sw, err := mem.SwapMemoryWithContext(ctx); err == nil {
		s.Memory.SwapTotalBytes = sw.Total
		s.Memory.SwapUsedBytes = sw.Used
		s.Memory.SwapUsedPct = roundTo(sw.UsedPercent, 1)
	}
}

func collectLoad(ctx context.Context, s *Snapshot) {
	// gopsutil's load package is a no-op on Windows but doesn't error;
	// it returns zeros. We only attach LoadAvg on platforms where it
	// has meaning (anything with /proc/loadavg).
	if runtime.GOOS == "windows" {
		return
	}
	if avg, err := load.AvgWithContext(ctx); err == nil {
		s.LoadAvg = &LoadAvg{
			Load1:  roundTo(avg.Load1, 2),
			Load5:  roundTo(avg.Load5, 2),
			Load15: roundTo(avg.Load15, 2),
		}
	}
}

func collectDisks(ctx context.Context, s *Snapshot) {
	parts, err := disk.PartitionsWithContext(ctx, false) // physical only
	if err != nil {
		s.warn("disk.Partitions: " + err.Error())
		return
	}
	seen := map[string]bool{}
	for _, p := range parts {
		// Skip pseudo / overlay filesystems on Linux that aren't useful
		// to operators (overlayfs from Docker, squashfs from snaps,
		// rootfs, tmpfs, etc.). Keep windows volumes (NTFS, ReFS, FAT*)
		// as-is.
		if isPseudoFS(p.Fstype) {
			continue
		}
		key := p.Device + "|" + p.Mountpoint
		if seen[key] {
			continue
		}
		seen[key] = true

		u, err := disk.UsageWithContext(ctx, p.Mountpoint)
		if err != nil || u == nil || u.Total == 0 {
			continue
		}
		s.Disks = append(s.Disks, Disk{
			Device:     p.Device,
			Mountpoint: p.Mountpoint,
			FSType:     p.Fstype,
			TotalBytes: u.Total,
			UsedBytes:  u.Used,
			FreeBytes:  u.Free,
			UsedPct:    roundTo(u.UsedPercent, 1),
		})
	}
}

// isPseudoFS returns true for Linux pseudo-filesystems we don't want to
// surface as "disks" in the UI. The list is intentionally permissive —
// when in doubt, show the volume.
func isPseudoFS(fs string) bool {
	switch strings.ToLower(fs) {
	case "tmpfs", "devtmpfs", "squashfs", "overlay", "overlayfs",
		"proc", "sysfs", "cgroup", "cgroup2", "pstore", "bpf",
		"tracefs", "debugfs", "configfs", "fusectl", "securityfs",
		"hugetlbfs", "mqueue", "ramfs", "autofs", "binfmt_misc":
		return true
	}
	return false
}

func collectNICs(ctx context.Context, s *Snapshot) {
	ifaces, err := psnet.InterfacesWithContext(ctx)
	if err != nil {
		s.warn("net.Interfaces: " + err.Error())
		return
	}
	counters, _ := psnet.IOCountersWithContext(ctx, true) // per-NIC
	cmap := make(map[string]psnet.IOCountersStat, len(counters))
	for _, c := range counters {
		cmap[c.Name] = c
	}
	now := time.Now()
	seen := make(map[string]struct{}, len(ifaces))
	for _, iface := range ifaces {
		// Skip down loopback noise but keep loopback if it's up — some
		// hosts bind services exclusively to lo.
		if iface.Name == "" {
			continue
		}
		nic := NIC{
			Name: iface.Name,
			MAC:  iface.HardwareAddr,
			MTU:  iface.MTU,
			Up:   contains(iface.Flags, "up"),
		}
		for _, a := range iface.Addrs {
			nic.Addresses = append(nic.Addresses, a.Addr)
		}
		if c, ok := cmap[iface.Name]; ok {
			nic.BytesSent = c.BytesSent
			nic.BytesRecv = c.BytesRecv
			nic.PktsSent = c.PacketsSent
			nic.PktsRecv = c.PacketsRecv
			nic.ErrIn = c.Errin
			nic.ErrOut = c.Errout
			nic.DropIn = c.Dropin
			nic.DropOut = c.Dropout
			nic.BytesSentBps, nic.BytesRecvBps = nicDelta.recordAndDelta(iface.Name, nicCounters{
				bytesSent: c.BytesSent,
				bytesRecv: c.BytesRecv,
				at:        now,
			})
			seen[iface.Name] = struct{}{}
		}
		nic.Kind = classifyNIC(iface.Name, nic.Addresses)
		s.NICs = append(s.NICs, nic)
	}
	nicDelta.reapMissing(seen)
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if strings.EqualFold(h, needle) {
			return true
		}
	}
	return false
}

// collectProcesses populates TopByCPU and TopByMem. We deliberately
// cap the lists at 10 each: longer lists balloon the snapshot and the
// long tail is rarely actionable.
//
// Each row carries a small constellation of per-process stats:
//   * cpuPct  — gopsutil's internal delta tracking, needs one prior
//               call to be non-zero (first snapshot shows 0).
//   * rss / memPct — single-shot syscall.
//   * diskRead/Write Bps — delta of cumulative I/O bytes between
//               this snapshot and the last, tracked by procDelta
//               keyed on (pid, create_time) to defend against pid
//               reuse. Net rates require platform-specific kernel
//               support (Linux: cgroup or BPF; Windows: WMI Perf
//               counters) that gopsutil doesn't surface, so they're
//               left zero for now and we'll fill them in a later
//               pass without breaking the wire format.
//   * openConns — populated post-hoc by collectSockets via
//               s.applyOpenConns once the socket pass is done.
func collectProcesses(ctx context.Context, s *Snapshot) {
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		s.warn("process.Processes: " + err.Error())
		return
	}
	now := time.Now()
	totalMem := float64(s.Memory.TotalBytes)
	rows := make([]ProcessRow, 0, len(procs))
	seen := make(map[procKey]struct{}, len(procs))
	for _, p := range procs {
		name, _ := p.NameWithContext(ctx)
		if name == "" {
			continue
		}
		row := ProcessRow{PID: p.Pid, Name: name}
		if u, err := p.UsernameWithContext(ctx); err == nil {
			row.User = u
		}
		if cl, err := p.CmdlineWithContext(ctx); err == nil {
			row.Cmdline = trimMiddle(cl, 200)
		}
		if c, err := p.CPUPercentWithContext(ctx); err == nil {
			row.CPUPct = roundTo(c, 1)
		}
		if mi, err := p.MemoryInfoWithContext(ctx); err == nil && mi != nil {
			row.RSSBytes = mi.RSS
			if totalMem > 0 {
				row.MemPct = roundTo(float64(mi.RSS)/totalMem*100, 1)
			}
		}
		startedMs, _ := p.CreateTimeWithContext(ctx)
		key := procKey{pid: p.Pid, started: startedMs}
		seen[key] = struct{}{}

		var counters procCounters
		counters.at = now
		if io, err := p.IOCountersWithContext(ctx); err == nil && io != nil {
			counters.readBytes = io.ReadBytes
			counters.writeBytes = io.WriteBytes
		}
		// Net IO per-process is not portable through gopsutil; leave
		// the counters at zero so the delta calc returns zero too.
		readBps, writeBps, netSent, netRecv := procDelta.recordAndDelta(key, counters)
		row.DiskReadBps = readBps
		row.DiskWriteBps = writeBps
		row.NetSentBps = netSent
		row.NetRecvBps = netRecv

		rows = append(rows, row)
	}
	procDelta.reapMissing(seen)

	byCPU := append([]ProcessRow(nil), rows...)
	sort.Slice(byCPU, func(i, j int) bool { return byCPU[i].CPUPct > byCPU[j].CPUPct })
	if len(byCPU) > 10 {
		byCPU = byCPU[:10]
	}
	byMem := append([]ProcessRow(nil), rows...)
	sort.Slice(byMem, func(i, j int) bool { return byMem[i].RSSBytes > byMem[j].RSSBytes })
	if len(byMem) > 10 {
		byMem = byMem[:10]
	}
	s.TopByCPU = byCPU
	s.TopByMem = byMem
}

// applyOpenConns counts per-PID active sockets (any state, both
// listeners and conversations) and writes the count back into the
// TopBy* tables. Called after collectSockets so we can do this in a
// single pass — saves a second connections enumeration.
func (s *Snapshot) applyOpenConns(perPid map[int32]int) {
	for i := range s.TopByCPU {
		s.TopByCPU[i].OpenConns = perPid[s.TopByCPU[i].PID]
	}
	for i := range s.TopByMem {
		s.TopByMem[i].OpenConns = perPid[s.TopByMem[i].PID]
	}
}

// collectSockets enumerates TCP+UDP sockets via gopsutil/net in a
// single pass and produces two outputs from one expensive system call:
//
//   - Listeners — every "LISTEN" TCP socket plus every UDP socket
//     (UDP has no listen state; gopsutil reports "NONE"). This is the
//     "what is this host exposing" view.
//
//   - Conversations — every active connection to a non-loopback peer
//     in a meaningful state (ESTABLISHED / CLOSE_WAIT / FIN_WAIT*).
//     We aggregate by (proto, direction, remote_ip, remote_port,
//     process) so a process holding 50 sockets to one peer collapses
//     to one row with Count=50. Direction is inferred from whether
//     the local port matches one of our own listeners (inbound) or
//     not (outbound). TIME_WAIT and SYN_SENT are dropped — they're
//     largely noise for an operator looking at "what is this box
//     talking to right now".
//
// Reverse DNS is best-effort and bounded; see dnscache.go.
//
// The sockets-with-pid lookup needs admin on Windows and CAP_NET_RAW
// (or root) on Linux; the probe runs as LocalSystem / root, so this
// should always succeed.
func collectSockets(ctx context.Context, s *Snapshot) {
	conns, err := psnet.ConnectionsWithContext(ctx, "all")
	if err != nil {
		s.warn("net.Connections: " + err.Error())
		return
	}

	// Per-PID socket count — fed back into the Top* tables so each
	// process row can show "openConns" alongside CPU/RSS.
	pidConns := make(map[int32]int, 64)
	for _, c := range conns {
		if c.Pid == 0 {
			continue
		}
		pidConns[c.Pid]++
	}

	pidName := map[int32]string{}
	resolvePidName := func(pid int32) string {
		if pid == 0 {
			return ""
		}
		if name, ok := pidName[pid]; ok {
			return name
		}
		if p, err := process.NewProcessWithContext(ctx, pid); err == nil {
			if n, err := p.NameWithContext(ctx); err == nil {
				pidName[pid] = n
				return n
			}
		}
		pidName[pid] = ""
		return ""
	}

	// First pass: build listener list and a quick lookup of our own
	// listening (proto, port) pairs for direction inference.
	type listenKey struct {
		proto string
		port  uint32
	}
	ourListeners := map[listenKey]bool{}

	type convKey struct {
		proto       string
		direction   string
		remoteIP    string
		remotePort  uint32
		localPort   uint32 // only set for inbound, 0 for outbound
		processName string
	}
	convAgg := map[convKey]*Conversation{}

	for _, c := range conns {
		proto := "tcp"
		if c.Type == 2 { // SOCK_DGRAM
			proto = "udp"
		}
		state := strings.ToUpper(c.Status)

		// Listener rows: TCP LISTEN + UDP NONE (gopsutil idiom).
		if state == "LISTEN" || state == "NONE" {
			row := Listener{
				Proto:       proto,
				Address:     c.Laddr.IP,
				Port:        c.Laddr.Port,
				PID:         c.Pid,
				ProcessName: resolvePidName(c.Pid),
			}
			s.Listeners = append(s.Listeners, row)
			ourListeners[listenKey{proto: proto, port: c.Laddr.Port}] = true
			continue
		}

		// Conversation rows: active sockets only. TIME_WAIT and the
		// transient SYN states are excluded as noise.
		switch state {
		case "ESTABLISHED", "CLOSE_WAIT", "FIN_WAIT1", "FIN_WAIT2",
			"CLOSING", "LAST_ACK":
			// keep
		default:
			continue
		}
		if c.Raddr.IP == "" || c.Raddr.Port == 0 {
			continue
		}
		// Loopback peers are not actionable in a "talking to" view.
		if isLoopbackIP(c.Raddr.IP) {
			continue
		}

		direction := "outbound"
		var localPort uint32
		if ourListeners[listenKey{proto: proto, port: c.Laddr.Port}] {
			direction = "inbound"
			localPort = c.Laddr.Port
		}

		procName := resolvePidName(c.Pid)
		key := convKey{
			proto:       proto,
			direction:   direction,
			remoteIP:    c.Raddr.IP,
			remotePort:  c.Raddr.Port,
			localPort:   localPort,
			processName: procName,
		}
		if existing, ok := convAgg[key]; ok {
			existing.Count++
			// Promote to ESTABLISHED if we ever saw it; otherwise
			// keep the most-recent state we observed.
			if state == "ESTABLISHED" {
				existing.State = state
			}
			continue
		}
		convAgg[key] = &Conversation{
			Proto:       proto,
			Direction:   direction,
			RemoteIP:    c.Raddr.IP,
			RemotePort:  c.Raddr.Port,
			LocalPort:   localPort,
			State:       state,
			PID:         c.Pid,
			ProcessName: procName,
			Count:       1,
		}
	}

	sort.Slice(s.Listeners, func(i, j int) bool {
		if s.Listeners[i].Proto != s.Listeners[j].Proto {
			return s.Listeners[i].Proto < s.Listeners[j].Proto
		}
		return s.Listeners[i].Port < s.Listeners[j].Port
	})

	// Materialise + sort conversations: highest count first, then
	// remote port asc, then remote IP asc, so the "talking to"
	// table is stable across snapshots.
	convs := make([]Conversation, 0, len(convAgg))
	for _, v := range convAgg {
		convs = append(convs, *v)
	}
	sort.Slice(convs, func(i, j int) bool {
		if convs[i].Count != convs[j].Count {
			return convs[i].Count > convs[j].Count
		}
		if convs[i].RemotePort != convs[j].RemotePort {
			return convs[i].RemotePort < convs[j].RemotePort
		}
		return convs[i].RemoteIP < convs[j].RemoteIP
	})
	if len(convs) > 200 {
		convs = convs[:200]
	}

	// Reverse-DNS the unique remote IPs in this snapshot. Best-effort:
	// resolveBatch internally enforces total + per-lookup deadlines so
	// a hostile resolver can't stall the snapshot loop.
	if len(convs) > 0 {
		uniq := map[string]struct{}{}
		ips := make([]string, 0, len(convs))
		for _, c := range convs {
			if _, ok := uniq[c.RemoteIP]; ok {
				continue
			}
			uniq[c.RemoteIP] = struct{}{}
			ips = append(ips, c.RemoteIP)
		}
		names := resolveBatch(ctx, ips)
		for i := range convs {
			if n, ok := names[convs[i].RemoteIP]; ok {
				convs[i].RemoteHost = n
			}
		}
	}

	s.Conversations = convs
	s.applyOpenConns(pidConns)
}

// isLoopbackIP returns true for 127.0.0.0/8 and ::1. We strip these
// before aggregating conversations so the "talking to" view doesn't
// fill up with same-host RPC chatter.
func isLoopbackIP(ip string) bool {
	if ip == "" {
		return false
	}
	if strings.HasPrefix(ip, "127.") || ip == "::1" {
		return true
	}
	return false
}

func collectUsers(ctx context.Context, s *Snapshot) {
	// Windows: gopsutil's host.Users is not implemented (returns
	// "not implemented" on every call). We dispatch to a real
	// implementation in sessions_windows.go that enumerates
	// terminal-services sessions via WTSEnumerateSessionsEx.
	// Linux + macOS: gopsutil works fine, keep using it.
	if rows, ok := collectSessionsOS(ctx); ok {
		s.LoggedInUsers = append(s.LoggedInUsers, rows...)
		return
	}
	us, err := host.UsersWithContext(ctx)
	if err != nil {
		if !strings.Contains(err.Error(), "not implemented") {
			s.warn("host.Users: " + err.Error())
		}
		return
	}
	for _, u := range us {
		s.LoggedInUsers = append(s.LoggedInUsers, SessionRow{
			User:    u.User,
			Tty:     u.Terminal,
			Host:    u.Host,
			Started: time.Unix(int64(u.Started), 0).UTC().Format(time.RFC3339),
		})
	}
}

// trimMiddle keeps the head and tail of a long string with an ellipsis
// in the middle. Useful for command lines that are mostly noise in the
// middle (long classpaths, etc.).
func trimMiddle(s string, max int) string {
	if len(s) <= max {
		return s
	}
	keep := (max - 3) / 2
	return s[:keep] + "..." + s[len(s)-keep:]
}

func roundTo(v float64, decimals int) float64 {
	mult := 1.0
	for i := 0; i < decimals; i++ {
		mult *= 10
	}
	return float64(int64(v*mult+0.5)) / mult
}
