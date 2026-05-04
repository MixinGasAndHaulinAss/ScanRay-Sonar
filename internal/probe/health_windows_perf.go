//go:build windows

package probe

import (
	"context"
	"encoding/csv"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// winRunTypeperf snapshots two Windows Performance Counters that the
// dashboards call "Processor Queue Length" and "Total Disk Queue
// Length". typeperf is a built-in Windows utility (not part of the
// optional Performance Logs and Alerts feature) that emits one CSV
// sample and exits. We collect both counters in a single invocation
// so we pay the process-spawn cost once.
//
// Output layout (typeperf -sc 1 -y):
//
//	"(PDH-CSV 4.0)","\\<host>\System\Processor Queue Length","\\<host>\PhysicalDisk(_Total)\Current Disk Queue Length"
//	"05/03/2026 22:30:00.000","2.000000","0.000000"
//
// Two header lines, then one data line. typeperf doesn't return
// non-zero on counter-not-found; that path emits an error message
// to stderr and a malformed CSV — we just bail.
func winRunTypeperf(ctx context.Context, h *HealthSignals) {
	cmd := exec.CommandContext(ctx,
		"typeperf.exe",
		`\System\Processor Queue Length`,
		`\PhysicalDisk(_Total)\Current Disk Queue Length`,
		"-sc", "1",
		"-y", // suppress the "Press Ctrl-C to end" prompt
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return
	}
	r := csv.NewReader(strings.NewReader(string(out)))
	rows, err := r.ReadAll()
	if err != nil || len(rows) < 2 {
		return
	}
	// Find the row that has a timestamp in column 0 and numeric
	// values in 1..n. The PDH header line starts with "(PDH-CSV";
	// data rows start with quoted timestamps.
	for _, row := range rows {
		if len(row) < 3 {
			continue
		}
		if strings.HasPrefix(row[0], "(PDH-CSV") {
			continue
		}
		// Some typeperf builds emit a banner row "Output written to"
		// at the end; skip non-numeric data rows.
		cpuQ, err1 := strconv.ParseFloat(strings.TrimSpace(row[1]), 64)
		diskQ, err2 := strconv.ParseFloat(strings.TrimSpace(row[2]), 64)
		if err1 != nil || err2 != nil {
			continue
		}
		v1 := round1(cpuQ)
		v2 := round1(diskQ)
		h.CPUQueueLength = &v1
		h.DiskQueueLength = &v2
		return
	}
}
