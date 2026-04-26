// Package geoip wraps the MaxMind GeoLite2 City + ASN databases for
// the API server. It is intentionally thin: open two .mmdb files at
// startup, look up an IP, return a Result struct.
//
// Design choices:
//
//   - Optional. If either file is missing the reader still constructs
//     successfully and Lookup returns ok=false. The world map page and
//     the network-topology API both have to degrade anyway (the very
//     first install has nothing in the volume yet) and crashing the
//     API on a missing data file would be hostile.
//
//   - Read-only. The .mmdb files are refreshed by an external script
//     (scripts/refresh-geoip.sh) into a docker volume. The API never
//     writes them.
//
//   - No caching here. The caller (api.ingestMetrics) caches by
//     persisting the lookup result on the agents row; recomputing on
//     every request would be wasted work but caching in-process would
//     be wasted complexity.
package geoip

import (
	"errors"
	"net"
	"os"
	"sync"

	"github.com/oschwald/maxminddb-golang"
)

// Result is the denormalized lookup result. Zero values indicate the
// underlying database had nothing for that field — common for ASN
// lookups against IPs that aren't routable, and for City lookups
// against private prefixes.
type Result struct {
	CountryISO     string  `json:"countryIso,omitempty"`
	CountryName    string  `json:"countryName,omitempty"`
	Subdivision    string  `json:"subdivision,omitempty"` // state / region / province
	City           string  `json:"city,omitempty"`
	Lat            float64 `json:"lat,omitempty"`
	Lon            float64 `json:"lon,omitempty"`
	ASN            uint    `json:"asn,omitempty"`
	Organization   string  `json:"org,omitempty"`
	AccuracyRadius uint16  `json:"accuracyRadius,omitempty"`
}

// Reader is the long-lived handle. Safe for concurrent Lookup calls.
type Reader struct {
	mu   sync.RWMutex
	city *maxminddb.Reader
	asn  *maxminddb.Reader
}

// Open returns a Reader. If a path is empty or missing, that database
// is silently skipped — Lookup will simply not populate the
// corresponding fields. Returning a usable zero-value reader means
// callers don't need to nil-check on every code path.
func Open(cityPath, asnPath string) (*Reader, error) {
	r := &Reader{}
	if cityPath != "" {
		if mr, err := openIfExists(cityPath); err != nil {
			return nil, err
		} else {
			r.city = mr
		}
	}
	if asnPath != "" {
		if mr, err := openIfExists(asnPath); err != nil {
			r.Close()
			return nil, err
		} else {
			r.asn = mr
		}
	}
	return r, nil
}

func openIfExists(path string) (*maxminddb.Reader, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return maxminddb.Open(path)
}

// Has reports whether at least one of the two databases is loaded.
// Used by the API to decide whether to run lookups at all.
func (r *Reader) Has() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.city != nil || r.asn != nil
}

// Close releases both readers. Safe to call on a zero-value Reader.
func (r *Reader) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.city != nil {
		_ = r.city.Close()
		r.city = nil
	}
	if r.asn != nil {
		_ = r.asn.Close()
		r.asn = nil
	}
}

// Reload reopens both files atomically. Used when the volume is
// refreshed without restarting the container. Returns the previous
// readers' Close errors aggregated as a single error if any.
func (r *Reader) Reload(cityPath, asnPath string) error {
	next, err := Open(cityPath, asnPath)
	if err != nil {
		return err
	}
	r.mu.Lock()
	old := struct {
		city, asn *maxminddb.Reader
	}{r.city, r.asn}
	r.city = next.city
	r.asn = next.asn
	r.mu.Unlock()
	if old.city != nil {
		_ = old.city.Close()
	}
	if old.asn != nil {
		_ = old.asn.Close()
	}
	return nil
}

// cityRecord mirrors the GeoLite2-City schema. We pick out only the
// fields the dashboard renders — the schema is huge and we have no
// reason to materialise the rest.
type cityRecord struct {
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	Country struct {
		ISO   string            `maxminddb:"iso_code"`
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	Subdivisions []struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"subdivisions"`
	Location struct {
		Latitude       float64 `maxminddb:"latitude"`
		Longitude      float64 `maxminddb:"longitude"`
		AccuracyRadius uint16  `maxminddb:"accuracy_radius"`
	} `maxminddb:"location"`
}

type asnRecord struct {
	ASN          uint   `maxminddb:"autonomous_system_number"`
	Organization string `maxminddb:"autonomous_system_organization"`
}

// Lookup returns the merged City + ASN result for ip. Returns
// (zero, false) when the IP doesn't parse, both databases are nil, or
// neither database has a record for it.
func (r *Reader) Lookup(ip string) (Result, bool) {
	if r == nil {
		return Result{}, false
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return Result{}, false
	}
	r.mu.RLock()
	city, asn := r.city, r.asn
	r.mu.RUnlock()

	var got bool
	var out Result
	if city != nil {
		var rec cityRecord
		if err := city.Lookup(parsed, &rec); err == nil {
			if name := rec.Country.Names["en"]; name != "" {
				out.CountryName = name
				got = true
			}
			if rec.Country.ISO != "" {
				out.CountryISO = rec.Country.ISO
				got = true
			}
			if name := rec.City.Names["en"]; name != "" {
				out.City = name
				got = true
			}
			if len(rec.Subdivisions) > 0 {
				if name := rec.Subdivisions[0].Names["en"]; name != "" {
					out.Subdivision = name
					got = true
				}
			}
			if rec.Location.Latitude != 0 || rec.Location.Longitude != 0 {
				out.Lat = rec.Location.Latitude
				out.Lon = rec.Location.Longitude
				out.AccuracyRadius = rec.Location.AccuracyRadius
				got = true
			}
		}
	}
	if asn != nil {
		var rec asnRecord
		if err := asn.Lookup(parsed, &rec); err == nil {
			if rec.ASN != 0 {
				out.ASN = rec.ASN
				got = true
			}
			if rec.Organization != "" {
				out.Organization = rec.Organization
				got = true
			}
		}
	}
	return out, got
}
