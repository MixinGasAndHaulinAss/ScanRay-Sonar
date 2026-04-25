package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// Run holds open a websocket to the ingest endpoint, sending periodic
// heartbeat frames and reconnecting on error. It returns when ctx is
// cancelled.
func Run(ctx context.Context, log *slog.Logger, cfg *Config) error {
	if cfg.IngestWS == "" {
		return fmt.Errorf("probe: missing ingestWs in config")
	}

	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return nil
		}
		if err := dialAndPump(ctx, log, cfg); err != nil {
			log.Warn("ingest connection ended", "err", err, "next_retry_s", backoff.Seconds())
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(backoff):
			}
			if backoff < 60*time.Second {
				backoff *= 2
				if backoff > 60*time.Second {
					backoff = 60 * time.Second
				}
			}
			continue
		}
		backoff = time.Second
	}
}

func dialAndPump(ctx context.Context, log *slog.Logger, cfg *Config) error {
	u, err := url.Parse(cfg.IngestWS)
	if err != nil {
		return fmt.Errorf("parse ingestWs: %w", err)
	}
	q := u.Query()
	q.Set("token", cfg.JWT)
	u.RawQuery = q.Encode()

	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(dialCtx, u.String(), nil)
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}
	defer c.CloseNow()
	log.Info("connected to ingest", "url", redactQuery(u.String()))

	hello, _ := json.Marshal(map[string]any{
		"type":         "hello",
		"agentId":      cfg.AgentID,
		"hostname":     cfg.Hostname,
		"agentVersion": cfg.AgentVersion,
		"sentAt":       time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err := c.Write(ctx, websocket.MessageText, hello); err != nil {
		return fmt.Errorf("ws write hello: %w", err)
	}

	hbCtx, hbCancel := context.WithCancel(ctx)
	defer hbCancel()
	go heartbeat(hbCtx, log, c, cfg)

	for {
		_, _, err := c.Read(ctx)
		if err != nil {
			return err
		}
		// Phase 2.5 will demux command frames here (run-on-demand
		// checks, push config, etc.). For now we just consume.
	}
}

func heartbeat(ctx context.Context, log *slog.Logger, c *websocket.Conn, cfg *Config) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			payload, _ := json.Marshal(map[string]any{
				"type":    "heartbeat",
				"agentId": cfg.AgentID,
				"sentAt":  time.Now().UTC().Format(time.RFC3339Nano),
			})
			ctxW, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.Write(ctxW, websocket.MessageText, payload)
			cancel()
			if err != nil {
				log.Warn("heartbeat write failed", "err", err)
				return
			}
		}
	}
}

// redactQuery strips the JWT before logging the URL.
func redactQuery(s string) string {
	i := strings.Index(s, "?")
	if i < 0 {
		return s
	}
	return s[:i] + "?token=REDACTED"
}
