// Package snmp — collectors that turn SNMP walks into Snapshot.
//
// Each Collect* function returns its piece of the snapshot plus a
// non-fatal warning string (or "") so a partial failure on one MIB
// (a vendor that doesn't expose ENTITY-MIB, say) doesn't fail the
// whole poll.
//
// All OIDs are written as numeric strings, not MIB names, so the
// collectors don't need a MIB compiler at runtime.
package snmp

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// CollectAll runs every standard collector and assembles a Snapshot.
// The vendor-specific chassis collector is dispatched separately by
// the caller (it needs the appliance vendor row to know which adapter
// to use). System and Interfaces are mandatory; Entity and LLDP are
// best-effort and any error becomes a CollectionWarnings entry.
func CollectAll(ctx context.Context, c *Client) Snapshot {
	start := time.Now()
	s := Snapshot{
		SchemaVersion: 1,
		CapturedAt:    start.UTC(),
		Interfaces:    []Interface{},
	}

	if sys, err := CollectSystem(ctx, c); err != nil {
		s.CollectionWarnings = append(s.CollectionWarnings, "system: "+err.Error())
	} else {
		s.System = sys
	}

	if ifs, err := CollectInterfaces(ctx, c); err != nil {
		s.CollectionWarnings = append(s.CollectionWarnings, "interfaces: "+err.Error())
	} else {
		s.Interfaces = ifs
	}

	if ents, err := CollectEntities(ctx, c); err != nil {
		s.CollectionWarnings = append(s.CollectionWarnings, "entity: "+err.Error())
	} else {
		s.Entities = ents
	}

	if neighbors, err := CollectLLDP(ctx, c); err != nil {
		s.CollectionWarnings = append(s.CollectionWarnings, "lldp: "+err.Error())
	} else {
		s.LLDP = neighbors
	}

	s.CollectMs = time.Since(start).Milliseconds()
	return s
}

// ---------------------------------------------------------------------
// system group  (1.3.6.1.2.1.1)
// ---------------------------------------------------------------------

const (
	oidSysDescr    = "1.3.6.1.2.1.1.1.0"
	oidSysObjectID = "1.3.6.1.2.1.1.2.0"
	oidSysUpTime   = "1.3.6.1.2.1.1.3.0"
	oidSysContact  = "1.3.6.1.2.1.1.4.0"
	oidSysName     = "1.3.6.1.2.1.1.5.0"
	oidSysLocation = "1.3.6.1.2.1.1.6.0"
)

func CollectSystem(_ context.Context, c *Client) (System, error) {
	res, err := c.Get([]string{
		oidSysDescr, oidSysObjectID, oidSysUpTime,
		oidSysContact, oidSysName, oidSysLocation,
	})
	if err != nil {
		return System{}, err
	}
	sys := System{
		Description: res[oidSysDescr].String(),
		ObjectID:    cleanOID(res[oidSysObjectID].String()),
		Contact:     res[oidSysContact].String(),
		Name:        res[oidSysName].String(),
		Location:    res[oidSysLocation].String(),
	}
	if u, ok := res[oidSysUpTime].Uint64(); ok {
		sys.UptimeTicks = u
		sys.UptimeSecs = int64(u / 100) // sysUpTime is 1/100s
	}
	return sys, nil
}

// ---------------------------------------------------------------------
// IF-MIB / ifXTable
// ---------------------------------------------------------------------

