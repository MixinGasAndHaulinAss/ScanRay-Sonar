package checks

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// Run executes a check of the given type_id with JSON-like params.
func Run(ctx context.Context, typeID string, params map[string]any) Result {
	switch strings.ToLower(strings.TrimSpace(typeID)) {
	case "icmp":
		return runICMP(ctx, params)
	case "tcp":
		return runTCP(ctx, params)
	case "http":
		return runHTTP(ctx, params)
	case "dns":
		return runDNS(ctx, params)
	case "tls":
		return runTLS(ctx, params)
	default:
		return Result{OK: false, Error: "unknown check type: " + typeID}
	}
}

func paramString(params map[string]any, key, def string) string {
	if params == nil {
		return def
	}
	v, ok := params[key]
	if !ok || v == nil {
		return def
	}
	switch t := v.(type) {
	case string:
		if t == "" {
			return def
		}
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	default:
		return fmt.Sprint(t)
	}
}

func paramFloat(params map[string]any, key string, def float64) float64 {
	if params == nil {
		return def
	}
	v, ok := params[key]
	if !ok || v == nil {
		return def
	}
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case string:
		f, err := strconv.ParseFloat(t, 64)
		if err != nil {
			return def
		}
		return f
	default:
		return def
	}
}

func paramInt(params map[string]any, key string, def int) int {
	return int(paramFloat(params, key, float64(def)))
}

func sampleNum(key string, v float64) Sample {
	return Sample{Key: key, Value: v, HasNum: true}
}

func runICMP(ctx context.Context, params map[string]any) Result {
	host := paramString(params, "host", "")
	if host == "" {
		return Result{OK: false, Error: "host required"}
	}
	ip := host
	if net.ParseIP(host) == nil {
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
		if err != nil || len(ips) == 0 {
			return Result{OK: false, Error: "resolve: " + errString(err), Samples: []Sample{sampleNum("up", 0)}}
		}
		ip = ips[0].String()
	}
	avgMs, lossPct, err := pingICMP(ctx, ip)
	if err != nil {
		return Result{OK: false, Error: err.Error(), Samples: []Sample{sampleNum("up", 0), sampleNum("packet_loss_pct", 100)}}
	}
	up := 0.0
	if lossPct < 100 {
		up = 1
	}
	return Result{
		OK: up == 1,
		Samples: []Sample{
			sampleNum("response_time_ms", avgMs),
			sampleNum("packet_loss_pct", lossPct),
			sampleNum("up", up),
		},
	}
}

func pingICMP(ctx context.Context, addr string) (avgMs, lossPct float64, err error) {
	ip := net.ParseIP(addr)
	if ip == nil || ip.To4() == nil {
		return 0, 100, fmt.Errorf("icmp: %q is not a valid IPv4 address", addr)
	}
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return 0, 100, fmt.Errorf("icmp listen failed on %s: %w", runtime.GOOS, err)
	}
	defer conn.Close()

	const tries = 4
	const gap = 200 * time.Millisecond
	const replyWait = 1 * time.Second
	rtts := make([]time.Duration, 0, tries)
	id := os.Getpid() & 0xffff
	dst := &net.IPAddr{IP: ip}

	for i := 0; i < tries; i++ {
		if ctx.Err() != nil {
			break
		}
		seq := i + 1
		body := make([]byte, 16)
		binary.BigEndian.PutUint64(body[0:8], uint64(time.Now().UnixNano()))
		msg := icmp.Message{
			Type: ipv4.ICMPTypeEcho,
			Code: 0,
			Body: &icmp.Echo{ID: id, Seq: seq, Data: body},
		}
		out, mErr := msg.Marshal(nil)
		if mErr != nil {
			continue
		}
		start := time.Now()
		if _, wErr := conn.WriteTo(out, dst); wErr != nil {
			continue
		}
		_ = conn.SetReadDeadline(time.Now().Add(replyWait))
		buf := make([]byte, 1500)
		for {
			n, _, rErr := conn.ReadFrom(buf)
			if rErr != nil {
				break
			}
			parsed, pErr := icmp.ParseMessage(1, buf[:n])
			if pErr != nil {
				continue
			}
			if parsed.Type != ipv4.ICMPTypeEchoReply {
				continue
			}
			echo, ok := parsed.Body.(*icmp.Echo)
			if !ok || echo.ID != id || echo.Seq != seq {
				continue
			}
			rtts = append(rtts, time.Since(start))
			break
		}
		if i < tries-1 {
			select {
			case <-ctx.Done():
			case <-time.After(gap):
			}
		}
	}
	if len(rtts) == 0 {
		return 0, 100, nil
	}
	sort.Slice(rtts, func(i, j int) bool { return rtts[i] < rtts[j] })
	var sum time.Duration
	for _, r := range rtts {
		sum += r
	}
	avg := float64(sum) / float64(len(rtts)) / float64(time.Millisecond)
	loss := float64(tries-len(rtts)) / float64(tries) * 100
	return avg, loss, nil
}

