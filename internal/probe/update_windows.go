//go:build windows

package probe

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Windows CreateProcess flags. DETACHED_PROCESS alone is not enough when
// sonar-probe runs as a service: SCM places the process in a job object
// and kills children on exit. CREATE_BREAKAWAY_FROM_JOB lets the updater
// outlive the service long enough to swap the binary and Start-Service.
const (
	createDetachedProcess  = 0x00000008
	createNewProcessGroup  = 0x00000200
	createBreakawayFromJob = 0x01000000
	createNoWindow         = 0x08000000
	updaterCreationFlags   = createDetachedProcess | createNewProcessGroup | createBreakawayFromJob | createNoWindow
)

// applyUpdate schedules a detached PowerShell helper that waits for
// this PID to exit, replaces the binary, and starts SonarProbe again.
// The helper must outlive the service process (job breakaway); SCM
// failure actions do not fire on a clean os.Exit(0).
func applyUpdate(exe, staging string) error {
	pid := os.Getpid()
	dataDir := filepath.Join(os.Getenv("ProgramData"), "Sonar")
	_ = os.MkdirAll(dataDir, 0o755)
	logPath := filepath.Join(dataDir, "update.log")
	scriptPath := filepath.Join(dataDir, "update-"+strconv.Itoa(pid)+".ps1")

	script := fmt.Sprintf(`
$ErrorActionPreference = 'Continue'
$exe = %s
$new = %s
$pidWait = %d
$log = %s
function Write-UpdateLog([string]$msg) {
  $line = '{0:u} {1}' -f (Get-Date).ToUniversalTime(), $msg
  try { Add-Content -LiteralPath $log -Value $line -ErrorAction SilentlyContinue } catch {}
}
Write-UpdateLog ("updater start pidWait=" + $pidWait + " exe=" + $exe + " new=" + $new)
$deadline = (Get-Date).AddSeconds(120)
while ((Get-Process -Id $pidWait -ErrorAction SilentlyContinue) -and ((Get-Date) -lt $deadline)) {
  Start-Sleep -Milliseconds 400
}
Write-UpdateLog 'target process exited (or wait timed out)'
Stop-Service -Name SonarProbe -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 2
$moved = $false
if (-not (Test-Path -LiteralPath $new)) {
  Write-UpdateLog 'staging file missing; assuming binary already replaced'
  $moved = $true
} else {
  for ($i = 0; $i -lt 40; $i++) {
    try {
      Move-Item -LiteralPath $new -Destination $exe -Force -ErrorAction Stop
      Write-UpdateLog ('moved staging into place on attempt ' + ($i + 1))
      $moved = $true
      break
    } catch {
      Write-UpdateLog ('move attempt ' + ($i + 1) + ' failed: ' + $_)
      Start-Sleep -Seconds 1
    }
  }
}
if (-not $moved) {
  Write-UpdateLog 'FAILED to replace binary; attempting Start-Service anyway'
}
for ($i = 0; $i -lt 30; $i++) {
  try {
    Start-Service -Name SonarProbe -ErrorAction Stop
  } catch {
    Write-UpdateLog ('Start-Service attempt ' + ($i + 1) + ': ' + $_)
  }
  $st = $null
  try { $st = (Get-Service -Name SonarProbe -ErrorAction SilentlyContinue).Status } catch {}
  Write-UpdateLog ('service status after attempt ' + ($i + 1) + ': ' + $st)
  if ($st -eq 'Running') { break }
  Start-Sleep -Seconds 2
}
# Defense in depth: keep the periodic watchdog registered.
try {
  $taskName = 'SonarProbeWatchdog'
  $tr = 'powershell.exe -NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -Command "if ((Get-Service -Name SonarProbe -ErrorAction SilentlyContinue).Status -ne ''Running'') { Start-Service -Name SonarProbe -ErrorAction SilentlyContinue }"'
  & schtasks.exe /Create /TN $taskName /SC MINUTE /MO 5 /RU SYSTEM /RL HIGHEST /F /TR $tr | Out-Null
  Write-UpdateLog 'watchdog task ensured'
} catch {
  Write-UpdateLog ('watchdog ensure failed: ' + $_)
}
Write-UpdateLog 'updater done'
Remove-Item -LiteralPath $MyInvocation.MyCommand.Path -Force -ErrorAction SilentlyContinue
`, psQuote(exe), psQuote(staging), pid, psQuote(logPath))

	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		return err
	}
	if err := spawnWindowsUpdater(scriptPath); err != nil {
		_ = os.Remove(scriptPath)
		return err
	}
	return nil
}

func spawnWindowsUpdater(scriptPath string) error {
	// Prefer a SYSTEM one-shot scheduled task: it is never a child of
	// the service job, so it survives os.Exit(0). Fall back to
	// CREATE_BREAKAWAY_FROM_JOB CreateProcess when task registration
	// is blocked by policy.
	if err := scheduleUpdaterTask(scriptPath); err == nil {
		return nil
	} else if err2 := spawnBreakawayUpdater(scriptPath); err2 == nil {
		return nil
	} else {
		return fmt.Errorf("spawn updater: scheduled task: %v; breakaway: %w", err, err2)
	}
}

func spawnBreakawayUpdater(scriptPath string) error {
	cmd := exec.Command("powershell.exe",
		"-NoProfile", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-File", scriptPath)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: updaterCreationFlags}
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	return nil
}

// scheduleUpdaterTask runs the updater via a one-shot scheduled task so
// it is not a child of the service job at all. Used when CreateProcess
// breakaway flags are rejected by the host job policy.
func scheduleUpdaterTask(scriptPath string) error {
	// Register + run from a short PowerShell snippet so we avoid
	// locale-sensitive schtasks /ST /SD parsing.
	ps := fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
$name = 'SonarProbeUpdateOnce'
$arg = '-NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File %s'
$action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument $arg
$trigger = New-ScheduledTaskTrigger -Once -At ((Get-Date).AddSeconds(3))
Register-ScheduledTask -TaskName $name -Action $action -Trigger $trigger -User 'SYSTEM' -RunLevel Highest -Force | Out-Null
Start-ScheduledTask -TaskName $name
`, psQuote(scriptPath))
	cmd := exec.Command("powershell.exe",
		"-NoProfile", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-Command", ps)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("spawn updater (scheduled task): %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
