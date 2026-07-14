package api

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/NCLGISA/ScanRay-Sonar/internal/agentevents"
)

// ---------- Device groups --------------------------------------------------

func (s *Server) handleListDeviceGroups(w http.ResponseWriter, r *http.Request) {
	siteID := r.URL.Query().Get("siteId")
	q := `
		SELECT g.id, g.site_id, g.name, g.description, g.created_at, g.updated_at,
		       (SELECT COUNT(*) FROM agents a WHERE a.group_id = g.id)
		  FROM device_groups g`
	args := []any{}
	if siteID != "" {
		q += ` WHERE g.site_id = $1`
		args = append(args, siteID)
	}
	q += ` ORDER BY g.name`
	rows, err := s.pool.Query(r.Context(), q, args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "list groups failed")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, sid uuid.UUID
		var name, desc string
		var created, updated time.Time
		var members int64
		if rows.Scan(&id, &sid, &name, &desc, &created, &updated, &members) != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id.String(), "siteId": sid.String(), "name": name,
			"description": desc, "memberCount": members,
			"createdAt": created, "updatedAt": updated,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateDeviceGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SiteID      string `json:"siteId"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.SiteID == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "siteId and name required")
		return
	}
	sid, err := uuid.Parse(req.SiteID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid siteId")
		return
	}
	var id uuid.UUID
	err = s.pool.QueryRow(r.Context(), `
		INSERT INTO device_groups (site_id, name, description)
		VALUES ($1, $2, $3) RETURNING id`, sid, strings.TrimSpace(req.Name), req.Description).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "create failed (duplicate name?)")
		return
	}
	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "device_group.create", &uid, clientIP(r),
		map[string]any{"groupId": id.String(), "siteId": sid.String()})
	writeJSON(w, http.StatusCreated, map[string]any{"id": id.String()})
}

func (s *Server) handlePatchDeviceGroup(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	sets := []string{}
	args := []any{}
	add := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if req.Name != nil {
		add("name", strings.TrimSpace(*req.Name))
	}
	if req.Description != nil {
		add("description", *req.Description)
	}
	if len(sets) == 0 {
		writeErr(w, http.StatusBadRequest, "bad_request", "no fields")
		return
	}
	args = append(args, id)
	tag, err := s.pool.Exec(r.Context(),
		`UPDATE device_groups SET `+strings.Join(sets, ", ")+fmt.Sprintf(` WHERE id = $%d`, len(args)),
		args...)
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusBadRequest, "bad_request", "update failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteDeviceGroup(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	tag, err := s.pool.Exec(r.Context(), `DELETE FROM device_groups WHERE id = $1`, id)
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "not_found", "group not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAddDeviceGroupMembers(w http.ResponseWriter, r *http.Request) {
	gid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var req struct {
		AgentIDs []string `json:"agentIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.AgentIDs) == 0 {
		writeErr(w, http.StatusBadRequest, "bad_request", "agentIds required")
		return
	}
	var siteID uuid.UUID
	var gname string
	if err := s.pool.QueryRow(r.Context(), `SELECT site_id, name FROM device_groups WHERE id = $1`, gid).
		Scan(&siteID, &gname); err != nil {
		writeErr(w, http.StatusNotFound, "not_found", "group not found")
		return
	}
	n := 0
	for _, raw := range req.AgentIDs {
		aid, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		tag, err := s.pool.Exec(r.Context(), `
			UPDATE agents SET group_id = $1
			 WHERE id = $2 AND site_id = $3`, gid, aid, siteID)
		if err != nil || tag.RowsAffected() == 0 {
			continue
		}
		n++
		a := aid
		_ = agentevents.Emit(r.Context(), s.pool, siteID, &a, agentevents.KindGroupChanged, "info",
			"Device group changed", "Assigned to group "+gname,
			map[string]any{"groupId": gid.String(), "groupName": gname})
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": n})
}

