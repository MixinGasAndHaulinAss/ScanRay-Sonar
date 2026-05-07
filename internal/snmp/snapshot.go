// Package snmp — Snapshot is the wire/storage shape we persist for one
// poll cycle. It mirrors the structure of internal/probe.Snapshot so
// the UI can lean on similar conventions, but the contents are the
// network-device equivalents (interfaces instead of disks, sensors
// instead of services, etc.).
//
// SchemaVersion exists for the same reason as on the agent side: when
// we add a backwards-incompatible field shape, bump it and the UI can
// branch off the version. v1 is the initial Phase 3a release.
package snmp

import "time"

// Snapshot is one full collect of an appliance. Stored verbatim as
// JSONB on appliances.last_snapshot.
type Snapshot struct {
	SchemaVersion int       `json:"schemaVersion"`
	CapturedAt    time.Time `json:"capturedAt"`
	CollectMs     int64     `json:"collectMs"`

	System     System      `json:"system"`
	Chassis    Chassis     `json:"chassis"`
	Interfaces []Interface `json:"interfaces"`
	Entities   []Entity    `json:"entities,omitempty"`
	LLDP       []LLDP      `json:"lldp,omitempty"`
	CDP        []CDP       `json:"cdp,omitempty"`

	// Vendor carries optional vendor-specific health structures
	// populated by CollectVendor based on the appliance vendor row.
	// Each sub-field is nil unless we successfully collected at
	// least one OID for it. Schema version 2 added this field.
	Vendor *VendorHealth `json:"vendor,omitempty"`

	// CollectionWarnings accumulates non-fatal issues (timeouts on a
	// single MIB, unsupported OID on this vendor, etc.) so an operator
	// can see why the snapshot is partial without us failing the
	// whole poll.
	CollectionWarnings []string `json:"collectionWarnings,omitempty"`
}

// VendorHealth is the umbrella for everything we collect that doesn't
// fit the universal IF-MIB / ENTITY-MIB / LLDP shape. Each pointer is
// nil unless the matching collector ran and got at least one value.
type VendorHealth struct {
	UPS      *UPSHealth      `json:"ups,omitempty"`
	Synology *SynologyHealth `json:"synology,omitempty"`
	PaloAlto *PaloAltoHealth `json:"paloAlto,omitempty"`
	Alletra  *AlletraHealth  `json:"alletra,omitempty"`
	Cisco    *CiscoExtras    `json:"ciscoExtras,omitempty"`
}

// UPSHealth covers RFC1628 UPS-MIB plus APC enterprise extensions.
// Pointers everywhere because most fields are independently optional.
type UPSHealth struct {
	Model                string   `json:"model,omitempty"`
	FirmwareVersion      string   `json:"firmwareVersion,omitempty"`
	SerialNumber         string   `json:"serialNumber,omitempty"`
	ManufactureDate      string   `json:"manufactureDate,omitempty"`
	BatteryReplaceDate   string   `json:"batteryReplaceDate,omitempty"`
	BatteryStatus        *int32   `json:"batteryStatus,omitempty"`        // 1=unknown 2=normal 3=low 4=depleted
	BatteryReplaceNeeded *bool    `json:"batteryReplaceNeeded,omitempty"` // upsAdvBatteryReplaceIndicator
	OutputStatus         *int32   `json:"outputStatus,omitempty"`         // upsBasicOutputStatus
	InputLineFailCause   *int32   `json:"inputLineFailCause,omitempty"`   // last transfer reason
	EstRuntimeMin        *int32   `json:"estRuntimeMin,omitempty"`        // upsEstimatedMinutesRemaining
	EstChargePct         *int32   `json:"estChargePct,omitempty"`         // upsEstimatedChargeRemaining
	BatteryTempC         *float64 `json:"batteryTempC,omitempty"`         // upsBatteryTemperature
	OutputLoadPct        *int32   `json:"outputLoadPct,omitempty"`        // upsOutputPercentLoad / advanced
	InputVoltage         *float64 `json:"inputVoltageV,omitempty"`        // upsInputVoltage
	OutputVoltage        *float64 `json:"outputVoltageV,omitempty"`       // upsAdvOutputVoltage
}

// SynologyHealth covers .1.3.6.1.4.1.6574.{1,2,3} — system, disks, RAID.
type SynologyHealth struct {
	Model        string               `json:"model,omitempty"`
	Serial       string               `json:"serial,omitempty"`
	DSMVersion   string               `json:"dsmVersion,omitempty"`
	SystemStatus *int32               `json:"systemStatus,omitempty"` // 1=normal 2=failed
	PowerStatus  *int32               `json:"powerStatus,omitempty"`  // 1=normal 2=failed
	TempC        *float64             `json:"tempC,omitempty"`
	Disks        []SynologyDisk       `json:"disks,omitempty"`
	Volumes      []SynologyRAIDVolume `json:"volumes,omitempty"`
}

