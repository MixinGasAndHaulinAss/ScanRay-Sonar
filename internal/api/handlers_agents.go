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
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/NCLGISA/ScanRay-Sonar/internal/auth"
)

// ---- Enrollment tokens ----------------------------------------------------

type createEnrollmentTokenReq struct {
	SiteID   string `json:"siteId"`
	Label    string `json:"label"`
	TTLHours int    `json:"ttlHours"`
	MaxUses  int    `json:"maxUses"`
}

// installCommands is the per-OS install one-liner bundle returned with
// every newly-issued enrollment token. The legacy InstallCmd field on
// the parent struct is kept (= the linux command) for older UIs and
// scripts; new clients should prefer InstallCmds.
type installCommands struct {
	Linux   string `json:"linux"`
	Windows string `json:"windows"`
}

type createEnrollmentTokenResp struct {
	ID          string          `json:"id"`
	SiteID      string          `json:"siteId"`
	Label       string          `json:"label"`
	Token       string          `json:"token"` // PLAINTEXT — shown exactly once
	ExpiresAt   time.Time       `json:"expiresAt"`
	MaxUses     int             `json:"maxUses"`
	InstallCmd  string          `json:"installCmd"`  // linux (kept for back-compat)
	InstallCmds installCommands `json:"installCmds"` // per-OS one-liners
}

