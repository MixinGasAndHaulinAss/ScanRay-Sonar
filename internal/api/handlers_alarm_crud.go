package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (s *Server) handleCreateAlarmRule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SiteID          *string  `json:"siteId"`
		Name            string   `json:"name"`
		Severity        string   `json:"severity"`
		Expression      string   `json:"expression"`
		ChannelIDs      []string `json:"channelIds"`
		Enabled         *bool    `json:"enabled"`
		ForSeconds      *int     `json:"forSeconds,omitempty"`
		ClearForSeconds *int     `json:"clearForSeconds,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.Expression == "" || req.Severity == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "name, severity, expression required")
		return
	}
	chIDs := []uuid.UUID{}
	for _, x := range req.ChannelIDs {
		id, err := uuid.Parse(x)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid channel id")
			return
		}
		chIDs = append(chIDs, id)
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	uid := userIDFromCtx(r.Context())
	var sitePtr *uuid.UUID
	if req.SiteID != nil && *req.SiteID != "" {
		sid, err := uuid.Parse(*req.SiteID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid siteId")
			return
		}
		sitePtr = &sid
	}
	forSec := 0
	if req.ForSeconds != nil && *req.ForSeconds >= 0 {
		forSec = *req.ForSeconds
	}
	clearSec := 0
	if req.ClearForSeconds != nil && *req.ClearForSeconds >= 0 {
		clearSec = *req.ClearForSeconds
	}
	var id uuid.UUID
	err := s.pool.QueryRow(r.Context(), `
		INSERT INTO alarm_rules (site_id, name, severity, expression, channel_ids, enabled, created_by, for_seconds, clear_for_seconds)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		sitePtr, req.Name, req.Severity, req.Expression, chIDs, enabled, nullableUID(uid), forSec, clearSec).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "insert failed")
		return
	}
	s.store.Audit(r.Context(), "user", "alarm_rule.create", &uid, clientIP(r), map[string]any{"rule_id": id.String()})
	writeJSON(w, http.StatusCreated, map[string]any{"id": id.String()})
}

func (s *Server) handlePatchAlarmRule(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var req struct {
		Name            *string `json:"name,omitempty"`
		Severity        *string `json:"severity,omitempty"`
		Expression      *string `json:"expression,omitempty"`
		Enabled         *bool   `json:"enabled,omitempty"`
		ForSeconds      *int    `json:"forSeconds,omitempty"`
		ClearForSeconds *int    `json:"clearForSeconds,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	sets := []string{}
	args := []any{}
	add := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, col+" = $"+itoa(len(args)))
	}
	if req.Name != nil {
		add("name", *req.Name)
	}
	if req.Severity != nil {
		add("severity", *req.Severity)
	}
	if req.Expression != nil {
		add("expression", *req.Expression)
	}
	if req.Enabled != nil {
		add("enabled", *req.Enabled)
	}
	if req.ForSeconds != nil && *req.ForSeconds >= 0 {
		add("for_seconds", *req.ForSeconds)
	}
	if req.ClearForSeconds != nil && *req.ClearForSeconds >= 0 {
		add("clear_for_seconds", *req.ClearForSeconds)
	}
	if len(sets) == 0 {
		writeErr(w, http.StatusBadRequest, "bad_request", "no fields")
		return
	}
	args = append(args, id)
	q := "UPDATE alarm_rules SET " + join(sets, ", ") + " WHERE id = $" + itoa(len(args))
	tag, err := s.pool.Exec(r.Context(), q, args...)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "update failed")
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "not_found", "rule not found")
		return
	}
	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "alarm_rule.update", &uid, clientIP(r), map[string]any{"rule_id": id.String()})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteAlarmRule(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	tag, err := s.pool.Exec(r.Context(), `DELETE FROM alarm_rules WHERE id = $1`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "delete failed")
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "not_found", "rule not found")
		return
	}
	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "alarm_rule.delete", &uid, clientIP(r), map[string]any{"rule_id": id.String()})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListNotificationChannels(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(),
		`SELECT id, kind, name, config, is_active, created_at FROM notification_channels ORDER BY created_at DESC`)
	if err != nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var kind, name string
		var cfg []byte
		var active bool
		var created interface{}
		if rows.Scan(&id, &kind, &name, &cfg, &active, &created) != nil {
			continue
		}
		row := map[string]any{
			"id": id.String(), "kind": kind, "name": name,
			"isActive": active, "createdAt": created,
		}
		if len(cfg) > 0 {
			var mj map[string]any
			if json.Unmarshal(cfg, &mj) == nil {
				row["config"] = mj
			}
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

type createNotifChannelReq struct {
	Kind          string          `json:"kind"`
	Name          string          `json:"name"`
	Config        json.RawMessage `json:"config"`
	SigningSecret string          `json:"signingSecret,omitempty"`
}

func (s *Server) handleCreateNotificationChannel(w http.ResponseWriter, r *http.Request) {
	var req createNotifChannelReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Kind == "" || req.Name == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "kind and name required")
		return
	}
	if len(req.Config) == 0 {
		req.Config = json.RawMessage(`{}`)
	}
	nid := uuid.New()
	var encSecret []byte
	var err error
	if req.SigningSecret != "" {
		encSecret, err = s.sealer.Seal([]byte(req.SigningSecret), []byte("notification:"+nid.String()))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "seal failed")
			return
		}
	}
	_, err = s.pool.Exec(r.Context(), `
		INSERT INTO notification_channels (id, kind, name, config, enc_secret)
		VALUES ($1, $2, $3, $4::jsonb, $5)`,
		nid, req.Kind, req.Name, req.Config, encSecret)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "create failed")
		return
	}
	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "notification_channel.create", &uid, clientIP(r), map[string]any{"id": nid.String()})
	writeJSON(w, http.StatusCreated, map[string]any{"id": nid.String()})
}

func (s *Server) handleDeleteNotificationChannel(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	if _, err := s.pool.Exec(r.Context(), `DELETE FROM notification_channels WHERE id = $1`, id); err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "delete failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
