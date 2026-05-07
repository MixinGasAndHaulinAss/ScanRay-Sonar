// Package discovery — passive SNMP discovery shared types.
//
// Passive SNMP discovery learns what's on a network *without* scanning
// it: we listen on a NIC for outbound UDP/161 packets that some other
// monitoring tool (LibreNMS, Observium, a vendor cloud appliance, an
// MSP collector, etc.) is already sending, then probe each captured
// destination IP once with a public-OID SNMP GET to classify the
// device.
//
// The capture itself is OS-specific (AF_PACKET on Linux) and lives in
// passive_snmp_linux.go. This file holds the shared types and the
// classification logic, both of which are pure Go and platform-neutral.
package discovery

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/NCLGISA/ScanRay-Sonar/internal/snmp"
)

// ErrPassiveCaptureUnsupported is returned by CapturePassiveSNMP on
// platforms that lack a raw-socket capture path (anything other than
// Linux today). Callers can errors.Is against it to suppress repeat
// warnings instead of treating it as a real failure.
var ErrPassiveCaptureUnsupported = errors.New("passive SNMP capture only supported on Linux")

// PassiveDevice is one classified result from a passive-discovery
// sweep. Every field except IP is best-effort: a device that didn't
// answer the community-string SNMP GET still appears with IP set and
// the rest blank, so the operator at least knows the IP is being
// polled by something upstream.
type PassiveDevice struct {
	IP          string `json:"ip"`
	SysDescr    string `json:"sysDescr,omitempty"`
	SysObjectID string `json:"sysObjectId,omitempty"`
	SysName     string `json:"sysName,omitempty"`
	SysLocation string `json:"sysLocation,omitempty"`

	// Vendor is one of the broad classifications: cisco, meraki,
	// paloalto, synology, alletra, apc, hp, arista, ubiquiti, etc.
	// Empty when classification couldn't decide.
	Vendor string `json:"vendor,omitempty"`

	// Type is the device role: switch, router, firewall, ups,
	// printer, wireless-ap, server, nas, san, unknown.
	Type string `json:"type,omitempty"`

	// SubType is finer-grained where it makes sense — for "ups" we
	// distinguish "ups-apc" (APC NMC) from "ups-generic" (RFC1628
	// only); for "synology" we'd pin a model family.
	SubType string `json:"subType,omitempty"`
}

// PassiveCaptureOpts controls one capture cycle. Defaults documented
// on each field; the zero value is "60 seconds on the default-route
// interface".
type PassiveCaptureOpts struct {
	// Interface is the NIC name to capture on (e.g. "ens32",
	// "eth0"). Empty means auto-detect: pick the first non-loopback,
	// non-down interface that has at least one IPv4 address.
	Interface string

	// CaptureSeconds is the wall-clock duration of the capture
	// window. 60s is the documented default; below 10s rarely
	// catches anything because most upstream pollers tick on a
	// 30–60s cadence.
	CaptureSeconds int

	// MaxIPs caps the number of unique destination IPs we'll record.
	// Past this we stop collecting (saves memory on busy NICs).
	// 0 means 4096.
	MaxIPs int
}

// SNMPClassifier is what the platform-specific capture layer hands
// each captured IP to so it can probe + classify in one place. It's
// an interface so we can stub it out in tests.
type SNMPClassifier interface {
	Classify(ctx context.Context, ip string) PassiveDevice
}

