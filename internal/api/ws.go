package api

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Hub is a minimal websocket fan-out used by both the agent ingest path
// (/agent/ws) and the UI live-updates path (/ws). Phase 1 just keeps
// connections alive with ping/pong and a write loop; real ingest handlers
// land in Phase 2 once the Probe sends data.
type Hub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]struct{}
	log     *slog.Logger
}

func NewHub(log *slog.Logger) *Hub {
	return &Hub{
		clients: make(map[*websocket.Conn]struct{}),
		log:     log,
	}
}

func (h *Hub) add(c *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[c] = struct{}{}
}

func (h *Hub) drop(c *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, c)
}

// Count returns the current connected client count (cheap; takes a read lock).
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Serve handles a websocket upgrade and runs the read/keepalive loop
// until the client disconnects or ctx cancels.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, kind string) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Cloudflare-fronted; in-cluster compose will set this list explicitly.
		InsecureSkipVerify: true,
	})
	if err != nil {
		h.log.Warn("ws accept failed", "err", err, "kind", kind)
		return
	}
	defer c.CloseNow()

	h.add(c)
	defer h.drop(c)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Server-side keepalive every 25s. Browsers and Cloudflare both like
	// frequent enough pings to avoid the idle-timeout reaper.
	go func() {
		t := time.NewTicker(25 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				ping, pcancel := context.WithTimeout(ctx, 5*time.Second)
				if err := c.Ping(ping); err != nil {
					pcancel()
					return
				}
				pcancel()
			}
		}
	}()

	for {
		_, _, err := c.Read(ctx)
		if err != nil {
			return
		}
		// Phase 2 will route inbound frames to ingest handlers.
	}
}
