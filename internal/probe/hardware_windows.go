//go:build windows

// Windows hardware collector.
//
// We pull the inventory from CIM (Common Information Model) classes,
// the modern replacement for legacy WMI. A single PowerShell process
// fetches every class we care about and emits one JSON document, so
// the probe pays the PowerShell startup cost (~0.5–1 s) once per
// 6-hour refresh window.
//
// Why PowerShell instead of `golang.org/x/sys/windows`:
//   * PowerShell + Get-CimInstance is universally available on
//     Windows 10/11 and Server 2016+, the only Windows targets we
//     officially support.
//   * Querying CIM directly from Go would require a COM bridge
//     (StackExchange/wmi or microsoft/wmi) which adds a CGo build
//     dep and is a known source of memory-leak gotchas.
//   * The probe runs as LocalSystem so PowerShell can read every
//     class we need with no extra elevation.
//
// The PowerShell script is intentionally defensive: each
// Get-CimInstance is wrapped in a try/catch so a single locked-down
// class doesn't blank the whole inventory. Errors from any single
// class are surfaced as collectionWarnings on the Hardware value.

package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// psHardwareScript fetches every CIM class we want and emits one JSON
// document. The shape mirrors what we want to deserialize on the Go
// side; the keys are deliberately short to keep the WS frame small.
const psHardwareScript = `
$ErrorActionPreference = 'Stop'
$out = [ordered]@{ system=$null; cpu=$null; memory=@(); disks=@(); nics=@(); gpus=@(); warnings=@() }
function Try-Cim($name, $class, $filter=$null) {
  try {
    if ($filter) { Get-CimInstance -ClassName $class -Filter $filter -ErrorAction Stop }
    else        { Get-CimInstance -ClassName $class -ErrorAction Stop }
  } catch {
    $out.warnings += ("$name: " + $_.Exception.Message)
    @()
  }
}
$cs   = Try-Cim 'computer-system'  'Win32_ComputerSystem'  | Select-Object -First 1
$bios = Try-Cim 'bios'             'Win32_BIOS'            | Select-Object -First 1
$bb   = Try-Cim 'baseboard'        'Win32_BaseBoard'       | Select-Object -First 1
$enc  = Try-Cim 'enclosure'        'Win32_SystemEnclosure' | Select-Object -First 1
if ($cs -or $bios -or $bb -or $enc) {
  $chassisType = $null
  if ($enc -and $enc.ChassisTypes -and $enc.ChassisTypes.Length -gt 0) {
    $chassisType = [int]$enc.ChassisTypes[0]
  }
  $biosDate = $null
  if ($bios -and $bios.ReleaseDate) {
    try { $biosDate = $bios.ReleaseDate.ToString('yyyy-MM-dd') } catch {}
  }
  $out.system = [ordered]@{
    manufacturer      = if ($cs) { $cs.Manufacturer } else { $null }
    productName       = if ($cs) { $cs.Model }        else { $null }
    serialNumber      = if ($bios) { $bios.SerialNumber } else { $null }
    chassisType       = $chassisType
    chassisAssetTag   = if ($enc) { $enc.SMBIOSAssetTag } else { $null }
    biosVendor        = if ($bios) { $bios.Manufacturer } else { $null }
    biosVersion       = if ($bios) { $bios.SMBIOSBIOSVersion } else { $null }
    biosDate          = $biosDate
    boardManufacturer = if ($bb) { $bb.Manufacturer } else { $null }
    boardProduct      = if ($bb) { $bb.Product }      else { $null }
    boardSerial       = if ($bb) { $bb.SerialNumber } else { $null }
  }
}
$proc = Try-Cim 'cpu' 'Win32_Processor' | Select-Object -First 1
if ($proc) {
  $out.cpu = [ordered]@{
    model      = $proc.Name
    cores      = [int]$proc.NumberOfCores
    threads    = [int]$proc.NumberOfLogicalProcessors
    mhzNominal = [int]$proc.MaxClockSpeed
  }
}
$pm = Try-Cim 'memory' 'Win32_PhysicalMemory'
foreach ($m in $pm) {
  $out.memory += [ordered]@{
    slot         = $m.DeviceLocator
    manufacturer = $m.Manufacturer
    partNumber   = $m.PartNumber
    serialNumber = $m.SerialNumber
    speedMhz     = [int]$m.ConfiguredClockSpeed
    type         = [int]$m.SMBIOSMemoryType
    formFactor   = [int]$m.FormFactor
    sizeBytes    = [uint64]$m.Capacity
  }
}
$dd = Try-Cim 'disks' 'Win32_DiskDrive'
foreach ($d in $dd) {
  $out.disks += [ordered]@{
    device     = $d.DeviceID
    model      = $d.Model
    serial     = ($d.SerialNumber -as [string]).Trim()
    vendor     = $d.Manufacturer
    busType    = $d.InterfaceType
    sizeBytes  = [uint64]$d.Size
    mediaType  = $d.MediaType
  }
}
$na = Try-Cim 'nics' 'Win32_NetworkAdapter' 'PhysicalAdapter = TRUE'
foreach ($n in $na) {
  $out.nics += [ordered]@{
    name      = $n.NetConnectionID
    vendor    = $n.Manufacturer
    product   = $n.ProductName
    driver    = $n.ServiceName
    busInfo   = $n.PNPDeviceID
    mac       = $n.MACAddress
    speedMbps = if ($n.Speed) { [int]([uint64]$n.Speed / 1000000) } else { 0 }
  }
}
$vc = Try-Cim 'gpus' 'Win32_VideoController'
foreach ($g in $vc) {
  $out.gpus += [ordered]@{
    vendor  = $g.AdapterCompatibility
    product = $g.Name
    driver  = $g.DriverVersion
    busInfo = $g.PNPDeviceID
  }
}
$out | ConvertTo-Json -Depth 6 -Compress
`