const (
	oidIfTable    = "1.3.6.1.2.1.2.2"   // base
	oidIfDescr    = "1.3.6.1.2.1.2.2.1.2"
	oidIfType     = "1.3.6.1.2.1.2.2.1.3"
	oidIfMTU      = "1.3.6.1.2.1.2.2.1.4"
	oidIfPhys     = "1.3.6.1.2.1.2.2.1.6"
	oidIfAdmin    = "1.3.6.1.2.1.2.2.1.7"
	oidIfOper     = "1.3.6.1.2.1.2.2.1.8"
	oidIfLastChg  = "1.3.6.1.2.1.2.2.1.9"
	oidIfInErrs   = "1.3.6.1.2.1.2.2.1.14"
	oidIfOutErrs  = "1.3.6.1.2.1.2.2.1.20"
	oidIfInDisc   = "1.3.6.1.2.1.2.2.1.13"
	oidIfOutDisc  = "1.3.6.1.2.1.2.2.1.19"

	// ifXTable (1.3.6.1.2.1.31.1.1.1)
	oidIfName       = "1.3.6.1.2.1.31.1.1.1.1"
	oidIfHCInOctets = "1.3.6.1.2.1.31.1.1.1.6"
	oidIfHCInUcast  = "1.3.6.1.2.1.31.1.1.1.7"
	oidIfHCOutOctets = "1.3.6.1.2.1.31.1.1.1.10"
	oidIfHCOutUcast  = "1.3.6.1.2.1.31.1.1.1.11"
	oidIfHighSpeed   = "1.3.6.1.2.1.31.1.1.1.15"
	oidIfAlias       = "1.3.6.1.2.1.31.1.1.1.18"
)

// CollectInterfaces walks both ifTable and ifXTable, joining on
// ifIndex. Missing per-row attributes are tolerated (different
// vendors expose different subsets of the table — some stub-out
// ifAlias, others the HC counters on virtual interfaces).
func CollectInterfaces(_ context.Context, c *Client) ([]Interface, error) {
	rows := map[int32]*Interface{}

	walk := func(root string, fn func(idx int32, v Value)) error {
		vars, err := c.BulkWalk(root)
		if err != nil {
			return err
		}
		for _, kv := range vars {
			idx := lastIndex(kv.OID)
			if idx == 0 {
				continue
			}
			fn(idx, kv.Value)
		}
		return nil
	}

	row := func(idx int32) *Interface {
		if r := rows[idx]; r != nil {
			return r
		}
		r := &Interface{Index: idx}
		rows[idx] = r
		return r
	}

	if err := walk(oidIfDescr, func(idx int32, v Value) { row(idx).Descr = v.String() }); err != nil {
		return nil, err
	}
	_ = walk(oidIfName, func(idx int32, v Value) { row(idx).Name = v.String() })
	_ = walk(oidIfAlias, func(idx int32, v Value) { row(idx).Alias = v.String() })
	_ = walk(oidIfType, func(idx int32, v Value) {
		if n, ok := v.Int64(); ok {
			row(idx).Type = int32(n)
		}
	})
	_ = walk(oidIfMTU, func(idx int32, v Value) {
		if n, ok := v.Int64(); ok {
			row(idx).MTU = int32(n)
		}
	})
	_ = walk(oidIfPhys, func(idx int32, v Value) {
		row(idx).MAC = formatMAC(v.Bytes())
	})
	_ = walk(oidIfAdmin, func(idx int32, v Value) {
		if n, ok := v.Int64(); ok {
			row(idx).AdminUp = n == 1
		}
	})
	_ = walk(oidIfOper, func(idx int32, v Value) {
		if n, ok := v.Int64(); ok {
			row(idx).OperUp = n == 1
		}
	})
	_ = walk(oidIfLastChg, func(idx int32, v Value) {
		if n, ok := v.Uint64(); ok {
			row(idx).LastChangeS = int64(n / 100)
		}
	})
	_ = walk(oidIfHighSpeed, func(idx int32, v Value) {
		if n, ok := v.Uint64(); ok && n > 0 {
			row(idx).SpeedBps = n * 1_000_000
		}
	})

	// HC counters — these matter most. Failure is non-fatal; we still
	// return the row with operational status.
	_ = walk(oidIfHCInOctets, func(idx int32, v Value) {
		if n, ok := v.Uint64(); ok {
			row(idx).InOctets = n
		}
	})
	_ = walk(oidIfHCOutOctets, func(idx int32, v Value) {
		if n, ok := v.Uint64(); ok {
			row(idx).OutOctets = n
		}
	})
	_ = walk(oidIfHCInUcast, func(idx int32, v Value) {
		if n, ok := v.Uint64(); ok {
			row(idx).InUcast = n
		}
	})
	_ = walk(oidIfHCOutUcast, func(idx int32, v Value) {
		if n, ok := v.Uint64(); ok {
			row(idx).OutUcast = n
		}
	})
	_ = walk(oidIfInErrs, func(idx int32, v Value) {
		if n, ok := v.Uint64(); ok {
			row(idx).InErrors = n
		}
	})
	_ = walk(oidIfOutErrs, func(idx int32, v Value) {
		if n, ok := v.Uint64(); ok {
			row(idx).OutErrors = n
		}
	})
	_ = walk(oidIfInDisc, func(idx int32, v Value) {
		if n, ok := v.Uint64(); ok {
			row(idx).InDiscards = n
		}
	})
	_ = walk(oidIfOutDisc, func(idx int32, v Value) {
		if n, ok := v.Uint64(); ok {
			row(idx).OutDiscards = n
		}
	})

	out := make([]Interface, 0, len(rows))
	for _, r := range rows {
		// Fall back to ifDescr if the vendor doesn't expose ifName
		// (happens on older Aruba and most printers).
		if r.Name == "" {
			r.Name = r.Descr
		}
		out = append(out, *r)
	}
	// Sort by ifIndex for determinism.
	sortByIfIndex(out)
	return out, nil
}

