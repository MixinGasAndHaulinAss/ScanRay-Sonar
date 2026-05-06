package notify

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"
)

// SMTPConfig holds outbound SMTP settings for alarm emails and tests.
type SMTPConfig struct {
	Host   string
	Port   int
	User   string
	Pass   string
	From   string
	UseTLS bool
}

// Valid reports whether this config can attempt a send (host/port/from required).
func (c SMTPConfig) Valid() bool {
	return c.Host != "" && c.Port > 0 && c.From != ""
}

// SendMailMsg sends a plain-text RFC822 message to all recipients in `to`.
// TLS uses STARTTLS after connect when UseTLS is true (matches Settings SMTP test).
func SendMailMsg(ctx context.Context, cfg SMTPConfig, to []string, subject, body string) error {
	if len(to) == 0 {
		return fmt.Errorf("notify: no recipients")
	}
	if !cfg.Valid() {
		return fmt.Errorf("notify: incomplete SMTP config")
	}
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	msg := buildPlainMessage(cfg.From, to, subject, body)

	if cfg.UseTLS {
		c, err := smtp.Dial(addr)
		if err != nil {
			return err
		}
		defer c.Close()
		tcfg := &tls.Config{ServerName: cfg.Host}
		if err := c.StartTLS(tcfg); err != nil {
			return err
		}
		if cfg.User != "" {
			auth := smtp.PlainAuth("", cfg.User, cfg.Pass, cfg.Host)
			if err := c.Auth(auth); err != nil {
				return err
			}
		}
		if err := c.Mail(cfg.From); err != nil {
			return err
		}
		for _, rcpt := range to {
			if err := c.Rcpt(strings.TrimSpace(rcpt)); err != nil {
				return err
			}
		}
		wc, err := c.Data()
		if err != nil {
			return err
		}
		if _, err := wc.Write(msg); err != nil {
			return err
		}
		return wc.Close()
	}

	auth := smtp.PlainAuth("", cfg.User, cfg.Pass, cfg.Host)
	done := make(chan error, 1)
	go func() { done <- smtp.SendMail(addr, auth, cfg.From, to, msg) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

func buildPlainMessage(from string, to []string, subject, body string) []byte {
	var b strings.Builder
	for _, t := range to {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		fmt.Fprintf(&b, "To: %s\r\n", t)
	}
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "Content-Type: text/plain; charset=UTF-8\r\n\r\n%s", body)
	return []byte(b.String())
}