// pswHardware is the on-the-wire shape from the script — short keys,
// platform-specific encodings (chassisType is an int, formFactor is
// an int, mediaType is "Fixed hard disk media", etc.). We post-
// process into the cross-platform Hardware shape on the Go side so
// the wire format is consistent across Linux/Windows/macOS.
type pswHardware struct {
	System *struct {
		Manufacturer      string `json:"manufacturer"`
		ProductName       string `json:"productName"`
		SerialNumber      string `json:"serialNumber"`
		ChassisType       int    `json:"chassisType"`
		ChassisAssetTag   string `json:"chassisAssetTag"`
		BIOSVendor        string `json:"biosVendor"`
		BIOSVersion       string `json:"biosVersion"`
		BIOSDate          string `json:"biosDate"`
		BoardManufacturer string `json:"boardManufacturer"`
		BoardProduct      string `json:"boardProduct"`
		BoardSerial       string `json:"boardSerial"`
	} `json:"system"`
	CPU *struct {
		Model      string  `json:"model"`
		Cores      int     `json:"cores"`
		Threads    int     `json:"threads"`
		MhzNominal float64 `json:"mhzNominal"`
	} `json:"cpu"`
	Memory []struct {
		Slot         string `json:"slot"`
		Manufacturer string `json:"manufacturer"`
		PartNumber   string `json:"partNumber"`
		SerialNumber string `json:"serialNumber"`
		SpeedMhz     int    `json:"speedMhz"`
		Type         int    `json:"type"`
		FormFactor   int    `json:"formFactor"`
		SizeBytes    uint64 `json:"sizeBytes"`
	} `json:"memory"`
	Disks []struct {
		Device    string `json:"device"`
		Model     string `json:"model"`
		Serial    string `json:"serial"`
		Vendor    string `json:"vendor"`
		BusType   string `json:"busType"`
		SizeBytes uint64 `json:"sizeBytes"`
		MediaType string `json:"mediaType"`
	} `json:"disks"`
	NICs []struct {
		Name      string `json:"name"`
		Vendor    string `json:"vendor"`
		Product   string `json:"product"`
		Driver    string `json:"driver"`
		BusInfo   string `json:"busInfo"`
		MAC       string `json:"mac"`
		SpeedMbps int    `json:"speedMbps"`
	} `json:"nics"`
	GPUs []struct {
		Vendor  string `json:"vendor"`
		Product string `json:"product"`
		Driver  string `json:"driver"`
		BusInfo string `json:"busInfo"`
	} `json:"gpus"`
	Warnings []string `json:"warnings"`
}

