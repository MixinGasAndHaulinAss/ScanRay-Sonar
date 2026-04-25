//go:build linux

package probe

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// collectOSExtras populates Linux-specific Snapshot fields: pending
// reboot detection (Debian/Ubuntu and RHEL idioms) and the list of
// failed systemd units.
func collectOSExtras(_ context.Context, s *Snapshot) {
	if reason := linuxPendingRebootReason(); reason != "" {
		s.PendingReboot = true
		s.PendingRebootReason = reason
	}
	s.FailedUnits = listFailedSystemdUnits()
}

// linuxPendingRebootReason returns a non-empty string when one of the
// well-known marker files is present:
//
//   - /var/run/reboot-required (Debian/Ubuntu unattended-upgrades and
//     update-notifier set this after kernel/glibc updates).
//   - /var/run/reboot-required.pkgs lists which packages triggered it,
//     which we surface in the reason if available.
//   - /run/reboot-needed (some derivatives use this path).
//
// We deliberately do NOT shell out to `needs-restarting -r` (RHEL) here
// to keep the probe dependency-free; the marker files cover ~90% of
// the common cases.
func linuxPendingRebootReason() string {
	for _, p := range []string{"/var/run/reboot-required", "/run/reboot-required", "/run/reboot-needed"} {
		if _, err := os.Stat(p); err == nil {
			if pkgs, err := os.ReadFile(p + ".pkgs"); err == nil && len(pkgs) > 0 {
				short := strings.ReplaceAll(strings.TrimSpace(string(pkgs)), "\n", ", ")
				if len(short) > 200 {
					short = short[:200] + "..."
				}
				return "Reboot required for: " + short
			}
			return "Reboot required (marker file " + p + " is present)."
		}
	}
	return ""
}

// listFailedSystemdUnits parses `systemctl --failed --plain --no-legend
// --no-pager`. Returns nil if systemctl isn't available (containers
// without an init system, etc.).
func listFailedSystemdUnits() []string {
	out, err := exec.Command("systemctl", "--failed", "--plain", "--no-legend", "--no-pager").Output()
	if err != nil {
		return nil
	}
	var units []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 {
			units = append(units, fields[0])
		}
	}
	return units
}
