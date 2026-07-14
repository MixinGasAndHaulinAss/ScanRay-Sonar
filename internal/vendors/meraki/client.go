// Package meraki is a minimal Dashboard API client for inventory sync
// and live health telemetry.
package meraki

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	Name        string   `json:"name"`
	Model       string   `json:"model"`
	Serial      string   `json:"serial"`
	MAC         string   `json:"mac"`
	LANIP       string   `json:"lanIp"`
	NetworkID   string   `json:"networkId"`
	ProductType string   `json:"productType"`
	Firmware    string   `json:"firmware"`
	Address     string   `json:"address"`
	Notes       string   `json:"notes"`
	URL         string   `json:"url"`
	Tags        []string `json:"tags"`
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
	Name           string   `json:"name"`
	Serial         string   `json:"serial"`
	MAC            string   `json:"mac"`
	PublicIP       string   `json:"publicIp"`
	NetworkID      string   `json:"networkId"`
	Status         string   `json:"status"`
	LastReportedAt string   `json:"lastReportedAt"`
	LANIP          string   `json:"lanIp"`
	Gateway        string   `json:"gateway"`
	IPType         string   `json:"ipType"`
	PrimaryDNS     string   `json:"primaryDns"`
	SecondaryDNS   string   `json:"secondaryDns"`
	ProductType    string   `json:"productType"`
	Model          string   `json:"model"`
	Tags           []string `json:"tags"`
	Components     *struct {
		PowerSupplies []PowerSupply `json:"powerSupplies"`
	} `json:"components,omitempty"`
	// Clients is present on wireless (and some other) status payloads.
	Clients *struct {
		Counts struct {
			Total int `json:"total"`
		} `json:"counts"`
	} `json:"clients,omitempty"`
}

// PowerSupply is a chassis PSU row on switch statuses.
type PowerSupply struct {
	Slot   int    `json:"slot"`
	Serial string `json:"serial"`
	Model  string `json:"model"`
	Status string `json:"status"`
	PoE    *struct {
		Unit    string `json:"unit"`
		Maximum int    `json:"maximum"`
	} `json:"poe,omitempty"`
}

// ApplianceUplinkStatus is one MX/Z appliance's uplink snapshot.
type ApplianceUplinkStatus struct {
	NetworkID        string `json:"networkId"`
	Serial           string `json:"serial"`
	Model            string `json:"model"`
	LastReportedAt   string `json:"lastReportedAt"`
	HighAvailability *struct {
		Enabled bool   `json:"enabled"`
		Role    string `json:"role"`
	} `json:"highAvailability,omitempty"`
	Uplinks []UplinkStatus `json:"uplinks"`
}

