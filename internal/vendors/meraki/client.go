// Package meraki is a minimal Dashboard API client for inventory sync.
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
		http:   &http.Client{Timeout: 45 * time.Second},
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
