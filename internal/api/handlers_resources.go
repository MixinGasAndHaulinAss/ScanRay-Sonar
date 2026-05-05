package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

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

type updateSiteReq struct {
	Slug        *string `json:"slug,omitempty"`
	Name        *string `json:"name,omitempty"`
	Timezone    *string `json:"timezone,omitempty"`
	Description *string `json:"description,omitempty"`
}

// handleUpdateSite is a true partial PATCH: any field that is omitted
// (or sent as null) is left untouched. Slug is validated against the
// same regex used at create time.
func (s *Server) handleUpdateSite(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be a UUID")
		return
	}
	var req updateSiteReq
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
	if req.Slug != nil {
		if !slugRE.MatchString(*req.Slug) {
			writeErr(w, http.StatusBadRequest, "bad_request", "slug must match ^[a-z0-9-]+$")
			return
		}
		add("slug", *req.Slug)
	}
	if req.Name != nil {
		if *req.Name == "" {
			writeErr(w, http.StatusBadRequest, "bad_request", "name cannot be empty")
			return
		}
		add("name", *req.Name)
	}
	if req.Timezone != nil {
		add("timezone", *req.Timezone)
	}
	if req.Description != nil {
		// Empty-string description normalizes to NULL so the column
		// stays consistent with the create handler's NULLIF behavior.
		if *req.Description == "" {
			sets = append(sets, "description = NULL")
		} else {
			add("description", *req.Description)
		}
	}
	if len(sets) == 0 {
		writeErr(w, http.StatusBadRequest, "bad_request", "no fields to update")
		return
	}

	args = append(args, id)
	q := "UPDATE sites SET " + join(sets, ", ") + " WHERE id = $" + itoa(len(args)) +
		" RETURNING id, slug, name, timezone, description, created_at"
	var v siteView
	if err := s.pool.QueryRow(r.Context(), q, args...).
		Scan(&v.ID, &v.Slug, &v.Name, &v.Timezone, &v.Description, &v.CreatedAt); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "update failed (slug conflict?)")
		return
	}
	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "site.update", &uid, clientIP(r),
		map[string]any{"site_id": v.ID})
	writeJSON(w, http.StatusOK, v)
}

// handleDeleteSite refuses to remove a site that still has appliances
// or agents attached — those cascade-deleting silently would be a
// huge gun pointed at the foot. The operator must reassign or remove
// the children first, at which point the row deletes cleanly.
func (s *Server) handleDeleteSite(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be a UUID")
		return
	}

	var appCount, agCount int
	if err := s.pool.QueryRow(r.Context(),
		`SELECT (SELECT COUNT(*) FROM appliances WHERE site_id = $1),
		        (SELECT COUNT(*) FROM agents WHERE site_id = $1)`, id).
		Scan(&appCount, &agCount); err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "child count failed")
		return
	}
	if appCount > 0 || agCount > 0 {
		writeErr(w, http.StatusConflict, "site_not_empty",
			"site still has appliances or agents; remove or reassign them first")
		return
	}

	tag, err := s.pool.Exec(r.Context(), `DELETE FROM sites WHERE id = $1`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "delete failed")
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "not_found", "site not found")
		return
	}
	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "site.delete", &uid, clientIP(r),
		map[string]any{"site_id": id.String()})
	w.WriteHeader(http.StatusNoContent)
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

