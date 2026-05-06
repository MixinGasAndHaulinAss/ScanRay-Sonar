package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type apiKeyView struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r.Context())
	rows, err := s.pool.Query(r.Context(), `
		SELECT id, name, scopes, expires_at, created_at FROM api_keys
		 WHERE user_id = $1 AND revoked_at IS NULL ORDER BY created_at DESC`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "query failed")
		return
	}
	defer rows.Close()
	out := []apiKeyView{}
	for rows.Next() {
		var v apiKeyView
		var id uuid.UUID
		if err := rows.Scan(&id, &v.Name, &v.Scopes, &v.ExpiresAt, &v.CreatedAt); err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "scan failed")
			return
		}
		v.ID = id.String()
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

type createAPIKeyReq struct {
	Name      string    `json:"name"`
	Scopes    []string  `json:"scopes"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

type createAPIKeyResp struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Token string `json:"token"`
}

func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req createAPIKeyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "name required")
		return
	}
	if req.Scopes == nil {
		req.Scopes = []string{}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "rng failed")
		return
	}
	token := apiKeyBearerPrefix + base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	uid := userIDFromCtx(r.Context())
	var id uuid.UUID
	err := s.pool.QueryRow(r.Context(), `
		INSERT INTO api_keys (user_id, name, token_hash, scopes, expires_at)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		uid, req.Name, sum[:], req.Scopes, req.ExpiresAt).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "create failed")
		return
	}
	s.store.Audit(r.Context(), "user", "api_key.create", &uid, clientIP(r), map[string]any{"key_id": id.String()})
	writeJSON(w, http.StatusCreated, createAPIKeyResp{ID: id.String(), Name: req.Name, Token: token})
}

func (s *Server) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	uid := userIDFromCtx(r.Context())
	tag, err := s.pool.Exec(r.Context(), `
		UPDATE api_keys SET revoked_at = NOW() WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		id, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "revoke failed")
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "not_found", "key not found")
		return
	}
	s.store.Audit(r.Context(), "user", "api_key.revoke", &uid, clientIP(r), map[string]any{"key_id": id.String()})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSuperListAPIKeys(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `
		SELECT id, user_id, name, scopes, expires_at, created_at FROM api_keys
		 WHERE revoked_at IS NULL ORDER BY created_at DESC LIMIT 500`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "query failed")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, userID uuid.UUID
		var name string
		var scopes []string
		var exp *time.Time
		var created time.Time
		if err := rows.Scan(&id, &userID, &name, &scopes, &exp, &created); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id.String(), "userId": userID.String(), "name": name,
			"scopes": scopes, "expiresAt": exp, "createdAt": created,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

type putAPIKeySitesReq struct {
	SiteIDs []string `json:"siteIds"`
}

func (s *Server) handlePutAPIKeySites(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var req putAPIKeySitesReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	uid := userIDFromCtx(r.Context())
	var owner uuid.UUID
	err = s.pool.QueryRow(r.Context(),
		`SELECT user_id FROM api_keys WHERE id = $1 AND revoked_at IS NULL`, id).Scan(&owner)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not_found", "key not found")
		return
	}
	if owner != uid {
		writeErr(w, http.StatusForbidden, "forbidden", "not your API key")
		return
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "tx failed")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), `DELETE FROM api_key_sites WHERE api_key_id = $1`, id); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "clear sites failed")
		return
	}
	for _, raw := range req.SiteIDs {
		sid, err := uuid.Parse(raw)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid site id")
			return
		}
		if _, err := tx.Exec(r.Context(),
			`INSERT INTO api_key_sites (api_key_id, site_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, sid); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "bind site failed")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "commit failed")
		return
	}
	s.store.Audit(r.Context(), "user", "api_key.sites", &uid, clientIP(r), map[string]any{"key_id": id.String()})
	w.WriteHeader(http.StatusNoContent)
}
