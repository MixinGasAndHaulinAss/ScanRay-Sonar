package discovery

import (
	"context"
	"errors"
	"time"
)

// VMwareCred is what's required to talk to vCenter REST. The full discovery
// logic lives in a follow-up commit that adds the `govmomi` dependency; this
// file is the placeholder so the rest of the discovery poller can compile and
// reference a stable interface today.
type VMwareCred struct {
	URL      string
	Username string
	Password string
	Insecure bool
}

// VMwareInventory is the (intentionally narrow) shape the poller wants back
// from vCenter: hosts, VMs, datacenters, and last-poll timestamp.
type VMwareInventory struct {
	Hosts       []VMwareHost `json:"hosts"`
	VMs         []VMwareVM   `json:"vms"`
	Datacenters []string     `json:"datacenters"`
	CollectedAt time.Time    `json:"collectedAt"`
}

type VMwareHost struct {
	Name       string `json:"name"`
	MgmtIP     string `json:"mgmtIp,omitempty"`
	HardwareID string `json:"hardwareId,omitempty"`
	Vendor     string `json:"vendor,omitempty"`
	Model      string `json:"model,omitempty"`
}

type VMwareVM struct {
	Name     string `json:"name"`
	GuestOS  string `json:"guestOs,omitempty"`
	HostName string `json:"hostName,omitempty"`
	PowerOn  bool   `json:"powerOn"`
}

// ErrVMwareNotImplemented signals that govmomi isn't wired in yet. The
// collector treats this as a soft-skip: vmware credentials present, no VMware
// host on the wire, log a warning and move on.
var ErrVMwareNotImplemented = errors.New("vmware: govmomi backend not yet wired in")

// VMwareCollect is the future inventory call. Today returns ErrVMwareNotImplemented
// so the rest of the discovery pipeline can ship without the heavy govmomi dep.
func VMwareCollect(ctx context.Context, cred VMwareCred) (*VMwareInventory, error) {
	return nil, ErrVMwareNotImplemented
}
