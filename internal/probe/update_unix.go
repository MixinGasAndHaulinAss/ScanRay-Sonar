//go:build !windows

package probe

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// applyUpdate renames the running binary aside and moves the staging
// file into place. systemd (Restart=always) brings us back.
func applyUpdate(exe, staging string) error {
	bak := exe + ".bak"
	_ = os.Remove(bak)
	if err := os.Rename(exe, bak); err != nil {
		// Some filesystems refuse rename of a running executable; fall
		// back to a helper script.
		return spawnUnixUpdater(exe, staging)
	}
	if err := os.Rename(staging, exe); err != nil {
		_ = os.Rename(bak, exe)
		return err
	}
	_ = os.Chmod(exe, 0o755)
	return nil
}

func spawnUnixUpdater(exe, staging string) error {
	script := fmt.Sprintf(`#!/bin/sh
set -eu
pid=%d
exe=%q
new=%q
i=0
while kill -0 "$pid" 2>/dev/null && [ "$i" -lt 90 ]; do sleep 1; i=$((i+1)); done
mv -f "$new" "$exe"
chmod 755 "$exe"
systemctl restart sonar-probe.service 2>/dev/null || true
`, os.Getpid(), exe, staging)
	tmp, err := os.CreateTemp("", "sonar-probe-update-*.sh")
	if err != nil {
		return err
	}
	path := tmp.Name()
	if _, err := tmp.WriteString(script); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	_ = os.Chmod(path, 0o700)
	cmd := exec.Command("/bin/sh", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn updater: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}