func runTCP(ctx context.Context, params map[string]any) Result {
	host := paramString(params, "host", "")
	port := paramInt(params, "port", 0)
	timeout := time.Duration(paramFloat(params, "timeoutSec", 5)) * time.Second
	if host == "" || port <= 0 {
		return Result{OK: false, Error: "host and port required"}
	}
	d := net.Dialer{Timeout: timeout}
	start := time.Now()
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	ms := float64(time.Since(start).Milliseconds())
	if err != nil {
		return Result{OK: false, Error: err.Error(), Samples: []Sample{sampleNum("up", 0), sampleNum("response_time_ms", ms)}}
	}
	_ = conn.Close()
	return Result{OK: true, Samples: []Sample{sampleNum("up", 1), sampleNum("response_time_ms", ms)}}
}

func runHTTP(ctx context.Context, params map[string]any) Result {
	rawURL := paramString(params, "url", "")
	if rawURL == "" {
		host := paramString(params, "host", "")
		if host == "" {
			return Result{OK: false, Error: "url or host required"}
		}
		rawURL = "https://" + host
	}
	expect := paramInt(params, "expectCode", 200)
	timeout := time.Duration(paramFloat(params, "timeoutSec", 10)) * time.Second
	cli := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // synthetic check; cert validity is separate tls type
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Result{OK: false, Error: err.Error(), Samples: []Sample{sampleNum("up", 0)}}
	}
	start := time.Now()
	resp, err := cli.Do(req)
	ms := float64(time.Since(start).Milliseconds())
	if err != nil {
		return Result{OK: false, Error: err.Error(), Samples: []Sample{
			sampleNum("up", 0), sampleNum("response_time_ms", ms), sampleNum("status_code", 0),
		}}
	}
	defer resp.Body.Close()
	up := 0.0
	if resp.StatusCode == expect || (expect == 200 && resp.StatusCode >= 200 && resp.StatusCode < 400) {
		up = 1
	}
	return Result{
		OK: up == 1,
		Samples: []Sample{
			sampleNum("response_time_ms", ms),
			sampleNum("status_code", float64(resp.StatusCode)),
			sampleNum("up", up),
		},
	}
}

func runDNS(ctx context.Context, params map[string]any) Result {
	host := paramString(params, "host", "")
	if host == "" {
		return Result{OK: false, Error: "host required"}
	}
	rtype := strings.ToUpper(paramString(params, "recordType", "A"))
	resolverName := paramString(params, "resolver", "")
	r := net.DefaultResolver
	if resolverName != "" {
		r = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: 5 * time.Second}
				return d.DialContext(ctx, "udp", net.JoinHostPort(resolverName, "53"))
			},
		}
	}
	start := time.Now()
	var count int
	var err error
	switch rtype {
	case "AAAA":
		var ips []net.IP
		ips, err = r.LookupIP(ctx, "ip6", host)
		count = len(ips)
	case "CNAME":
		var cname string
		cname, err = r.LookupCNAME(ctx, host)
		if err == nil && cname != "" {
			count = 1
		}
	default:
		var ips []net.IP
		ips, err = r.LookupIP(ctx, "ip4", host)
		count = len(ips)
	}
	ms := float64(time.Since(start).Milliseconds())
	if err != nil || count == 0 {
		return Result{OK: false, Error: errString(err), Samples: []Sample{
			sampleNum("up", 0), sampleNum("record_count", 0), sampleNum("response_time_ms", ms),
		}}
	}
	return Result{OK: true, Samples: []Sample{
		sampleNum("up", 1), sampleNum("record_count", float64(count)), sampleNum("response_time_ms", ms),
	}}
}

func runTLS(ctx context.Context, params map[string]any) Result {
	host := paramString(params, "host", "")
	port := paramInt(params, "port", 443)
	sni := paramString(params, "sni", host)
	timeout := time.Duration(paramFloat(params, "timeoutSec", 10)) * time.Second
	if host == "" {
		return Result{OK: false, Error: "host required"}
	}
	d := &net.Dialer{Timeout: timeout}
	raw, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return Result{OK: false, Error: err.Error(), Samples: []Sample{sampleNum("up", 0)}}
	}
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(timeout))
	cfg := &tls.Config{
		ServerName:         sni,
		InsecureSkipVerify: true, //nolint:gosec // we inspect PeerCertificates ourselves
	}
	conn := tls.Client(raw, cfg)
	if err := conn.HandshakeContext(ctx); err != nil {
		return Result{OK: false, Error: err.Error(), Samples: []Sample{sampleNum("up", 0)}}
	}
	defer conn.Close()
	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return Result{OK: false, Error: "no peer certificate", Samples: []Sample{sampleNum("up", 0)}}
	}
	cert := state.PeerCertificates[0]
	days := float64(time.Until(cert.NotAfter).Hours() / 24)
	cnOK := 0.0
	if certMatches(cert, sni) {
		cnOK = 1
	}
	return Result{
		OK: true,
		Samples: []Sample{
			sampleNum("days_to_expiration", days),
			sampleNum("cn_match", cnOK),
			sampleNum("up", 1),
		},
	}
}

func certMatches(cert *x509.Certificate, name string) bool {
	if name == "" {
		return true
	}
	if err := cert.VerifyHostname(name); err == nil {
		return true
	}
	return strings.EqualFold(cert.Subject.CommonName, name)
}

func errString(err error) string {
	if err == nil {
		return "no records"
	}
	return err.Error()
}
