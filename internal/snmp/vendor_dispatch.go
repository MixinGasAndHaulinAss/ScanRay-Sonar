package snmp

import (
	"context"
	"strings"

	"github.com/NCLGISA/ScanRay-Sonar/internal/snmp/oidpack"
)

// CollectVendor populates the Snapshot's Vendor sub-struct based on
// the appliances.vendor enum value. It also layers vendor-specific
// chassis data into snap.Chassis where there's a clean one-to-one
// (Cisco CPU/mem). Pure best-effort: a non-Cisco device misclassified
// as cisco still gets sane partial data plus a CollectionWarnings
// entry.
//
// After typed collectors, CollectOIDPacks supplements Vendor.OIDMetrics
// from the embedded OID pack catalog (selected by vendor + sysObjectID).
//
// Caller convention: invoke after CollectAll. The function may set
// snap.Chassis fields too (Cisco), so it must run before persistence
// reads them.
func CollectVendor(ctx context.Context, c *Client, vendor string, snap *Snapshot) {
	v := strings.ToLower(strings.TrimSpace(vendor))
	if snap.Vendor == nil {
		snap.Vendor = &VendorHealth{}
	}

	if v != "" {
		switch v {
		case "cisco":
			snap.Chassis = CollectCiscoChassis(ctx, c)
			if extras := CollectCiscoExtras(ctx, c); extras != nil {
				snap.Vendor.Cisco = extras
			}
		case "ups", "ups-apc", "ups-generic", "apc":
			if h := CollectUPS(ctx, c); h != nil {
				snap.Vendor.UPS = h
			}
		case "synology":
			if h := CollectSynology(ctx, c); h != nil {
				snap.Vendor.Synology = h
			}
		case "paloalto", "palo-alto", "panw":
			if h := CollectPaloAlto(ctx, c); h != nil {
				snap.Vendor.PaloAlto = h
			}
		case "alletra", "nimble", "hpe-alletra":
			if h := CollectAlletra(ctx, c); h != nil {
				snap.Vendor.Alletra = h
			}
		}
	}

	_ = oidpack.Load()
	CollectOIDPacks(ctx, c, vendor, snap)

	if snap.Vendor != nil &&
		snap.Vendor.UPS == nil &&
		snap.Vendor.Synology == nil &&
		snap.Vendor.PaloAlto == nil &&
		snap.Vendor.Alletra == nil &&
		snap.Vendor.Cisco == nil &&
		len(snap.Vendor.OIDMetrics) == 0 {
		snap.Vendor = nil
	}
}