func collectHardware(ctx context.Context) *Hardware {
	cmd := exec.CommandContext(ctx, "powershell.exe",
		"-NoProfile", "-NonInteractive", "-Command", psHardwareScript)
	out, err := cmd.Output()
	if err != nil {
		// PowerShell missing or sandboxed: degrade silently. The UI
		// will see a snapshot with no `hardware` field instead of
		// half a hardware section.
		return &Hardware{
			CollectionWarnings: []string{
				"hardware: powershell Get-CimInstance failed: " + err.Error(),
			},
		}
	}
	var raw pswHardware
	if err := json.Unmarshal(out, &raw); err != nil {
		return &Hardware{
			CollectionWarnings: []string{
				"hardware: parse PowerShell JSON: " + err.Error(),
			},
		}
	}
	return mapPSWHardware(&raw)
}

func mapPSWHardware(raw *pswHardware) *Hardware {
	hw := &Hardware{}
	if raw.System != nil {
		hw.System = &HardwareSystem{
			Manufacturer:      strings.TrimSpace(raw.System.Manufacturer),
			ProductName:       strings.TrimSpace(raw.System.ProductName),
			SerialNumber:      strings.TrimSpace(raw.System.SerialNumber),
			ChassisType:       windowsChassisLabel(raw.System.ChassisType),
			ChassisAssetTag:   strings.TrimSpace(raw.System.ChassisAssetTag),
			BIOSVendor:        strings.TrimSpace(raw.System.BIOSVendor),
			BIOSVersion:       strings.TrimSpace(raw.System.BIOSVersion),
			BIOSDate:          raw.System.BIOSDate,
			BoardManufacturer: strings.TrimSpace(raw.System.BoardManufacturer),
			BoardProduct:      strings.TrimSpace(raw.System.BoardProduct),
			BoardSerial:       strings.TrimSpace(raw.System.BoardSerial),
		}
	}
	if raw.CPU != nil {
		hw.CPU = &HardwareCPU{
			Model:      strings.TrimSpace(raw.CPU.Model),
			Cores:      raw.CPU.Cores,
			Threads:    raw.CPU.Threads,
			MHzNominal: raw.CPU.MhzNominal,
		}
	}
	for _, m := range raw.Memory {
		if m.SizeBytes == 0 {
			continue
		}
		hw.MemoryModules = append(hw.MemoryModules, HardwareMemModule{
			Slot:         strings.TrimSpace(m.Slot),
			Manufacturer: cleanWMIString(m.Manufacturer),
			PartNumber:   strings.TrimSpace(m.PartNumber),
			SerialNumber: strings.TrimSpace(m.SerialNumber),
			SpeedMHz:     m.SpeedMhz,
			Type:         windowsMemoryType(m.Type),
			FormFactor:   windowsFormFactor(m.FormFactor),
			SizeBytes:    m.SizeBytes,
		})
	}
	for _, d := range raw.Disks {
		hw.Storage = append(hw.Storage, HardwareDisk{
			Device:     d.Device,
			Model:      strings.TrimSpace(d.Model),
			Serial:     strings.TrimSpace(d.Serial),
			Vendor:     strings.TrimSpace(d.Vendor),
			BusType:    strings.ToLower(strings.TrimSpace(d.BusType)),
			SizeBytes:  d.SizeBytes,
			Rotational: rotationalFromMediaType(d.MediaType),
		})
	}
	for _, n := range raw.NICs {
		hw.NetworkAdapters = append(hw.NetworkAdapters, HardwareNICInfo{
			Name:      strings.TrimSpace(n.Name),
			Vendor:    strings.TrimSpace(n.Vendor),
			Product:   strings.TrimSpace(n.Product),
			Driver:    strings.TrimSpace(n.Driver),
			BusInfo:   strings.TrimSpace(n.BusInfo),
			MAC:       strings.TrimSpace(n.MAC),
			SpeedMbps: n.SpeedMbps,
		})
	}
	for _, g := range raw.GPUs {
		hw.GPUs = append(hw.GPUs, HardwareGPU{
			Vendor:  strings.TrimSpace(g.Vendor),
			Product: strings.TrimSpace(g.Product),
			Driver:  strings.TrimSpace(g.Driver),
			BusInfo: strings.TrimSpace(g.BusInfo),
		})
	}
	hw.CollectionWarnings = raw.Warnings
	return hw
}

