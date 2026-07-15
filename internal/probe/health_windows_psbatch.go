//go:build windows

package probe

import (
	"context"
	"encoding/json"
	"os/exec"
	"syscall"
)

// winPSScript is the consolidated PowerShell script that produces a
// single JSON object with every "expensive" health field. Each
// section is wrapped in try/catch so one failure (e.g. no battery,
// COM Microsoft.Update unavailable on a server SKU) doesn't kill the
// rest of the batch.
//
// We deliberately use Get-CimInstance (cmdlets backed by WS-Man /
// CIM) rather than the older Get-WmiObject because Microsoft is
// removing WMIC and discouraging the legacy WMI cmdlets in the
// Windows 11 / Server 2025 timeframe.
//
// SignalPct from netsh wlan is preferred over the raw RSSI because
// the bar gauges in the dashboard show a 0–100 percentage. RSSI in
// dBm comes from a separate netsh row when available.
const winPSScript = `
$out = [ordered]@{}

# Battery health = full charge / design capacity * 100. Hosts without
# a battery (desktops, servers, VMs) leave the key absent.
try {
  $b = Get-CimInstance Win32_Battery -ErrorAction Stop | Select-Object -First 1
  if ($b) {
    $stat = Get-CimInstance -Namespace root\wmi -ClassName BatteryStaticData -ErrorAction SilentlyContinue | Select-Object -First 1
    $full = Get-CimInstance -Namespace root\wmi -ClassName BatteryFullChargedCapacity -ErrorAction SilentlyContinue | Select-Object -First 1
    $cycle = Get-CimInstance -Namespace root\wmi -ClassName BatteryCycleCount -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($stat -and $full -and $stat.DesignedCapacity -gt 0) {
      $out.batteryHealthPct = [math]::Round(($full.FullChargedCapacity / $stat.DesignedCapacity) * 100, 1)
      $out.batteryDesignedMWh = [int]$stat.DesignedCapacity
      $out.batteryFullChargeMWh = [int]$full.FullChargedCapacity
      $out.batteryWearPct = [math]::Round(100 - $out.batteryHealthPct, 1)
    }
    if ($cycle -and $cycle.CycleCount -ne $null) {
      $out.batteryCycleCount = [int]$cycle.CycleCount
    }
  }
} catch {}

# Last cold-boot duration from Diagnostics-Performance Event ID 100 (BootPerformance)
try {
  $boot = Get-WinEvent -FilterHashtable @{LogName='Microsoft-Windows-Diagnostics-Performance/Operational'; Id=100} -MaxEvents 1 -ErrorAction Stop
  if ($boot) {
    $xml = [xml]$boot.ToXml()
    $bd = $xml.Event.EventData.Data | Where-Object { $_.Name -eq 'BootTime' } | Select-Object -First 1
    if ($bd -and $bd.'#text') { $out.bootDurationMs = [int64]$bd.'#text' }
  }
} catch {}

# Missing patches — count of "is not installed and not hidden" updates.
# COM Microsoft.Update.Session Search can hang for minutes on AVD /
# WSUS-broken hosts; run it in a job with a hard timeout so the rest
# of the batch (event-log rollups, etc.) still completes.
$warnings = @()
try {
  $job = Start-Job -ScriptBlock {
    $session = New-Object -ComObject Microsoft.Update.Session
    $searcher = $session.CreateUpdateSearcher()
    $r = $searcher.Search("IsInstalled=0 and Type='Software' and IsHidden=0")
    if ($r -and $r.Updates) { return [int]$r.Updates.Count }
    return 0
  }
  if (Wait-Job $job -Timeout 20) {
    $c = Receive-Job $job -ErrorAction SilentlyContinue
    if ($null -ne $c) { $out.missingPatchCount = [int]$c }
  } else {
    $warnings += 'health: Windows Update search timed out'
    Stop-Job $job -Force -ErrorAction SilentlyContinue
  }
  Remove-Job $job -Force -ErrorAction SilentlyContinue
} catch {}

# Event log roll-ups, last 24h.
$since = (Get-Date).AddDays(-1)
function safeCount {
  param($filter)
  try {
    $r = Get-WinEvent -FilterHashtable $filter -MaxEvents 5000 -ErrorAction Stop
    if ($null -eq $r) { return 0 }
    return @($r).Count
  } catch { return 0 }
}
$out.bsodCount24h           = safeCount @{LogName='System';  Id=41,1001,6008; StartTime=$since}
$out.userRebootCount24h     = safeCount @{LogName='System';  Id=1074;         StartTime=$since}
$out.appCrashCount24h       = safeCount @{LogName='Application'; ProviderName='Application Error'; StartTime=$since}
$out.eventLogErrorCount24h  = safeCount @{LogName='Application','System'; Level=2; StartTime=$since}

# Highload CPU incidents — count of Event ID 6008 / Resource Exhaustion
# Detector entries (Event ID 2004 in Microsoft-Windows-Resource-Exhaustion-Detector).
$out.highloadCpuIncidents24h = safeCount @{LogName='Microsoft-Windows-Resource-Exhaustion-Detector/Operational'; Id=2004; StartTime=$since}

# Logon timings — Microsoft-Windows-GroupPolicy/Operational Event ID
# 8001 fires once per logon with a PolicyElaspedTimeInSeconds data
# field (yes, Microsoft misspelled "Elapsed" — the field name is
# normative). It's the time the user spent waiting for Group Policy
# to apply during login, which is the dominant component of the
# user-perceived logon delay on most managed workstations.
#
# We sample the last 7 days, filter to user (IsMachine=0) events,
# and convert seconds to ms for consistency with the other timing
# fields. Hosts without GP (workgroup machines) just emit no rows
# and we leave the keys absent.
try {
  $logonSince = (Get-Date).AddDays(-7)
  $events = Get-WinEvent -FilterHashtable @{
    LogName='Microsoft-Windows-GroupPolicy/Operational';
    Id=8001;
    StartTime=$logonSince
  } -ErrorAction Stop
  if ($events) {
    $logonMs = @()
    foreach ($e in $events) {
      try {
        $xml = [xml]$e.ToXml()
        $isMachine = ($xml.Event.EventData.Data | Where-Object { $_.Name -eq 'IsMachine' }).'#text'
        if ($isMachine -ne '0') { continue }
        $secs = ($xml.Event.EventData.Data | Where-Object { $_.Name -eq 'PolicyElaspedTimeInSeconds' }).'#text'
        if ($secs) { $logonMs += [double]$secs * 1000.0 }
      } catch {}
    }
    if ($logonMs.Count -gt 0) {
      $out.logonAvgMs = [math]::Round(($logonMs | Measure-Object -Average).Average, 0)
      $out.logonMaxMs = [math]::Round(($logonMs | Measure-Object -Maximum).Maximum, 0)
    }
  }
} catch {}

# Input delay approx — Diagnostics-Performance Operational event 100
# (boot/performance) DurationMS when present; else leave absent.
try {
  $perfSince = (Get-Date).AddDays(-1)
  $pevs = Get-WinEvent -FilterHashtable @{
    LogName='Microsoft-Windows-Diagnostics-Performance/Operational';
    Id=100;
    StartTime=$perfSince
  } -MaxEvents 40 -ErrorAction Stop
  $delays = @()
  foreach ($e in @($pevs)) {
    try {
      $xml = [xml]$e.ToXml()
      $dur = ($xml.Event.EventData.Data | Where-Object { $_.Name -eq 'DurationMS' -or $_.Name -eq 'BootTime' }).'#text' | Select-Object -First 1
      if ($dur) { $delays += [double]$dur }
    } catch {}
  }
  if ($delays.Count -gt 0) {
    $out.inputDelayAvgMs = [math]::Round(($delays | Measure-Object -Average).Average, 0)
  }
} catch {}

# App launch max — Shell-Core Operational 9705/9706 timing fields when present.
try {
  $launchSince = (Get-Date).AddDays(-1)
  $levs = Get-WinEvent -FilterHashtable @{
    LogName='Microsoft-Windows-Shell-Core/Operational';
    Id=9705,9706;
    StartTime=$launchSince
  } -MaxEvents 50 -ErrorAction SilentlyContinue
  $launches = @()
  foreach ($e in @($levs)) {
    try {
      $xml = [xml]$e.ToXml()
      $ms = ($xml.Event.EventData.Data | Where-Object { $_.Name -match 'Time|Duration|Elapsed' }).'#text' | Select-Object -First 1
      if ($ms) { $launches += [double]$ms }
    } catch {}
  }
  if ($launches.Count -gt 0) {
    $out.appLaunchMaxMs = [math]::Round(($launches | Measure-Object -Maximum).Maximum, 0)
  }
} catch {}

# Per-app crash tallies from Application Error (Event ID 1000) — top 10.
try {
  $crashSince = (Get-Date).AddDays(-1)
  $cevs = Get-WinEvent -FilterHashtable @{
    LogName='Application';
    ProviderName='Application Error';
    Id=1000;
    StartTime=$crashSince
  } -MaxEvents 200 -ErrorAction SilentlyContinue
  $counts = @{}
  foreach ($e in @($cevs)) {
    try {
      $xml = [xml]$e.ToXml()
      $app = ($xml.Event.EventData.Data | Select-Object -First 1).'#text'
      if (-not $app) { $app = 'unknown' }
      $app = [string]$app
      if ($counts.ContainsKey($app)) { $counts[$app]++ } else { $counts[$app] = 1 }
    } catch {}
  }
  $rows = @()
  foreach ($k in ($counts.Keys | Sort-Object { -$counts[$_] } | Select-Object -First 10)) {
    $rows += [ordered]@{ name = $k; count = [int]$counts[$k] }
  }
  if ($rows.Count -gt 0) { $out.appCrashesByName = $rows }
} catch {}

# WiFi via netsh wlan show interfaces. Empty on hosts without a wireless adapter.
try {
  $w = (& netsh.exe wlan show interfaces 2>$null | Out-String) -split "` + "`" + `r?` + "`" + `n"
  foreach ($line in $w) {
    if ($line -match '^\s*SSID\s+:\s+(.+?)\s*$' -and -not ($line -match 'BSSID')) {
      $out.wifiSsid = $Matches[1]
    }
    if ($line -match '^\s*Signal\s+:\s+(\d+)\s*%') {
      $out.wifiSignalPct = [int]$Matches[1]
    }
  }
} catch {}

if ($warnings.Count -gt 0) { $out.warnings = $warnings }

$out | ConvertTo-Json -Compress
`