type SynologyDisk struct {
	Index  int32   `json:"index"`
	ID     string  `json:"id,omitempty"`
	Model  string  `json:"model,omitempty"`
	Type   string  `json:"type,omitempty"`
	Status int32   `json:"status"` // 1=normal 2=initialized 3=notInitialized 4=systemPartitionFailed 5=crashed
	TempC  float64 `json:"tempC,omitempty"`
}

type SynologyRAIDVolume struct {
	Index  int32  `json:"index"`
	Name   string `json:"name,omitempty"`
	Status int32  `json:"status"` // 1=normal 11=degraded 12=crashed; see synology MIB
}

// PaloAltoHealth covers PAN-COMMON-MIB session table.
type PaloAltoHealth struct {
	SessionActive    *int64   `json:"sessionActive,omitempty"`
	SessionMax       *int64   `json:"sessionMax,omitempty"`
	SessionActiveTcp *int64   `json:"sessionActiveTcp,omitempty"`
	SessionActiveUdp *int64   `json:"sessionActiveUdp,omitempty"`
	SessionUtilPct   *float64 `json:"sessionUtilPct,omitempty"` // derived
}

// AlletraHealth covers Nimble/HPE Alletra .1.3.6.1.4.1.37447.
type AlletraHealth struct {
	GlobalVolCount  *int64          `json:"globalVolCount,omitempty"`
	GlobalSnapCount *int64          `json:"globalSnapCount,omitempty"`
	Volumes         []AlletraVolume `json:"volumes,omitempty"`
}

type AlletraVolume struct {
	Index      int32   `json:"index"`
	Name       string  `json:"name,omitempty"`
	SizeBytes  uint64  `json:"sizeBytes,omitempty"`
	UsageBytes uint64  `json:"usageBytes,omitempty"`
	UsedPct    float64 `json:"usedPct,omitempty"`
	Online     bool    `json:"online"`
}

// CiscoExtras carries the Cisco-only counters that don't have a clean
// home in Chassis (which is universal). VLAN inventory and CPU
// breakdown live here.
type CiscoExtras struct {
	VLANs   []CiscoVLAN `json:"vlans,omitempty"`
	CPU5sec *float64    `json:"cpu5secPct,omitempty"`
	CPU1min *float64    `json:"cpu1minPct,omitempty"`
	CPU5min *float64    `json:"cpu5minPct,omitempty"`
}

type CiscoVLAN struct {
	ID    int32  `json:"id"`
	Name  string `json:"name,omitempty"`
	State int32  `json:"state"` // 1=operational 2=suspended
}

// System is the standard SNMPv2-MIB::system group.
type System struct {
	Description string `json:"description"`
	Name        string `json:"name"`
	Contact     string `json:"contact,omitempty"`
	Location    string `json:"location,omitempty"`
	ObjectID    string `json:"objectId,omitempty"` // sysObjectID — vendor enum
	UptimeTicks uint64 `json:"uptimeTicks"`        // sysUpTime in 1/100s
	UptimeSecs  int64  `json:"uptimeSeconds"`      // derived
}

// Chassis collapses vendor-specific CPU/mem/sensor data into a single
// shape the UI can render without a vendor switch.
type Chassis struct {
	CPUPct        *float64 `json:"cpuPct,omitempty"`
	MemUsedBytes  *uint64  `json:"memUsedBytes,omitempty"`
	MemTotalBytes *uint64  `json:"memTotalBytes,omitempty"`
	TempC         *float64 `json:"tempC,omitempty"`
}

