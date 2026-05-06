// Package winagent is the Windows-only WMI/WinRM bridge that
// sonar-collector dials over HTTPS to enrich discovered_devices with
// inventory information only available locally on the Windows endpoint
// (BIOS serial, monitor EDID, installed software, …).
//
// The cross-platform types live here so the collector-side client can
// import the package on any OS.
package winagent

import "time"

// Inventory is the JSON payload returned by /v1/inventory on a winagent.
type Inventory struct {
	CollectedAt time.Time `json:"collectedAt"`
	OS          OSInfo    `json:"os"`
	Computer    Computer  `json:"computer"`
	BIOS        BIOSInfo  `json:"bios,omitempty"`
	NICs        []NIC     `json:"nics,omitempty"`
	Disks       []Disk    `json:"disks,omitempty"`
	Software    []Program `json:"software,omitempty"`
}

type OSInfo struct {
	Name          string `json:"name"`          // Win32_OperatingSystem.Caption
	Version       string `json:"version"`       // .Version
	Architecture  string `json:"architecture"`  // .OSArchitecture
	InstallDate   string `json:"installDate"`   // .InstallDate
	LastBootUp    string `json:"lastBootUp"`    // .LastBootUpTime
	BuildNumber   string `json:"buildNumber"`   // .BuildNumber
}

type Computer struct {
	Hostname     string `json:"hostname"`
	Domain       string `json:"domain,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Model        string `json:"model,omitempty"`
	SerialNumber string `json:"serialNumber,omitempty"`
}

type BIOSInfo struct {
	Vendor   string `json:"vendor,omitempty"`
	Version  string `json:"version,omitempty"`
	Serial   string `json:"serial,omitempty"`
	Released string `json:"released,omitempty"`
}

type NIC struct {
	Description string   `json:"description"`
	MAC         string   `json:"mac,omitempty"`
	IPv4        []string `json:"ipv4,omitempty"`
	SpeedMbps   int64    `json:"speedMbps,omitempty"`
	Connected   bool     `json:"connected"`
}

type Disk struct {
	Mount       string `json:"mount"`
	FSType      string `json:"fsType,omitempty"`
	SizeBytes   int64  `json:"sizeBytes"`
	FreeBytes   int64  `json:"freeBytes"`
}

type Program struct {
	Name      string `json:"name"`
	Version   string `json:"version,omitempty"`
	Publisher string `json:"publisher,omitempty"`
}
