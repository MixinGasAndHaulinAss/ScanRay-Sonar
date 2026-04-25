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
	CPU                 CPU          `json:"cpu"`
	Memory              Memory       `json:"memory"`
	LoadAvg             *LoadAvg     `json:"loadAvg,omitempty"` // linux/macos only
	Disks               []Disk       `json:"disks"`
	NICs                []NIC        `json:"nics"`
	TopByCPU            []ProcessRow `json:"topByCpu"`
	TopByMem            []ProcessRow `json:"topByMem"`
	Listeners           []Listener   `json:"listeners"`
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
	Name       string   `json:"name"`
	MAC        string   `json:"mac,omitempty"`
	MTU        int      `json:"mtu,omitempty"`
	Up         bool     `json:"up"`
	Addresses  []string `json:"addresses,omitempty"`
	BytesSent  uint64   `json:"bytesSent"`
	BytesRecv  uint64   `json:"bytesRecv"`
	PktsSent   uint64   `json:"pktsSent"`
	PktsRecv   uint64   `json:"pktsRecv"`
	ErrIn      uint64   `json:"errIn"`
	ErrOut     uint64   `json:"errOut"`
	DropIn     uint64   `json:"dropIn"`
	DropOut    uint64   `json:"dropOut"`
}

type ProcessRow struct {
	PID      int32   `json:"pid"`
	Name     string  `json:"name"`
	User     string  `json:"user,omitempty"`
	Cmdline  string  `json:"cmdline,omitempty"`
	CPUPct   float64 `json:"cpuPct"`
	RSSBytes uint64  `json:"rssBytes"`
}

type Listener struct {
	Proto       string `json:"proto"` // tcp, udp
	Address     string `json:"address"`
	Port        uint32 `json:"port"`
	PID         int32  `json:"pid,omitempty"`
	ProcessName string `json:"processName,omitempty"`
}

type SessionRow struct {
	User    string `json:"user"`
	Tty     string `json:"tty,omitempty"`
	Host    string `json:"host,omitempty"`
	Started string `json:"started,omitempty"` // RFC3339
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
		SchemaVersion: 1,
		CapturedAt:    start.UTC().Format(time.RFC3339Nano),
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

	collectCPU(ctx, &s)
	collectMemory(ctx, &s)
	collectLoad(ctx, &s)
	collectDisks(ctx, &s)
	collectNICs(ctx, &s)
	collectProcesses(ctx, &s)
	collectListeners(ctx, &s)
	collectUsers(ctx, &s)
	collectOSExtras(ctx, &s) // pending reboot + per-OS service/unit lists

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
		}
		s.NICs = append(s.NICs, nic)
	}
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
// long tail is rarely actionable. CPU% comes from gopsutil's internal
// delta tracking, which needs at least one prior call to be non-zero —
// so the first snapshot after probe start will show 0% for everything.
func collectProcesses(ctx context.Context, s *Snapshot) {
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		s.warn("process.Processes: " + err.Error())
		return
	}
	rows := make([]ProcessRow, 0, len(procs))
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
		}
		rows = append(rows, row)
	}
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

// collectListeners enumerates listening TCP+UDP sockets via
// gopsutil/net. The sockets-with-pid lookup needs admin on Windows and
// CAP_NET_RAW (or root) on Linux; the probe runs as LocalSystem /
// root respectively, so this should always succeed.
func collectListeners(ctx context.Context, s *Snapshot) {
	conns, err := psnet.ConnectionsWithContext(ctx, "all")
	if err != nil {
		s.warn("net.Connections: " + err.Error())
		return
	}
	pidName := map[int32]string{}
	for _, c := range conns {
		// We only care about listening sockets here; "established" /
		// "time_wait" are noise for an inventory view. UDP doesn't
		// have a listen state — gopsutil reports "NONE".
		switch strings.ToUpper(c.Status) {
		case "LISTEN", "NONE":
		default:
			continue
		}
		proto := "tcp"
		if c.Type == 2 { // SOCK_DGRAM
			proto = "udp"
		}
		row := Listener{
			Proto:   proto,
			Address: c.Laddr.IP,
			Port:    c.Laddr.Port,
			PID:     c.Pid,
		}
		if c.Pid != 0 {
			if name, ok := pidName[c.Pid]; ok {
				row.ProcessName = name
			} else if p, err := process.NewProcessWithContext(ctx, c.Pid); err == nil {
				if n, err := p.NameWithContext(ctx); err == nil {
					row.ProcessName = n
					pidName[c.Pid] = n
				}
			}
		}
		s.Listeners = append(s.Listeners, row)
	}
	sort.Slice(s.Listeners, func(i, j int) bool {
		if s.Listeners[i].Proto != s.Listeners[j].Proto {
			return s.Listeners[i].Proto < s.Listeners[j].Proto
		}
		return s.Listeners[i].Port < s.Listeners[j].Port
	})
}

func collectUsers(ctx context.Context, s *Snapshot) {
	us, err := host.UsersWithContext(ctx)
	if err != nil {
		// Windows often returns "not implemented" here; not a real
		// problem, just don't surface a warning for the common case.
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
