package discovery

import "strings"

// VendorProfile describes the commands needed to identify and back up a vendor
// device's running config over SSH/telnet. The CLI poller runs the commands in
// order until one returns output that looks like a config; the first matching
// vendor wins for identification.
type VendorProfile struct {
	Name          string
	IdentifyCmd   string
	IdentifyMatch []string
	ConfigCmds    []string
	NeedsEnable   bool
}

// VendorProfiles is a small library that's good enough to identify the most
// common kit on Currituck-style networks. Keep these short; the discovery
// poller iterates through every profile per device until one identifies.
var VendorProfiles = []VendorProfile{
	{
		Name:          "cisco-ios",
		IdentifyCmd:   "show version",
		IdentifyMatch: []string{"Cisco IOS", "IOS-XE", "Cisco Internetwork", "C2960", "C3560", "C3850", "Catalyst"},
		ConfigCmds:    []string{"terminal length 0", "show running-config"},
		NeedsEnable:   true,
	},
	{
		Name:          "cisco-nxos",
		IdentifyCmd:   "show version",
		IdentifyMatch: []string{"Nexus", "NX-OS"},
		ConfigCmds:    []string{"terminal length 0", "show running-config"},
		NeedsEnable:   false,
	},
	{
		Name:          "aruba-aoss",
		IdentifyCmd:   "show version",
		IdentifyMatch: []string{"Aruba", "ArubaOS-Switch", "ProCurve", "HP J", "HPE OfficeConnect"},
		ConfigCmds:    []string{"no page", "show running-config"},
		NeedsEnable:   false,
	},
	{
		Name:          "mikrotik",
		IdentifyCmd:   "/system resource print",
		IdentifyMatch: []string{"RouterOS", "MikroTik"},
		ConfigCmds:    []string{"/export"},
		NeedsEnable:   false,
	},
	{
		Name:          "junos",
		IdentifyCmd:   "show version",
		IdentifyMatch: []string{"JUNOS", "Junos OS", "Juniper"},
		ConfigCmds:    []string{"set cli screen-length 0", "show configuration | display set"},
		NeedsEnable:   false,
	},
	{
		Name:          "fortinet",
		IdentifyCmd:   "get system status",
		IdentifyMatch: []string{"FortiOS", "Fortigate", "Fortinet"},
		ConfigCmds:    []string{"show full-configuration"},
		NeedsEnable:   false,
	},
}

// IdentifyVendor finds the first profile whose identify match strings appear
// in the supplied banner/output blob. Returns ("", false) if no match.
func IdentifyVendor(out string) (VendorProfile, bool) {
	for _, p := range VendorProfiles {
		for _, m := range p.IdentifyMatch {
			if strings.Contains(out, m) {
				return p, true
			}
		}
	}
	return VendorProfile{}, false
}
