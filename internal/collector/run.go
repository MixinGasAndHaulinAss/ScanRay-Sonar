package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Run maintains outbound websocket + periodic heartbeats to central Sonar.
func Run(ctx context.Context, log *slog.Logger, cfg *Config) error {
	if cfg.IngestWS == "" || cfg.JWT == "" || cfg.CollectorID == "" {
		return fmt.Errorf("collector: incomplete config (need ingestWs, jwt, collectorId)")
	}
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return nil
		}
		if err := dialAndPump(ctx, log, cfg); err != nil {
			log.Warn("collector ingest ended", "err", err, "retry_s", backoff.Seconds())
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
	log.Info("collector connected", "collector_id", cfg.CollectorID)

	w := newWSWriter(ctx, log, c)

	hello, _ := json.Marshal(map[string]any{
		"type":             "hello",
		"collectorId":      cfg.CollectorID,
		"name":             cfg.Name,
		"hostname":         cfg.Hostname,
		"collectorVersion": cfg.CollectorVersion,
		"sentAt":           time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err := w.send(hello); err != nil {
		return fmt.Errorf("hello: %w", err)
	}

	loopCtx, loopCancel := context.WithCancel(ctx)
	defer loopCancel()
	go heartbeatLoop(loopCtx, log, w, cfg)

	for {
		_, _, err := c.Read(ctx)
		if err != nil {
			return err
		}
	}
}

type wsWriter struct {
	c   *websocket.Conn
	in  chan []byte
	ctx context.Context
	log *slog.Logger

	doneCh chan struct{}
	err    error
	errMu  sync.Mutex
}

func newWSWriter(ctx context.Context, log *slog.Logger, c *websocket.Conn) *wsWriter {
	w := &wsWriter{
		c:      c,
		in:     make(chan []byte, 8),
		ctx:    ctx,
		log:    log,
		doneCh: make(chan struct{}),
	}
	go w.pump()
	return w
}

func (w *wsWriter) pump() {
	defer close(w.doneCh)
	for {
		select {
		case <-w.ctx.Done():
			return
		case msg, ok := <-w.in:
			if !ok {
				return
			}
			ctxW, cancel := context.WithTimeout(w.ctx, 10*time.Second)
			err := w.c.Write(ctxW, websocket.MessageText, msg)
			cancel()
			if err != nil {
				w.errMu.Lock()
				w.err = err
				w.errMu.Unlock()
				w.log.Warn("collector ws write failed", "err", err)
				return
			}
		}
	}
}

func (w *wsWriter) send(msg []byte) error {
	w.errMu.Lock()
	if w.err != nil {
		err := w.err
		w.errMu.Unlock()
		return err
	}
	w.errMu.Unlock()
	select {
	case w.in <- msg:
		return nil
	case <-w.ctx.Done():
		return w.ctx.Err()
	default:
		return fmt.Errorf("collector ws writer buffer full")
	}
}

func heartbeatLoop(ctx context.Context, log *slog.Logger, w *wsWriter, cfg *Config) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			payload, _ := json.Marshal(map[string]any{
				"type":        "heartbeat",
				"collectorId": cfg.CollectorID,
				"sentAt":      time.Now().UTC().Format(time.RFC3339Nano),
			})
			if err := w.send(payload); err != nil {
				log.Warn("collector heartbeat failed", "err", err)
				return
			}
		}
	}
}

// RedactWSURL strips token query param for logging.
func RedactWSURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	if q.Has("token") {
		q.Set("token", "(redacted)")
		u.RawQuery = q.Encode()
	}
	return strings.TrimSpace(u.String())
}
