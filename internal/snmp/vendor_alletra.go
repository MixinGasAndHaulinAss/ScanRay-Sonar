// Package snmp — HPE Alletra (formerly Nimble) volume capacity collector.
//
// Nimble's enterprise OID is .1.3.6.1.4.1.37447. Capacity values are
// reported as 64-bit kilobyte counts split into two 32-bit halves
// (low + high), so we have to combine them manually.
//
//	.37447.1.2.1 — volTable rows:
//	    .3.{idx}  volName            (STRING)
//	    .4.{idx}  volSizeLow         (Gauge32, KiB low 32 bits)
//	    .5.{idx}  volSizeHigh        (Gauge32, KiB high 32 bits)
//	    .6.{idx}  volUsageLow        (Gauge32, KiB low)
//	    .7.{idx}  volUsageHigh       (Gauge32, KiB high)
//	    .10.{idx} volOnline          (1 = online)
//	.37447.1.4 — global stats:
//	    .1.0     globalVolCount
//	    .2.0     globalSnapCount
package snmp

import "context"

const (
	oidAlletraVolName     = "1.3.6.1.4.1.37447.1.2.1.3"
	oidAlletraVolSizeLow  = "1.3.6.1.4.1.37447.1.2.1.4"
	oidAlletraVolSizeHigh = "1.3.6.1.4.1.37447.1.2.1.5"
	oidAlletraVolUseLow   = "1.3.6.1.4.1.37447.1.2.1.6"
	oidAlletraVolUseHigh  = "1.3.6.1.4.1.37447.1.2.1.7"
	oidAlletraVolOnline   = "1.3.6.1.4.1.37447.1.2.1.10"

	oidAlletraGlobalVolCount  = "1.3.6.1.4.1.37447.1.4.1.0"
	oidAlletraGlobalSnapCount = "1.3.6.1.4.1.37447.1.4.2.0"
)

// CollectAlletra walks the volume table and combines the high/low
// 32-bit halves into byte totals. Returns nil if neither the global
// scalars nor the table produced anything.
func CollectAlletra(_ context.Context, c *Client) *AlletraHealth {
	scalars, _ := c.Get([]string{oidAlletraGlobalVolCount, oidAlletraGlobalSnapCount})

	h := &AlletraHealth{}
	if v, ok := scalars[oidAlletraGlobalVolCount]; ok {
		if n, ok2 := v.Int64(); ok2 {
			h.GlobalVolCount = &n
		}
	}
	if v, ok := scalars[oidAlletraGlobalSnapCount]; ok {
		if n, ok2 := v.Int64(); ok2 {
			h.GlobalSnapCount = &n
		}
	}

	type volAcc struct {
		name                                  string
		sizeLow, sizeHigh, useLow, useHigh    uint64
		online                                bool
		onlinePresent                         bool
	}
	rows := map[int32]*volAcc{}
	row := func(idx int32) *volAcc {
		if r := rows[idx]; r != nil {
			return r
		}
		r := &volAcc{}
		rows[idx] = r
		return r
	}
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
	walk(oidAlletraVolName, func(idx int32, v Value) { row(idx).name = v.String() })
	walk(oidAlletraVolSizeLow, func(idx int32, v Value) {
		if n, ok := v.Uint64(); ok {
			row(idx).sizeLow = n
		}
	})
	walk(oidAlletraVolSizeHigh, func(idx int32, v Value) {
		if n, ok := v.Uint64(); ok {
			row(idx).sizeHigh = n
		}
	})
	walk(oidAlletraVolUseLow, func(idx int32, v Value) {
		if n, ok := v.Uint64(); ok {
			row(idx).useLow = n
		}
	})
	walk(oidAlletraVolUseHigh, func(idx int32, v Value) {
		if n, ok := v.Uint64(); ok {
			row(idx).useHigh = n
		}
	})
	walk(oidAlletraVolOnline, func(idx int32, v Value) {
		if n, ok := v.Int64(); ok {
			row(idx).online = n == 1
			row(idx).onlinePresent = true
		}
	})

	for idx, r := range rows {
		// Combine to bytes: high<<32 + low gives KiB; ×1024 → bytes.
		sizeKB := (r.sizeHigh << 32) | r.sizeLow
		useKB := (r.useHigh << 32) | r.useLow
		v := AlletraVolume{
			Index:      idx,
			Name:       r.name,
			SizeBytes:  sizeKB * 1024,
			UsageBytes: useKB * 1024,
			Online:     r.online || !r.onlinePresent, // assume online if MIB doesn't say
		}
		if v.SizeBytes > 0 {
			v.UsedPct = 100.0 * float64(v.UsageBytes) / float64(v.SizeBytes)
		}
		h.Volumes = append(h.Volumes, v)
	}
	if h.GlobalVolCount == nil && h.GlobalSnapCount == nil && len(h.Volumes) == 0 {
		return nil
	}
	return h
}
