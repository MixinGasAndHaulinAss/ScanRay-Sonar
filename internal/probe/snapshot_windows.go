//go:build windows

package probe

import (
	"context"
	"strings"

	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc/mgr"
)

// collectOSExtras populates Windows-specific Snapshot fields:
//   - PendingReboot / PendingRebootReason from the canonical registry
//     keys that Windows sets when patches/installs need a restart.
//   - StoppedAutoServices: services configured "Automatic" but not
//     currently Running — these are the ones operators actually want
//     to see ("oh, the print spooler is supposed to be running but
//     isn't"). We deliberately ignore "Automatic (Delayed Start)"
//     services that haven't reached their start time yet by also
//     filtering on services that report stopped > a heuristic window;
//     keeping it simple here, we just list stopped autos and let the
//     UI badge it.
func collectOSExtras(ctx context.Context, s *Snapshot) {
	if reason := windowsPendingRebootReason(); reason != "" {
		s.PendingReboot = true
		s.PendingRebootReason = reason
	}
	s.StoppedAutoServices = enumerateStoppedAutoServices(s)
}

// windowsPendingRebootReason returns a non-empty human-readable string
// when Windows has flagged a pending restart, or "" otherwise. Sources
// (any one is sufficient):
//
//   - HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Component Based
//     Servicing\RebootPending  — set by CBS / WUA after servicing.
//   - HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\WindowsUpdate\
//     Auto Update\RebootRequired — set by Windows Update.
//   - HKLM\SYSTEM\CurrentControlSet\Control\Session Manager,
//     PendingFileRenameOperations value — set by anything that needs
//     to replace a locked file at next boot (MSI, in-place upgrades).
func windowsPendingRebootReason() string {
	check := func(root registry.Key, path string, value string, isKey bool) (bool, error) {
		k, err := registry.OpenKey(root, path, registry.READ)
		if err != nil {
			if err == registry.ErrNotExist {
				return false, nil
			}
			return false, err
		}
		defer k.Close()
		if isKey {
			return true, nil
		}
		if _, _, err := k.GetStringsValue(value); err == nil {
			return true, nil
		}
		// Some PendingFileRenameOperations payloads come back as MULTI_SZ
		// where the first string is empty; treat the value's existence as
		// truth.
		if _, _, err := k.GetStringValue(value); err == nil {
			return true, nil
		}
		return false, nil
	}

	if hit, _ := check(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows\CurrentVersion\Component Based Servicing\RebootPending`,
		"", true); hit {
		return "Component Based Servicing reports a pending reboot."
	}
	if hit, _ := check(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows\CurrentVersion\WindowsUpdate\Auto Update\RebootRequired`,
		"", true); hit {
		return "Windows Update has installed updates that require a reboot."
	}
	if hit, _ := check(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\Session Manager`,
		"PendingFileRenameOperations", false); hit {
		return "Pending file rename operations are queued for next boot."
	}
	return ""
}

// enumerateStoppedAutoServices lists every Windows service whose start
// type is "Automatic" but whose current state isn't "Running". These
// are the high-signal services for an operator: something that should
// be up isn't.
func enumerateStoppedAutoServices(s *Snapshot) []ServiceRow {
	m, err := mgr.Connect()
	if err != nil {
		s.warn("svc.mgr.Connect: " + err.Error())
		return nil
	}
	defer m.Disconnect()

	names, err := m.ListServices()
	if err != nil {
		s.warn("svc.mgr.ListServices: " + err.Error())
		return nil
	}

	var out []ServiceRow
	for _, name := range names {
		svc, err := m.OpenService(name)
		if err != nil {
			continue
		}
		cfg, errCfg := svc.Config()
		status, errStat := svc.Query()
		svc.Close()
		if errCfg != nil || errStat != nil {
			continue
		}
		// StartType: 2 = SERVICE_AUTO_START. Anything else
		// (manual/disabled/boot/system) is operator-controlled and not
		// noteworthy when stopped.
		if cfg.StartType != 2 {
			continue
		}
		// State: 4 = SERVICE_RUNNING. Skip the happy path.
		if status.State == 4 {
			continue
		}
		// Skip the "trigger-start" services that are auto but the SCM
		// only starts on demand (their DelayedAutoStart flag may also
		// be set). These show up as auto+stopped indefinitely and
		// produce false alarms — heuristic: if delayed-auto-start is
		// true, leave it alone unless the service is still stopped 5
		// minutes after boot, which we can't easily know here. Keep
		// it simple: skip delayed-auto-start.
		if cfg.DelayedAutoStart {
			continue
		}
		out = append(out, ServiceRow{
			Name:        name,
			DisplayName: cfg.DisplayName,
			StartType:   "auto",
			Status:      strings.ToLower(svcStateName(status.State)),
		})
	}
	return out
}

// svcStateName maps SERVICE_STATUS State numbers to readable names.
func svcStateName(state uint32) string {
	switch state {
	case 1:
		return "stopped"
	case 2:
		return "start_pending"
	case 3:
		return "stop_pending"
	case 4:
		return "running"
	case 5:
		return "continue_pending"
	case 6:
		return "pause_pending"
	case 7:
		return "paused"
	default:
		return "unknown"
	}
}
