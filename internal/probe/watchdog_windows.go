//go:build windows

package probe

import (
	"log/slog"
	"os/exec"
	"strings"
)

const watchdogTaskName = `SonarProbeWatchdog`

// EnsureServiceWatchdog registers a SYSTEM scheduled task that starts
// SonarProbe if it is not Running. Covers the case where a clean
// self-update stop leaves the service down without a host reboot
// (SCM failure actions do not fire on exit code 0).
func EnsureServiceWatchdog(log *slog.Logger) {
	tr := `powershell.exe -NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -Command "if ((Get-Service -Name SonarProbe -ErrorAction SilentlyContinue).Status -ne 'Running') { Start-Service -Name SonarProbe -ErrorAction SilentlyContinue }"`
	cmd := exec.Command("schtasks.exe",
		"/Create", "/TN", watchdogTaskName,
		"/SC", "MINUTE", "/MO", "5",
		"/RU", "SYSTEM", "/RL", "HIGHEST", "/F",
		"/TR", tr,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Warn("probe watchdog ensure failed",
			"err", err, "out", strings.TrimSpace(string(out)))
		return
	}
	log.Info("probe watchdog ensured", "task", watchdogTaskName, "interval_min", 5)
}
