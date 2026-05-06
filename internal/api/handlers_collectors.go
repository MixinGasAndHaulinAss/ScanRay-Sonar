// Package api — remote site collectors (enrollment, CRUD, WS ingest).
package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/NCLGISA/ScanRay-Sonar/internal/auth"
)

// ---- Collector enrollment tokens ------------------------------------------

type createCollectorEnrollmentTokenReq struct {
	SiteID   string `json:"siteId"`
	Label    string `json:"label"`
	TTLHours int    `json:"ttlHours"`
	MaxUses  int    `json:"maxUses"`
}

type createCollectorEnrollmentTokenResp struct {
	ID         string    `json:"id"`
	SiteID     string    `json:"siteId"`
	Label      string    `json:"label"`
	Token      string    `json:"token"`
	ExpiresAt  time.Time `json:"expiresAt"`
	MaxUses    int       `json:"maxUses"`
	InstallCmd string    `json:"installCmd"`
}

func (s *Server) handleCreateCollectorEnrollmentToken(w http.ResponseWriter, r *http.Request) {
	var req createCollectorEnrollmentTokenReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	siteID, err := uuid.Parse(req.SiteID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "siteId must be a UUID")
		return
	}
	if req.Label == "" {
		req.Label = "collector"
	}
	if req.TTLHours <= 0 {
		req.TTLHours = 72
	}
	if req.TTLHours > 24*30 {
		writeErr(w, http.StatusBadRequest, "bad_request", "ttlHours cannot exceed 720")
		return
	}
	if req.MaxUses <= 0 {
		req.MaxUses = 1
	}
	if req.MaxUses > 50 {
		writeErr(w, http.StatusBadRequest, "bad_request", "maxUses cannot exceed 50")
		return
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "rng failed")
		return
	}
	plaintext := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(plaintext))

	uid := userIDFromCtx(r.Context())
	expires := time.Now().UTC().Add(time.Duration(req.TTLHours) * time.Hour)
	const q = `
		INSERT INTO collector_enrollment_tokens
		  (site_id, label, token_hash, max_uses, expires_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`
	var id uuid.UUID
	if err := s.pool.QueryRow(r.Context(), q,
		siteID, req.Label, hash[:], req.MaxUses, expires, uid,
	).Scan(&id); err != nil {
		s.log.Warn("create collector enrollment token failed", "err", err)
		writeErr(w, http.StatusBadRequest, "bad_request", "create failed")
		return
	}
	s.store.Audit(r.Context(), "user", "collector.enroll_token.create", &uid, clientIP(r),
		map[string]any{"site_id": siteID.String(), "token_id": id.String()})

	cmd := collectorDockerInstallHint(r, plaintext)
	writeJSON(w, http.StatusCreated, createCollectorEnrollmentTokenResp{
		ID:         id.String(),
		SiteID:     siteID.String(),
		Label:      req.Label,
		Token:      plaintext,
		ExpiresAt:  expires,
		MaxUses:    req.MaxUses,
		InstallCmd: cmd,
	})
}

func (s *Server) collectorDockerInstallHint(r *http.Request, token string) string {
	base := s.cfg.PublicURL
	if base == "" {
		scheme := "https"
		if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
			scheme = "http"
		}
		base = scheme + "://" + r.Host
	}
	base = strings.TrimRight(base, "/")
	return fmt.Sprintf(
		`docker run -d --name sonar-collector --restart unless-stopped `+
			`-e SONAR_COLLECTOR_BASE=%q -e SONAR_COLLECTOR_TOKEN=%q `+
			`ghcr.io/nclgisa/scanray-sonar/collector:latest`,
		base, token,
	)
}

type collectorEnrollmentTokenView struct {
	ID        string     `json:"id"`
	SiteID    string     `json:"siteId"`
	Label     string     `json:"label"`
	MaxUses   int        `json:"maxUses"`
	UsedCount int        `json:"usedCount"`
	ExpiresAt time.Time  `json:"expiresAt"`
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	IsValid   bool       `json:"isValid"`
}

