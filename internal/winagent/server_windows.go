//go:build windows

package winagent

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/yusufpapurcu/wmi"
)

// Config holds runtime knobs for ServeHTTPS. SharedToken is the bearer secret
// the collector handed us at enrollment; CertFile/KeyFile point at the mTLS
// chain. ListenAddr defaults to "0.0.0.0:8443".
type Config struct {
	SharedToken string
	CertFile    string
	KeyFile     string
	ListenAddr  string
}

// ServeHTTPS spins up the local agent server. It blocks until ctx is cancelled
// or ListenAndServeTLS returns an error.
func ServeHTTPS(ctx context.Context, cfg Config) error {
	if cfg.SharedToken == "" {
		return errors.New("winagent: SharedToken required")
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "0.0.0.0:8443"
	}
	mux := http.NewServeMux()
	mux.Handle("/healthz", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	mux.Handle("/v1/inventory", auth(cfg.SharedToken, http.HandlerFunc(inventoryHandler)))

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		if cfg.CertFile != "" && cfg.KeyFile != "" {
			errCh <- srv.ListenAndServeTLS(cfg.CertFile, cfg.KeyFile)
		} else {
			errCh <- srv.ListenAndServe()
		}
	}()
	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func auth(token string, next http.Handler) http.Handler {
	want := sha256.Sum256([]byte(token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		got := sha256.Sum256([]byte(raw))
		if subtle.ConstantTimeCompare(want[:], got[:]) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func inventoryHandler(w http.ResponseWriter, r *http.Request) {
	inv, err := Collect(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf("collect failed: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(inv)
}

// Collect runs the WMI queries and returns a populated Inventory.
func Collect(ctx context.Context) (*Inventory, error) {
	inv := &Inventory{CollectedAt: time.Now().UTC()}

	var os []struct {
		Caption        string
		Version        string
		BuildNumber    string
		OSArchitecture string
		InstallDate    string
		LastBootUpTime string
	}
	if err := wmi.Query("SELECT Caption, Version, BuildNumber, OSArchitecture, InstallDate, LastBootUpTime FROM Win32_OperatingSystem", &os); err == nil && len(os) > 0 {
		inv.OS = OSInfo{
			Name:         os[0].Caption,
			Version:      os[0].Version,
			Architecture: os[0].OSArchitecture,
			InstallDate:  os[0].InstallDate,
			LastBootUp:   os[0].LastBootUpTime,
			BuildNumber:  os[0].BuildNumber,
		}
	}

	var cs []struct {
		DNSHostName  string
		Domain       string
		Manufacturer string
		Model        string
	}
	if err := wmi.Query("SELECT DNSHostName, Domain, Manufacturer, Model FROM Win32_ComputerSystem", &cs); err == nil && len(cs) > 0 {
		inv.Computer = Computer{
			Hostname:     cs[0].DNSHostName,
			Domain:       cs[0].Domain,
			Manufacturer: cs[0].Manufacturer,
			Model:        cs[0].Model,
		}
	}

	var bios []struct {
		Manufacturer      string
		SMBIOSBIOSVersion string
		SerialNumber      string
		ReleaseDate       string
	}
	if err := wmi.Query("SELECT Manufacturer, SMBIOSBIOSVersion, SerialNumber, ReleaseDate FROM Win32_BIOS", &bios); err == nil && len(bios) > 0 {
		inv.BIOS = BIOSInfo{
			Vendor:   bios[0].Manufacturer,
			Version:  bios[0].SMBIOSBIOSVersion,
			Serial:   bios[0].SerialNumber,
			Released: bios[0].ReleaseDate,
		}
		if inv.Computer.SerialNumber == "" {
			inv.Computer.SerialNumber = bios[0].SerialNumber
		}
	}

	var nics []struct {
		Description         string
		MACAddress          string
		IPAddress           []string
		Speed               uint64
		NetConnectionStatus uint16
	}
	if err := wmi.Query("SELECT Description, MACAddress, IPAddress, Speed, NetConnectionStatus FROM Win32_NetworkAdapterConfiguration WHERE IPEnabled=TRUE", &nics); err == nil {
		for _, n := range nics {
			inv.NICs = append(inv.NICs, NIC{
				Description: n.Description,
				MAC:         n.MACAddress,
				IPv4:        n.IPAddress,
				SpeedMbps:   int64(n.Speed) / 1_000_000,
				Connected:   n.NetConnectionStatus == 2,
			})
		}
	}

	var disks []struct {
		Name       string
		FileSystem string
		Size       uint64
		FreeSpace  uint64
	}
	if err := wmi.Query("SELECT Name, FileSystem, Size, FreeSpace FROM Win32_LogicalDisk WHERE DriveType=3", &disks); err == nil {
		for _, d := range disks {
			inv.Disks = append(inv.Disks, Disk{
				Mount:     d.Name,
				FSType:    d.FileSystem,
				SizeBytes: int64(d.Size),
				FreeBytes: int64(d.FreeSpace),
			})
		}
	}

	return inv, nil
}
