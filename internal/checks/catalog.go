// Package checks loads Sonar-native synthetic check type definitions and
// runs icmp/tcp/http/dns/tls probes. Additive to SNMP OID packs, Meraki,
// and agent DEX — never replaces those paths.
package checks

import (
	"encoding/json"
	"embed"
	"fmt"
	"path"
	"strings"
	"sync"
)

//go:embed catalog/*.json
var catalogFS embed.FS

// ParamSpec describes one configurable parameter on a check type.
type ParamSpec struct {
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Required bool    `json:"required"`
	Default  any     `json:"default,omitempty"`
	Label    string  `json:"label"`
}

// DefaultAlarm is an optional seeded threshold for a channel.
type DefaultAlarm struct {
	Op        string  `json:"op"`
	Value     float64 `json:"value"`
	Severity  string  `json:"severity"`
	Name      string  `json:"name"`
	FlatField string  `json:"flatField"`
}

// Channel is one metric produced by a check run.
type Channel struct {
	Key          string        `json:"key"`
	Label        string        `json:"label"`
	ValueKind    string        `json:"valueKind"`
	DefaultAlarm *DefaultAlarm `json:"defaultAlarm,omitempty"`
}

// Pack is one check type definition.
type Pack struct {
	ID         string      `json:"id"`
	Title      string      `json:"title"`
	Mechanism  string      `json:"mechanism"`
	Runner     string      `json:"runner"` // agent | central | either
	Params     []ParamSpec `json:"params"`
	Channels   []Channel   `json:"channels"`
	SourceIDs  []string    `json:"sourceIds,omitempty"`
}

var (
	loadOnce sync.Once
	packs    []Pack
	byID     map[string]Pack
	loadErr  error
)

// Load reads embedded check packs (idempotent).
func Load() error {
	loadOnce.Do(func() {
		byID = map[string]Pack{}
		entries, err := catalogFS.ReadDir("catalog")
		if err != nil {
			loadErr = err
			return
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || name == "index.json" || !strings.HasSuffix(name, ".json") {
				continue
			}
			b, err := catalogFS.ReadFile(path.Join("catalog", name))
			if err != nil {
				loadErr = err
				return
			}
			var p Pack
			if err := json.Unmarshal(b, &p); err != nil {
				loadErr = fmt.Errorf("checks %s: %w", name, err)
				return
			}
			if p.ID == "" {
				continue
			}
			packs = append(packs, p)
			byID[p.ID] = p
		}
	})
	return loadErr
}

// Packs returns all loaded check types.
func Packs() []Pack {
	_ = Load()
	return packs
}

// Lookup returns a pack by type id.
func Lookup(typeID string) (Pack, bool) {
	_ = Load()
	p, ok := byID[typeID]
	return p, ok
}

// Sample is one channel observation from a run.
type Sample struct {
	Key    string
	Value  float64
	Text   string
	HasNum bool
}

// Result is the outcome of executing one check.
type Result struct {
	OK      bool
	Error   string
	Samples []Sample
}

// FlatFields builds alarm-env keys from pack defaultAlarm flatField → sample value.
func FlatFields(typeID string, res Result) map[string]any {
	out := map[string]any{}
	p, ok := Lookup(typeID)
	if !ok {
		for _, s := range res.Samples {
			if s.HasNum {
				out[typeID+"_"+s.Key] = s.Value
			}
		}
		return out
	}
	byKey := map[string]Sample{}
	for _, s := range res.Samples {
		byKey[s.Key] = s
	}
	for _, ch := range p.Channels {
		s, ok := byKey[ch.Key]
		if !ok || !s.HasNum {
			continue
		}
		field := typeID + "_" + ch.Key
		if ch.DefaultAlarm != nil && ch.DefaultAlarm.FlatField != "" {
			field = ch.DefaultAlarm.FlatField
		}
		out[field] = s.Value
	}
	if !res.OK {
		out[typeID+"_up"] = 0.0
	}
	return out
}