func (s *Server) handleListCollectorEnrollmentTokens(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `
		SELECT id, site_id, label, max_uses, used_count, expires_at, revoked_at, created_at
		FROM collector_enrollment_tokens
		ORDER BY created_at DESC
	`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "list failed")
		return
	}
	defer rows.Close()

	now := time.Now().UTC()
	out := []collectorEnrollmentTokenView{}
	for rows.Next() {
		var v collectorEnrollmentTokenView
		var rev *time.Time
		if err := rows.Scan(&v.ID, &v.SiteID, &v.Label, &v.MaxUses, &v.UsedCount,
			&v.ExpiresAt, &rev, &v.CreatedAt); err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "scan failed")
			return
		}
		v.RevokedAt = rev
		v.IsValid = rev == nil && v.ExpiresAt.After(now) && v.UsedCount < v.MaxUses
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRevokeCollectorEnrollmentToken(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be a UUID")
		return
	}
	tag, err := s.pool.Exec(r.Context(),
		`UPDATE collector_enrollment_tokens SET revoked_at = NOW() WHERE id = $1 AND revoked_at IS NULL`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "revoke failed")
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "not_found", "token not found or already revoked")
		return
	}
	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "collector.enroll_token.revoke", &uid, clientIP(r),
		map[string]any{"token_id": id.String()})
	w.WriteHeader(http.StatusNoContent)
}

// ---- Collector probe enrollment -------------------------------------------

type collectorEnrollReq struct {
	Token             string `json:"token"`
	Name              string `json:"name"`
	Hostname          string `json:"hostname"`
	Fingerprint       string `json:"fingerprint"`
	CollectorVersion  string `json:"collectorVersion"`
}

type collectorEnrollResp struct {
	CollectorID string `json:"collectorId"`
	SiteID      string `json:"siteId"`
	JWT         string `json:"jwt"`
	IngestWS    string `json:"ingestWs"`
}

func (s *Server) handleCollectorEnroll(w http.ResponseWriter, r *http.Request) {
	var req collectorEnrollReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if req.Token == "" || req.Name == "" || req.Fingerprint == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "token, name, fingerprint are required")
		return
	}
	if req.Hostname == "" {
		req.Hostname = req.Name
	}

	hash := sha256.Sum256([]byte(req.Token))
	tx, err := s.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "begin tx")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	var (
		tokenID uuid.UUID
		siteID  uuid.UUID
		maxUses int
		used    int
		expires time.Time
		revoked *time.Time
	)
	err = tx.QueryRow(r.Context(), `
		SELECT id, site_id, max_uses, used_count, expires_at, revoked_at
		FROM collector_enrollment_tokens
		WHERE token_hash = $1
		FOR UPDATE
	`, hash[:]).Scan(&tokenID, &siteID, &maxUses, &used, &expires, &revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusUnauthorized, "unauthorized", "invalid enrollment token")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "lookup failed")
		return
	}
	if revoked != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized", "token revoked")
		return
	}
	if time.Now().UTC().After(expires) {
		writeErr(w, http.StatusUnauthorized, "unauthorized", "token expired")
		return
	}
	if used >= maxUses {
		writeErr(w, http.StatusUnauthorized, "unauthorized", "token exhausted")
		return
	}

	if _, err := tx.Exec(r.Context(),
		`UPDATE collector_enrollment_tokens SET used_count = used_count + 1 WHERE id = $1`, tokenID); err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "increment failed")
		return
	}

	const upsert = `
		INSERT INTO collectors (
		  site_id, name, hostname, fingerprint, collector_version,
		  last_seen_at, is_active
		) VALUES ($1, $2, $3, $4, $5, NOW(), TRUE)
		ON CONFLICT (site_id, name) DO UPDATE
		  SET hostname           = EXCLUDED.hostname,
		      fingerprint        = EXCLUDED.fingerprint,
		      collector_version  = EXCLUDED.collector_version,
		      last_seen_at       = NOW(),
		      is_active          = TRUE
		RETURNING id
	`
	var collectorID uuid.UUID
	if err := tx.QueryRow(r.Context(), upsert,
		siteID, req.Name, req.Hostname, req.Fingerprint, req.CollectorVersion,
	).Scan(&collectorID); err != nil {
		s.log.Warn("collector upsert failed", "err", err, "name", req.Name)
		writeErr(w, http.StatusInternalServerError, "server_error", "collector upsert failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "commit failed")
		return
	}

	jwt, _, err := s.iss.Issue(collectorID, "", auth.KindCollector)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "jwt issue failed")
		return
	}

	s.store.Audit(r.Context(), "system", "collector.enroll.ok", &collectorID, clientIP(r),
		map[string]any{"name": req.Name, "site_id": siteID.String(), "token_id": tokenID.String()})

	writeJSON(w, http.StatusCreated, collectorEnrollResp{
		CollectorID: collectorID.String(),
		SiteID:      siteID.String(),
		JWT:         jwt,
		IngestWS:    s.collectorWSURL(r),
	})
}