// UplinkStatus is a single WAN/cellular uplink.
type UplinkStatus struct {
	Interface      string `json:"interface"`
	Status         string `json:"status"`
	IP             string `json:"ip"`
	Gateway        string `json:"gateway"`
	PublicIP       string `json:"publicIp"`
	PrimaryDNS     string `json:"primaryDns"`
	SecondaryDNS   string `json:"secondaryDns"`
	IPAssignedBy   string `json:"ipAssignedBy"`
	Provider       string `json:"provider"`
	SignalType     string `json:"signalType"`
	ICCID          string `json:"iccid"`
	ConnectionType string `json:"connectionType"`
	SignalStat     *struct {
		RSRP string `json:"rsrp"`
		RSRQ string `json:"rsrq"`
	} `json:"signalStat,omitempty"`
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
	Duplex   string   `json:"duplex"`
	IsUplink bool     `json:"isUplink"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
	PoE      *struct {
		IsAllocated bool `json:"isAllocated"`
	} `json:"poe,omitempty"`
	SecurePort *struct {
		Active               bool   `json:"active"`
		AuthenticationStatus string `json:"authenticationStatus"`
	} `json:"securePort,omitempty"`
	SpanningTree *struct {
		Statuses []string `json:"statuses"`
	} `json:"spanningTree,omitempty"`
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

// ApplianceVPNStatus is org VPN status keyed by device serial.
type ApplianceVPNStatus struct {
	NetworkID      string `json:"networkId"`
	NetworkName    string `json:"networkName"`
	DeviceSerial   string `json:"deviceSerial"`
	DeviceStatus   string `json:"deviceStatus"`
	VPNMode        string `json:"vpnMode"`
	MerakiVPNPeers []struct {
		NetworkID    string `json:"networkId"`
		NetworkName  string `json:"networkName"`
		Reachability string `json:"reachability"`
		Priority     int    `json:"priority"`
	} `json:"merakiVpnPeers"`
	ThirdPartyVPNPeers []struct {
		Name         string `json:"name"`
		PublicIP     string `json:"publicIp"`
		Reachability string `json:"reachability"`
	} `json:"thirdPartyVpnPeers"`
}

// WirelessPacketLossByDevice is AP client-path loss for a timespan.
type WirelessPacketLossByDevice struct {
	Device struct {
		Name   string `json:"name"`
		Serial string `json:"serial"`
		MAC    string `json:"mac"`
	} `json:"device"`
	Downstream struct {
		Total          float64 `json:"total"`
		Lost           float64 `json:"lost"`
		LossPercentage float64 `json:"lossPercentage"`
	} `json:"downstream"`
	Upstream struct {
		Total          float64 `json:"total"`
		Lost           float64 `json:"lost"`
		LossPercentage float64 `json:"lossPercentage"`
	} `json:"upstream"`
}

// AppliancePerformance is MX performance score (0–100-ish).
type AppliancePerformance struct {
	PerfScore *float64 `json:"perfScore"`
}

// FirmwareUpgradeByDevice is pending/current firmware for a device.
type FirmwareUpgradeByDevice struct {
	Serial string `json:"serial"`
	Name   string `json:"name"`
	Device *struct {
		Serial   string `json:"serial"`
		Name     string `json:"name"`
		Firmware *struct {
			CurrentVersion *struct {
				Firmware  string `json:"firmware"`
				ShortName string `json:"shortName"`
			} `json:"currentVersion"`
			LastUpgrade *struct {
				Time        string `json:"time"`
				FromVersion *struct {
					Firmware string `json:"firmware"`
				} `json:"fromVersion"`
				ToVersion *struct {
					Firmware string `json:"firmware"`
				} `json:"toVersion"`
			} `json:"lastUpgrade"`
			NextUpgrade *struct {
				Time      string `json:"time"`
				ToVersion *struct {
					Firmware string `json:"firmware"`
				} `json:"toVersion"`
			} `json:"nextUpgrade"`
		} `json:"firmware"`
	} `json:"device"`
	// Flat fields some payloads use instead of nested device.
	ProductType string `json:"productType"`
	Status      string `json:"status"`
}

// SensorReadingLatest is one sensor's latest metrics.
type SensorReadingLatest struct {
	Serial  string `json:"serial"`
	Network *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"network"`
	Readings []struct {
		Metric      string `json:"metric"`
		TS          string `json:"ts"`
		Temperature *struct {
			Celsius float64 `json:"celsius"`
		} `json:"temperature,omitempty"`
		Humidity *struct {
			RelativePercentage float64 `json:"relativePercentage"`
		} `json:"humidity,omitempty"`
		Door *struct {
			Open bool `json:"open"`
		} `json:"door,omitempty"`
		Water *struct {
			Present bool `json:"present"`
		} `json:"water,omitempty"`
		Battery *struct {
			Percentage float64 `json:"percentage"`
		} `json:"battery,omitempty"`
		Co2 *struct {
			Concentration float64 `json:"concentration"`
		} `json:"co2,omitempty"`
		Tvoc *struct {
			Concentration float64 `json:"concentration"`
		} `json:"tvoc,omitempty"`
		Noise *struct {
			Ambient *struct {
				Level float64 `json:"level"`
			} `json:"ambient"`
		} `json:"noise,omitempty"`
		Pm25 *struct {
			Concentration float64 `json:"concentration"`
		} `json:"pm25,omitempty"`
	} `json:"readings"`
}

