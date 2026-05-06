package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
)

func (s *Server) handleQueryAlarms(w http.ResponseWriter, r *http.Request) {
	q := `
		SELECT id, COALESCE(rule_id::text,''), COALESCE(site_id::text,''), target_kind, target_id::text,
		       severity, title, opened_at, cleared_at
		  FROM alarms WHERE 1=1`
	args := []any{}
	if v := r.URL.Query().Get("siteId"); v != "" {
		sid, err := uuid.Parse(v)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid siteId")
			return
		}
		if !apiKeyAllowsSite(r.Context(), sid) {
			writeErr(w, http.StatusForbidden, "forbidden", "API key not scoped to this site")
			return
		}
		args = append(args, v)
		q += ` AND site_id = $` + strconv.Itoa(len(args))
	} else {
		q = appendAPISiteFilter(q, &args, r.Context(), "site_id")
	}
	if r.URL.Query().Get("activeOnly") == "1" || r.URL.Query().Get("activeOnly") == "true" {
		q += ` AND cleared_at IS NULL`
	}
	q += ` ORDER BY opened_at DESC LIMIT 500`

	rows, err := s.pool.Query(r.Context(), q, args...)
	if err != nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var ruleID, siteID, tgtKind, tgtID, sev, title string
		var opened time.Time
		var cleared *time.Time
		if rows.Scan(&id, &ruleID, &siteID, &tgtKind, &tgtID, &sev, &title, &opened, &cleared) != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "ruleId": nullIfEmpty(ruleID), "siteId": nullIfEmpty(siteID),
			"targetKind": tgtKind, "targetId": tgtID, "severity": sev, "title": title,
			"openedAt": opened, "clearedAt": cleared,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"alarms": out})
}

func (s *Server) handleQueryMetrics(w http.ResponseWriter, r *http.Request) {
	ap := r.URL.Query().Get("applianceId")
	ag := r.URL.Query().Get("agentId")
	rangeStr := r.URL.Query().Get("range")
	if rangeStr == "" {
		rangeStr = "24h"
	}
	dur, err := parseRangeDuration(rangeStr)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	to := time.Now().UTC()
	from := to.Add(-dur)

	if ap != "" {
		aid, err := uuid.Parse(ap)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid applianceId")
			return
		}
		var siteID uuid.UUID
		err = s.pool.QueryRow(r.Context(), `SELECT site_id FROM appliances WHERE id = $1 AND is_active`, aid).Scan(&siteID)
		if err != nil {
			writeErr(w, http.StatusNotFound, "not_found", "appliance not found")
			return
		}
		if !apiKeyAllowsSite(r.Context(), siteID) {
			writeErr(w, http.StatusForbidden, "forbidden", "API key not scoped to this site")
			return
		}
		rows, err := s.pool.Query(r.Context(), `
			SELECT time, cpu_pct, mem_used_bytes, mem_total_bytes
			  FROM appliance_metric_samples
			 WHERE appliance_id = $1 AND time >= $2 AND time <= $3
			 ORDER BY time ASC`, aid, from, to)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "query failed")
			return
		}
		defer rows.Close()
		samples := []map[string]any{}
		for rows.Next() {
			var t time.Time
			var cpu *float64
			var mu, mt *int64
			if rows.Scan(&t, &cpu, &mu, &mt) != nil {
				continue
			}
			samples = append(samples, map[string]any{"time": t, "cpuPct": cpu, "memUsedBytes": mu, "memTotalBytes": mt})
		}
		writeJSON(w, http.StatusOK, map[string]any{"kind": "appliance", "applianceId": ap, "range": rangeStr, "samples": samples})
		return
	}
	if ag != "" {
		gid, err := uuid.Parse(ag)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid agentId")
			return
		}
		var siteID uuid.UUID
		err = s.pool.QueryRow(r.Context(), `SELECT site_id FROM agents WHERE id = $1 AND is_active`, gid).Scan(&siteID)
		if err != nil {
			writeErr(w, http.StatusNotFound, "not_found", "agent not found")
			return
		}
		if !apiKeyAllowsSite(r.Context(), siteID) {
			writeErr(w, http.StatusForbidden, "forbidden", "API key not scoped to this site")
			return
		}
		rows, err := s.pool.Query(r.Context(), `
			SELECT time, cpu_pct, mem_used_bytes, mem_total_bytes
			  FROM agent_metric_samples
			 WHERE agent_id = $1 AND time >= $2 AND time <= $3
			 ORDER BY time ASC`, gid, from, to)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "query failed")
			return
		}
		defer rows.Close()
		samples := []map[string]any{}
		for rows.Next() {
			var t time.Time
			var cpu *float64
			var mu, mt *int64
			if rows.Scan(&t, &cpu, &mu, &mt) != nil {
				continue
			}
			samples = append(samples, map[string]any{"time": t, "cpuPct": cpu, "memUsedBytes": mu, "memTotalBytes": mt})
		}
		writeJSON(w, http.StatusOK, map[string]any{"kind": "agent", "agentId": ag, "range": rangeStr, "samples": samples})
		return
	}
	writeErr(w, http.StatusBadRequest, "bad_request", "applianceId or agentId required")
}
