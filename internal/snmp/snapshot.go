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

	// CollectionWarnings accumulates non-fatal issues (timeouts on a
	// single MIB, unsupported OID on this vendor, etc.) so an operator
	// can see why the snapshot is partial without us failing the
	// whole poll.
	CollectionWarnings []string `json:"collectionWarnings,omitempty"`
}

// System is the standard SNMPv2-MIB::system group.
type System struct {
	Description string `json:"description"`
	Name        string `json:"name"`
	Contact     string `json:"contact,omitempty"`
	Location    string `json:"location,omitempty"`
	ObjectID    string `json:"objectId,omitempty"`     // sysObjectID — vendor enum
	UptimeTicks uint64 `json:"uptimeTicks"`            // sysUpTime in 1/100s
	UptimeSecs  int64  `json:"uptimeSeconds"`          // derived
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
	Index        int32  `json:"ifIndex"`
	Name         string `json:"name"`        // ifName (short, e.g. Gi1/0/1)
	Descr        string `json:"descr"`       // ifDescr (long)
	Alias        string `json:"alias,omitempty"` // ifAlias (operator's port description)
	Type         int32  `json:"type"`        // ifType enum (6=ethernet, 53=propVirtual, …)

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
	IsUplink bool `json:"isUplink,omitempty"`
	MTU          int32  `json:"mtu,omitempty"`
	SpeedBps     uint64 `json:"speedBps,omitempty"` // ifHighSpeed × 1e6
	MAC          string `json:"mac,omitempty"`      // ifPhysAddress
	AdminUp      bool   `json:"adminUp"`
	OperUp       bool   `json:"operUp"`
	LastChangeS  int64  `json:"lastChangeSeconds,omitempty"` // sysUpTime - ifLastChange

	// HC counters — 64-bit. Zero means "not reported by the device";
	// gigantic values (close to MaxUint64) usually mean a bug, but we
	// still store what the device gave us.
	InOctets   uint64 `json:"inOctets"`
	OutOctets  uint64 `json:"outOctets"`
	InUcast    uint64 `json:"inUcast,omitempty"`
	OutUcast   uint64 `json:"outUcast,omitempty"`
	InErrors   uint64 `json:"inErrors,omitempty"`
	OutErrors  uint64 `json:"outErrors,omitempty"`
	InDiscards uint64 `json:"inDiscards,omitempty"`
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
	LocalIfIndex int32  `json:"localIfIndex"`
	LocalPort    string `json:"localPort,omitempty"`
	RemoteSysName string `json:"remoteSysName,omitempty"`
	RemoteSysDescr string `json:"remoteSysDescr,omitempty"`
	RemotePortID  string `json:"remotePortId,omitempty"`
	RemotePortDescr string `json:"remotePortDescr,omitempty"`
	RemoteChassisID string `json:"remoteChassisId,omitempty"`
}