// AssuranceAlert is a recent org alert.
type AssuranceAlert struct {
	ID           string `json:"id"`
	CategoryType string `json:"categoryType"`
	Type         string `json:"type"`
	Severity     string `json:"severity"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	StartedAt    string `json:"startedAt"`
	DeviceType   string `json:"deviceType"`
	DeviceSerial string `json:"deviceSerial"`
	DeviceName   string `json:"deviceName"`
	NetworkID    string `json:"networkId"`
	NetworkName  string `json:"networkName"`
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
	if err := c.get(ctx, "/organizations/"+orgID+"/devices?perPage=1000", &out); err != nil {
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
			path += "&startingAfter=" + url.QueryEscape(startingAfter)
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

// SwitchPortPackets is one port's packet counters over a timespan.
type SwitchPortPackets struct {
	PortID  string `json:"portId"`
	Packets []struct {
		Desc  string `json:"desc"`
		Total int64  `json:"total"`
		Sent  int64  `json:"sent"`
		Recv  int64  `json:"recv"`
	} `json:"packets"`
}

// SwitchPortsPacketsByDevice is per-switch port packet totals.
type SwitchPortsPacketsByDevice struct {
	Serial string              `json:"serial"`
	Name   string              `json:"name"`
	Ports  []SwitchPortPackets `json:"ports"`
}

type switchPortsPacketsPage struct {
	Items []SwitchPortsPacketsByDevice `json:"items"`
	Meta  struct {
		Counts struct {
			Items struct {
				Remaining int `json:"remaining"`
				Total     int `json:"total"`
			} `json:"items"`
		} `json:"counts"`
	} `json:"meta"`
}

// ListSwitchPortsStatusesPacketsByDeviceByPort returns org-wide switch port packet counters.
// Soft-fail callers should treat 404 as unsupported entitlement.
func (c *Client) ListSwitchPortsStatusesPacketsByDeviceByPort(ctx context.Context, orgID string) ([]SwitchPortsPacketsByDevice, error) {
	var out []SwitchPortsPacketsByDevice
	startingAfter := ""
	for {
		path := "/organizations/" + orgID + "/switch/ports/statuses/packets/byDevice/byPort?timespan=3600&perPage=20"
		if startingAfter != "" {
			path += "&startingAfter=" + url.QueryEscape(startingAfter)
		}
		var page switchPortsPacketsPage
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

// GetDeviceSwitchPortsStatusesPackets returns packet counters for one switch (fallback).
func (c *Client) GetDeviceSwitchPortsStatusesPackets(ctx context.Context, serial string) ([]SwitchPortPackets, error) {
	var out []SwitchPortPackets
	path := "/devices/" + url.PathEscape(serial) + "/switch/ports/statuses/packets?timespan=3600"
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListApplianceVPNStatuses returns site-to-site VPN status for an org.
func (c *Client) ListApplianceVPNStatuses(ctx context.Context, orgID string) ([]ApplianceVPNStatus, error) {
	var out []ApplianceVPNStatus
	if err := c.get(ctx, "/organizations/"+orgID+"/appliance/vpn/statuses?perPage=300", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListWirelessPacketLossByDevice returns AP packet-loss averages (default timespan 1h).
func (c *Client) ListWirelessPacketLossByDevice(ctx context.Context, orgID string) ([]WirelessPacketLossByDevice, error) {
	var out []WirelessPacketLossByDevice
	path := "/organizations/" + orgID + "/wireless/devices/packetLoss/byDevice?timespan=3600&perPage=1000"
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetAppliancePerformance returns MX performance score for one serial.
func (c *Client) GetAppliancePerformance(ctx context.Context, serial string) (AppliancePerformance, error) {
	var out AppliancePerformance
	if err := c.get(ctx, "/devices/"+url.PathEscape(serial)+"/appliance/performance", &out); err != nil {
		return AppliancePerformance{}, err
	}
	return out, nil
}

// ListFirmwareUpgradesByDevice returns firmware upgrade status for MS/MR (and some others).
func (c *Client) ListFirmwareUpgradesByDevice(ctx context.Context, orgID string) ([]FirmwareUpgradeByDevice, error) {
	var out []FirmwareUpgradeByDevice
	if err := c.get(ctx, "/organizations/"+orgID+"/firmware/upgrades/byDevice?perPage=1000", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListSensorReadingsLatest returns latest sensor metrics for an org.
func (c *Client) ListSensorReadingsLatest(ctx context.Context, orgID string) ([]SensorReadingLatest, error) {
	var out []SensorReadingLatest
	if err := c.get(ctx, "/organizations/"+orgID+"/sensor/readings/latest?perPage=1000", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListAssuranceAlerts returns recent active assurance alerts for an org.
func (c *Client) ListAssuranceAlerts(ctx context.Context, orgID string) ([]AssuranceAlert, error) {
	var out []AssuranceAlert
	path := "/organizations/" + orgID + "/assurance/alerts?active=true&perPage=100"
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return out, nil
}
