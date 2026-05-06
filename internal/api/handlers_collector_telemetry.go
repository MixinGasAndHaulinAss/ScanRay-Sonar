package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

type collectorFrameEnvelope struct {
	Type        string `json:"type"`
	CollectorID string `json:"collectorId"`
	SentAt      string `json:"sentAt"`
}

func (s *Server) runCollectorIngestLoop(parent context.Context, w http.ResponseWriter, r *http.Request, collectorID uuid.UUID) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		s.log.Warn("collector ws accept failed", "err", err, "collector_id", collectorID)
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(16 * 1024 * 1024)

	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	go agentKeepalive(ctx, c)

	log := s.log.With("collector_id", collectorID)

	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			if isNormalWSClose(err) || errors.Is(err, context.Canceled) {
				log.Debug("collector ws closed", "err", err)
			} else {
				log.Warn("collector ws read failed", "err", err)
			}
			return
		}
		s.handleCollectorFrame(ctx, log, collectorID, data)
	}
}

func (s *Server) handleCollectorFrame(ctx context.Context, log *slog.Logger, collectorID uuid.UUID, data []byte) {
	var env collectorFrameEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return
	}
	if env.CollectorID != "" {
		parsed, err := uuid.Parse(env.CollectorID)
		if err != nil || parsed != collectorID {
			return
		}
	}

	switch env.Type {
	case "hello":
		var full struct {
			Name             string `json:"name"`
			CollectorVersion string `json:"collectorVersion"`
			Hostname         string `json:"hostname"`
		}
		_ = json.Unmarshal(data, &full)
		name, ver, host := full.Name, full.CollectorVersion, full.Hostname
		if name != "" || ver != "" || host != "" {
			_, err := s.pool.Exec(ctx, `
				UPDATE collectors
				   SET name = COALESCE(NULLIF($2,''), name),
				       collector_version = COALESCE(NULLIF($3,''), collector_version),
				       hostname = COALESCE(NULLIF($4,''), hostname),
				       last_seen_at = NOW()
				 WHERE id = $1`,
				collectorID, name, ver, host)
			if err != nil {
				log.Warn("collector hello update failed", "err", err)
			}
		} else {
			_, _ = s.pool.Exec(ctx, `UPDATE collectors SET last_seen_at = NOW() WHERE id = $1`, collectorID)
		}
	case "heartbeat":
		_, err := s.pool.Exec(ctx, `UPDATE collectors SET last_seen_at = NOW() WHERE id = $1`, collectorID)
		if err != nil {
			log.Warn("collector heartbeat touch failed", "err", err)
		}
	default:
		// Phase 2+: discovery payloads, polled metrics, etc.
	}
}
