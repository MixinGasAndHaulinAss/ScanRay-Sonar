// Package snmp — UPS health collector.
//
// Two MIB families are useful here:
//   - RFC1628 UPS-MIB (.1.3.6.1.2.1.33.*) — works on any RFC-compliant
//     UPS regardless of vendor.
//   - APC enterprise (.1.3.6.1.4.1.318.*) — fills in the operationally
//     useful fields RFC1628 omits (battery replace indicator, last
//     transfer cause, output load %, manufacture date for age-based
//     replacement scheduling).
//
// Every field is optional. A vendor that exposes only the standard
// table still produces a useful UPSHealth; APC's NMC produces both.
package snmp

import "context"

const (
	// RFC1628 UPS-MIB scalars
	oidUPSIdentModel          = "1.3.6.1.2.1.33.1.1.2.0"
	oidUPSIdentAgentSWVersion = "1.3.6.1.2.1.33.1.1.3.0"
	oidUPSBatteryStatus       = "1.3.6.1.2.1.33.1.2.1.0"
	oidUPSEstMinutesRemaining = "1.3.6.1.2.1.33.1.2.3.0"
	oidUPSEstChargeRemaining  = "1.3.6.1.2.1.33.1.2.4.0"
	oidUPSBatteryTemperature  = "1.3.6.1.2.1.33.1.2.7.0"
	oidUPSInputVoltage        = "1.3.6.1.2.1.33.1.3.3.1.3.1"
	oidUPSOutputPercentLoad   = "1.3.6.1.2.1.33.1.4.4.1.5.1"

	// APC enterprise (.1.3.6.1.4.1.318.1.1.1)
	oidAPCIdentModel              = "1.3.6.1.4.1.318.1.1.1.1.1.1.0"
	oidAPCAdvIdentManufactureDate = "1.3.6.1.4.1.318.1.1.1.1.2.2.0"
	oidAPCAdvIdentSerialNumber    = "1.3.6.1.4.1.318.1.1.1.1.2.3.0"
	oidAPCAdvBatteryReplaceDate   = "1.3.6.1.4.1.318.1.1.1.2.1.3.0"
	oidAPCAdvBatteryReplaceInd    = "1.3.6.1.4.1.318.1.1.1.2.2.4.0"
	oidAPCAdvInputLineFailCause   = "1.3.6.1.4.1.318.1.1.1.3.2.5.0"
	oidAPCBasicOutputStatus       = "1.3.6.1.4.1.318.1.1.1.4.1.1.0"
	oidAPCAdvOutputVoltage        = "1.3.6.1.4.1.318.1.1.1.4.2.1.0"
	oidAPCAdvOutputLoadPct        = "1.3.6.1.4.1.318.1.1.1.4.2.3.0"
)

// CollectUPS does one GET of all known UPS scalars and assembles a
// UPSHealth. Returns nil when nothing came back (treats the vendor
// dispatch as best-effort: a misclassified appliance won't fail).
func CollectUPS(_ context.Context, c *Client) *UPSHealth {
	res, err := c.Get([]string{
		oidUPSIdentModel,
		oidUPSIdentAgentSWVersion,
		oidUPSBatteryStatus,
		oidUPSEstMinutesRemaining,
		oidUPSEstChargeRemaining,
		oidUPSBatteryTemperature,
		oidUPSInputVoltage,
		oidUPSOutputPercentLoad,
		oidAPCIdentModel,
		oidAPCAdvIdentManufactureDate,
		oidAPCAdvIdentSerialNumber,
		oidAPCAdvBatteryReplaceDate,
		oidAPCAdvBatteryReplaceInd,
		oidAPCAdvInputLineFailCause,
		oidAPCBasicOutputStatus,
		oidAPCAdvOutputVoltage,
		oidAPCAdvOutputLoadPct,
	})
	if err != nil || len(res) == 0 {
		return nil
	}
	h := &UPSHealth{}
	if v, ok := res[oidUPSIdentModel]; ok {
		h.Model = v.String()
	}
	if v, ok := res[oidAPCIdentModel]; ok && h.Model == "" {
		h.Model = v.String()
	}
	if v, ok := res[oidUPSIdentAgentSWVersion]; ok {
		h.FirmwareVersion = v.String()
	}
	if v, ok := res[oidAPCAdvIdentSerialNumber]; ok {
		h.SerialNumber = v.String()
	}
	if v, ok := res[oidAPCAdvIdentManufactureDate]; ok {
		h.ManufactureDate = v.String()
	}
	if v, ok := res[oidAPCAdvBatteryReplaceDate]; ok {
		h.BatteryReplaceDate = v.String()
	}
	if v, ok := res[oidUPSBatteryStatus]; ok {
		if n, ok2 := v.Int64(); ok2 {
			n32 := int32(n)
			h.BatteryStatus = &n32
		}
	}
	if v, ok := res[oidAPCAdvBatteryReplaceInd]; ok {
		if n, ok2 := v.Int64(); ok2 {
			b := n == 2 // 2 = batteryNeedsReplacing
			h.BatteryReplaceNeeded = &b
		}
	}
	// Output status: prefer APC (more reliable on the fleet); fall
	// back to nothing because RFC1628 doesn't define an equivalent.
	if v, ok := res[oidAPCBasicOutputStatus]; ok {
		if n, ok2 := v.Int64(); ok2 {
			n32 := int32(n)
			h.OutputStatus = &n32
		}
	}
	if v, ok := res[oidAPCAdvInputLineFailCause]; ok {
		if n, ok2 := v.Int64(); ok2 {
			n32 := int32(n)
			h.InputLineFailCause = &n32
		}
	}
	if v, ok := res[oidUPSEstMinutesRemaining]; ok {
		if n, ok2 := v.Int64(); ok2 {
			n32 := int32(n)
			h.EstRuntimeMin = &n32
		}
	}
	if v, ok := res[oidUPSEstChargeRemaining]; ok {
		if n, ok2 := v.Int64(); ok2 {
			n32 := int32(n)
			h.EstChargePct = &n32
		}
	}
	if v, ok := res[oidUPSBatteryTemperature]; ok {
		if n, ok2 := v.Int64(); ok2 {
			f := float64(n)
			h.BatteryTempC = &f
		}
	}
	// Output load % — APC's advanced reading is more reliable than
	// the standard table on multi-output APC NMCs.
	if v, ok := res[oidAPCAdvOutputLoadPct]; ok {
		if n, ok2 := v.Int64(); ok2 {
			n32 := int32(n)
			h.OutputLoadPct = &n32
		}
	}
	if h.OutputLoadPct == nil {
		if v, ok := res[oidUPSOutputPercentLoad]; ok {
			if n, ok2 := v.Int64(); ok2 {
				n32 := int32(n)
				h.OutputLoadPct = &n32
			}
		}
	}
	if v, ok := res[oidUPSInputVoltage]; ok {
		if n, ok2 := v.Int64(); ok2 {
			f := float64(n)
			h.InputVoltage = &f
		}
	}
	if v, ok := res[oidAPCAdvOutputVoltage]; ok {
		if n, ok2 := v.Int64(); ok2 {
			f := float64(n)
			h.OutputVoltage = &f
		}
	}
	return h
}
