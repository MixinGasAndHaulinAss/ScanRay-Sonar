package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NCLGISA/ScanRay-Sonar/internal/notify"
)

type smtpTestReq struct {
	To string `json:"to"`
}

func (s *Server) handleSMTPTest(w http.ResponseWriter, r *http.Request) {
	var req smtpTestReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.To == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "to required")
		return
	}
	var host, user, from string
	var port int
	var encPass []byte
	var useTLS bool
	err := s.pool.QueryRow(r.Context(), `
		SELECT host, port, "user", enc_password, from_addr, use_tls FROM smtp_settings WHERE id = 1`).
		Scan(&host, &port, &user, &encPass, &from, &useTLS)
	pass := ""
	if err == nil && len(encPass) > 0 {
		b, oerr := s.sealer.Open(encPass, []byte("smtp:password"))
		if oerr == nil {
			pass = string(b)
		}
	}
	if host == "" {
		host = s.cfg.SMTP.Host
		port = s.cfg.SMTP.Port
		user = s.cfg.SMTP.User
		pass = firstNonEmpty(pass, s.cfg.SMTP.Password)
		from = firstNonEmpty(from, s.cfg.SMTP.From)
		useTLS = useTLS || s.cfg.SMTP.TLS
	}
	if host == "" || port <= 0 || from == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "SMTP not configured")
		return
	}
	cfg := notify.SMTPConfig{
		Host: host, Port: port, User: user, Pass: pass, From: from, UseTLS: useTLS,
	}
	sendErr := notify.SendMailMsg(r.Context(), cfg, []string{req.To},
		"Sonar SMTP test", "Sonar SMTP test message.\r\n")
	if sendErr != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", sendErr.Error())
		return
	}
	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "settings.smtp.test", &uid, clientIP(r), map[string]any{"to": req.To})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

type patchWebhookReq struct {
	Name          *string `json:"name,omitempty"`
	URL           *string `json:"url,omitempty"`
	IsActive      *bool   `json:"isActive,omitempty"`
	SigningSecret *string `json:"signingSecret,omitempty"`
}

func (s *Server) handlePatchWebhook(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var req patchWebhookReq
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
	if req.URL != nil {
		add("url", *req.URL)
	}
	if req.IsActive != nil {
		add("is_active", *req.IsActive)
	}
	if req.SigningSecret != nil && *req.SigningSecret != "" {
		enc, err := s.sealer.Seal([]byte(*req.SigningSecret), []byte("webhook:secret"))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "seal failed")
			return
		}
		add("enc_signing_secret", enc)
	}
	if len(sets) == 0 {
		writeErr(w, http.StatusBadRequest, "bad_request", "no fields")
		return
	}
	args = append(args, id)
	q := "UPDATE webhook_endpoints SET " + strings.Join(sets, ", ") + " WHERE id = $" + itoa(len(args))
	tag, err := s.pool.Exec(r.Context(), q, args...)
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusBadRequest, "bad_request", "update failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleWebhookTest(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var url string
	var encSecret []byte
	err = s.pool.QueryRow(r.Context(),
		`SELECT url, enc_signing_secret FROM webhook_endpoints WHERE id = $1 AND is_active`, id).
		Scan(&url, &encSecret)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not_found", "webhook not found")
		return
	}
	var payload json.RawMessage
	_ = json.NewDecoder(r.Body).Decode(&payload)
	if len(payload) == 0 {
		payload = json.RawMessage(`{"event":"sonar.test","ts":""}`)
	}
	var plainSecret []byte
	if len(encSecret) > 0 {
		plainSecret, err = s.sealer.Open(encSecret, []byte("webhook:secret"))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "secret decrypt failed")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	cli := &http.Client{Timeout: 14 * time.Second}
	if err := notify.PostSignedJSON(ctx, cli, url, plainSecret, payload); err != nil {
		writeErr(w, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}
	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "webhook.test", &uid, clientIP(r), map[string]any{"id": id.String()})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
