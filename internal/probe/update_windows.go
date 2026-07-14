//go:build windows

package probe

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// applyUpdate schedules a detached PowerShell helper that waits for
// this PID to exit, replaces the binary, and starts SonarProbe again.
func applyUpdate(exe, staging string) error {
	pid := os.Getpid()
	script := fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
$exe = %s
$new = %s
$pidWait = %d
$deadline = (Get-Date).AddSeconds(90)
while ((Get-Process -Id $pidWait -ErrorAction SilentlyContinue) -and ((Get-Date) -lt $deadline)) {
  Start-Sleep -Milliseconds 400
}
Stop-Service -Name SonarProbe -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 1
Move-Item -LiteralPath $new -Destination $exe -Force
Start-Service -Name SonarProbe
`, psQuote(exe), psQuote(staging), pid)

	tmp := filepathJoinTemp("sonar-probe-update.ps1")
	if err := os.WriteFile(tmp, []byte(script), 0o644); err != nil {
		return err
	}
	cmd := exec.Command("powershell.exe",
		"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", tmp)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008} // DETACHED_PROCESS
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("spawn updater: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}

func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func filepathJoinTemp(name string) string {
	return os.TempDir() + string(os.PathSeparator) + name + "." + strconv.Itoa(os.Getpid())
}
