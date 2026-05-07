// Package snmp — unified vendor-dispatch entry point.
//
// Before this file existed there were two parallel `switch vendor`
// blocks (one in internal/poller/scheduler.go pollOnce and one in
// internal/collector/snmp_poller.go pollOne) that had to stay in
// sync. Adding a vendor meant editing two files and remembering to
// register it in both. CollectVendor centralises the dispatch so
// future additions go in one place.
package snmp

import (
	"context"
	"strings"
)

// CollectVendor populates the Snapshot's Vendor sub-struct based on
// the appliances.vendor enum value. It also layers vendor-specific
// chassis data into snap.Chassis where there's a clean one-to-one
// (Cisco CPU/mem). Pure best-effort: a non-Cisco device misclassified
// as cisco still gets sane partial data plus a CollectionWarnings
// entry.
//
// Caller convention: invoke after CollectAll. The function may set
// snap.Chassis fields too (Cisco), so it must run before persistence
// reads them.
func CollectVendor(ctx context.Context, c *Client, vendor string, snap *Snapshot) {
	v := strings.ToLower(strings.TrimSpace(vendor))
	if v == "" {
		return
	}
	if snap.Vendor == nil {
		snap.Vendor = &VendorHealth{}
	}

	switch v {
	case "cisco":
		// Universal-shape chassis CPU/mem (already-existing path).
		snap.Chassis = CollectCiscoChassis(ctx, c)
		// Cisco-only extras (per-window CPU + VLANs).
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

	// If nothing landed in Vendor, drop the empty wrapper so the
	// snapshot JSON stays clean.
	if snap.Vendor != nil &&
		snap.Vendor.UPS == nil &&
		snap.Vendor.Synology == nil &&
		snap.Vendor.PaloAlto == nil &&
		snap.Vendor.Alletra == nil &&
		snap.Vendor.Cisco == nil {
		snap.Vendor = nil
	}
}