type updateUserReq struct {
	DisplayName *string `json:"displayName,omitempty"`
	Role        *string `json:"role,omitempty"`
	IsActive    *bool   `json:"isActive,omitempty"`
	Password    *string `json:"password,omitempty"`
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be a UUID")
		return
	}
	var req updateUserReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	// Build the SET clause dynamically so PATCH is truly partial — fields
	// not present in the request body are left untouched.
	sets := []string{}
	args := []any{}
	add := func(col string, val any) {
		args = append(args, val)
		sets = append(sets, col+" = $"+itoa(len(args)))
	}
	if req.DisplayName != nil {
		if *req.DisplayName == "" {
			writeErr(w, http.StatusBadRequest, "bad_request", "displayName cannot be empty")
			return
		}
		add("display_name", *req.DisplayName)
	}
	if req.Role != nil {
		if !auth.Role(*req.Role).Valid() {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid role")
			return
		}
		add("role", *req.Role)
	}
	if req.IsActive != nil {
		add("is_active", *req.IsActive)
	}
	if req.Password != nil {
		if len(*req.Password) < 12 {
			writeErr(w, http.StatusBadRequest, "bad_request", "password must be at least 12 characters")
			return
		}
		hash, err := auth.HashPassword(*req.Password)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "hash failed")
			return
		}
		add("password_hash", hash)
	}
	if len(sets) == 0 {
		writeErr(w, http.StatusBadRequest, "bad_request", "no fields to update")
		return
	}

	args = append(args, id)
	q := "UPDATE users SET " + join(sets, ", ") + " WHERE id = $" + itoa(len(args)) +
		" RETURNING id, email, display_name, role, totp_enrolled, is_active, last_login_at, created_at"

	type out struct {
		ID           string     `json:"id"`
		Email        string     `json:"email"`
		DisplayName  string     `json:"displayName"`
		Role         string     `json:"role"`
		TOTPEnrolled bool       `json:"totpEnrolled"`
		IsActive     bool       `json:"isActive"`
		LastLoginAt  *time.Time `json:"lastLoginAt,omitempty"`
		CreatedAt    time.Time  `json:"createdAt"`
	}
	var o out
	if err := s.pool.QueryRow(r.Context(), q, args...).Scan(
		&o.ID, &o.Email, &o.DisplayName, &o.Role, &o.TOTPEnrolled, &o.IsActive, &o.LastLoginAt, &o.CreatedAt,
	); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "update failed")
		return
	}
	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "user.update", &uid, clientIP(r),
		map[string]any{"target_user_id": o.ID})
	writeJSON(w, http.StatusOK, o)
}