func (s *Server) collectorWSURL(r *http.Request) string {
	base := s.cfg.IngestURL
	if base == "" {
		base = s.cfg.PublicURL
	}
	if base == "" {
		scheme := "ws"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "wss"
		}
		return scheme + "://" + r.Host + "/collector/ws"
	}
	switch {
	case strings.HasPrefix(base, "https://"):
		return "wss://" + strings.TrimPrefix(base, "https://") + "/collector/ws"
	case strings.HasPrefix(base, "http://"):
		return "ws://" + strings.TrimPrefix(base, "http://") + "/collector/ws"
	}
	return base
}

func (s *Server) handleCollectorWS(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("token")
	if tok == "" {
		tok = bearerToken(r)
	}
	if tok == "" {
		writeErr(w, http.StatusUnauthorized, "unauthorized", "missing collector token")
		return
	}
	claims, err := s.iss.Parse(tok, auth.KindCollector)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized", "invalid collector token")
		return
	}
	collectorID := claims.UserID

	if _, err := s.pool.Exec(r.Context(),
		`UPDATE collectors SET last_seen_at = NOW() WHERE id = $1`, collectorID); err != nil {
		s.log.Warn("collector last_seen touch failed", "err", err, "collector_id", collectorID)
	}

	hbCtx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go s.runCollectorHeartbeatTouch(hbCtx, collectorID)

	s.runCollectorIngestLoop(r.Context(), w, r, collectorID)
}

func (s *Server) runCollectorHeartbeatTouch(ctx context.Context, collectorID uuid.UUID) {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tctx, cc := context.WithTimeout(context.Background(), 5*time.Second)
			_, _ = s.pool.Exec(tctx, `UPDATE collectors SET last_seen_at = NOW() WHERE id = $1`, collectorID)
			cc()
		}
	}
}

// ---- Collector REST ---------------------------------------------------------

type collectorView struct {
	ID                 uuid.UUID  `json:"id"`
	SiteID             uuid.UUID  `json:"siteId"`
	Name               string     `json:"name"`
	Hostname           string     `json:"hostname"`
	CollectorVersion   string     `json:"collectorVersion"`
	LastSeenAt         *time.Time `json:"lastSeenAt,omitempty"`
	IsActive           bool       `json:"isActive"`
	CreatedAt          time.Time  `json:"createdAt"`
}

func (s *Server) handleListCollectors(w http.ResponseWriter, r *http.Request) {
	siteID := r.URL.Query().Get("siteId")
	var rows pgx.Rows
	var err error
	if siteID != "" {
		sid, perr := uuid.Parse(siteID)
		if perr != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "siteId must be a UUID")
			return
		}
		rows, err = s.pool.Query(r.Context(), `
			SELECT id, site_id, name, hostname, collector_version, last_seen_at, is_active, created_at
			FROM collectors WHERE site_id = $1 ORDER BY name`, sid)
	} else {
		rows, err = s.pool.Query(r.Context(), `
			SELECT id, site_id, name, hostname, collector_version, last_seen_at, is_active, created_at
			FROM collectors ORDER BY site_id, name`)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "list failed")
		return
	}
	defer rows.Close()

	out := []collectorView{}
	for rows.Next() {
		var v collectorView
		if err := rows.Scan(&v.ID, &v.SiteID, &v.Name, &v.Hostname, &v.CollectorVersion,
			&v.LastSeenAt, &v.IsActive, &v.CreatedAt); err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "scan failed")
			return
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetCollector(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be a UUID")
		return
	}
	var v collectorView
	err = s.pool.QueryRow(r.Context(), `
		SELECT id, site_id, name, hostname, collector_version, last_seen_at, is_active, created_at
		FROM collectors WHERE id = $1`, id).
		Scan(&v.ID, &v.SiteID, &v.Name, &v.Hostname, &v.CollectorVersion,
			&v.LastSeenAt, &v.IsActive, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not_found", "collector not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "load failed")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

type patchCollectorReq struct {
	IsActive *bool   `json:"isActive,omitempty"`
	Name     *string `json:"name,omitempty"`
}

