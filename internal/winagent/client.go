package winagent

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is the collector-side caller for a winagent on a remote host. The
// caller supplies the host (e.g. "10.4.20.31") and the shared bearer token
// that the agent was enrolled with.
type Client struct {
	Host  string
	Token string
	HTTP  *http.Client
}

// NewInsecureClient is a quick-start factory for sites where the winagent
// uses a self-signed cert; production callers should mint a proper Client
// with a pinned x509 chain.
func NewInsecureClient(host, token string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		Host:  host,
		Token: token,
		HTTP: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // local LAN, mTLS pinning is a future bullet
			},
		},
	}
}

// FetchInventory dials https://<host>:8443/v1/inventory and decodes the
// agent's JSON response into an Inventory struct.
func (c *Client) FetchInventory(ctx context.Context) (*Inventory, error) {
	if c.Host == "" {
		return nil, errors.New("winagent: empty Host")
	}
	url := fmt.Sprintf("https://%s:8443/v1/inventory", c.Host)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("winagent: status %d: %s", resp.StatusCode, string(body))
	}
	var inv Inventory
	if err := json.Unmarshal(body, &inv); err != nil {
		return nil, err
	}
	return &inv, nil
}
