package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"

	"github.com/NCLGISA/ScanRay-Sonar/internal/auth"
	"github.com/NCLGISA/ScanRay-Sonar/internal/checks"
	"github.com/NCLGISA/ScanRay-Sonar/internal/poller"
)

func (s *Server) handleListCheckTypes(w http.ResponseWriter, r *http.Request) {
	_ = checks.Load()
	packs := checks.Packs()
	out := make([]map[string]any, 0, len(packs))
	for _, p := range packs {
		out = append(out, map[string]any{
			"id":        p.ID,
			"title":     p.Title,
			"mechanism": p.Mechanism,
			"runner":    p.Runner,
			"params":    p.Params,
			"channels":  p.Channels,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListChecks(w http.ResponseWriter, r *http.Request) {
	siteFilter := r.URL.Query().Get("siteId")
	q := `
		SELECT id, site_id, name, type_id, params, interval_seconds, enabled, preferred_runner,
		       assigned_agent_id, assigned_collector_id, appliance_id, credential_id,
		       last_run_at, last_ok, last_error, created_at
		  FROM checks`
	args := []any{}
	if siteFilter != "" {
		sid, err := uuid.Parse(siteFilter)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid siteId")
			return
		}
		q += ` WHERE site_id = $1`
		args = append(args, sid)
	}
	q += ` ORDER BY created_at DESC LIMIT 500`
	rows, err := s.pool.Query(r.Context(), q, args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", "list failed")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			id, siteID                                    uuid.UUID
			name, typeID, pref                            string
			params                                        []byte
			interval                                      int
			enabled                                       bool
			agentID, collectorID, applianceID, credID     *uuid.UUID
			lastRun                                       *time.Time
			lastOK                                        *bool
			lastErr                                       *string
			created                                       time.Time
		)
		if rows.Scan(&id, &siteID, &name, &typeID, &params, &interval, &enabled, &pref,
			&agentID, &collectorID, &applianceID, &credID, &lastRun, &lastOK, &lastErr, &created) != nil {
			continue
		}
		var paramsObj any
		_ = json.Unmarshal(params, &paramsObj)
		row := map[string]any{
			"id": id.String(), "siteId": siteID.String(), "name": name, "typeId": typeID,
			"params": paramsObj, "intervalSeconds": interval, "enabled": enabled,
			"preferredRunner": pref, "createdAt": created.UTC().Format(time.RFC3339),
		}
		if agentID != nil {
			row["assignedAgentId"] = agentID.String()
		}
		if collectorID != nil {
			row["assignedCollectorId"] = collectorID.String()
		}
		if applianceID != nil {
			row["applianceId"] = applianceID.String()
		}
		if credID != nil {
			row["credentialId"] = credID.String()
		}
		if lastRun != nil {
			row["lastRunAt"] = lastRun.UTC().Format(time.RFC3339)
		}
		if lastOK != nil {
			row["lastOk"] = *lastOK
		}
		if lastErr != nil {
			row["lastError"] = *lastErr
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SiteID              string         `json:"siteId"`
		Name                string         `json:"name"`
		TypeID              string         `json:"typeId"`
		Params              map[string]any `json:"params"`
		IntervalSeconds     *int           `json:"intervalSeconds"`
		PreferredRunner     string         `json:"preferredRunner"`
		AssignedAgentID     *string        `json:"assignedAgentId"`
		AssignedCollectorID *string        `json:"assignedCollectorId"`
		ApplianceID         *string        `json:"applianceId"`
		CredentialID        *string        `json:"credentialId"`
		Enabled             *bool          `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SiteID == "" || req.Name == "" || req.TypeID == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "siteId, name, typeId required")
		return
	}
	if _, ok := checks.Lookup(req.TypeID); !ok {
		writeErr(w, http.StatusBadRequest, "bad_request", "unknown typeId")
		return
	}
	siteID, err := uuid.Parse(req.SiteID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid siteId")
		return
	}
	pref := req.PreferredRunner
	if pref == "" {
		pref = "auto"
	}
	if checks.IsCentralOnly(req.TypeID) {
		pref = "central"
	}
	if pref != "auto" && pref != "agent" && pref != "collector" && pref != "central" {
		writeErr(w, http.StatusBadRequest, "bad_request", "preferredRunner must be auto|agent|collector|central")
		return
	}
	interval := 60
	if req.IntervalSeconds != nil && *req.IntervalSeconds >= 15 {
		interval = *req.IntervalSeconds
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if req.Params == nil {
		req.Params = map[string]any{}
	}
	if err := checks.RejectSecretParams(req.Params); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if req.CredentialID != nil && *req.CredentialID != "" {
		req.Params["credentialId"] = *req.CredentialID
	}
	credID, err := s.validateCheckCredential(r.Context(), siteID, req.TypeID, req.Params)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	paramsJSON, _ := json.Marshal(req.Params)
	agentID := parseOptionalUUID(req.AssignedAgentID)
	collectorID := parseOptionalUUID(req.AssignedCollectorID)
	applianceID := parseOptionalUUID(req.ApplianceID)
	if checks.IsCentralOnly(req.TypeID) {
		agentID = nil
		collectorID = nil
	}

	var id uuid.UUID
	err = s.pool.QueryRow(r.Context(), `
		INSERT INTO checks (site_id, name, type_id, params, interval_seconds, enabled, preferred_runner,
		                    assigned_agent_id, assigned_collector_id, appliance_id, credential_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`,
		siteID, req.Name, req.TypeID, paramsJSON, interval, enabled, pref, agentID, collectorID, applianceID, credID,
	).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "insert failed")
		return
	}
	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "check.create", &uid, clientIP(r), map[string]any{"check_id": id.String()})
	writeJSON(w, http.StatusCreated, map[string]any{"id": id.String()})
}

func (s *Server) validateCheckCredential(ctx context.Context, siteID uuid.UUID, typeID string, params map[string]any) (*uuid.UUID, error) {
	wantKind, needs := checks.ExpectedCredKind(typeID)
	id, ok := checks.CredentialIDFromParams(params)
	if !needs {
		if ok {
			// Allow unused credentialId on phase-1 but clear it from column.
			return nil, nil
		}
		return nil, nil
	}
	if !ok {
		return nil, errString("credentialId required for " + typeID)
	}
	var gotSite uuid.UUID
	var kind string
	err := s.pool.QueryRow(ctx, `SELECT site_id, kind FROM site_credentials WHERE id=$1`, id).Scan(&gotSite, &kind)
	if err != nil {
		return nil, errString("credentialId not found")
	}
	if gotSite != siteID {
		return nil, errString("credentialId must belong to the same site")
	}
	if kind != wantKind {
		return nil, errString("credential kind must be " + wantKind + " for " + typeID)
	}
	return &id, nil
}

type simpleErr string

func (e simpleErr) Error() string { return string(e) }

func errString(s string) error { return simpleErr(s) }

func parseOptionalUUID(p *string) *uuid.UUID {
	if p == nil || *p == "" {
		return nil
	}
	id, err := uuid.Parse(*p)
	if err != nil {
		return nil
	}
	return &id
}

func (s *Server) handlePatchCheck(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid json")
		return
	}

	var siteID uuid.UUID
	var typeID string
	if err := s.pool.QueryRow(r.Context(), `SELECT site_id, type_id FROM checks WHERE id=$1`, id).Scan(&siteID, &typeID); err != nil {
		writeErr(w, http.StatusNotFound, "not_found", "check not found")
		return
	}

	if v, ok := req["name"].(string); ok && v != "" {
		_, _ = s.pool.Exec(r.Context(), `UPDATE checks SET name=$2, updated_at=NOW() WHERE id=$1`, id, v)
	}
	if v, ok := req["enabled"].(bool); ok {
		_, _ = s.pool.Exec(r.Context(), `UPDATE checks SET enabled=$2, updated_at=NOW() WHERE id=$1`, id, v)
	}
	if v, ok := req["preferredRunner"].(string); ok && v != "" {
		if checks.IsCentralOnly(typeID) {
			v = "central"
		}
		_, _ = s.pool.Exec(r.Context(), `UPDATE checks SET preferred_runner=$2, updated_at=NOW() WHERE id=$1`, id, v)
	}
	if v, ok := req["intervalSeconds"].(float64); ok && v >= 15 {
		_, _ = s.pool.Exec(r.Context(), `UPDATE checks SET interval_seconds=$2, updated_at=NOW() WHERE id=$1`, id, int(v))
	}
	if params, ok := req["params"].(map[string]any); ok {
		if err := checks.RejectSecretParams(params); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		if cid, ok := req["credentialId"].(string); ok && cid != "" {
			params["credentialId"] = cid
		}
		credID, err := s.validateCheckCredential(r.Context(), siteID, typeID, params)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		b, _ := json.Marshal(params)
		_, _ = s.pool.Exec(r.Context(), `UPDATE checks SET params=$2, credential_id=$3, updated_at=NOW() WHERE id=$1`, id, b, credID)
	} else if cid, ok := req["credentialId"].(string); ok {
		// Merge credentialId into existing params
		var existing []byte
		_ = s.pool.QueryRow(r.Context(), `SELECT params FROM checks WHERE id=$1`, id).Scan(&existing)
		var m map[string]any
		_ = json.Unmarshal(existing, &m)
		if m == nil {
			m = map[string]any{}
		}
		m["credentialId"] = cid
		credID, err := s.validateCheckCredential(r.Context(), siteID, typeID, m)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		b, _ := json.Marshal(m)
		_, _ = s.pool.Exec(r.Context(), `UPDATE checks SET params=$2, credential_id=$3, updated_at=NOW() WHERE id=$1`, id, b, credID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id.String()})
}

func (s *Server) handleDeleteCheck(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	_, _ = s.pool.Exec(r.Context(), `DELETE FROM checks WHERE id=$1`, id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleCheckSamples(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	rows, err := s.pool.Query(r.Context(), `
		SELECT time, channel_key, value_double, value_text, runner, ok
		  FROM check_samples WHERE check_id=$1 AND time > NOW() - INTERVAL '24 hours'
		 ORDER BY time DESC LIMIT 500`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", "query failed")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			t      time.Time
			key    string
			vd     *float64
			vt     *string
			runner string
			ok     bool
		)
		if rows.Scan(&t, &key, &vd, &vt, &runner, &ok) != nil {
			continue
		}
		row := map[string]any{"time": t.UTC().Format(time.RFC3339), "channelKey": key, "runner": runner, "ok": ok}
		if vd != nil {
			row["valueDouble"] = *vd
		}
		if vt != nil {
			row["valueText"] = *vt
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAgentListChecks returns due checks for the authenticated probe.
func (s *Server) handleAgentListChecks(w http.ResponseWriter, r *http.Request) {
	agentID, ok := s.agentIDFromRequest(w, r)
	if !ok {
		return
	}
	var siteID uuid.UUID
	if err := s.pool.QueryRow(r.Context(), `SELECT site_id FROM agents WHERE id=$1`, agentID).Scan(&siteID); err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized", "unknown agent")
		return
	}
	_, _ = s.pool.Exec(r.Context(), `UPDATE agents SET last_seen_at=NOW() WHERE id=$1`, agentID)

	rows, err := s.pool.Query(r.Context(), `
		SELECT id, site_id, name, type_id, params, interval_seconds, preferred_runner,
		       assigned_agent_id, assigned_collector_id, appliance_id, credential_id,
		       COALESCE(last_run_at, TIMESTAMPTZ 'epoch')
		  FROM checks
		 WHERE enabled = TRUE AND site_id = $1
		   AND (preferred_runner IN ('auto','agent'))
		   AND (assigned_agent_id IS NULL OR assigned_agent_id = $2)`, agentID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", "list failed")
		return
	}
	defer rows.Close()

	agents := []checks.OnlineAgent{{ID: agentID, SiteID: siteID, LastSeen: time.Now()}}
	now := time.Now()
	out := []map[string]any{}
	for rows.Next() {
		var (
			c                    checks.CheckRow
			paramsRaw            []byte
			lastRun              time.Time
			aID, cID, apID, crID *uuid.UUID
		)
		if rows.Scan(&c.ID, &c.SiteID, &c.Name, &c.TypeID, &paramsRaw, &c.IntervalSeconds,
			&c.PreferredRunner, &aID, &cID, &apID, &crID, &lastRun) != nil {
			continue
		}
		if checks.IsCentralOnly(c.TypeID) {
			continue
		}
		c.AssignedAgentID = aID
		c.AssignedCollectorID = cID
		c.ApplianceID = apID
		c.CredentialID = crID
		_ = json.Unmarshal(paramsRaw, &c.Params)
		interval := time.Duration(c.IntervalSeconds) * time.Second
		if interval < 15*time.Second {
			interval = 15 * time.Second
		}
		if now.Sub(lastRun) < interval {
			continue
		}
		if checks.SelectRunner(c, agents, now) != "agent" {
			continue
		}
		out = append(out, map[string]any{
			"id": c.ID.String(), "typeId": c.TypeID, "name": c.Name, "params": c.Params,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAgentCheckResults(w http.ResponseWriter, r *http.Request) {
	agentID, ok := s.agentIDFromRequest(w, r)
	if !ok {
		return
	}
	var req struct {
		Results []struct {
			CheckID string           `json:"checkId"`
			OK      bool             `json:"ok"`
			Error   string           `json:"error"`
			Samples []map[string]any `json:"samples"`
		} `json:"results"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid json")
		return
	}
	var nc *nats.Conn
	if s.nats != nil {
		nc = s.nats
	}
	for _, item := range req.Results {
		cid, err := uuid.Parse(item.CheckID)
		if err != nil {
			continue
		}
		var c checks.CheckRow
		var paramsRaw []byte
		err = s.pool.QueryRow(r.Context(), `
			SELECT id, site_id, name, type_id, params FROM checks WHERE id=$1 AND (assigned_agent_id IS NULL OR assigned_agent_id=$2)`,
			cid, agentID).Scan(&c.ID, &c.SiteID, &c.Name, &c.TypeID, &paramsRaw)
		if err != nil {
			continue
		}
		if checks.IsCentralOnly(c.TypeID) {
			continue
		}
		_ = json.Unmarshal(paramsRaw, &c.Params)
		res := checks.Result{OK: item.OK, Error: item.Error}
		for _, sm := range item.Samples {
			key, _ := sm["key"].(string)
			if key == "" {
				continue
			}
			sample := checks.Sample{Key: key}
			if v, ok := sm["value"].(float64); ok {
				sample.Value = v
				sample.HasNum = true
			}
			if t, ok := sm["text"].(string); ok {
				sample.Text = t
			}
			res.Samples = append(res.Samples, sample)
		}
		_ = poller.PersistCheckResult(r.Context(), s.pool, nc, c, res, "agent")
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) agentIDFromRequest(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	tok := r.URL.Query().Get("token")
	if tok == "" {
		tok = bearerToken(r)
	}
	if tok == "" {
		writeErr(w, http.StatusUnauthorized, "unauthorized", "missing agent token")
		return uuid.Nil, false
	}
	claims, err := s.iss.Parse(tok, auth.KindAgent)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized", "invalid agent token")
		return uuid.Nil, false
	}
	return claims.UserID, true
}