func (s *Server) handleCreateEnrollmentToken(w http.ResponseWriter, r *http.Request) {
	var req createEnrollmentTokenReq
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
		req.Label = "untitled"
	}
	if req.TTLHours <= 0 {
		req.TTLHours = 24
	}
	if req.TTLHours > 24*30 {
		writeErr(w, http.StatusBadRequest, "bad_request", "ttlHours cannot exceed 720 (30 days)")
		return
	}
	if req.MaxUses <= 0 {
		req.MaxUses = 1
	}
	if req.MaxUses > 100 {
		writeErr(w, http.StatusBadRequest, "bad_request", "maxUses cannot exceed 100")
		return
	}

	// 32 random bytes -> base64url (no padding) is the bearer token the
	// operator pastes. It is hashed with SHA-256 before persisting; the
	// plaintext is returned exactly once in this response.
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
		INSERT INTO agent_enrollment_tokens
		  (site_id, label, token_hash, max_uses, expires_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`
	var id uuid.UUID
	if err := s.pool.QueryRow(r.Context(), q,
		siteID, req.Label, hash[:], req.MaxUses, expires, uid,
	).Scan(&id); err != nil {
		s.log.Warn("create enrollment token failed", "err", err)
		writeErr(w, http.StatusBadRequest, "bad_request", "create failed")
		return
	}
	s.store.Audit(r.Context(), "user", "agent.enroll_token.create", &uid, clientIP(r),
		map[string]any{"site_id": siteID.String(), "label": req.Label, "token_id": id.String()})

	cmds := s.installCommands(r, plaintext)
	writeJSON(w, http.StatusCreated, createEnrollmentTokenResp{
		ID:          id.String(),
		SiteID:      siteID.String(),
		Label:       req.Label,
		Token:       plaintext,
		ExpiresAt:   expires,
		MaxUses:     req.MaxUses,
		InstallCmd:  cmds.Linux,
		InstallCmds: cmds,
	})
}

// installCommands returns the per-OS install one-liners for a freshly
// minted enrollment token. The base URL is derived the same way as the
// install scripts themselves (configured PublicURL, falling back to
// the inbound request host) so the operator can copy the one-liner
// straight into a target shell without surgery.
func (s *Server) installCommands(r *http.Request, token string) installCommands {
	base := s.cfg.PublicURL
	if base == "" {
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	base = strings.TrimRight(base, "/")

	linux := fmt.Sprintf(
		"curl -fsSL %s/api/v1/probe/install.sh | sudo INSTALL_TOKEN=%s SONAR_BASE=%s bash",
		base, token, base,
	)
	// The Windows one-liner has to survive being pasted into BOTH
	// cmd.exe and an already-running PowerShell prompt. The trap we
	// hit before was setting $env:INSTALL_TOKEN inside the -Command
	// string: PowerShell parents expand that variable in the OUTER
	// shell before the inner powershell.exe ever sees it, dropping
	// the value and leaving a bare `=...token` on the command line.
	//
	// The cleanest fix is to remove the env-var bootstrap entirely
	// and bake the token into the install.ps1 URL itself. The query
	// string contains only safe characters (base64url + literals),
	// so neither shell mangles it on the way through. The URL is
	// single-quoted, so PowerShell does no $-expansion either.
	//
	// Tokens are base64url so they are URL-safe by construction; we
	// still call url.QueryEscape to be defensive against any future
	// change that introduces reserved characters.
	q := url.Values{}
	q.Set("token", token)
	q.Set("base", base)
	windows := fmt.Sprintf(
		"powershell -NoProfile -ExecutionPolicy Bypass -Command \"iwr -UseBasicParsing '%s/api/v1/probe/install.ps1?%s' | iex\"",
		base, q.Encode(),
	)
	return installCommands{Linux: linux, Windows: windows}
}

type enrollmentTokenView struct {
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

func (s *Server) handleListEnrollmentTokens(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `
		SELECT id, site_id, label, max_uses, used_count, expires_at, revoked_at, created_at
		FROM agent_enrollment_tokens
		ORDER BY created_at DESC
	`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "list failed")
		return
	}
	defer rows.Close()

	now := time.Now().UTC()
	out := []enrollmentTokenView{}
	for rows.Next() {
		var v enrollmentTokenView
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

func (s *Server) handleRevokeEnrollmentToken(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be a UUID")
		return
	}
	tag, err := s.pool.Exec(r.Context(),
		`UPDATE agent_enrollment_tokens SET revoked_at = NOW() WHERE id = $1 AND revoked_at IS NULL`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "revoke failed")
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "not_found", "token not found or already revoked")
		return
	}
	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "agent.enroll_token.revoke", &uid, clientIP(r),
		map[string]any{"token_id": id.String()})
	w.WriteHeader(http.StatusNoContent)
}

// ---- Probe enrollment -----------------------------------------------------

type enrollReq struct {
	Token        string `json:"token"`
	Hostname     string `json:"hostname"`
	Fingerprint  string `json:"fingerprint"`
	OS           string `json:"os"`
	OSVersion    string `json:"osVersion"`
	AgentVersion string `json:"agentVersion"`
}

type enrollResp struct {
	AgentID  string `json:"agentId"`
	SiteID   string `json:"siteId"`
	JWT      string `json:"jwt"`
	IngestWS string `json:"ingestWs"`
}

// handleAgentEnroll consumes an enrollment token and returns a long-lived
// agent JWT. The caller is unauthenticated at the bearer-token layer; the
// only credential required is the enrollment token itself, validated by
// SHA-256 hash lookup against agent_enrollment_tokens.
func (s *Server) handleAgentEnroll(w http.ResponseWriter, r *http.Request) {
	var req enrollReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if req.Token == "" || req.Hostname == "" || req.Fingerprint == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "token, hostname, fingerprint are required")
		return
	}
	if req.OS == "" {
		req.OS = "linux"
	}

	hash := sha256.Sum256([]byte(req.Token))
	tx, err := s.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "begin tx")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	var (
		tokenID  uuid.UUID
		siteID   uuid.UUID
		maxUses  int
		used     int
		expires  time.Time
		revoked  *time.Time
	)
	err = tx.QueryRow(r.Context(), `
		SELECT id, site_id, max_uses, used_count, expires_at, revoked_at
		FROM agent_enrollment_tokens
		WHERE token_hash = $1
		FOR UPDATE
	`, hash[:]).Scan(&tokenID, &siteID, &maxUses, &used, &expires, &revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		s.store.Audit(r.Context(), "system", "agent.enroll.deny", nil, clientIP(r),
			map[string]any{"reason": "unknown_token", "hostname": req.Hostname})
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
		`UPDATE agent_enrollment_tokens SET used_count = used_count + 1 WHERE id = $1`, tokenID); err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "increment failed")
		return
	}

	// UPSERT keyed by (site_id, fingerprint) — re-enrolling an existing
	// host (e.g. after a reinstall) replaces its old row rather than
	// creating a duplicate. Hostname collisions inside a site update
	// the fingerprint instead, so renaming the host is also fine.
	const upsert = `
		INSERT INTO agents (
		  site_id, hostname, fingerprint, os, os_version, agent_version,
		  enrolled_at, last_seen_at, is_active
		) VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW(), TRUE)
		ON CONFLICT (site_id, hostname) DO UPDATE
		  SET fingerprint   = EXCLUDED.fingerprint,
		      os            = EXCLUDED.os,
		      os_version    = EXCLUDED.os_version,
		      agent_version = EXCLUDED.agent_version,
		      enrolled_at   = NOW(),
		      last_seen_at  = NOW(),
		      is_active     = TRUE
		RETURNING id
	`
	var agentID uuid.UUID
	if err := tx.QueryRow(r.Context(), upsert,
		siteID, req.Hostname, req.Fingerprint, req.OS, req.OSVersion, req.AgentVersion,
	).Scan(&agentID); err != nil {
		s.log.Warn("agent upsert failed", "err", err, "hostname", req.Hostname)
		writeErr(w, http.StatusInternalServerError, "server_error", "agent upsert failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "commit failed")
		return
	}

	jwt, _, err := s.iss.Issue(agentID, "", auth.KindAgent)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "jwt issue failed")
		return
	}

	s.store.Audit(r.Context(), "agent", "agent.enroll.ok", &agentID, clientIP(r),
		map[string]any{"hostname": req.Hostname, "site_id": siteID.String(), "token_id": tokenID.String()})

	writeJSON(w, http.StatusCreated, enrollResp{
		AgentID:  agentID.String(),
		SiteID:   siteID.String(),
		JWT:      jwt,
		IngestWS: s.ingestWSURL(r),
	})
}

func (s *Server) ingestWSURL(r *http.Request) string {
	base := s.cfg.IngestURL
	if base == "" {
		base = s.cfg.PublicURL
	}
	if base == "" {
		// Dev fallback: derive from the inbound request, so a probe
		// installed by curling this very host has a working ingest
		// URL even without Cloudflare in front.
		scheme := "ws"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "wss"
		}
		return scheme + "://" + r.Host + "/agent/ws"
	}
	switch {
	case strings.HasPrefix(base, "https://"):
		return "wss://" + strings.TrimPrefix(base, "https://") + "/agent/ws"
	case strings.HasPrefix(base, "http://"):
		return "ws://" + strings.TrimPrefix(base, "http://") + "/agent/ws"
	}
	return base
}

// handleAgentWS authenticates the JWT in ?token=, marks the agent
// last-seen, then runs an inline read loop that demuxes inbound frames
// by `type` and dispatches each kind to the appropriate ingest path.
//
// We deliberately do NOT use the generic Hub.Serve here — that hub is
// a fan-out/keepalive scaffold; agent ingest needs typed handling per
// frame (metrics today, command-response and event streams later) so
// it owns the read loop directly.
func (s *Server) handleAgentWS(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("token")
	if tok == "" {
		tok = bearerToken(r)
	}
	if tok == "" {
		writeErr(w, http.StatusUnauthorized, "unauthorized", "missing agent token")
		return
	}
	claims, err := s.iss.Parse(tok, auth.KindAgent)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized", "invalid agent token")
		return
	}
	agentID := claims.UserID

	if _, err := s.pool.Exec(r.Context(),
		`UPDATE agents SET last_seen_at = NOW() WHERE id = $1`, agentID); err != nil {
		s.log.Warn("agent last_seen touch failed", "err", err, "agent_id", agentID)
	}

	hbCtx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go s.runAgentHeartbeatTouch(hbCtx, agentID)

	s.runAgentIngestLoop(r.Context(), w, r, agentID)
}

// runAgentHeartbeatTouch periodically updates last_seen_at while the
// websocket is open. The caller wires its lifetime to the WS request
// context so the goroutine exits the moment the agent disconnects.
func (s *Server) runAgentHeartbeatTouch(ctx context.Context, agentID uuid.UUID) {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			// Use a fresh background context with a short timeout so
			// the touch still runs even if r.Context() is on the way
			// out — losing the very last touch on disconnect is OK.
			tctx, cc := context.WithTimeout(context.Background(), 5*time.Second)
			_, _ = s.pool.Exec(tctx, `UPDATE agents SET last_seen_at = NOW() WHERE id = $1`, agentID)
			cc()
		}
	}
}

func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be a UUID")
		return
	}
	tag, err := s.pool.Exec(r.Context(), `DELETE FROM agents WHERE id = $1`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "delete failed")
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "not_found", "agent not found")
		return
	}
	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "agent.delete", &uid, clientIP(r),
		map[string]any{"agent_id": id.String()})
	w.WriteHeader(http.StatusNoContent)
}