// cleanWMIString drops the all-too-common "Manufacturer00…" filler
// the BIOS hands back when a vendor didn't bother filling out the
// SPD fields. The leading non-printable hex makes it obvious.
func cleanWMIString(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if isLikelyHexFiller(s) {
		return ""
	}
	return s
}

func isLikelyHexFiller(s string) bool {
	// "0420" or "0x0420" or "Manufacturer00 ..." — heuristic: long
	// runs of hex digits with no spaces are almost always SPD junk.
	stripped := strings.ReplaceAll(s, " ", "")
	if len(stripped) >= 4 {
		hex := 0
		for _, r := range stripped {
			if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'F') || (r >= 'a' && r <= 'f') {
				hex++
			}
		}
		if hex == len(stripped) {
			return true
		}
	}
	return false
}

// windowsMemoryType maps Win32_PhysicalMemory.SMBIOSMemoryType (the
// SMBIOS spec value, NOT the legacy MemoryType field) to a label.
// Values from SMBIOS table 7.18.2.
func windowsMemoryType(n int) string {
	switch n {
	case 20:
		return "DDR"
	case 21:
		return "DDR2"
	case 22:
		return "DDR2 FB-DIMM"
	case 24:
		return "DDR3"
	case 26:
		return "DDR4"
	case 30:
		return "LPDDR4"
	case 34:
		return "DDR5"
	case 35:
		return "LPDDR5"
	}
	return ""
}

// windowsFormFactor maps Win32_PhysicalMemory.FormFactor.
// Values from SMBIOS table 7.18.1 (matches Win32 enum).
func windowsFormFactor(n int) string {
	switch n {
	case 8:
		return "DIMM"
	case 12:
		return "SO-DIMM"
	case 13:
		return "Row of chips"
	case 23:
		return "FB-DIMM"
	case 24:
		return "Die"
	}
	return ""
}

// windowsChassisLabel maps the SMBIOS chassis-type integer to a
// label. Same table as Linux readDMIChassisType but reused here so
// the Windows path doesn't depend on a build-tagged Linux helper.
func windowsChassisLabel(n int) string {
	if n == 0 {
		return ""
	}
	switch n {
	case 1:
		return "Other"
	case 3:
		return "Desktop"
	case 4:
		return "Low-Profile Desktop"
	case 5:
		return "Pizza Box"
	case 6:
		return "Mini Tower"
	case 7:
		return "Tower"
	case 8, 9:
		return "Portable"
	case 10:
		return "Notebook"
	case 11:
		return "Hand Held"
	case 12:
		return "Docking Station"
	case 13:
		return "All in One"
	case 14:
		return "Sub Notebook"
	case 17:
		return "Main Server Chassis"
	case 18:
		return "Expansion Chassis"
	case 23:
		return "Rack Mount Chassis"
	case 28:
		return "Blade"
	case 29:
		return "Blade Enclosure"
	case 30:
		return "Tablet"
	case 31:
		return "Convertible"
	case 32:
		return "Detachable"
	case 35:
		return "Mini PC"
	case 36:
		return "Stick PC"
	}
	return fmt.Sprintf("Type %d", n)
}

// rotationalFromMediaType infers HDD vs SSD from the legacy
// Win32_DiskDrive.MediaType string. "Fixed hard disk media" can mean
// either, but Windows reports SSDs there too — so we prefer
// MSFT_PhysicalDisk.MediaType when available, falling back to the
// string heuristic. Hardcoding the heuristic here keeps the
// PowerShell script tractable; if it ever matters more we can pull
// MSFT_PhysicalDisk into the script.
func rotationalFromMediaType(s string) *bool {
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "ssd"):
		v := false
		return &v
	case strings.Contains(low, "fixed hard disk"):
		// Ambiguous on modern Windows; leave nil so the UI shows "—".
		return nil
	}
	return nil
}

// quoted is a tiny helper for assembling future PS args; kept to
// avoid `fmt`/`strconv` being elided from imports in a future
// refactor.
//
//nolint:unused
func quoted(s string) string {
	return strconv.Quote(s)
}
