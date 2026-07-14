// Package oidpack loads Sonar-native SNMP OID pack definitions (JSON).
// Collection against a live SNMP client lives in package snmp so we
// avoid an import cycle (snmp → oidpack → snmp).
//
// Packs are generated offline by scripts/build-oidpacks.py from a local
// analysis CSV; runtime code never references third-party product names.
package oidpack

import (
	"encoding/json"
	"embed"
	"fmt"
	"path"
	"strings"
	"sync"
)

//go:embed data/*.json data/enums/*.json
var packFS embed.FS

// Metric is one OID definition inside a pack.
type Metric struct {
	Key       string  `json:"key"`
	OID       string  `json:"oid"`
	Mode      string  `json:"mode"` // get | walk
	ValueKind string  `json:"valueKind"`
	Scale     float64 `json:"scale"`
	Unit      string  `json:"unit"`
	Label     string  `json:"label"`
	EnumMap   string  `json:"enumMap,omitempty"`
	Alarm     *Alarm  `json:"alarm,omitempty"`
}

// Alarm is an optional default threshold carried on a metric.
type Alarm struct {
	Op        string  `json:"op"`
	Value     float64 `json:"value"`
	Severity  string  `json:"severity"`
	Name      string  `json:"name"`
	FlatField string  `json:"flatField"`
}

// Pack is one vendor/family OID catalog.
type Pack struct {
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	Enterprises       []string `json:"enterprises"`
	VendorAliases     []string `json:"vendorAliases"`
	SysObjectPrefixes []string `json:"sysObjectPrefixes"`
	Metrics           []Metric `json:"metrics"`
}

type enumMap struct {
	ID     string            `json:"id"`
	Values map[string]string `json:"values"`
}

var (
	loadOnce sync.Once
	packs    []Pack
	enums    map[string]map[string]string
	loadErr  error
)

// Load reads embedded packs (idempotent).
func Load() error {
	loadOnce.Do(func() {
		enums = map[string]map[string]string{}
		entries, err := packFS.ReadDir("data/enums")
		if err == nil {
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
					continue
				}
				b, err := packFS.ReadFile(path.Join("data/enums", e.Name()))
				if err != nil {
					continue
				}
				var em enumMap
				if json.Unmarshal(b, &em) == nil && em.ID != "" {
					enums[em.ID] = em.Values
				}
			}
		}
		files, err := packFS.ReadDir("data")
		if err != nil {
			loadErr = err
			return
		}
		for _, f := range files {
			name := f.Name()
			if f.IsDir() || name == "index.json" || name == "alarms.json" || !strings.HasSuffix(name, ".json") {
				continue
			}
			b, err := packFS.ReadFile(path.Join("data", name))
			if err != nil {
				loadErr = err
				return
			}
			var p Pack
			if err := json.Unmarshal(b, &p); err != nil {
				loadErr = fmt.Errorf("oidpack %s: %w", name, err)
				return
			}
			if p.ID == "" {
				continue
			}
			packs = append(packs, p)
		}
	})
	return loadErr
}

// EnumLookup returns a human label for an enum map + numeric value.
func EnumLookup(enumID string, value float64) string {
	_ = Load()
	em := enums[enumID]
	if em == nil {
		return ""
	}
	return em[fmt.Sprintf("%.0f", value)]
}

// AlarmableFlatFields returns flatField → metric key for NATS flattening.
func AlarmableFlatFields() map[string]string {
	_ = Load()
	out := map[string]string{}
	for _, p := range packs {
		for _, m := range p.Metrics {
			if m.Alarm != nil && m.Alarm.FlatField != "" {
				out[m.Alarm.FlatField] = m.Key
			}
		}
	}
	return out
}

// Select returns packs that match appliances.vendor and/or sysObjectID.
func Select(vendor, sysObjectID string) []Pack {
	_ = Load()
	v := strings.ToLower(strings.TrimSpace(vendor))
	oid := strings.TrimPrefix(strings.TrimSpace(sysObjectID), ".")
	var out []Pack
	seen := map[string]bool{}
	for _, p := range packs {
		if !matchPack(p, v, oid) || seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		out = append(out, p)
	}
	return out
}

func matchPack(p Pack, vendor, sysObjectID string) bool {
	if vendor != "" {
		for _, a := range p.VendorAliases {
			if strings.EqualFold(a, vendor) {
				return true
			}
		}
	}
	if sysObjectID != "" {
		for _, pref := range p.SysObjectPrefixes {
			pref = strings.TrimPrefix(pref, ".")
			if pref != "" && strings.HasPrefix(sysObjectID, pref) {
				return true
			}
		}
		for _, ent := range p.Enterprises {
			if ent == "" {
				continue
			}
			prefix := "1.3.6.1.4.1." + ent
			if strings.HasPrefix(sysObjectID, prefix) {
				if p.ID == "linux_net" || p.ID == "host_resources" {
					continue
				}
				return true
			}
		}
	}
	return false
}

// Packs returns all loaded packs (for tests / diagnostics).
func Packs() []Pack {
	_ = Load()
	return packs
}