// winPSResult mirrors the JSON keys emitted by winPSScript. Unset
// keys decode as zero values; we treat the zero value as "absent".
type winPSResult struct {
	BatteryHealthPct        *float64            `json:"batteryHealthPct,omitempty"`
	BatteryCycleCount       *int                `json:"batteryCycleCount,omitempty"`
	BatteryDesignedMWh      *int                `json:"batteryDesignedMWh,omitempty"`
	BatteryFullChargeMWh    *int                `json:"batteryFullChargeMWh,omitempty"`
	BatteryWearPct          *float64            `json:"batteryWearPct,omitempty"`
	BootDurationMs          *int64              `json:"bootDurationMs,omitempty"`
	MissingPatchCount       *int                `json:"missingPatchCount,omitempty"`
	BSODCount24h            *int                `json:"bsodCount24h,omitempty"`
	UserRebootCount24h      *int                `json:"userRebootCount24h,omitempty"`
	AppCrashCount24h        *int                `json:"appCrashCount24h,omitempty"`
	EventLogErrorCount24h   *int                `json:"eventLogErrorCount24h,omitempty"`
	HighloadCPUIncidents24h *int                `json:"highloadCpuIncidents24h,omitempty"`
	WiFiSSID                string              `json:"wifiSsid,omitempty"`
	WiFiSignalPct           *int                `json:"wifiSignalPct,omitempty"`
	LogonAvgMs              *float64            `json:"logonAvgMs,omitempty"`
	LogonMaxMs              *float64            `json:"logonMaxMs,omitempty"`
	AppLaunchMaxMs          *float64            `json:"appLaunchMaxMs,omitempty"`
	InputDelayAvgMs         *float64            `json:"inputDelayAvgMs,omitempty"`
	AppCrashesByName        []AppCrashNameCount `json:"appCrashesByName,omitempty"`
	Warnings                []string            `json:"warnings,omitempty"`
}