// ClassifyDevice returns a PassiveDevice with vendor/type/subType set
// from the four standard SNMP system-group fields. sysObjectID prefix
// (vendor enterprise OIDs) is authoritative; sysDescr keyword fallback
// only fires when the sysObjectID gives us nothing.
//
// Pure function; tested standalone.
func ClassifyDevice(ip, sysDescr, sysObjectID, sysName, sysLocation string) PassiveDevice {
	d := PassiveDevice{
		IP:          ip,
		SysDescr:    sysDescr,
		SysObjectID: sysObjectID,
		SysName:     sysName,
		SysLocation: sysLocation,
	}
	oid := strings.TrimPrefix(sysObjectID, ".")
	descrLow := strings.ToLower(sysDescr)

	// Enterprise-OID prefixes — most reliable.
	switch {
	case strings.HasPrefix(oid, "1.3.6.1.4.1.318.1.1.1"):
		d.Vendor = "apc"
		d.Type = "ups"
		d.SubType = "ups-apc"
	case strings.HasPrefix(oid, "1.3.6.1.4.1.318"):
		d.Vendor = "apc"
		d.Type = "power"
		d.SubType = "apc-other"
	case strings.HasPrefix(oid, "1.3.6.1.4.1.6574"):
		d.Vendor = "synology"
		d.Type = "nas"
	case strings.HasPrefix(oid, "1.3.6.1.4.1.37447"):
		d.Vendor = "alletra"
		d.Type = "san"
	case strings.HasPrefix(oid, "1.3.6.1.4.1.25461"):
		d.Vendor = "paloalto"
		d.Type = "firewall"
	case strings.HasPrefix(oid, "1.3.6.1.4.1.9"):
		// Cisco enterprise space. Meraki appliances also live here
		// (.1.3.6.1.4.1.9.1.x for Catalyst, .1.3.6.1.4.1.29671 for
		// older Meraki, but newer Meraki often reports under .9
		// and uses sysDescr to distinguish). Use sysDescr as
		// tiebreaker.
		if strings.Contains(descrLow, "meraki") {
			d.Vendor = "meraki"
		} else {
			d.Vendor = "cisco"
		}
		switch {
		case strings.Contains(descrLow, "wireless"), strings.Contains(descrLow, "access point"):
			d.Type = "wireless-ap"
		case strings.Contains(descrLow, "router"):
			d.Type = "router"
		case strings.Contains(descrLow, "firewall"), strings.Contains(descrLow, "asa"), strings.Contains(descrLow, "ftd"):
			d.Type = "firewall"
		default:
			d.Type = "switch"
		}
	case strings.HasPrefix(oid, "1.3.6.1.4.1.29671"):
		d.Vendor = "meraki"
		d.Type = "switch"
	case strings.HasPrefix(oid, "1.3.6.1.4.1.11"):
		d.Vendor = "hp"
		if strings.Contains(descrLow, "switch") {
			d.Type = "switch"
		} else if strings.Contains(descrLow, "printer") || strings.Contains(descrLow, "laserjet") {
			d.Type = "printer"
		}
	case strings.HasPrefix(oid, "1.3.6.1.4.1.30065"):
		d.Vendor = "arista"
		d.Type = "switch"
	case strings.HasPrefix(oid, "1.3.6.1.4.1.41112"):
		d.Vendor = "ubiquiti"
		if strings.Contains(descrLow, "uap") || strings.Contains(descrLow, "wireless") {
			d.Type = "wireless-ap"
		} else {
			d.Type = "switch"
		}
	case strings.HasPrefix(oid, "1.3.6.1.4.1.14988"):
		d.Vendor = "mikrotik"
		d.Type = "router"
	case strings.HasPrefix(oid, "1.3.6.1.4.1.4413"):
		d.Vendor = "broadcom"
		d.Type = "switch"
	}

	// sysDescr fallback when the enterprise OID didn't help (vendor
	// still empty). This is fuzzier — only use it as a tiebreaker.
	if d.Vendor == "" {
		switch {
		case strings.Contains(descrLow, "meraki"):
			d.Vendor = "meraki"
			d.Type = "switch"
		case strings.Contains(descrLow, "synology"):
			d.Vendor = "synology"
			d.Type = "nas"
		case strings.Contains(descrLow, "ubiquiti"), strings.Contains(descrLow, "ubnt"):
			d.Vendor = "ubiquiti"
		}
	}
	if d.Type == "" {
		switch {
		case strings.Contains(descrLow, "ups"), strings.Contains(descrLow, "uninterruptible"):
			d.Type = "ups"
			if d.SubType == "" {
				d.SubType = "ups-generic"
			}
		case strings.Contains(descrLow, "printer"), strings.Contains(descrLow, "laserjet"), strings.Contains(descrLow, "ricoh"), strings.Contains(descrLow, "konica"):
			d.Type = "printer"
		case strings.Contains(descrLow, "switch"):
			d.Type = "switch"
		case strings.Contains(descrLow, "firewall"):
			d.Type = "firewall"
		case strings.Contains(descrLow, "access point"), strings.Contains(descrLow, "wireless"):
			d.Type = "wireless-ap"
		case strings.Contains(descrLow, "router"):
			d.Type = "router"
		}
	}
	if d.Type == "" {
		d.Type = "unknown"
	}
	return d
}

// snmpProbeClassifier dials each IP with a v2c GET of the four system
// fields, then runs ClassifyDevice. Used by the live capture path; the
// test path stubs it out.
type snmpProbeClassifier struct {
	community string
	timeout   time.Duration
}

// NewSNMPProbe builds a Classifier that tries one v2c community per
// IP. Picks v2c specifically because that's what's on the wire in the
// captured traffic — the upstream tool is using v2c, so v2c is what
// the device is configured for.
func NewSNMPProbe(community string, perProbeTimeout time.Duration) SNMPClassifier {
	if perProbeTimeout <= 0 {
		perProbeTimeout = 2 * time.Second
	}
	return &snmpProbeClassifier{community: community, timeout: perProbeTimeout}
}

func (p *snmpProbeClassifier) Classify(ctx context.Context, ip string) PassiveDevice {
	c, err := snmp.Dial(ctx, snmp.Target{Host: ip, Timeout: p.timeout, Retries: 1},
		snmp.Creds{Version: "v2c", Community: p.community})
	if err != nil {
		return PassiveDevice{IP: ip, Type: "unknown"}
	}
	defer c.Close()

	// All four scalars in one GET. Missing fields just come back
	// empty — same shape as a plain snmpget against the device.
	res, err := c.Get([]string{
		"1.3.6.1.2.1.1.1.0", // sysDescr
		"1.3.6.1.2.1.1.2.0", // sysObjectID
		"1.3.6.1.2.1.1.5.0", // sysName
		"1.3.6.1.2.1.1.6.0", // sysLocation
	})
	if err != nil {
		return PassiveDevice{IP: ip, Type: "unknown"}
	}
	return ClassifyDevice(
		ip,
		res["1.3.6.1.2.1.1.1.0"].String(),
		strings.TrimPrefix(res["1.3.6.1.2.1.1.2.0"].String(), "."),
		res["1.3.6.1.2.1.1.5.0"].String(),
		res["1.3.6.1.2.1.1.6.0"].String(),
	)
}
