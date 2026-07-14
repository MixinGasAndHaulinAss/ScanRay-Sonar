// Package meraki is a minimal Dashboard API client for inventory sync
// and live health telemetry.
package meraki

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const baseURL = "https://api.meraki.com/api/v1"

// Client talks to the Meraki Dashboard REST API.
type Client struct {
	apiKey string
	http   *http.Client
}

// New returns a Dashboard API client.
func New(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		http:   &http.Client{Timeout: 60 * time.Second},
	}
}

// Organization is a Meraki org row.
type Organization struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Network is a Meraki network row.
type Network struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Device is a Meraki device row.
type Device struct {
	Name        string `json:"name"`
	Model       string `json:"model"`
	Serial      string `json:"serial"`
	MAC         string `json:"mac"`
	LANIP       string `json:"lanIp"`
	NetworkID   string `json:"networkId"`
	ProductType string `json:"productType"`
}

// ApplianceVLAN is one Addressing & VLANs row (MX LAN/management side).
type ApplianceVLAN struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	ApplianceIP string `json:"applianceIp"`
	Subnet      string `json:"subnet"`
}

// ApplianceSingleLAN is the MX LAN when VLANs are disabled.
type ApplianceSingleLAN struct {
	ApplianceIP string `json:"applianceIp"`
	Subnet      string `json:"subnet"`
}

// DeviceStatus is one org-wide device reachability row.
type DeviceStatus struct {
	Name           string `json:"name"`
	Serial         string `json:"serial"`
	MAC            string `json:"mac"`
	NetworkID      string `json:"networkId"`
	ProductType    string `json:"productType"`
	Status         string `json:"status"`
	LastReportedAt string `json:"lastReportedAt"`
	// Clients is present on wireless (and some other) status payloads.
	Clients *struct {
		Counts struct {
			Total int `json:"total"`
		} `json:"counts"`
	} `json:"clients,omitempty"`
}

// ApplianceUplinkStatus is one MX/Z appliance's uplink snapshot.
type ApplianceUplinkStatus struct {
	NetworkID string         `json:"networkId"`
	Serial    string         `json:"serial"`
	Model     string         `json:"model"`
	Uplinks   []UplinkStatus `json:"uplinks"`
}

// UplinkStatus is a single WAN/cellular uplink.
type UplinkStatus struct {
	Interface string `json:"interface"`
	Status    string `json:"status"`
	IP        string `json:"ip"`
	PublicIP  string `json:"publicIp"`
}

// SwitchPortsBySwitch is one switch with all port statuses.
type SwitchPortsBySwitch struct {
	Serial string             `json:"serial"`
	Name   string             `json:"name"`
	Model  string             `json:"model"`
	Ports  []SwitchPortStatus `json:"ports"`
}

// SwitchPortStatus is one switch port's live status.
type SwitchPortStatus struct {
	PortID   string   `json:"portId"`
	Enabled  bool     `json:"enabled"`
	Status   string   `json:"status"`
	Speed    string   `json:"speed"`
	IsUplink bool     `json:"isUplink"`
	Errors   []string `json:"errors"`
}

// UplinkLossLatency is one device uplink path-quality series.
type UplinkLossLatency struct {
	NetworkID  string `json:"networkId"`
	Serial     string `json:"serial"`
	Uplink     string `json:"uplink"`
	IP         string `json:"ip"`
	TimeSeries []struct {
		TS          string   `json:"ts"`
		LossPercent *float64 `json:"lossPercent"`
		LatencyMs   *float64 `json:"latencyMs"`
	} `json:"timeSeries"`
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("meraki: GET %s: %s: %s", path, resp.Status, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// ListOrganizations returns orgs visible to the API key.
func (c *Client) ListOrganizations(ctx context.Context) ([]Organization, error) {
	var out []Organization
	if err := c.get(ctx, "/organizations", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListNetworks returns networks for an org.
func (c *Client) ListNetworks(ctx context.Context, orgID string) ([]Network, error) {
	var out []Network
	if err := c.get(ctx, "/organizations/"+orgID+"/networks", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListDevices returns devices for an org.
func (c *Client) ListDevices(ctx context.Context, orgID string) ([]Device, error) {
	var out []Device
	if err := c.get(ctx, "/organizations/"+orgID+"/devices", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListApplianceVLANs returns MX VLAN / appliance LAN IPs for a network.
func (c *Client) ListApplianceVLANs(ctx context.Context, networkID string) ([]ApplianceVLAN, error) {
	var out []ApplianceVLAN
	if err := c.get(ctx, "/networks/"+networkID+"/appliance/vlans", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetApplianceSingleLAN returns the MX LAN IP when the network has VLANs disabled.
func (c *Client) GetApplianceSingleLAN(ctx context.Context, networkID string) (ApplianceSingleLAN, error) {
	var out ApplianceSingleLAN
	if err := c.get(ctx, "/networks/"+networkID+"/appliance/singleLan", &out); err != nil {
		return ApplianceSingleLAN{}, err
	}
	return out, nil
}

// ListDeviceStatuses returns online/offline status for every device in an org.
func (c *Client) ListDeviceStatuses(ctx context.Context, orgID string) ([]DeviceStatus, error) {
	var out []DeviceStatus
	if err := c.get(ctx, "/organizations/"+orgID+"/devices/statuses?perPage=1000", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListApplianceUplinkStatuses returns MX/Z uplink IPs and state for an org.
func (c *Client) ListApplianceUplinkStatuses(ctx context.Context, orgID string) ([]ApplianceUplinkStatus, error) {
	var out []ApplianceUplinkStatus
	if err := c.get(ctx, "/organizations/"+orgID+"/appliance/uplink/statuses?perPage=1000", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// switchPortsBySwitchPage is the paginated envelope for bySwitch.
// Meraki returns {"items":[...],"meta":{...}} rather than a bare array.
// perPage is capped at 20 for this endpoint.
type switchPortsBySwitchPage struct {
	Items []SwitchPortsBySwitch `json:"items"`
	Meta  struct {
		Counts struct {
			Items struct {
				Remaining int `json:"remaining"`
				Total     int `json:"total"`
			} `json:"items"`
		} `json:"counts"`
	} `json:"meta"`
}

// ListSwitchPortsStatusesBySwitch returns all switch port statuses for an org.
func (c *Client) ListSwitchPortsStatusesBySwitch(ctx context.Context, orgID string) ([]SwitchPortsBySwitch, error) {
	var out []SwitchPortsBySwitch
	startingAfter := ""
	for {
		path := "/organizations/" + orgID + "/switch/ports/statuses/bySwitch?perPage=20"
		if startingAfter != "" {
			path += "&startingAfter=" + startingAfter
		}
		var page switchPortsBySwitchPage
		if err := c.get(ctx, path, &page); err != nil {
			return nil, err
		}
		if len(page.Items) == 0 {
			break
		}
		out = append(out, page.Items...)
		if page.Meta.Counts.Items.Remaining <= 0 {
			break
		}
		last := page.Items[len(page.Items)-1].Serial
		if last == "" || last == startingAfter {
			break
		}
		startingAfter = last
	}
	return out, nil
}

// ListUplinksLossAndLatency returns MX path-quality samples for an org.
func (c *Client) ListUplinksLossAndLatency(ctx context.Context, orgID string) ([]UplinkLossLatency, error) {
	var out []UplinkLossLatency
	if err := c.get(ctx, "/organizations/"+orgID+"/devices/uplinksLossAndLatency?perPage=1000", &out); err != nil {
		return nil, err
	}
	return out, nil
}
