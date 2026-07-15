//go:build windows

package probe

import (
	"context"
	"encoding/json"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// winDEXScript collects installed apps, Win11 readiness, recent
// event-log rows, power events, and extensions first (fast path), then
// missing patch titles via Windows Update COM last — with a job
// timeout so a hung Search cannot drop the fast inventory (JSON is
// only emitted at the end of the script).
const winDEXScript = `
$out = [ordered]@{
  installedApps = @()
  installedExtensions = @()
  missingPatches = @()
  eventLog = @()
  powerEvents = @()
}
$warnings = @()

# Installed applications from Uninstall registry keys (x64 + WOW6432).
try {
  $paths = @(
    'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*',
    'HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*'
  )
  $apps = foreach ($p in $paths) {
    Get-ItemProperty $p -ErrorAction SilentlyContinue |
      Where-Object { $_.DisplayName -and -not $_.SystemComponent -and $_.DisplayName -notmatch '^Update for|^Security Update' } |
      Select-Object DisplayName, DisplayVersion, Publisher, InstallDate, InstallLocation
  }
  $seen = @{}
  foreach ($a in ($apps | Sort-Object DisplayName)) {
    $n = [string]$a.DisplayName
    if ($seen.ContainsKey($n)) { continue }
    $seen[$n] = $true
    $out.installedApps += [ordered]@{
      name = $n
      version = [string]$a.DisplayVersion
      publisher = [string]$a.Publisher
      installDate = [string]$a.InstallDate
      installLocation = [string]$a.InstallLocation
    }
    if ($out.installedApps.Count -ge 200) { break }
  }
} catch {}

# Win11 readiness — lightweight checks (not the full PC Health Check app).
try {
  $win11 = [ordered]@{}
  $os = Get-CimInstance Win32_OperatingSystem -ErrorAction Stop
  $cs = Get-CimInstance Win32_ComputerSystem -ErrorAction SilentlyContinue
  $ramOk = $false
  if ($cs -and $cs.TotalPhysicalMemory -ge 4GB) { $ramOk = $true }
  $win11.ramOk = $ramOk
  $diskOk = $false
  $sys = Get-CimInstance Win32_LogicalDisk -Filter "DeviceID='C:'" -ErrorAction SilentlyContinue
  if ($sys -and $sys.Size -ge 64GB) { $diskOk = $true }
  $win11.storageOk = $diskOk
  $tpmOk = $false
  try {
    $tpm = Get-CimInstance -Namespace root\cimv2\Security\MicrosoftTpm -ClassName Win32_Tpm -ErrorAction Stop
    if ($tpm -and $tpm.IsEnabled_InitialValue) { $tpmOk = $true }
  } catch {}
  $win11.tpmReady = $tpmOk
  $sbOk = $false
  try {
    $sb = Confirm-SecureBootUEFI -ErrorAction Stop
    $sbOk = [bool]$sb
  } catch {}
  $win11.secureBoot = $sbOk
  $cpuOk = $true
  $win11.cpuOk = $cpuOk
  $eligible = $ramOk -and $diskOk -and $tpmOk
  $win11.eligible = $eligible
  if (-not $eligible) {
    $reasons = @()
    if (-not $ramOk) { $reasons += 'RAM < 4GB' }
    if (-not $diskOk) { $reasons += 'System disk < 64GB' }
    if (-not $tpmOk) { $reasons += 'TPM not ready' }
    $win11.reason = ($reasons -join '; ')
  } else {
    $win11.reason = 'Meets basic Win11 hardware checks'
  }
  # Already on Win11?
  if ($os.Caption -match 'Windows 11') {
    $win11.eligible = $true
    $win11.reason = 'Already running Windows 11'
  }
  $out.win11Readiness = $win11
} catch {}

# Recent Application + System errors/warnings (last 50).
try {
  $since = (Get-Date).AddHours(-24)
  $evs = Get-WinEvent -FilterHashtable @{
    LogName = 'Application','System'
    Level = 2,3
    StartTime = $since
  } -MaxEvents 50 -ErrorAction SilentlyContinue
  foreach ($e in @($evs)) {
    $lvl = switch ($e.Level) { 1 {'Critical'} 2 {'Error'} 3 {'Warning'} default {'Info'} }
    $msg = ''
    try { $msg = ($e.Message -replace '\s+',' ').Substring(0, [Math]::Min(240, ($e.Message -replace '\s+',' ').Length)) } catch { $msg = $e.Id.ToString() }
    $out.eventLog += [ordered]@{
      time = $e.TimeCreated.ToUniversalTime().ToString('o')
      log = [string]$e.LogName
      level = $lvl
      provider = [string]$e.ProviderName
      eventId = [int]$e.Id
      message = $msg
    }
  }
} catch {}

# Power / reboot related System events.
try {
  $since = (Get-Date).AddDays(-7)
  $ids = 1,12,13,41,42,107,1074,6006,6008,6005
  $evs = Get-WinEvent -FilterHashtable @{
    LogName = 'System'
    Id = $ids
    StartTime = $since
  } -MaxEvents 50 -ErrorAction SilentlyContinue
  foreach ($e in @($evs)) {
    $kind = 'other'
    switch ($e.Id) {
      42 { $kind = 'sleep' }
      1 { $kind = 'wake' }
      107 { $kind = 'wake' }
      13 { $kind = 'shutdown' }
      1074 { $kind = 'reboot' }
      6006 { $kind = 'shutdown' }
      6005 { $kind = 'wake' }
      6008 { $kind = 'reboot' }
      41 { $kind = 'reboot' }
      12 { $kind = 'wake' }
    }
    $msg = ''
    try { $msg = ($e.Message -replace '\s+',' ').Substring(0, [Math]::Min(200, ($e.Message -replace '\s+',' ').Length)) } catch {}
    $out.powerEvents += [ordered]@{
      time = $e.TimeCreated.ToUniversalTime().ToString('o')
      kind = $kind
      eventId = [int]$e.Id
      message = $msg
    }
  }
} catch {}

# Chrome / Edge extension inventory (best-effort, first profile).
try {
  $extRoots = @(
    @{ browser = 'Chrome'; path = "$env:LOCALAPPDATA\Google\Chrome\User Data\Default\Extensions" },
    @{ browser = 'Edge'; path = "$env:LOCALAPPDATA\Microsoft\Edge\User Data\Default\Extensions" }
  )
  foreach ($root in $extRoots) {
    if (-not (Test-Path $root.path)) { continue }
    Get-ChildItem $root.path -Directory -ErrorAction SilentlyContinue | ForEach-Object {
      $extId = $_.Name
      $verDir = Get-ChildItem $_.FullName -Directory -ErrorAction SilentlyContinue | Select-Object -First 1
      if (-not $verDir) { return }
      $manifest = Join-Path $verDir.FullName 'manifest.json'
      if (-not (Test-Path $manifest)) { return }
      try {
        $m = Get-Content $manifest -Raw -ErrorAction Stop | ConvertFrom-Json
        $name = [string]$m.name
        if ($name -match '^__MSG_') { $name = $extId }
        $out.installedExtensions += [ordered]@{
          browser = $root.browser
          name = $name
          version = [string]$m.version
          id = $extId
        }
      } catch {}
      if ($out.installedExtensions.Count -ge 100) { break }
    }
  }
} catch {}

# Missing patches with titles (cap 50) — last, job-timeout so COM hang
# cannot prevent ConvertTo-Json of the fast inventory above.
try {
  $job = Start-Job -ScriptBlock {
    $rows = @()
    $session = New-Object -ComObject Microsoft.Update.Session
    $searcher = $session.CreateUpdateSearcher()
    $r = $searcher.Search("IsInstalled=0 and Type='Software' and IsHidden=0")
    if ($r -and $r.Updates) {
      $i = 0
      foreach ($u in $r.Updates) {
        $kb = ''
        try {
          if ($u.KBArticleIDs -and $u.KBArticleIDs.Count -gt 0) {
            $kb = 'KB' + $u.KBArticleIDs.Item(0)
          }
        } catch {}
        $sev = ''
        try { $sev = [string]$u.MsrcSeverity } catch {}
        $sz = $null
        try { if ($u.MaxDownloadSize) { $sz = [math]::Round($u.MaxDownloadSize / 1MB, 1) } } catch {}
        $row = @{ title = [string]$u.Title; kb = $kb; severity = $sev }
        if ($null -ne $sz) { $row.sizeMb = $sz }
        $rows += $row
        $i++
        if ($i -ge 50) { break }
      }
    }
    return $rows
  }
  if (Wait-Job $job -Timeout 25) {
    $rows = Receive-Job $job -ErrorAction SilentlyContinue
    foreach ($row in @($rows)) {
      if (-not $row) { continue }
      $item = [ordered]@{
        title = [string]$row.title
        kb = [string]$row.kb
        severity = [string]$row.severity
      }
      if ($null -ne $row.sizeMb) { $item.sizeMb = $row.sizeMb }
      $out.missingPatches += $item
    }
  } else {
    $warnings += 'dex: Windows Update search timed out'
    Stop-Job $job -Force -ErrorAction SilentlyContinue
  }
  Remove-Job $job -Force -ErrorAction SilentlyContinue
} catch {}

if ($warnings.Count -gt 0) { $out.warnings = $warnings }

$out | ConvertTo-Json -Compress -Depth 6
`

type winDEXResult struct {
	InstalledApps       []InstalledApp     `json:"installedApps"`
	InstalledExtensions []BrowserExtension `json:"installedExtensions"`
	MissingPatches      []MissingPatch     `json:"missingPatches"`
	Win11Readiness      *Win11Readiness    `json:"win11Readiness"`
	EventLog            []EventLogRow      `json:"eventLog"`
	PowerEvents         []PowerEvent       `json:"powerEvents"`
	Warnings            []string           `json:"warnings"`
}

func collectDEXInventoryOS(ctx context.Context) *DexInventory {
	cmd := exec.CommandContext(ctx,
		"powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-Command", winDEXScript,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	now := time.Now().UTC().Format(time.RFC3339)
	if err != nil || len(out) == 0 {
		inv := &DexInventory{CollectedAt: now}
		if ctx.Err() != nil {
			inv.Warnings = []string{"dex: inventory PowerShell timed out"}
		} else if err != nil {
			inv.Warnings = []string{"dex: inventory PowerShell failed"}
		}
		return inv
	}
	var r winDEXResult
	if err := json.Unmarshal(out, &r); err != nil {
		return &DexInventory{
			CollectedAt: now,
			Warnings:    []string{"dex: inventory JSON parse failed"},
		}
	}
	return &DexInventory{
		InstalledApps:       r.InstalledApps,
		InstalledExtensions: r.InstalledExtensions,
		MissingPatches:      r.MissingPatches,
		Win11Readiness:      r.Win11Readiness,
		EventLog:            r.EventLog,
		PowerEvents:         r.PowerEvents,
		Warnings:            r.Warnings,
		CollectedAt:         now,
	}
}

var (
	modUser32                    = windows.NewLazySystemDLL("user32.dll")
	procGetForegroundWindow      = modUser32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessId = modUser32.NewProc("GetWindowThreadProcessId")
	procGetWindowTextW           = modUser32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW     = modUser32.NewProc("GetWindowTextLengthW")
)

func sampleAppFocusOS() (name string, pid int32, ok bool) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return "", 0, false
	}
	var pidU uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pidU)))
	if pidU == 0 {
		return "", 0, false
	}
	// Prefer process image name via OpenProcess + QueryFullProcessImageName.
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pidU)
	if err == nil {
		defer windows.CloseHandle(h)
		var buf [260]uint16
		size := uint32(len(buf))
		if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err == nil && size > 0 {
			path := windows.UTF16ToString(buf[:size])
			name = baseName(path)
		}
	}
	if name == "" {
		length, _, _ := procGetWindowTextLengthW.Call(hwnd)
		if length > 0 {
			buf := make([]uint16, length+1)
			procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(length+1))
			name = windows.UTF16ToString(buf)
		}
	}
	if name == "" {
		name = "pid:" + itoa(int(pidU))
	}
	return name, int32(pidU), true
}

func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '\\' || path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}
