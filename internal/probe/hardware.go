// hardware.go — collected ONCE per probe lifetime and stitched into
// every snapshot the probe ships.
//
// Why "once": none of these fields actually change while a host is
// running. CPU model, BIOS version, memory DIMMs, disk model+serial
// — they all require either a reboot, a power-down DIMM swap, or a
// disk hot-swap. Re-querying DMI/WMI every minute would waste battery
// on laptops, generate audit-log noise on Windows (Get-CimInstance
// is logged), and add ~200–500 ms to each snapshot for no signal.
//
// Why a separate file: the operator-facing "hardware spec sheet" lives
// or dies on platform-specific sources (DMI on Linux, CIM/WMI on
// Windows, IOKit on macOS) that are messy to inline into snapshot.go.
// Keeping them isolated also makes "this probe runs in a docker
// container without /sys/class/dmi" a clean partial-failure story:
// `Hardware` is just nil and the UI degrades gracefully.
//
// The struct shape is the contract — field names and JSON tags must
// stay in sync with web/src/api/types.ts (SnapshotHardware et al.).

package probe

import (
	"context"
	"sync"
	"time"
)

// Hardware is the once-per-boot inventory. Every sub-section is a
// pointer or slice and every leaf field is omitempty so a degraded
// collector (containerized host, missing root, locked-down WMI) emits
// just the parts it could read instead of a wall of empty strings.
type Hardware struct {
	System          *HardwareSystem      `json:"system,omitempty"`
	CPU             *HardwareCPU         `json:"cpu,omitempty"`
	MemoryModules   []HardwareMemModule  `json:"memoryModules,omitempty"`
	Storage         []HardwareDisk       `json:"storage,omitempty"`
	NetworkAdapters []HardwareNICInfo    `json:"networkAdapters,omitempty"`
	GPUs            []HardwareGPU        `json:"gpus,omitempty"`

	// Per-collector warnings so the UI can show "memory DIMM details
	// unavailable: dmidecode requires root" without dropping the rest.
	CollectionWarnings []string `json:"collectionWarnings,omitempty"`
}

// HardwareSystem mirrors the "what computer is this" fields you'd
// read off a DMI / Win32_ComputerSystem dump: chassis identity, BIOS
// + motherboard provenance.
type HardwareSystem struct {
	Manufacturer      string `json:"manufacturer,omitempty"`
	ProductName       string `json:"productName,omitempty"`
	SerialNumber      string `json:"serialNumber,omitempty"`
	ChassisType       string `json:"chassisType,omitempty"`
	ChassisAssetTag   string `json:"chassisAssetTag,omitempty"`
	BIOSVendor        string `json:"biosVendor,omitempty"`
	BIOSVersion       string `json:"biosVersion,omitempty"`
	BIOSDate          string `json:"biosDate,omitempty"`
	BoardManufacturer string `json:"boardManufacturer,omitempty"`
	BoardProduct      string `json:"boardProduct,omitempty"`
	BoardSerial       string `json:"boardSerial,omitempty"`
}

// HardwareCPU is the static identity of the CPU, distinct from the
// runtime utilisation we already capture in CPU. It exists so the
// hardware tab can stand alone and so we don't have to refactor the
// existing CPU struct's contract (which is consumed by the metrics
// time-series writer).
type HardwareCPU struct {
	Model      string  `json:"model,omitempty"`
	Cores      int     `json:"cores,omitempty"`      // physical
	Threads    int     `json:"threads,omitempty"`    // logical
	MHzNominal float64 `json:"mhzNominal,omitempty"` // base clock
}

type HardwareMemModule struct {
	Slot         string `json:"slot,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	PartNumber   string `json:"partNumber,omitempty"`
	SerialNumber string `json:"serialNumber,omitempty"`
	SpeedMHz     int    `json:"speedMhz,omitempty"`
	Type         string `json:"type,omitempty"`       // DDR3, DDR4, DDR5, ...
	FormFactor   string `json:"formFactor,omitempty"` // DIMM, SO-DIMM, ...
	SizeBytes    uint64 `json:"sizeBytes,omitempty"`
}

type HardwareDisk struct {
	Device     string `json:"device,omitempty"`     // /dev/sda, \\.\PHYSICALDRIVE0
	Model      string `json:"model,omitempty"`
	Serial     string `json:"serial,omitempty"`
	Vendor     string `json:"vendor,omitempty"`
	BusType    string `json:"busType,omitempty"`    // sata, nvme, sas, usb
	FormFactor string `json:"formFactor,omitempty"` // 2.5", 3.5", M.2
	SizeBytes  uint64 `json:"sizeBytes,omitempty"`
	// Rotational is a tristate disguised as a *bool: nil = unknown,
	// true = HDD, false = SSD/NVMe. We use a pointer so the JSON
	// output omits it on unknown rather than lying with `false`.
	Rotational *bool `json:"rotational,omitempty"`
}

type HardwareNICInfo struct {
	Name      string `json:"name,omitempty"`
	Vendor    string `json:"vendor,omitempty"`
	Product   string `json:"product,omitempty"`
	Driver    string `json:"driver,omitempty"`
	BusInfo   string `json:"busInfo,omitempty"` // pci@0000:01:00.0
	MAC       string `json:"mac,omitempty"`
	SpeedMbps int    `json:"speedMbps,omitempty"`
}

type HardwareGPU struct {
	Vendor  string `json:"vendor,omitempty"`
	Product string `json:"product,omitempty"`
	Driver  string `json:"driver,omitempty"`
	BusInfo string `json:"busInfo,omitempty"`
}

// hardwareCache holds the once-collected inventory plus the time of
// last successful collection. Refresh logic re-runs the collector at
// most every refreshHardwareEvery — small enough that adding RAM is
// reflected within a single working day, big enough that an idle box
// isn't burning CPU on DMI reads.
type hardwareCache struct {
	mu        sync.Mutex
	hw        *Hardware
	collected time.Time
}

var hwCache hardwareCache

// refreshHardwareEvery is how often we rerun the collector. 6h
// balances responsiveness ("I added a DIMM, when does the UI catch
// up?") against not spamming `dmidecode` / WMI on idle hosts.
const refreshHardwareEvery = 6 * time.Hour

// snapshotHardware returns the cached Hardware value or kicks off a
// re-collection if the cache is stale. It is safe to call from the
// snapshot loop: the lock is held only across the platform call, and
// the platform call has its own bounded timeout.
func snapshotHardware(ctx context.Context) *Hardware {
	hwCache.mu.Lock()
	defer hwCache.mu.Unlock()
	if hwCache.hw != nil && time.Since(hwCache.collected) < refreshHardwareEvery {
		return hwCache.hw
	}
	// Bound platform-specific syscalls (dmidecode, PowerShell) so a
	// hung subprocess can't stall the snapshot.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	hw := collectHardware(ctx)
	if hw != nil {
		hwCache.hw = hw
		hwCache.collected = time.Now()
	}
	return hw
}
