package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/NCLGISA/ScanRay-Sonar/internal/agentevents"
)

// handleAckAlarm marks an open alarm as acknowledged without closing it.
// Idempotent: re-acking an already-acked alarm is a no-op (still 204).
func (s *Server) handleAckAlarm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "alarm id must be int")
		return
	}
	uid := userIDFromCtx(r.Context())
	tag, err := s.pool.Exec(r.Context(), `
		UPDATE alarms
		   SET acked_at = COALESCE(acked_at, NOW()),
		       acked_by = COALESCE(acked_by, $2)
		 WHERE id = $1 AND cleared_at IS NULL`, id, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "ack failed")
		return
	}
	if tag.RowsAffected() == 0 {
		var cleared *string
		qerr := s.pool.QueryRow(r.Context(), `SELECT cleared_at::text FROM alarms WHERE id = $1`, id).Scan(&cleared)
		if errors.Is(qerr, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "not_found", "alarm not found")
			return
		}
		writeErr(w, http.StatusConflict, "already_cleared", "alarm already cleared")
		return
	}
	s.emitAlarmSystemEvent(r, id, agentevents.KindAlarmAcked, "info", "acked")
	s.store.Audit(r.Context(), "user", "alarm.ack", &uid, clientIP(r), map[string]any{"alarmId": id})
	w.WriteHeader(http.StatusNoContent)
}

// handleClearAlarm closes an open alarm.
func (s *Server) handleClearAlarm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "alarm id must be int")
		return
	}
	uid := userIDFromCtx(r.Context())
	tag, err := s.pool.Exec(r.Context(), `
		UPDATE alarms
		   SET cleared_at = NOW(), cleared_by = $2, auto_cleared = FALSE
		 WHERE id = $1 AND cleared_at IS NULL`, id, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "clear failed")
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "not_found", "alarm not found or already cleared")
		return
	}
	if s.nats != nil && s.nats.IsConnected() {
		_ = s.nats.Publish("alarm.cleared", []byte(`{"alarmId":`+strconv.FormatInt(id, 10)+`,"manual":true}`))
	}
	s.emitAlarmSystemEvent(r, id, agentevents.KindAlarmCleared, "info", "cleared")
	s.store.Audit(r.Context(), "user", "alarm.clear", &uid, clientIP(r), map[string]any{"alarmId": id})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) emitAlarmSystemEvent(r *http.Request, alarmID int64, kind, severity, verb string) {
	var siteID uuid.UUID
	var targetKind string
	var targetID uuid.UUID
	var title string
	err := s.pool.QueryRow(r.Context(), `
		SELECT site_id, target_kind, target_id, title FROM alarms WHERE id = $1`, alarmID).
		Scan(&siteID, &targetKind, &targetID, &title)
	if err != nil || targetKind != "agent" {
		return
	}
	aid := targetID
	_ = agentevents.Emit(r.Context(), s.pool, siteID, &aid, kind, severity,
		title+" "+verb, "", map[string]any{"alarmId": alarmID})
}
