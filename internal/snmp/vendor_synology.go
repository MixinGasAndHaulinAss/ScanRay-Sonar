// Package snmp — Synology NAS health collector for the .1.3.6.1.4.1.6574
// enterprise tree (RackStation/DiskStation lineup).
//
// Three sub-trees:
//   - .6574.1   — system scalars (status, temperature, model, DSM version)
//   - .6574.2.1 — per-disk table
//   - .6574.3.1 — RAID volume table
//
// Walks rather than GETs the disk + RAID tables because the row count
// varies (12+ disks on a fully-loaded RS3617xs+, but a DS220+ has 2).
package snmp

import "context"

const (
	// System scalars
	oidSynoSystemStatus = "1.3.6.1.4.1.6574.1.1.0"
	oidSynoSystemTemp   = "1.3.6.1.4.1.6574.1.2.0"
	oidSynoPowerStatus  = "1.3.6.1.4.1.6574.1.3.0"
	oidSynoModelName    = "1.3.6.1.4.1.6574.1.5.1.0"
	oidSynoSerialNumber = "1.3.6.1.4.1.6574.1.5.2.0"
	oidSynoDSMVersion   = "1.3.6.1.4.1.6574.1.5.3.0"

	// Disk table (.6574.2.1.1)
	oidSynoDiskID     = "1.3.6.1.4.1.6574.2.1.1.2"
	oidSynoDiskModel  = "1.3.6.1.4.1.6574.2.1.1.3"
	oidSynoDiskType   = "1.3.6.1.4.1.6574.2.1.1.4"
	oidSynoDiskStatus = "1.3.6.1.4.1.6574.2.1.1.5"
	oidSynoDiskTemp   = "1.3.6.1.4.1.6574.2.1.1.6"

	// RAID table (.6574.3.1.1)
	oidSynoRAIDName   = "1.3.6.1.4.1.6574.3.1.1.2"
	oidSynoRAIDStatus = "1.3.6.1.4.1.6574.3.1.1.3"
)

// CollectSynology assembles a SynologyHealth from the system, disks
// and RAID sub-trees. Returns nil on a non-Synology device (everything
// would come back NoSuchObject).
func CollectSynology(_ context.Context, c *Client) *SynologyHealth {
	scalars, err := c.Get([]string{
		oidSynoSystemStatus, oidSynoSystemTemp, oidSynoPowerStatus,
		oidSynoModelName, oidSynoSerialNumber, oidSynoDSMVersion,
	})
	if err != nil || len(scalars) == 0 {
		return nil
	}

	h := &SynologyHealth{}
	if v, ok := scalars[oidSynoModelName]; ok {
		h.Model = v.String()
	}
	if v, ok := scalars[oidSynoSerialNumber]; ok {
		h.Serial = v.String()
	}
	if v, ok := scalars[oidSynoDSMVersion]; ok {
		h.DSMVersion = v.String()
	}
	if v, ok := scalars[oidSynoSystemStatus]; ok {
		if n, ok2 := v.Int64(); ok2 {
			n32 := int32(n)
			h.SystemStatus = &n32
		}
	}
	if v, ok := scalars[oidSynoPowerStatus]; ok {
		if n, ok2 := v.Int64(); ok2 {
			n32 := int32(n)
			h.PowerStatus = &n32
		}
	}
	if v, ok := scalars[oidSynoSystemTemp]; ok {
		if n, ok2 := v.Int64(); ok2 {
			f := float64(n)
			h.TempC = &f
		}
	}

	// Disk walk — collect by index suffix (a single integer).
	disks := map[int32]*SynologyDisk{}
	walkInto := func(root string, fn func(idx int32, v Value)) {
		vars, err := c.BulkWalk(root)
		if err != nil {
			return
		}
		for _, kv := range vars {
			idx := lastIndex(kv.OID)
			if idx == 0 {
				continue
			}
			fn(idx, kv.Value)
		}
	}
	disk := func(idx int32) *SynologyDisk {
		if d := disks[idx]; d != nil {
			return d
		}
		d := &SynologyDisk{Index: idx}
		disks[idx] = d
		return d
	}
	walkInto(oidSynoDiskID, func(idx int32, v Value) { disk(idx).ID = v.String() })
	walkInto(oidSynoDiskModel, func(idx int32, v Value) { disk(idx).Model = v.String() })
	walkInto(oidSynoDiskType, func(idx int32, v Value) { disk(idx).Type = v.String() })
	walkInto(oidSynoDiskStatus, func(idx int32, v Value) {
		if n, ok := v.Int64(); ok {
			disk(idx).Status = int32(n)
		}
	})
	walkInto(oidSynoDiskTemp, func(idx int32, v Value) {
		if n, ok := v.Int64(); ok {
			disk(idx).TempC = float64(n)
		}
	})
	for _, d := range disks {
		h.Disks = append(h.Disks, *d)
	}

	// RAID walk
	vols := map[int32]*SynologyRAIDVolume{}
	vol := func(idx int32) *SynologyRAIDVolume {
		if v := vols[idx]; v != nil {
			return v
		}
		v := &SynologyRAIDVolume{Index: idx}
		vols[idx] = v
		return v
	}
	walkInto(oidSynoRAIDName, func(idx int32, v Value) { vol(idx).Name = v.String() })
	walkInto(oidSynoRAIDStatus, func(idx int32, v Value) {
		if n, ok := v.Int64(); ok {
			vol(idx).Status = int32(n)
		}
	})
	for _, v := range vols {
		h.Volumes = append(h.Volumes, *v)
	}
	return h
}
