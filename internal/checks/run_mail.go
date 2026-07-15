package checks

import (
	"context"
	"crypto/tls"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

func runSMTP(ctx context.Context, params map[string]any, cred *ResolvedCred) Result {
	if cred == nil {
		return Result{OK: false, Error: "credential required", Samples: []Sample{sampleNum("up", 0)}}
	}
	host := paramString(params, "host", "")
	if host == "" {
		return Result{OK: false, Error: "host required", Samples: []Sample{sampleNum("up", 0)}}
	}
	port := paramInt(params, "port", 587)
	tlsMode := strings.ToLower(paramString(params, "tlsMode", "starttls"))
	from := paramString(params, "from", cred.Username)
	to := paramString(params, "to", "")
	timeout := time.Duration(paramFloat(params, "timeoutSec", 15)) * time.Second

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	err := smtpProbe(rctx, addr, host, tlsMode, from, to, cred)
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

func smtpProbe(ctx context.Context, addr, serverName, tlsMode, from, to string, cred *ResolvedCred) error {
	d := net.Dialer{}
	raw, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer raw.Close()
	_ = raw.SetDeadline(deadlineFrom(ctx))

	var c *smtp.Client
	switch tlsMode {
	case "tls", "smtps":
		tlsConn := tls.Client(raw, &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return err
		}
		c, err = smtp.NewClient(tlsConn, serverName)
	default:
		c, err = smtp.NewClient(raw, serverName)
	}
	if err != nil {
		return err
	}
	defer c.Close()

	if tlsMode == "starttls" || tlsMode == "" {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12}); err != nil {
				return err
			}
		}
	}

	if ok, _ := c.Extension("AUTH"); ok && cred.Username != "" {
		auth := smtp.PlainAuth("", cred.Username, cred.Password, serverName)
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if from != "" {
		if err := c.Mail(from); err != nil {
			return err
		}
		if to != "" {
			if err := c.Rcpt(to); err != nil {
				return err
			}
		}
		// Reset so we never DATA a message.
		_ = c.Reset()
	}
	return c.Quit()
}

func runIMAP(ctx context.Context, params map[string]any, cred *ResolvedCred) Result {
	if cred == nil {
		return Result{OK: false, Error: "credential required", Samples: []Sample{sampleNum("up", 0)}}
	}
	host := paramString(params, "host", "")
	if host == "" {
		return Result{OK: false, Error: "host required", Samples: []Sample{sampleNum("up", 0)}}
	}
	tlsMode := strings.ToLower(paramString(params, "tlsMode", "tls"))
	port := paramInt(params, "port", 993)
	if port == 993 && tlsMode == "plain" {
		port = 143
	}
	if port == 0 {
		if tlsMode == "tls" {
			port = 993
		} else {
			port = 143
		}
	}
	timeout := time.Duration(paramFloat(params, "timeoutSec", 15)) * time.Second
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	count, err := imapProbe(rctx, addr, host, tlsMode, cred)
	ms := float64(time.Since(start).Milliseconds())
	if err != nil {
		return Result{OK: false, Error: err.Error(), Samples: []Sample{
			sampleNum("up", 0), sampleNum("response_time_ms", ms), sampleNum("mailbox_count", 0),
		}}
	}
	return Result{OK: true, Samples: []Sample{
		sampleNum("up", 1), sampleNum("response_time_ms", ms), sampleNum("mailbox_count", float64(count)),
	}}
}

func imapProbe(ctx context.Context, addr, serverName, tlsMode string, cred *ResolvedCred) (int, error) {
	d := net.Dialer{}
	raw, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return 0, err
	}
	_ = raw.SetDeadline(deadlineFrom(ctx))

	var c *client.Client
	switch tlsMode {
	case "tls", "imaps", "":
		tlsConn := tls.Client(raw, &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = raw.Close()
			return 0, err
		}
		c, err = client.New(tlsConn)
	case "starttls":
		c, err = client.New(raw)
		if err == nil {
			if err = c.StartTLS(&tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12}); err != nil {
				_ = c.Logout()
				return 0, err
			}
		}
	default: // plain
		c, err = client.New(raw)
	}
	if err != nil {
		_ = raw.Close()
		return 0, err
	}
	defer func() { _ = c.Logout() }()

	if err := c.Login(cred.Username, cred.Password); err != nil {
		return 0, err
	}

	mailboxes := make(chan *imap.MailboxInfo, 16)
	done := make(chan error, 1)
	go func() {
		done <- c.List("", "*", mailboxes)
	}()
	n := 0
	for range mailboxes {
		n++
	}
	if err := <-done; err != nil {
		return n, err
	}
	return n, nil
}

func deadlineFrom(ctx context.Context) time.Time {
	if dl, ok := ctx.Deadline(); ok {
		return dl
	}
	return time.Now().Add(15 * time.Second)
}
