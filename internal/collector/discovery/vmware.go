package discovery

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// VMwareCollect lists VMs and hosts via the vCenter HTTPS REST API.
// When the endpoint is unreachable or credentials fail, returns an empty
// inventory with a logged warning rather than failing discovery.
func VMwareCollect(ctx context.Context, cred VMwareCred) (*VMwareInventory, error) {
	inv, err := vmwareRESTCollect(ctx, cred)
	if err != nil {
		slog.Default().Warn("vmware: inventory collect failed", "url", cred.URL, "err", err)
		return &VMwareInventory{CollectedAt: time.Now().UTC()}, nil
	}
	return inv, nil
}

func vmwareRESTCollect(ctx context.Context, cred VMwareCred) (*VMwareInventory, error) {
	base := strings.TrimRight(strings.TrimSpace(cred.URL), "/")
	if base == "" {
		return nil, fmt.Errorf("vmware: empty url")
	}
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cred.Insecure}, //nolint:gosec // operator opt-in
	}
	cli := &http.Client{Timeout: 60 * time.Second, Transport: tr}

	session, err := vmwareLogin(ctx, cli, base, cred.Username, cred.Password)
	if err != nil {
		return nil, err
	}

	inv := &VMwareInventory{CollectedAt: time.Now().UTC()}
	if hosts, err := vmwareListHosts(ctx, cli, base, session); err == nil {
		inv.Hosts = hosts
	}
	if vms, err := vmwareListVMs(ctx, cli, base, session); err == nil {
		inv.VMs = vms
	}
	if dcs, err := vmwareListDatacenters(ctx, cli, base, session); err == nil {
		inv.Datacenters = dcs
	}
	_ = vmwareLogout(ctx, cli, base, session)
	return inv, nil
}

func vmwareLogin(ctx context.Context, cli *http.Client, base, user, pass string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/session", nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(user, pass)
	resp, err := cli.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("vmware session: %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	var session string
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return "", err
	}
	return strings.Trim(session, `"`), nil
}

func vmwareLogout(ctx context.Context, cli *http.Client, base, session string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, base+"/api/session", nil)
	if err != nil {
		return err
	}
	req.Header.Set("vmware-api-session-id", session)
	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func vmwareListVMs(ctx context.Context, cli *http.Client, base, session string) ([]VMwareVM, error) {
	var raw []struct {
		Name     string `json:"name"`
		PowerState string `json:"power_state"`
		VM       string `json:"vm"`
	}
	if err := vmwareGET(ctx, cli, base+"/api/vcenter/vm", session, &raw); err != nil {
		return nil, err
	}
	out := make([]VMwareVM, 0, len(raw))
	for _, r := range raw {
		out = append(out, VMwareVM{
			Name:    r.Name,
			PowerOn: strings.EqualFold(r.PowerState, "POWERED_ON"),
		})
	}
	return out, nil
}

func vmwareListHosts(ctx context.Context, cli *http.Client, base, session string) ([]VMwareHost, error) {
	var raw []struct {
		Name string `json:"name"`
		Host string `json:"host"`
	}
	if err := vmwareGET(ctx, cli, base+"/api/vcenter/host", session, &raw); err != nil {
		return nil, err
	}
	out := make([]VMwareHost, 0, len(raw))
	for _, r := range raw {
		out = append(out, VMwareHost{Name: r.Name, Vendor: "vmware"})
	}
	return out, nil
}

func vmwareListDatacenters(ctx context.Context, cli *http.Client, base, session string) ([]string, error) {
	var raw []struct {
		Name string `json:"name"`
	}
	if err := vmwareGET(ctx, cli, base+"/api/vcenter/datacenter", session, &raw); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		if r.Name != "" {
			out = append(out, r.Name)
		}
	}
	return out, nil
}

func vmwareGET(ctx context.Context, cli *http.Client, url, session string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("vmware-api-session-id", session)
	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("vmware GET %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
