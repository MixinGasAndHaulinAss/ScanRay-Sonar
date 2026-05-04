package probe

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

	// writer serialises every Write to the connection. websocket.Conn
	// is read-safe but only one writer at a time, and we now have two
	// goroutines that want to write: the heartbeat ticker and the
	// snapshot ticker. A small buffered channel + dedicated writer
	// goroutine sidesteps the need for a mutex on the connection.
	w := newWSWriter(ctx, log, c)

	hello, _ := json.Marshal(map[string]any{
		"type":         "hello",
		"agentId":      cfg.AgentID,
		"hostname":     cfg.Hostname,
		"agentVersion": cfg.AgentVersion,
		"sentAt":       time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err := w.send(hello); err != nil {
		return fmt.Errorf("ws write hello: %w", err)
	}

	loopCtx, loopCancel := context.WithCancel(ctx)
	defer loopCancel()
	go heartbeatLoop(loopCtx, log, w, cfg)
	go snapshotLoop(loopCtx, log, w, cfg)
	go runLatencyLoop(loopCtx, log, cfg.LatencyTarget)
	go runHealthLoop(loopCtx, log)

	for {
		_, _, err := c.Read(ctx)
		if err != nil {
			return err
		}
		// Phase 2.5 will demux command frames here (run-on-demand
		// checks, push config, etc.). For now we just consume.
	}
}

// wsWriter funnels every outbound text frame through a single
// goroutine so heartbeat + snapshot writers don't trip over each other
// (the websocket library only allows one concurrent writer).
type wsWriter struct {
	c   *websocket.Conn
	in  chan []byte
	ctx context.Context
	log *slog.Logger

	closeOnce sync.Once
	doneCh    chan struct{}
	err       error
	errMu     sync.Mutex
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
				w.log.Warn("ws write failed", "err", err)
				return
			}
		}
	}
}

// send queues msg for the writer goroutine. Drops a frame and returns
// an error if the buffer is full (back-pressure: the connection is
// stuck or the server is slow; we'd rather miss one snapshot than
// stall the probe).
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
		return fmt.Errorf("ws writer buffer full; dropping frame")
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
				"type":    "heartbeat",
				"agentId": cfg.AgentID,
				"sentAt":  time.Now().UTC().Format(time.RFC3339Nano),
			})
			if err := w.send(payload); err != nil {
				log.Warn("heartbeat send failed", "err", err)
				return
			}
		}
	}
}

// snapshotLoop ships a fresh telemetry snapshot at startup (after a
// brief warm-up so gopsutil's CPU% has a delta to compute against) and
// then once per minute. The capture is done in a goroutine so it
// can't block the ticker if a particular sub-collector hangs.
func snapshotLoop(ctx context.Context, log *slog.Logger, w *wsWriter, cfg *Config) {
	// Warm-up: prime gopsutil's CPU counters so the first real
	// snapshot has non-zero per-process CPU%.
	select {
	case <-ctx.Done():
		return
	case <-time.After(5 * time.Second):
	}

	send := func() {
		captureCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		snap := CollectSnapshot(captureCtx, cfg.Hostname)
		cancel()
		payload, err := json.Marshal(map[string]any{
			"type":     "metrics",
			"agentId":  cfg.AgentID,
			"sentAt":   time.Now().UTC().Format(time.RFC3339Nano),
			"snapshot": snap,
		})
		if err != nil {
			log.Warn("snapshot marshal failed", "err", err)
			return
		}
		if err := w.send(payload); err != nil {
			log.Warn("snapshot send failed", "err", err)
		} else {
			log.Info("snapshot sent",
				"capture_ms", snap.CaptureMs,
				"bytes", len(payload),
				"warnings", len(snap.CollectionWarnings),
			)
		}
	}

	send()
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			send()
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