// handleDeleteUser permanently removes a user. user_sites and api_keys
// rows cascade away via FK; enrollment_tokens.created_by is SET NULL;
// audit_log.actor_id is plain UUID (no FK) so the user's history is
// preserved as an orphan reference, which is the right call — we want
// the audit trail to outlive the account.
//
// Two guards: superadmins can't delete themselves (would lock the
// caller out mid-request and break the audit/Audit context), and we
// refuse to delete the last remaining superadmin so the platform can
// never end up with no one able to manage users.
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be a UUID")
		return
	}
	caller := userIDFromCtx(r.Context())
	if id.String() == caller.String() {
		writeErr(w, http.StatusBadRequest, "self_delete", "you cannot delete your own account; ask another superadmin")
		return
	}

	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "begin tx failed")
		return
	}
	defer tx.Rollback(r.Context())

	// Check role + last-superadmin invariant atomically with the delete
	// so a concurrent role flip can't smuggle us into an empty-admin
	// state.
	var (
		targetEmail string
		targetRole  string
	)
	if err := tx.QueryRow(r.Context(), `SELECT email, role FROM users WHERE id = $1`, id).
		Scan(&targetEmail, &targetRole); err != nil {
		writeErr(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	if targetRole == string(auth.RoleSuperAdmin) {
		var remaining int
		if err := tx.QueryRow(r.Context(),
			`SELECT COUNT(*) FROM users WHERE role = $1 AND id <> $2 AND is_active = TRUE`,
			auth.RoleSuperAdmin, id).Scan(&remaining); err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "count superadmins failed")
			return
		}
		if remaining == 0 {
			writeErr(w, http.StatusBadRequest, "last_superadmin",
				"refusing to delete the last active superadmin")
			return
		}
	}

	tag, err := tx.Exec(r.Context(), `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "delete failed")
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "commit failed")
		return
	}

	s.store.Audit(r.Context(), "user", "user.delete", &caller, clientIP(r),
		map[string]any{
			"target_user_id": id.String(),
			"target_email":   targetEmail,
			"target_role":    targetRole,
		})
	w.WriteHeader(http.StatusNoContent)
}

// itoa / join are tiny stdlib-free helpers used only by the dynamic
// PATCH builder above. Inlining keeps the hot path allocation-free
// without dragging in fmt for a handful of integer-to-string calls.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for n > 0 {
		pos--
		b[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(b[pos:])
}

func join(parts []string, sep string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	n := len(sep) * (len(parts) - 1)
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, 0, n)
	out = append(out, parts[0]...)
	for _, p := range parts[1:] {
		out = append(out, sep...)
		out = append(out, p...)
	}
	return string(out)
}

// ---- Agents / Appliances (read-only stubs for Phase 1) ------------------

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	siteID := r.URL.Query().Get("siteId")
	// host(primary_ip) returns the address as text, or NULL — perfect
	// for scanning into *string. We deliberately do NOT pull
	// last_metrics here (it's 10–50 KB per row); the detail endpoint
	// covers anyone who needs the full payload.
	q := `SELECT id, site_id, hostname, fingerprint, os, os_version, agent_version,
	             enrolled_at, last_seen_at, is_active, tags, created_at,
	             cpu_pct, mem_used_bytes, mem_total_bytes,
	             root_disk_used_bytes, root_disk_total_bytes,
	             uptime_seconds, pending_reboot, host(primary_ip), host(public_ip),
	             geo_country_iso, geo_country_name, geo_subdivision, geo_city,
	             geo_lat, geo_lon, geo_asn, geo_org,
	             last_metrics_at
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
			cpuPct                       *float64
			memUsed, memTotal            *int64
			rootUsed, rootTotal          *int64
			uptimeS                      *int64
			pendingReboot                bool
			primaryIP, publicIP          *string
			geoCountryIso, geoCountry    *string
			geoSubdivision, geoCity      *string
			geoLat, geoLon               *float64
			geoASN                       *int
			geoOrg                       *string
			lastMetricsAt                *time.Time
		)
		if err := rows.Scan(&id, &sid, &host, &fp, &os, &osver, &av,
			&enrolled, &last, &active, &tags, &created,
			&cpuPct, &memUsed, &memTotal,
			&rootUsed, &rootTotal,
			&uptimeS, &pendingReboot, &primaryIP, &publicIP,
			&geoCountryIso, &geoCountry, &geoSubdivision, &geoCity,
			&geoLat, &geoLon, &geoASN, &geoOrg,
			&lastMetricsAt); err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "scan failed")
			return
		}
		out = append(out, map[string]any{
			"id":                 id,
			"siteId":             sid,
			"hostname":           host,
			"fingerprint":        fp,
			"os":                 os,
			"osVersion":          osver,
			"agentVersion":       av,
			"enrolledAt":         enrolled,
			"lastSeenAt":         last,
			"isActive":           active,
			"tags":               tags,
			"createdAt":          created,
			"cpuPct":             cpuPct,
			"memUsedBytes":       memUsed,
			"memTotalBytes":      memTotal,
			"rootDiskUsedBytes":  rootUsed,
			"rootDiskTotalBytes": rootTotal,
			"uptimeSeconds":      uptimeS,
			"pendingReboot":      pendingReboot,
			"primaryIp":          primaryIP,
			"publicIp":           publicIP,
			"geoCountryIso":      geoCountryIso,
			"geoCountryName":     geoCountry,
			"geoSubdivision":     geoSubdivision,
			"geoCity":            geoCity,
			"geoLat":             geoLat,
			"geoLon":             geoLon,
			"geoAsn":             geoASN,
			"geoOrg":             geoOrg,
			"lastMetricsAt":      lastMetricsAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListAppliances(w http.ResponseWriter, r *http.Request) {
	siteID := r.URL.Query().Get("siteId")
	q := `SELECT id, site_id, name, vendor, model, serial, host(mgmt_ip), snmp_version,
	             poll_interval_s, is_active, tags, last_polled_at, last_error, created_at,
	             sys_name, uptime_seconds, cpu_pct, mem_used_bytes, mem_total_bytes,
	             if_up_count, if_total_count,
	             phys_total_count, phys_up_count, uplink_count
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
			sysName                          *string
			uptimeS                          *int64
			cpuPct                           *float64
			memUsed, memTotal                *int64
			ifUp, ifTotal                    *int
			physTotal, physUp, uplinks       *int
		)
		if err := rows.Scan(&id, &sid, &name, &vendor, &model, &serial, &ip, &snmpv, &pollSec, &active, &tags, &lastPolled, &lastErr, &created,
			&sysName, &uptimeS, &cpuPct, &memUsed, &memTotal, &ifUp, &ifTotal,
			&physTotal, &physUp, &uplinks); err != nil {
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
			"sysName":             sysName,
			"uptimeSeconds":       uptimeS,
			"cpuPct":              cpuPct,
			"memUsedBytes":        memUsed,
			"memTotalBytes":       memTotal,
			"ifUpCount":           ifUp,
			"ifTotalCount":        ifTotal,
			"physTotalCount":      physTotal,
			"physUpCount":         physUp,
			"uplinkCount":         uplinks,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
