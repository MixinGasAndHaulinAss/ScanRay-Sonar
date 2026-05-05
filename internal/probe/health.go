// health.go — cross-platform "slow" host signals that drive the
// Devices-Performance, Network-Performance, and User-Experience
// dashboards.
//
// HealthSignals is intentionally a flat struct of pointers so that:
//
//   * any sub-collector can be unimplemented on a given platform and
//     simply leave its field nil — the JSON omitempty drops it from
//     the wire, and the dashboards display "—" instead of "0";
//   * adding a new signal is one struct field and one filler line per
//     platform; the schema doesn't need a migration because the whole
//     thing rides inside the JSONB last_metrics blob.
//
// The per-OS file (health_windows.go / health_linux.go) implements
// CollectHealthSignals with that platform's data sources. health_other.go
// is a no-op for environments we don't actively support.

package probe

// HealthSignals is the per-host snapshot of "slow" telemetry, refreshed
// every 5 minutes by extras.runHealthLoop.
type HealthSignals struct {
	BatteryHealthPct        *float64 `json:"batteryHealthPct,omitempty"`
	BSODCount24h            *int     `json:"bsodCount24h,omitempty"`
	UserRebootCount24h      *int     `json:"userRebootCount24h,omitempty"`
	AppCrashCount24h        *int     `json:"appCrashCount24h,omitempty"`
	EventLogErrorCount24h   *int     `json:"eventLogErrorCount24h,omitempty"`
	MissingPatchCount       *int     `json:"missingPatchCount,omitempty"`
	CPUQueueLength          *float64 `json:"cpuQueueLength,omitempty"`
	DiskQueueLength         *float64 `json:"diskQueueLength,omitempty"`
	HighloadCPUIncidents24h *int     `json:"highloadCpuIncidents24h,omitempty"`
	WiFiSSID                string   `json:"wifiSsid,omitempty"`
	WiFiRSSIdBm             *int     `json:"wifiRssiDbm,omitempty"`
	WiFiSignalPct           *int     `json:"wifiSignalPct,omitempty"`
	// LogonAvgMs / LogonMaxMs aggregate the LogonTime ms property of
	// Microsoft-Windows-Diagnostics-Performance/Operational events
	// over the past 7 days. Linux/macOS leave these nil.
	LogonAvgMs *float64 `json:"logonAvgMs,omitempty"`
	LogonMaxMs *float64 `json:"logonMaxMs,omitempty"`
	// AppLaunchMaxMs / InputDelayAvgMs are reserved for future
	// collectors. Currently always nil; the dashboards render a
	// "data not yet collected" placeholder.
	AppLaunchMaxMs  *float64 `json:"appLaunchMaxMs,omitempty"`
	InputDelayAvgMs *float64 `json:"inputDelayAvgMs,omitempty"`
	// TracerouteHops is the count of distinct hops on a TTL-ramp
	// traceroute to the primary latency target (8.8.8.8 by default).
	// Set by latency.go after the periodic ICMP run.
	TracerouteHops *int `json:"tracerouteHops,omitempty"`
	// ISPName mirrors agents.geo_org so client-side filters can group
	// without consulting a separate endpoint.
	ISPName string `json:"ispName,omitempty"`
}