// winRunPSBatch runs winPSScript once and copies the parsed result
// into h. Timeouts and malformed JSON leave whatever partial fields
// we have; warnings are attached for CollectionWarnings.
func winRunPSBatch(ctx context.Context, h *HealthSignals) {
	cmd := exec.CommandContext(ctx,
		"powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-Command", winPSScript,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		if ctx.Err() != nil {
			h.SlowCollectionWarnings = append(h.SlowCollectionWarnings, "health: PowerShell timed out")
		} else if err != nil {
			h.SlowCollectionWarnings = append(h.SlowCollectionWarnings, "health: PowerShell failed")
		}
		return
	}
	var r winPSResult
	if err := json.Unmarshal(out, &r); err != nil {
		h.SlowCollectionWarnings = append(h.SlowCollectionWarnings, "health: PowerShell JSON parse failed")
		return
	}
	if len(r.Warnings) > 0 {
		h.SlowCollectionWarnings = append(h.SlowCollectionWarnings, r.Warnings...)
	}
	if r.BatteryHealthPct != nil {
		h.BatteryHealthPct = r.BatteryHealthPct
	}
	if r.BatteryCycleCount != nil {
		h.BatteryCycleCount = r.BatteryCycleCount
	}
	if r.BatteryDesignedMWh != nil {
		h.BatteryDesignedMWh = r.BatteryDesignedMWh
	}
	if r.BatteryFullChargeMWh != nil {
		h.BatteryFullChargeMWh = r.BatteryFullChargeMWh
	}
	if r.BatteryWearPct != nil {
		h.BatteryWearPct = r.BatteryWearPct
	}
	if r.BootDurationMs != nil {
		h.BootDurationMs = r.BootDurationMs
	}
	if r.MissingPatchCount != nil {
		h.MissingPatchCount = r.MissingPatchCount
	}
	if r.BSODCount24h != nil {
		h.BSODCount24h = r.BSODCount24h
	}
	if r.UserRebootCount24h != nil {
		h.UserRebootCount24h = r.UserRebootCount24h
	}
	if r.AppCrashCount24h != nil {
		h.AppCrashCount24h = r.AppCrashCount24h
	}
	if r.EventLogErrorCount24h != nil {
		h.EventLogErrorCount24h = r.EventLogErrorCount24h
	}
	if r.HighloadCPUIncidents24h != nil {
		h.HighloadCPUIncidents24h = r.HighloadCPUIncidents24h
	}
	if r.LogonAvgMs != nil {
		h.LogonAvgMs = r.LogonAvgMs
	}
	if r.LogonMaxMs != nil {
		h.LogonMaxMs = r.LogonMaxMs
	}
	if r.AppLaunchMaxMs != nil {
		h.AppLaunchMaxMs = r.AppLaunchMaxMs
	}
	if r.InputDelayAvgMs != nil {
		h.InputDelayAvgMs = r.InputDelayAvgMs
	}
	if len(r.AppCrashesByName) > 0 {
		h.AppCrashesByName = r.AppCrashesByName
	}
	h.WiFiSSID = r.WiFiSSID
	if r.WiFiSignalPct != nil {
		h.WiFiSignalPct = r.WiFiSignalPct
		// Approximate dBm from percentage when explicit RSSI isn't
		// reported (common on Windows). Linear interpolation between
		// -90 dBm (1%) and -30 dBm (100%) — Microsoft documents this
		// scaling for the WLAN_ASSOCIATION_ATTRIBUTES wlanSignalQuality
		// field.
		dbm := -90 + int(float64(*r.WiFiSignalPct)*60.0/100.0)
		h.WiFiRSSIdBm = &dbm
	}
}