// Interface is one row of IF-MIB::ifTable + ifXTable — the
// per-physical-or-logical-port view.
//
// Counters are *cumulative* (Counter64 64-bit) and may have wrapped
// since we last polled. The poller computes deltas/rates separately
// and writes them to appliance_iface_samples; the values here are the
// raw SNMP reading at CollectedAt for the snapshot detail page.
type Interface struct {
	Index int32  `json:"ifIndex"`
	Name  string `json:"name"`            // ifName (short, e.g. Gi1/0/1)
	Descr string `json:"descr"`           // ifDescr (long)
	Alias string `json:"alias,omitempty"` // ifAlias (operator's port description)
	Type  int32  `json:"type"`            // ifType enum (6=ethernet, 53=propVirtual, …)

	// Kind is our human-readable classification derived from ifType +
	// name prefix. One of: "physical", "vlan", "loopback", "tunnel",
	// "lag", "mgmt", "other". Lets the UI hide logical interfaces by
	// default and give an accurate "physical port count" for a switch.
	Kind string `json:"kind,omitempty"`

	// IsUplink is a heuristic flag: true when this looks like an
	// inter-switch trunk (high speed relative to access ports, alias
	// contains "uplink"/"trunk", or LLDP shows a switch on the other
	// end). The poller sets it from a combination of speed + alias;
	// LLDP cross-referencing is layered on at persist time.
	IsUplink    bool   `json:"isUplink,omitempty"`
	MTU         int32  `json:"mtu,omitempty"`
	SpeedBps    uint64 `json:"speedBps,omitempty"` // ifHighSpeed × 1e6
	MAC         string `json:"mac,omitempty"`      // ifPhysAddress
	AdminUp     bool   `json:"adminUp"`
	OperUp      bool   `json:"operUp"`
	LastChangeS int64  `json:"lastChangeSeconds,omitempty"` // sysUpTime - ifLastChange

	// HC counters — 64-bit. Zero means "not reported by the device";
	// gigantic values (close to MaxUint64) usually mean a bug, but we
	// still store what the device gave us.
	InOctets    uint64 `json:"inOctets"`
	OutOctets   uint64 `json:"outOctets"`
	InUcast     uint64 `json:"inUcast,omitempty"`
	OutUcast    uint64 `json:"outUcast,omitempty"`
	InErrors    uint64 `json:"inErrors,omitempty"`
	OutErrors   uint64 `json:"outErrors,omitempty"`
	InDiscards  uint64 `json:"inDiscards,omitempty"`
	OutDiscards uint64 `json:"outDiscards,omitempty"`

	// Computed deltas — populated by the poller, not by the SNMP
	// collector. nil means "this is the first poll for this index, no
	// rate yet" (so the UI can render "—" instead of 0).
	InBps  *uint64 `json:"inBps,omitempty"`
	OutBps *uint64 `json:"outBps,omitempty"`
}

// Entity is one row of ENTITY-MIB::entPhysicalTable. We only keep the
// containers that look like chassis/modules/PSUs/fans (excluding the
// thousands of "port slot" entries that don't add operator value).
type Entity struct {
	Index       int32  `json:"index"`
	Class       int32  `json:"class"` // 3=chassis, 6=power, 7=fan, 9=module, …
	Description string `json:"description"`
	Name        string `json:"name,omitempty"`
	HardwareRev string `json:"hardwareRev,omitempty"`
	FirmwareRev string `json:"firmwareRev,omitempty"`
	SoftwareRev string `json:"softwareRev,omitempty"`
	Serial      string `json:"serial,omitempty"`
	ModelName   string `json:"modelName,omitempty"`
}

// LLDP is one neighbor row from LLDP-MIB::lldpRemTable. Local port →
// remote chassis/port. Drives the future topology view.
type LLDP struct {
	LocalIfIndex    int32  `json:"localIfIndex"`
	LocalPort       string `json:"localPort,omitempty"`
	RemoteSysName   string `json:"remoteSysName,omitempty"`
	RemoteSysDescr  string `json:"remoteSysDescr,omitempty"`
	RemotePortID    string `json:"remotePortId,omitempty"`
	RemotePortDescr string `json:"remotePortDescr,omitempty"`
	RemoteChassisID string `json:"remoteChassisId,omitempty"`

	// RemoteCaps is the lldpRemSysCapEnabled bitmap when the device
	// reported it. Bit 5 (0x20) = telephone, bit 1 (0x02) = bridge,
	// bit 2 (0x04) = router. The topology view uses this to filter
	// out IP phones by default.
	RemoteCaps int32 `json:"remoteCapabilities,omitempty"`
}

// CDP is one neighbor row from CISCO-CDP-MIB::cdpCacheTable. Cisco gear
// almost universally has CDP on by default, while LLDP is often left
// disabled — so for a Cisco-heavy estate CDP is the more reliable
// topology source. Conceptually this is the same shape as LLDP and the
// /topology API merges both lists into a single edge set, deduping by
// (localIfIndex, remoteSysName).
type CDP struct {
	LocalIfIndex   int32  `json:"localIfIndex"`
	RemoteSysName  string `json:"remoteSysName,omitempty"`      // cdpCacheDeviceId
	RemotePortID   string `json:"remotePortId,omitempty"`       // cdpCacheDevicePort
	RemoteAddress  string `json:"remoteAddress,omitempty"`      // cdpCacheAddress (IPv4 dotted-quad when v4)
	RemotePlatform string `json:"remotePlatform,omitempty"`     // cdpCachePlatform
	RemoteVersion  string `json:"remoteVersion,omitempty"`      // cdpCacheVersion
	RemoteCaps     int32  `json:"remoteCapabilities,omitempty"` // cdpCacheCapabilities (bitmask)
}