func (s *Server) handlePatchCollector(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be a UUID")
		return
	}
	var req patchCollectorReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	sets := []string{}
	args := []any{}
	add := func(col string, val any) {
		args = append(args, val)
		sets = append(sets, col+" = $"+itoa(len(args)))
	}
	if req.IsActive != nil {
		add("is_active", *req.IsActive)
	}
	if req.Name != nil {
		if *req.Name == "" {
			writeErr(w, http.StatusBadRequest, "bad_request", "name cannot be empty")
			return
		}
		add("name", *req.Name)
	}
	if len(sets) == 0 {
		writeErr(w, http.StatusBadRequest, "bad_request", "no fields to update")
		return
	}
	args = append(args, id)
	q := "UPDATE collectors SET " + join(sets, ", ") + " WHERE id = $" + itoa(len(args)) +
		` RETURNING id, site_id, name, hostname, collector_version, last_seen_at, is_active, created_at`
	var v collectorView
	if err := s.pool.QueryRow(r.Context(), q, args...).
		Scan(&v.ID, &v.SiteID, &v.Name, &v.Hostname, &v.CollectorVersion,
			&v.LastSeenAt, &v.IsActive, &v.CreatedAt); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "update failed")
		return
	}
	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "collector.update", &uid, clientIP(r),
		map[string]any{"collector_id": id.String()})
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleDeleteCollector(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be a UUID")
		return
	}
	tag, err := s.pool.Exec(r.Context(), `DELETE FROM collectors WHERE id = $1`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "delete failed")
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "not_found", "collector not found")
		return
	}
	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "collector.delete", &uid, clientIP(r),
		map[string]any{"collector_id": id.String()})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCollectorHealth(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be a UUID")
		return
	}
	var last *time.Time
	var active bool
	err = s.pool.QueryRow(r.Context(),
		`SELECT last_seen_at, is_active FROM collectors WHERE id = $1`, id).Scan(&last, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not_found", "collector not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "load failed")
		return
	}
	online := false
	if last != nil && time.Since(*last) < 3*time.Minute {
		online = true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"collectorId": id.String(),
		"online":      online && active,
		"lastSeenAt":  last,
		"isActive":    active,
	})
}

func (s *Server) handleCollectorMe(w http.ResponseWriter, r *http.Request) {
	id := collectorIDFromCtx(r.Context())
	var v collectorView
	err := s.pool.QueryRow(r.Context(), `
		SELECT id, site_id, name, hostname, collector_version, last_seen_at, is_active, created_at
		FROM collectors WHERE id = $1 AND is_active`, id).
		Scan(&v.ID, &v.SiteID, &v.Name, &v.Hostname, &v.CollectorVersion,
			&v.LastSeenAt, &v.IsActive, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not_found", "collector not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "load failed")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

type collectorJob struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"` // "snmp_poll" | "discovery_scan" | ...
	Description string          `json:"description,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

func (s *Server) handleCollectorJobs(w http.ResponseWriter, r *http.Request) {
	id := collectorIDFromCtx(r.Context())
	// Appliances delegated to this collector are polled locally by sonar-collector (Phase 2+).
	rows, err := s.pool.Query(r.Context(), `
		SELECT id::text, name
		FROM appliances
		WHERE collector_id = $1 AND is_active AND enc_snmp_creds IS NOT NULL
		ORDER BY name`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "jobs query failed")
		return
	}
	defer rows.Close()
	jobs := []collectorJob{}
	for rows.Next() {
		var aid, name string
		if err := rows.Scan(&aid, &name); err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "scan failed")
			return
		}
		jobs = append(jobs, collectorJob{
			ID:          aid,
			Kind:        "snmp_poll",
			Description: name,
		})
	}
	var dSubnets []byte
	var dIcmp int
	if qerr := s.pool.QueryRow(r.Context(), `
		SELECT ds.subnets, ds.icmp_timeout_ms
		  FROM site_discovery_settings ds
		  JOIN collectors c ON c.site_id = ds.site_id
		 WHERE c.id = $1`, id).Scan(&dSubnets, &dIcmp); qerr == nil {
		if len(dSubnets) > 0 && string(dSubnets) != "[]" && string(dSubnets) != "null" {
			payload := fmt.Sprintf(`{"subnets":%s,"icmpTimeoutMs":%d}`, string(dSubnets), dIcmp)
			jobs = append(jobs, collectorJob{
				ID:          "discovery",
				Kind:        "discovery_scan",
				Description: "ICMP/TCP discovery sweep",
				Payload:     json.RawMessage(payload),
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}
