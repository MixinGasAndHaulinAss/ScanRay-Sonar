package api

import "github.com/NCLGISA/ScanRay-Sonar/internal/snmp"

// addVendorMetricsToPayload wraps snmp.AddVendorMetricsToPayload.
func addVendorMetricsToPayload(p map[string]any, snap *snmp.Snapshot) {
	snmp.AddVendorMetricsToPayload(p, snap)
}