func (s *Server) handleRemoveDeviceGroupMembers(w http.ResponseWriter, r *http.Request) {
	gid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var req struct {
		AgentIDs []string `json:"agentIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.AgentIDs) == 0 {
		writeErr(w, http.StatusBadRequest, "bad_request", "agentIds required")
		return
	}
	var siteID uuid.UUID
	var gname string
	_ = s.pool.QueryRow(r.Context(), `SELECT site_id, name FROM device_groups WHERE id = $1`, gid).
		Scan(&siteID, &gname)
	n := 0
	for _, raw := range req.AgentIDs {
		aid, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		tag, err := s.pool.Exec(r.Context(), `
			UPDATE agents SET group_id = NULL
			 WHERE id = $1 AND group_id = $2`, aid, gid)
		if err != nil || tag.RowsAffected() == 0 {
			continue
		}
		n++
		a := aid
		_ = agentevents.Emit(r.Context(), s.pool, siteID, &a, agentevents.KindGroupChanged, "info",
			"Device group changed", "Removed from group "+gname,
			map[string]any{"groupId": gid.String(), "groupName": gname, "cleared": true})
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": n})
}

// ---------- Data indices ---------------------------------------------------

type dataIndexMeta struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	RowEstimate int64  `json:"rowEstimate"`
}

func (s *Server) handleListAgentDataIndices(w http.ResponseWriter, r *http.Request) {
	indices := []struct {
		name, desc, table string
	}{
		{"devices", "Latest agent columns + group membership", "agents"},
		{"scores", "UX score time series", "agent_score_samples"},
		{"health", "Flattened health samples", "agent_health_samples"},
		{"processes", "Top process samples", "agent_process_samples"},
		{"apps", "Daily installed app inventory", "agent_app_inventory_daily"},
		{"patches", "Missing patch samples", "agent_patch_samples"},
		{"compliance_issues", "Open/closed compliance issues", "agent_compliance_issues"},
		{"vulnerabilities", "CVE-lite hits", "agent_vulnerabilities"},
	}
	out := make([]dataIndexMeta, 0, len(indices))
	for _, ix := range indices {
		var n int64
		_ = s.pool.QueryRow(r.Context(), `SELECT COALESCE(reltuples,0)::bigint FROM pg_class WHERE relname = $1`, ix.table).Scan(&n)
		if n < 0 {
			n = 0
		}
		out = append(out, dataIndexMeta{Name: ix.name, Description: ix.desc, RowEstimate: n})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleQueryAgentDataIndex(w http.ResponseWriter, r *http.Request) {
	index := chi.URLParam(r, "index")
	q := r.URL.Query()
	siteID := q.Get("siteId")
	agentID := q.Get("agentId")
	groupID := q.Get("groupId")
	since := q.Get("since")
	until := q.Get("until")
	export := q.Get("export") == "1"
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	size, _ := strconv.Atoi(q.Get("size"))
	if size <= 0 || size > 1000 {
		size = 100
	}
	offset := (page - 1) * size

	rows, cols, err := s.queryDEXIndex(r, index, siteID, agentID, groupID, since, until, size, offset)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if export {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", `attachment; filename="`+index+`.csv"`)
		cw := csv.NewWriter(w)
		_ = cw.Write(cols)
		for _, row := range rows {
			rec := make([]string, len(cols))
			for i, c := range cols {
				rec[i] = fmt.Sprint(row[c])
			}
			_ = cw.Write(rec)
		}
		cw.Flush()
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"index": index, "page": page, "size": size, "columns": cols, "rows": rows,
	})
}

func (s *Server) queryDEXIndex(r *http.Request, index, siteID, agentID, groupID, since, until string, limit, offset int) ([]map[string]any, []string, error) {
	ctx := r.Context()
	args := []any{}
	add := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	timeCol := "time"

	var sql string
	var cols []string
	switch index {
	case "devices":
		cols = []string{"id", "siteId", "hostname", "groupId", "groupName", "cpuPct", "pendingReboot",
			"complianceScore", "complianceSeverity", "gpuName", "lastSeenAt"}
		sql = `
			SELECT a.id::text, a.site_id::text, a.hostname, COALESCE(a.group_id::text,''),
			       COALESCE(g.name,''), a.cpu_pct, a.pending_reboot,
			       a.compliance_score, COALESCE(a.compliance_severity,''), COALESCE(a.gpu_name,''),
			       a.last_seen_at
			  FROM agents a
			  LEFT JOIN device_groups g ON g.id = a.group_id
			 WHERE TRUE`
		if siteID != "" {
			sql += ` AND a.site_id = ` + add(siteID)
		}
		if agentID != "" {
			sql += ` AND a.id = ` + add(agentID)
		}
		if groupID != "" {
			sql += ` AND a.group_id = ` + add(groupID)
		}
		sql += ` ORDER BY a.hostname LIMIT ` + add(limit) + ` OFFSET ` + add(offset)
	case "scores":
		cols = []string{"time", "agentId", "siteId", "score"}
		sql = `SELECT time, agent_id::text, site_id::text, score FROM agent_score_samples WHERE TRUE`
	case "health":
		cols = []string{"time", "agentId", "siteId", "batteryPct", "bsod24h", "crash24h", "patchCount", "wifiRssi", "pendingReboot"}
		sql = `SELECT time, agent_id::text, site_id::text, battery_pct, bsod_24h, crash_24h, patch_count, wifi_rssi, pending_reboot
		         FROM agent_health_samples WHERE TRUE`
	case "processes":
		cols = []string{"time", "agentId", "siteId", "pid", "name", "userName", "cpuPct", "rssBytes"}
		sql = `SELECT time, agent_id::text, site_id::text, pid, name, user_name, cpu_pct, rss_bytes
		         FROM agent_process_samples WHERE TRUE`
	case "apps":
		timeCol = "day"
		cols = []string{"day", "agentId", "siteId", "name", "version", "publisher"}
		sql = `SELECT day, agent_id::text, site_id::text, name, version, publisher
		         FROM agent_app_inventory_daily WHERE TRUE`
	case "patches":
		cols = []string{"time", "agentId", "siteId", "title", "kb", "severity", "sizeMb"}
		sql = `SELECT time, agent_id::text, site_id::text, title, kb, severity, size_mb
		         FROM agent_patch_samples WHERE TRUE`
	case "compliance_issues":
		timeCol = "detected_at"
		cols = []string{"id", "agentId", "siteId", "category", "code", "severity", "title", "detectedAt", "clearedAt"}
		sql = `SELECT id::text, agent_id::text, site_id::text, category, code, severity, title, detected_at, cleared_at
		         FROM agent_compliance_issues WHERE TRUE`
	case "vulnerabilities":
		timeCol = "detected_at"
		cols = []string{"id", "agentId", "cveId", "severity", "product", "detectedAt", "clearedAt"}
		sql = `SELECT v.id::text, v.agent_id::text, v.cve_id, v.severity, v.product, v.detected_at, v.cleared_at
		         FROM agent_vulnerabilities v
		         JOIN agents a ON a.id = v.agent_id WHERE TRUE`
	default:
		return nil, nil, fmt.Errorf("unknown index %q", index)
	}

	if index != "devices" {
		siteCol := "site_id"
		agentCol := "agent_id"
		if index == "vulnerabilities" {
			siteCol = "a.site_id"
			agentCol = "v.agent_id"
			timeCol = "v.detected_at"
		}
		if siteID != "" {
			sql += ` AND ` + siteCol + ` = ` + add(siteID)
		}
		if agentID != "" {
			sql += ` AND ` + agentCol + ` = ` + add(agentID)
		}
		if groupID != "" {
			sql += ` AND ` + agentCol + ` IN (SELECT id FROM agents WHERE group_id = ` + add(groupID) + `)`
		}
		if since != "" {
			sql += ` AND ` + timeCol + ` >= ` + add(since) + `::timestamptz`
		}
		if until != "" {
			sql += ` AND ` + timeCol + ` <= ` + add(until) + `::timestamptz`
		}
		sql += ` ORDER BY ` + timeCol + ` DESC LIMIT ` + add(limit) + ` OFFSET ` + add(offset)
	}

	pgRows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("query failed: %w", err)
	}
	defer pgRows.Close()

	out := []map[string]any{}
	for pgRows.Next() {
		vals, err := pgRows.Values()
		if err != nil {
			continue
		}
		row := map[string]any{}
		for i := range cols {
			key := cols[i]
			v := vals[i]
			switch t := v.(type) {
			case time.Time:
				row[key] = t.UTC().Format(time.RFC3339Nano)
			case [16]byte:
				row[key] = uuid.UUID(t).String()
			default:
				row[key] = v
			}
		}
		out = append(out, row)
	}
	return out, cols, nil
}

// ---------- System events --------------------------------------------------

func (s *Server) handleListAgentEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	args := []any{}
	where := []string{"TRUE"}
	add := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if v := q.Get("siteId"); v != "" {
		where = append(where, "site_id = "+add(v))
	}
	if v := q.Get("agentId"); v != "" {
		where = append(where, "agent_id = "+add(v))
	}
	if v := q.Get("kind"); v != "" {
		where = append(where, "kind = "+add(v))
	}
	if v := q.Get("severity"); v != "" {
		where = append(where, "severity = "+add(v))
	}
	if v := q.Get("since"); v != "" {
		where = append(where, "time >= "+add(v)+"::timestamptz")
	}
	if v := q.Get("until"); v != "" {
		where = append(where, "time <= "+add(v)+"::timestamptz")
	}
	limit := 100
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	sql := `
		SELECT id, time, site_id::text, COALESCE(agent_id::text,''), kind, severity, title, body, metadata
		  FROM agent_system_events
		 WHERE ` + strings.Join(where, " AND ") + `
		 ORDER BY time DESC LIMIT ` + add(limit)
	rows, err := s.pool.Query(r.Context(), sql, args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "list events failed")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var t time.Time
		var siteID, agentID, kind, sev, title, body string
		var meta []byte
		if rows.Scan(&id, &t, &siteID, &agentID, &kind, &sev, &title, &body, &meta) != nil {
			continue
		}
		m := map[string]any{
			"id": id, "time": t, "siteId": siteID, "kind": kind,
			"severity": sev, "title": title, "body": body,
			"metadata": json.RawMessage(meta),
		}
		if agentID != "" {
			m["agentId"] = agentID
		}
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, out)
}

// ---------- Compliance -----------------------------------------------------

func (s *Server) handleAgentsComplianceSummary(w http.ResponseWriter, r *http.Request) {
	siteID := r.URL.Query().Get("siteId")
	args := []any{}
	where := "WHERE is_active"
	if siteID != "" {
		where += " AND site_id = $1"
		args = append(args, siteID)
	}
	var agentCount, openIssues, openCVEs, pendingReboot int64
	var avgScore *float64
	_ = s.pool.QueryRow(r.Context(), `
		SELECT COUNT(*), AVG(compliance_score),
		       COALESCE(SUM(compliance_issues_count),0),
		       COUNT(*) FILTER (WHERE pending_reboot)
		  FROM agents `+where, args...).Scan(&agentCount, &avgScore, &openIssues, &pendingReboot)
	cveQ := `SELECT COUNT(*) FROM agent_vulnerabilities v JOIN agents a ON a.id = v.agent_id WHERE v.cleared_at IS NULL AND a.is_active`
	cveArgs := []any{}
	if siteID != "" {
		cveQ += ` AND a.site_id = $1`
		cveArgs = append(cveArgs, siteID)
	}
	_ = s.pool.QueryRow(r.Context(), cveQ, cveArgs...).Scan(&openCVEs)

	listQ := `
		SELECT id::text, hostname, COALESCE(compliance_score,0), COALESCE(compliance_severity,''),
		       compliance_issues_count, pending_reboot, group_id::text
		  FROM agents ` + where + ` ORDER BY compliance_score NULLS FIRST, hostname LIMIT 500`
	rows, err := s.pool.Query(r.Context(), listQ, args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "compliance list failed")
		return
	}
	defer rows.Close()
	agents := []map[string]any{}
	for rows.Next() {
		var id, host, sev string
		var score float64
		var issues int
		var reboot bool
		var gid *string
		if rows.Scan(&id, &host, &score, &sev, &issues, &reboot, &gid) != nil {
			continue
		}
		row := map[string]any{
			"id": id, "hostname": host, "complianceScore": score,
			"complianceSeverity": sev, "issuesCount": issues, "pendingReboot": reboot,
		}
		if gid != nil && *gid != "" {
			row["groupId"] = *gid
		}
		agents = append(agents, row)
	}
	avg := 0.0
	if avgScore != nil {
		avg = *avgScore
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agentCount": agentCount, "avgScore": avg, "openIssues": openIssues,
		"openCves": openCVEs, "pendingRebootCount": pendingReboot, "agents": agents,
	})
}

func (s *Server) handleAgentComplianceDetail(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be UUID")
		return
	}
	var score *float64
	var sev *string
	var issuesCount int
	var lastAt *time.Time
	err = s.pool.QueryRow(r.Context(), `
		SELECT compliance_score, compliance_severity, compliance_issues_count, last_compliance_at
		  FROM agents WHERE id = $1`, id).Scan(&score, &sev, &issuesCount, &lastAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeErr(w, http.StatusNotFound, "not_found", "agent not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "server_error", "load failed")
		return
	}
	issueRows, _ := s.pool.Query(r.Context(), `
		SELECT id::text, category, code, severity, title, detail, detected_at, cleared_at
		  FROM agent_compliance_issues WHERE agent_id = $1
		 ORDER BY cleared_at NULLS FIRST, detected_at DESC LIMIT 200`, id)
	issues := []map[string]any{}
	if issueRows != nil {
		defer issueRows.Close()
		for issueRows.Next() {
			var iid, cat, code, severity, title, detail string
			var det time.Time
			var cleared *time.Time
			if issueRows.Scan(&iid, &cat, &code, &severity, &title, &detail, &det, &cleared) != nil {
				continue
			}
			issues = append(issues, map[string]any{
				"id": iid, "category": cat, "code": code, "severity": severity,
				"title": title, "detail": detail, "detectedAt": det, "clearedAt": cleared,
			})
		}
	}
	vrows, _ := s.pool.Query(r.Context(), `
		SELECT id::text, cve_id, severity, product, detected_at, cleared_at
		  FROM agent_vulnerabilities WHERE agent_id = $1
		 ORDER BY cleared_at NULLS FIRST, detected_at DESC LIMIT 100`, id)
	vulns := []map[string]any{}
	if vrows != nil {
		defer vrows.Close()
		for vrows.Next() {
			var vid, cve, severity, product string
			var det time.Time
			var cleared *time.Time
			if vrows.Scan(&vid, &cve, &severity, &product, &det, &cleared) != nil {
				continue
			}
			vulns = append(vulns, map[string]any{
				"id": vid, "cveId": cve, "severity": severity, "product": product,
				"detectedAt": det, "clearedAt": cleared,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agentId": id.String(), "complianceScore": score, "complianceSeverity": sev,
		"issuesCount": issuesCount, "lastComplianceAt": lastAt,
		"issues": issues, "vulnerabilities": vulns,
	})
}
