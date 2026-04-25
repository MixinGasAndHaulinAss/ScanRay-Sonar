package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"github.com/NCLGISA/ScanRay-Sonar/internal/auth"
)

// ---- Sites ---------------------------------------------------------------

type siteView struct {
	ID          string    `json:"id"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Timezone    string    `json:"timezone"`
	Description *string   `json:"description,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (s *Server) handleListSites(w http.ResponseWriter, r *http.Request) {
	sites, err := s.store.ListSites(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "list sites failed")
		return
	}
	out := make([]siteView, 0, len(sites))
	for _, st := range sites {
		out = append(out, siteView{
			ID:          st.ID.String(),
			Slug:        st.Slug,
			Name:        st.Name,
			Timezone:    st.Timezone,
			Description: st.Description,
			CreatedAt:   st.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

type createSiteReq struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Timezone    string `json:"timezone"`
	Description string `json:"description"`
}

var slugRE = regexp.MustCompile(`^[a-z0-9-]+$`)

func (s *Server) handleCreateSite(w http.ResponseWriter, r *http.Request) {
	var req createSiteReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if !slugRE.MatchString(req.Slug) {
		writeErr(w, http.StatusBadRequest, "bad_request", "slug must match ^[a-z0-9-]+$")
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "name is required")
		return
	}
	if req.Timezone == "" {
		req.Timezone = "UTC"
	}

	const q = `
		INSERT INTO sites (slug, name, timezone, description)
		VALUES ($1, $2, $3, NULLIF($4,''))
		RETURNING id, slug, name, timezone, description, created_at
	`
	var v siteView
	if err := s.pool.QueryRow(r.Context(), q, req.Slug, req.Name, req.Timezone, req.Description).
		Scan(&v.ID, &v.Slug, &v.Name, &v.Timezone, &v.Description, &v.CreatedAt); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "site exists or invalid")
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

// ---- Users ---------------------------------------------------------------

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `
		SELECT id, email, display_name, password_hash, role,
		       totp_enrolled, is_active, last_login_at, created_at
		FROM users ORDER BY created_at
	`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "list users failed")
		return
	}
	defer rows.Close()

	type minUser struct {
		ID           string     `json:"id"`
		Email        string     `json:"email"`
		DisplayName  string     `json:"displayName"`
		Role         string     `json:"role"`
		TOTPEnrolled bool       `json:"totpEnrolled"`
		IsActive     bool       `json:"isActive"`
		LastLoginAt  *time.Time `json:"lastLoginAt,omitempty"`
		CreatedAt    time.Time  `json:"createdAt"`
	}

	out := []minUser{}
	for rows.Next() {
		var (
			id, email, dn, pw, role string
			totp, active            bool
			last                    *time.Time
			created                 time.Time
		)
		if err := rows.Scan(&id, &email, &dn, &pw, &role, &totp, &active, &last, &created); err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "scan failed")
			return
		}
		out = append(out, minUser{id, email, dn, role, totp, active, last, created})
	}
	writeJSON(w, http.StatusOK, out)
}

type createUserReq struct {
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Password    string `json:"password"`
	Role        string `json:"role"`
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if req.Email == "" || req.DisplayName == "" || req.Password == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "email, displayName and password are required")
		return
	}
	if len(req.Password) < 12 {
		writeErr(w, http.StatusBadRequest, "bad_request", "password must be at least 12 characters")
		return
	}
	if !auth.Role(req.Role).Valid() {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid role")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "hash failed")
		return
	}

	const q = `
		INSERT INTO users (email, display_name, password_hash, role)
		VALUES ($1, $2, $3, $4)
		RETURNING id, email, display_name, role, totp_enrolled, is_active, created_at
	`
	type out struct {
		ID           string    `json:"id"`
		Email        string    `json:"email"`
		DisplayName  string    `json:"displayName"`
		Role         string    `json:"role"`
		TOTPEnrolled bool      `json:"totpEnrolled"`
		IsActive     bool      `json:"isActive"`
		CreatedAt    time.Time `json:"createdAt"`
	}
	var o out
	if err := s.pool.QueryRow(r.Context(), q, req.Email, req.DisplayName, hash, req.Role).
		Scan(&o.ID, &o.Email, &o.DisplayName, &o.Role, &o.TOTPEnrolled, &o.IsActive, &o.CreatedAt); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "user exists or invalid")
		return
	}
	writeJSON(w, http.StatusCreated, o)
}

// ---- Agents / Appliances (read-only stubs for Phase 1) ------------------

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	siteID := r.URL.Query().Get("siteId")
	q := `SELECT id, site_id, hostname, fingerprint, os, os_version, agent_version,
	             enrolled_at, last_seen_at, is_active, tags, created_at
	      FROM agents`
	args := []any{}
	if siteID != "" {
		q += ` WHERE site_id = $1`
		args = append(args, siteID)
	}
	q += ` ORDER BY hostname`

	rows, err := s.pool.Query(r.Context(), q, args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "list agents failed")
		return
	}
	defer rows.Close()

	out := []map[string]any{}
	for rows.Next() {
		var (
			id, sid, host, os, osver, av string
			fp                           *string
			enrolled, last               *time.Time
			active                       bool
			tags                         []string
			created                      time.Time
		)
		if err := rows.Scan(&id, &sid, &host, &fp, &os, &osver, &av, &enrolled, &last, &active, &tags, &created); err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "scan failed")
			return
		}
		out = append(out, map[string]any{
			"id":           id,
			"siteId":       sid,
			"hostname":     host,
			"fingerprint":  fp,
			"os":           os,
			"osVersion":    osver,
			"agentVersion": av,
			"enrolledAt":   enrolled,
			"lastSeenAt":   last,
			"isActive":     active,
			"tags":         tags,
			"createdAt":    created,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListAppliances(w http.ResponseWriter, r *http.Request) {
	siteID := r.URL.Query().Get("siteId")
	q := `SELECT id, site_id, name, vendor, model, serial, host(mgmt_ip), snmp_version,
	             poll_interval_s, is_active, tags, last_polled_at, last_error, created_at
	      FROM appliances`
	args := []any{}
	if siteID != "" {
		q += ` WHERE site_id = $1`
		args = append(args, siteID)
	}
	q += ` ORDER BY name`

	rows, err := s.pool.Query(r.Context(), q, args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "list appliances failed")
		return
	}
	defer rows.Close()

	out := []map[string]any{}
	for rows.Next() {
		var (
			id, sid, name, vendor, ip, snmpv string
			model, serial, lastErr           *string
			pollSec                          int
			active                           bool
			tags                             []string
			lastPolled                       *time.Time
			created                          time.Time
		)
		if err := rows.Scan(&id, &sid, &name, &vendor, &model, &serial, &ip, &snmpv, &pollSec, &active, &tags, &lastPolled, &lastErr, &created); err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "scan failed")
			return
		}
		out = append(out, map[string]any{
			"id":                  id,
			"siteId":              sid,
			"name":                name,
			"vendor":              vendor,
			"model":               model,
			"serial":              serial,
			"mgmtIp":              ip,
			"snmpVersion":         snmpv,
			"pollIntervalSeconds": pollSec,
			"isActive":            active,
			"tags":                tags,
			"lastPolledAt":        lastPolled,
			"lastError":           lastErr,
			"createdAt":           created,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