// ---------------------------------------------------------------------
// ENTITY-MIB::entPhysicalTable
// ---------------------------------------------------------------------

const (
	oidEntDescr     = "1.3.6.1.2.1.47.1.1.1.1.2"
	oidEntClass     = "1.3.6.1.2.1.47.1.1.1.1.5"
	oidEntName      = "1.3.6.1.2.1.47.1.1.1.1.7"
	oidEntHWRev     = "1.3.6.1.2.1.47.1.1.1.1.8"
	oidEntFWRev     = "1.3.6.1.2.1.47.1.1.1.1.9"
	oidEntSWRev     = "1.3.6.1.2.1.47.1.1.1.1.10"
	oidEntSerial    = "1.3.6.1.2.1.47.1.1.1.1.11"
	oidEntModelName = "1.3.6.1.2.1.47.1.1.1.1.13"
)

// CollectEntities walks entPhysicalTable and returns rows whose class
// is "interesting" for an operator dashboard: chassis(3), backplane(4),
// container(5), powerSupply(6), fan(7), sensor(8), module(9). Port
// slots (entPhysicalClass=10) are filtered out — those duplicate the
// IF-MIB and would triple the snapshot size on a 48-port switch.
func CollectEntities(_ context.Context, c *Client) ([]Entity, error) {
	rows := map[int32]*Entity{}

	walk := func(root string, fn func(idx int32, v Value)) {
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
	row := func(idx int32) *Entity {
		if r := rows[idx]; r != nil {
			return r
		}
		r := &Entity{Index: idx}
		rows[idx] = r
		return r
	}

	walk(oidEntClass, func(idx int32, v Value) {
		if n, ok := v.Int64(); ok {
			row(idx).Class = int32(n)
		}
	})
	walk(oidEntDescr, func(idx int32, v Value) { row(idx).Description = v.String() })
	walk(oidEntName, func(idx int32, v Value) { row(idx).Name = v.String() })
	walk(oidEntHWRev, func(idx int32, v Value) { row(idx).HardwareRev = v.String() })
	walk(oidEntFWRev, func(idx int32, v Value) { row(idx).FirmwareRev = v.String() })
	walk(oidEntSWRev, func(idx int32, v Value) { row(idx).SoftwareRev = v.String() })
	walk(oidEntSerial, func(idx int32, v Value) { row(idx).Serial = v.String() })
	walk(oidEntModelName, func(idx int32, v Value) { row(idx).ModelName = v.String() })

	out := make([]Entity, 0, len(rows))
	for _, r := range rows {
		switch r.Class {
		case 3, 4, 5, 6, 7, 8, 9: // see comment above
			out = append(out, *r)
		}
	}
	sortByEntIndex(out)
	return out, nil
}

// ---------------------------------------------------------------------
// LLDP-MIB::lldpRemTable
// ---------------------------------------------------------------------

const (
	oidLldpRemChassisID    = "1.0.8802.1.1.2.1.4.1.1.5"
	oidLldpRemPortID       = "1.0.8802.1.1.2.1.4.1.1.7"
	oidLldpRemPortDescr    = "1.0.8802.1.1.2.1.4.1.1.8"
	oidLldpRemSysName      = "1.0.8802.1.1.2.1.4.1.1.9"
	oidLldpRemSysDescr     = "1.0.8802.1.1.2.1.4.1.1.10"
)

// CollectLLDP walks the standard lldpRemTable. The index format is
// (lldpRemTimeMark, lldpRemLocalPortNum, lldpRemIndex), so we extract
// the second component as the local ifIndex and use the third as a
// disambiguator inside our internal map.
func CollectLLDP(_ context.Context, c *Client) ([]LLDP, error) {
	type key struct {
		time, local, idx int32
	}
	rows := map[key]*LLDP{}

	walk := func(root string, fn func(k key, v Value)) {
		vars, err := c.BulkWalk(root)
		if err != nil {
			return
		}
		for _, kv := range vars {
			parts := suffixParts(kv.OID, root)
			if len(parts) < 3 {
				continue
			}
			k := key{time: parts[0], local: parts[1], idx: parts[2]}
			fn(k, kv.Value)
		}
	}
	row := func(k key) *LLDP {
		if r := rows[k]; r != nil {
			return r
		}
		r := &LLDP{LocalIfIndex: k.local}
		rows[k] = r
		return r
	}

	walk(oidLldpRemSysName, func(k key, v Value) { row(k).RemoteSysName = v.String() })
	walk(oidLldpRemSysDescr, func(k key, v Value) { row(k).RemoteSysDescr = v.String() })
	walk(oidLldpRemPortID, func(k key, v Value) { row(k).RemotePortID = v.String() })
	walk(oidLldpRemPortDescr, func(k key, v Value) { row(k).RemotePortDescr = v.String() })
	walk(oidLldpRemChassisID, func(k key, v Value) { row(k).RemoteChassisID = formatMAC(v.Bytes()) })

	out := make([]LLDP, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	return out, nil
}

// ---------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------

// lastIndex returns the final dotted component of an OID as int32.
// Returns 0 on parse failure (treated as "skip" by callers).
func lastIndex(oid string) int32 {
	i := strings.LastIndex(oid, ".")
	if i < 0 || i == len(oid)-1 {
		return 0
	}
	n, err := strconv.Atoi(oid[i+1:])
	if err != nil {
		return 0
	}
	return int32(n)
}

// suffixParts returns the dotted-int suffix of oid after the prefix
// root, or nil if oid isn't under root.
func suffixParts(oid, root string) []int32 {
	root = strings.TrimSuffix(root, ".")
	if !strings.HasPrefix(oid, root+".") {
		return nil
	}
	tail := strings.TrimPrefix(oid, root+".")
	parts := strings.Split(tail, ".")
	out := make([]int32, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		out = append(out, int32(n))
	}
	return out
}

// formatMAC renders 6 bytes as colon-separated hex. Returns "" for
// other lengths (Cisco lldpRemChassisID for IP-typed subtypes, etc.).
func formatMAC(b []byte) string {
	if len(b) != 6 {
		return ""
	}
	const hex = "0123456789abcdef"
	out := make([]byte, 0, 17)
	for i, x := range b {
		if i > 0 {
			out = append(out, ':')
		}
		out = append(out, hex[x>>4], hex[x&0x0f])
	}
	return string(out)
}

// cleanOID strips a leading "." that some agents prepend.
func cleanOID(s string) string {
	return strings.TrimPrefix(s, ".")
}

func sortByIfIndex(s []Interface) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1].Index > s[j].Index; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
func sortByEntIndex(s []Entity) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1].Index > s[j].Index; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
