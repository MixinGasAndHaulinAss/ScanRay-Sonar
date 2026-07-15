package checks

import (
	"context"
	"crypto/tls"
	"net"
	"strconv"
	"time"

	ldap "github.com/go-ldap/ldap/v3"
)

func runLDAPBind(ctx context.Context, params map[string]any, cred *ResolvedCred) Result {
	if cred == nil {
		return Result{OK: false, Error: "credential required", Samples: []Sample{sampleNum("up", 0)}}
	}
	host := paramString(params, "host", "")
	if host == "" {
		return Result{OK: false, Error: "host required", Samples: []Sample{sampleNum("up", 0)}}
	}
	useTLS := paramString(params, "useTLS", "") == "true" || paramString(params, "useTLS", "") == "1"
	if v, ok := params["useTLS"].(bool); ok {
		useTLS = v
	}
	if cred.UseTLS {
		useTLS = true
	}
	port := paramInt(params, "port", 0)
	if port == 0 {
		if useTLS {
			port = 636
		} else {
			port = 389
		}
	}
	bindDN := paramString(params, "bindDN", cred.Username)
	timeout := time.Duration(paramFloat(params, "timeoutSec", 15)) * time.Second

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	err := ldapBind(rctx, addr, host, useTLS, bindDN, cred.Password, timeout)
	ms := float64(time.Since(start).Milliseconds())
	if err != nil {
		return Result{OK: false, Error: err.Error(), Samples: []Sample{
			sampleNum("up", 0), sampleNum("response_time_ms", ms),
		}}
	}
	return Result{OK: true, Samples: []Sample{
		sampleNum("up", 1), sampleNum("response_time_ms", ms),
	}}
}

func ldapBind(ctx context.Context, addr, serverName string, useTLS bool, bindDN, password string, timeout time.Duration) error {
	opts := []ldap.DialOpt{
		ldap.DialWithDialer(&net.Dialer{Timeout: timeout}),
	}
	var conn *ldap.Conn
	var err error
	if useTLS {
		opts = append(opts, ldap.DialWithTLSConfig(&tls.Config{
			ServerName: serverName,
			MinVersion: tls.VersionTLS12,
		}))
		conn, err = ldap.DialURL("ldaps://"+addr, opts...)
	} else {
		conn, err = ldap.DialURL("ldap://"+addr, opts...)
	}
	if err != nil {
		return err
	}
	defer conn.Close()

	done := make(chan error, 1)
	go func() {
		done <- conn.Bind(bindDN, password)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return err
		}
	}
	return nil
}
