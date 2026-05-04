//go:build linux

package probe

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CollectHealthSignals is the Linux entry point. Battery comes from
// /sys/class/power_supply, WiFi from /proc/net/wireless, queue
// lengths from /proc/loadavg + /proc/diskstats, and the Windows-only
// signals (BSOD, missing patches, app crashes from the Application
// log) stay nil — there's no equivalent surface.
func CollectHealthSignals(ctx context.Context) *HealthSignals {
	h := &HealthSignals{}
	linuxBattery(ctx, h)
	linuxQueueLengths(ctx, h)
	linuxWiFi(ctx, h)
	return h
}

// linuxBattery walks /sys/class/power_supply/BAT* and computes
// "battery health" as energy_full / energy_full_design × 100. This is
// the wear figure the Devices-Performance dashboard wants — current
// charge level (capacity) is irrelevant for "is this battery wearing
// out?".
//
// Hosts without a battery (desktops, servers, VMs) return nil and
// the field stays nil on the wire.
func linuxBattery(_ context.Context, h *HealthSignals) {
	matches, err := filepath.Glob("/sys/class/power_supply/BAT*")
	if err != nil || len(matches) == 0 {
		return
	}
	for _, dir := range matches {
		full := readUintFile(filepath.Join(dir, "energy_full"))
		design := readUintFile(filepath.Join(dir, "energy_full_design"))
		if full == 0 || design == 0 {
			// Some kernels expose charge_full/charge_full_design instead
			// of energy_*. Try those.
			full = readUintFile(filepath.Join(dir, "charge_full"))
			design = readUintFile(filepath.Join(dir, "charge_full_design"))
		}
		if full == 0 || design == 0 {
			continue
		}
		pct := float64(full) / float64(design) * 100
		if pct > 100 {
			pct = 100 // freshly-calibrated batteries can read >100%; clamp.
		}
		h.BatteryHealthPct = float64Ptr(round1(pct))
		return
	}
}

// linuxQueueLengths reads CPU run-queue depth from /proc/loadavg
// (load1 is roughly the 1-minute average runnable+running process
// count; close enough to the Windows "Processor Queue Length" the
// dashboard expects) and disk queue depth as the sum of column 12
// of /proc/diskstats (in-flight requests across all physical disks).
func linuxQueueLengths(_ context.Context, h *HealthSignals) {
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 1 {
			if v, err := strconv.ParseFloat(fields[0], 64); err == nil {
				h.CPUQueueLength = float64Ptr(round1(v))
			}
		}
	}
	if data, err := os.ReadFile("/proc/diskstats"); err == nil {
		var sum float64
		scan := bufio.NewScanner(strings.NewReader(string(data)))
		for scan.Scan() {
			fields := strings.Fields(scan.Text())
			// /proc/diskstats columns (1-indexed):
			//   1=major 2=minor 3=name 4..14= read/write/IO stats
			//   12 = ios_in_flight (current in-flight count)
			if len(fields) < 12 {
				continue
			}
			// Skip partitions (we want whole disks only). Heuristic:
			// names ending in a digit are partitions on standard
			// /dev/sd?N, /dev/nvme?n?p? layouts. Imperfect but cheap.
			name := fields[2]
			if len(name) > 0 && name[len(name)-1] >= '0' && name[len(name)-1] <= '9' &&
				!strings.HasPrefix(name, "nvme") {
				continue
			}
			if v, err := strconv.ParseFloat(fields[11], 64); err == nil {
				sum += v
			}
		}
		h.DiskQueueLength = float64Ptr(round1(sum))
	}
}

// linuxWiFi parses /proc/net/wireless. Format is two header lines
// followed by per-interface rows. Column 3 is "link" (signal quality
// 0–100), column 4 is "level" (signal strength in dBm or 0–255 byte
// depending on driver). We pick the first interface with a non-zero
// link.
//
// The SSID isn't in /proc — we'd need iw or wpa_cli for that. The
// dashboards prefer signal strength anyway, so SSID stays "".
func linuxWiFi(_ context.Context, h *HealthSignals) {
	data, err := os.ReadFile("/proc/net/wireless")
	if err != nil {
		return
	}
	scan := bufio.NewScanner(strings.NewReader(string(data)))
	header := 2
	for scan.Scan() {
		if header > 0 {
			header--
			continue
		}
		line := scan.Text()
		// Each row: "  wlp4s0: 0000   70.  -40.  -256 ..."
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		fields := strings.Fields(line[colon+1:])
		if len(fields) < 3 {
			continue
		}
		linkStr := strings.TrimRight(fields[1], ".")
		levelStr := strings.TrimRight(fields[2], ".")

		if v, err := strconv.ParseFloat(linkStr, 64); err == nil && v > 0 {
			pct := int(v + 0.5)
			if pct > 100 {
				pct = 100
			}
			h.WiFiSignalPct = intPtr(pct)
		}
		if v, err := strconv.ParseFloat(levelStr, 64); err == nil {
			// Drivers report level either as dBm (negative integer
			// like -40) or as a 0–255 byte. Treat anything <=0 as
			// dBm, otherwise scale to dBm by subtracting 256.
			dbm := int(v)
			if dbm > 0 {
				dbm = dbm - 256
			}
			if dbm < 0 {
				h.WiFiRSSIdBm = intPtr(dbm)
			}
		}
		return
	}
}

// readUintFile reads a /sys file expected to contain a single integer.
// Returns 0 on any failure.
func readUintFile(path string) uint64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func intPtr(v int) *int             { return &v }
func float64Ptr(v float64) *float64 { return &v }
