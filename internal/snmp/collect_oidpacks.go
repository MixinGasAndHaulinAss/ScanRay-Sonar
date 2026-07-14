package snmp

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/NCLGISA/ScanRay-Sonar/internal/snmp/oidpack"
)

const (
	oidPackMaxTableRows   = 200
	oidPackPerPackTimeout = 8 * time.Second
	oidPackMaxMetricsGet  = 80
)

// CollectOIDPacks selects and runs OID packs for this device into
// snap.Vendor.OIDMetrics.
func CollectOIDPacks(ctx context.Context, c *Client, vendor string, snap *Snapshot) {
	if snap == nil || c == nil {
		return
	}
	selected := oidpack.Select(vendor, snap.System.ObjectID)
	if len(selected) == 0 {
		return
	}
	var metrics []OIDMetric
	for _, p := range selected {
		ms, warns := collectOIDPack(ctx, c, p)
		snap.CollectionWarnings = append(snap.CollectionWarnings, warns...)
		metrics = append(metrics, ms...)
	}
	if len(metrics) == 0 {
		return
	}
	if snap.Vendor == nil {
		snap.Vendor = &VendorHealth{}
	}
	snap.Vendor.OIDMetrics = metrics
}

func collectOIDPack(ctx context.Context, c *Client, p oidpack.Pack) ([]OIDMetric, []string) {
	pctx, cancel := context.WithTimeout(ctx, oidPackPerPackTimeout)
	defer cancel()

	var out []OIDMetric
	var warnings []string
	gets := 0
	walked := map[string]bool{}

	for _, m := range p.Metrics {
		select {
		case <-pctx.Done():
			warnings = append(warnings, "oidpack "+p.ID+": timed out")
			return out, warnings
		default:
		}
		mode := strings.ToLower(m.Mode)
		if mode == "walk" {
			root := oidWalkRoot(m.OID)
			if walked[root] {
				continue
			}
			walked[root] = true
			rows, err := c.BulkWalk(root)
			if err != nil {
				warnings = append(warnings, "oidpack "+p.ID+" walk "+root+": "+err.Error())
				continue
			}
			n := 0
			for _, row := range rows {
				if n >= oidPackMaxTableRows {
					break
				}
				idx := strings.TrimPrefix(row.OID, root)
				idx = strings.TrimPrefix(idx, ".")
				val, ok := oidNumeric(row.Value)
				if !ok {
					continue
				}
				val *= oidScale(m.Scale)
				key := m.Key
				if idx != "" {
					key = m.Key + "." + strings.ReplaceAll(idx, ".", "_")
				}
				text := ""
				if m.EnumMap != "" {
					text = oidpack.EnumLookup(m.EnumMap, val)
				}
				out = append(out, OIDMetric{
					PackID: p.ID, Key: key, Value: val, Text: text, Unit: m.Unit, Label: m.Label,
				})
				n++
			}
			continue
		}
		if gets >= oidPackMaxMetricsGet {
			continue
		}
		vars, err := c.Get([]string{m.OID})
		gets++
		if err != nil || len(vars) == 0 {
			continue
		}
		v, ok := vars[m.OID]
		if !ok {
			for _, vv := range vars {
				v = vv
				ok = true
				break
			}
		}
		if !ok {
			continue
		}
		val, nOk := oidNumeric(v)
		if !nOk {
			s := v.String()
			if s == "" {
				continue
			}
			out = append(out, OIDMetric{
				PackID: p.ID, Key: m.Key, Value: 0, Text: s, Unit: m.Unit, Label: m.Label,
			})
			continue
		}
		val *= oidScale(m.Scale)
		text := ""
		if m.EnumMap != "" {
			text = oidpack.EnumLookup(m.EnumMap, val)
		}
		out = append(out, OIDMetric{
			PackID: p.ID, Key: m.Key, Value: val, Text: text, Unit: m.Unit, Label: m.Label,
		})
	}
	return out, warnings
}

func oidWalkRoot(oid string) string {
	oid = strings.TrimPrefix(oid, ".")
	if strings.HasSuffix(oid, ".0") {
		return strings.TrimSuffix(oid, ".0")
	}
	return oid
}

func oidScale(s float64) float64 {
	if s == 0 {
		return 1
	}
	return s
}

func oidNumeric(v Value) (float64, bool) {
	if i, ok := v.Int64(); ok {
		return float64(i), true
	}
	s := strings.TrimSpace(v.String())
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) {
		return 0, false
	}
	return f, true
}
