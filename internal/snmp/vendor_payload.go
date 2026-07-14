package snmp

import (
	"strings"

	"github.com/NCLGISA/ScanRay-Sonar/internal/snmp/oidpack"
)

// AddVendorMetricsToPayload flattens snap.Vendor into a NATS
// metrics.appliance payload map. Field names are operator-friendly
// (no dotted nesting) so alarm-rule expressions stay short.
func AddVendorMetricsToPayload(p map[string]any, snap *Snapshot) {
	if snap == nil || snap.Vendor == nil {
		return
	}
	if u := snap.Vendor.UPS; u != nil {
		if u.EstChargePct != nil {
			p["battery_charge_pct"] = *u.EstChargePct
		}
		if u.EstRuntimeMin != nil {
			p["battery_runtime_min"] = *u.EstRuntimeMin
		}
		if u.OutputLoadPct != nil {
			p["ups_load_pct"] = *u.OutputLoadPct
		}
		if u.BatteryTempC != nil {
			p["battery_temp_c"] = *u.BatteryTempC
		}
		if u.BatteryStatus != nil {
			p["battery_status"] = *u.BatteryStatus
		}
		if u.OutputStatus != nil {
			p["ups_output_status"] = *u.OutputStatus
		}
		if u.BatteryReplaceNeeded != nil {
			b := 0
			if *u.BatteryReplaceNeeded {
				b = 1
			}
			p["battery_replace_needed"] = b
		}
	}
	if s := snap.Vendor.Synology; s != nil {
		if s.SystemStatus != nil {
			p["synology_system_status"] = *s.SystemStatus
		}
		if s.PowerStatus != nil {
			p["synology_power_status"] = *s.PowerStatus
		}
		if s.TempC != nil {
			p["synology_temp_c"] = *s.TempC
		}
		var worstDiskTemp float64
		var worstDiskStatus int32
		for _, d := range s.Disks {
			if d.TempC > worstDiskTemp {
				worstDiskTemp = d.TempC
			}
			if d.Status != 1 && d.Status > worstDiskStatus {
				worstDiskStatus = d.Status
			}
		}
		if worstDiskTemp > 0 {
			p["synology_disk_temp_max_c"] = worstDiskTemp
		}
		if worstDiskStatus > 0 {
			p["synology_disk_worst_status"] = worstDiskStatus
		}
		var worstRAID int32
		for _, v := range s.Volumes {
			if v.Status != 1 && v.Status > worstRAID {
				worstRAID = v.Status
			}
		}
		if worstRAID > 0 {
			p["synology_raid_worst_status"] = worstRAID
		}
	}
	if pa := snap.Vendor.PaloAlto; pa != nil {
		if pa.SessionUtilPct != nil {
			p["session_util_pct"] = *pa.SessionUtilPct
		}
		if pa.SessionActive != nil {
			p["session_active"] = *pa.SessionActive
		}
	}
	if a := snap.Vendor.Alletra; a != nil {
		var maxUsed float64
		for _, v := range a.Volumes {
			if v.UsedPct > maxUsed {
				maxUsed = v.UsedPct
			}
		}
		if maxUsed > 0 {
			p["volume_used_pct_max"] = maxUsed
		}
	}
	if c := snap.Vendor.Cisco; c != nil {
		if c.CPU5min != nil {
			p["cisco_cpu_5min_pct"] = *c.CPU5min
		}
	}

	// OID pack alarmable fields: flatField → catalog metric key.
	for flat, metricKey := range oidpack.AlarmableFlatFields() {
		for _, m := range snap.Vendor.OIDMetrics {
			if m.Key == metricKey || strings.HasPrefix(m.Key, metricKey+".") || strings.HasPrefix(m.Key, metricKey+"_") {
				p[flat] = m.Value
				break
			}
		}
	}
}
