// Package api — appliance telemetry read paths.
//
// Mirrors the agent telemetry handlers: GET /appliances/{id} returns
// the full denormalized row + verbatim last_snapshot for the detail
// page, and GET /appliances/{id}/metrics returns chassis-level
// time-series (CPU + memory) for sparklines. Per-port time-series
// lives at /appliances/{id}/interfaces/{ifIndex}/metrics so the UI
// can lazily fetch a single port's bps history when the operator
// expands it.
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// applianceDetailView is what GET /appliances/{id} returns.
type applianceDetailView struct {
	ID                  string          `json:"id"`
	SiteID              string          `json:"siteId"`
	Name                string          `json:"name"`
	Vendor              string          `json:"vendor"`
	Model               *string         `json:"model,omitempty"`
	Serial              *string         `json:"serial,omitempty"`
	MgmtIP              string          `json:"mgmtIp"`
	SNMPVersion         string          `json:"snmpVersion"`
	PollIntervalSeconds int             `json:"pollIntervalSeconds"`
	IsActive            bool            `json:"isActive"`
	Tags                []string        `json:"tags"`
	LastPolledAt        *time.Time      `json:"lastPolledAt,omitempty"`
	LastError           *string         `json:"lastError,omitempty"`
	CreatedAt           time.Time       `json:"createdAt"`
	SysDescr            *string         `json:"sysDescr,omitempty"`
	SysName             *string         `json:"sysName,omitempty"`
	UptimeSeconds       *int64          `json:"uptimeSeconds,omitempty"`
	CPUPct              *float64        `json:"cpuPct,omitempty"`
	MemUsedBytes        *int64          `json:"memUsedBytes,omitempty"`
	MemTotalBytes       *int64          `json:"memTotalBytes,omitempty"`
	IfUpCount           *int            `json:"ifUpCount,omitempty"`
	IfTotalCount        *int            `json:"ifTotalCount,omitempty"`
	PhysTotalCount      *int            `json:"physTotalCount,omitempty"`
	PhysUpCount         *int            `json:"physUpCount,omitempty"`
	UplinkCount         *int            `json:"uplinkCount,omitempty"`
	LastSnapshotAt      *time.Time      `json:"lastSnapshotAt,omitempty"`
	LastSnapshot        json.RawMessage `json:"lastSnapshot,omitempty"`
}

func (s *Server) handleGetAppliance(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be a UUID")
		return
	}

	const q = `
		SELECT id, site_id, name, vendor, model, serial, host(mgmt_ip), snmp_version,
		       poll_interval_s, is_active, tags, last_polled_at, last_error, created_at,
		       sys_descr, sys_name, uptime_seconds, cpu_pct,
		       mem_used_bytes, mem_total_bytes, if_up_count, if_total_count,
		       phys_total_count, phys_up_count, uplink_count,
		       last_snapshot_at, last_snapshot
		  FROM appliances
		 WHERE id = $1
	`
	var (
		v        applianceDetailView
		lastSnap []byte
	)
	err = s.pool.QueryRow(r.Context(), q, id).Scan(
		&v.ID, &v.SiteID, &v.Name, &v.Vendor, &v.Model, &v.Serial, &v.MgmtIP, &v.SNMPVersion,
		&v.PollIntervalSeconds, &v.IsActive, &v.Tags, &v.LastPolledAt, &v.LastError, &v.CreatedAt,
		&v.SysDescr, &v.SysName, &v.UptimeSeconds, &v.CPUPct,
		&v.MemUsedBytes, &v.MemTotalBytes, &v.IfUpCount, &v.IfTotalCount,
		&v.PhysTotalCount, &v.PhysUpCount, &v.UplinkCount,
		&v.LastSnapshotAt, &lastSnap,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not_found", "appliance not found")
		return
	}
	if err != nil {
		s.log.Warn("get appliance failed", "err", err, "id", id)
		writeErr(w, http.StatusInternalServerError, "server_error", "load appliance failed")
		return
	}
	if len(lastSnap) > 0 {
		v.LastSnapshot = json.RawMessage(lastSnap)
	}
	writeJSON(w, http.StatusOK, v)
}

// applianceMetricSample is one chassis-level row.
type applianceMetricSample struct {
	Time          time.Time `json:"time"`
	CPUPct        *float64  `json:"cpuPct,omitempty"`
	MemUsedBytes  *int64    `json:"memUsedBytes,omitempty"`
	MemTotalBytes *int64    `json:"memTotalBytes,omitempty"`
}

func (s *Server) handleApplianceMetrics(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be a UUID")
		return
	}
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

	rows, err := s.pool.Query(r.Context(), `
		SELECT time, cpu_pct, mem_used_bytes, mem_total_bytes
		  FROM appliance_metric_samples
		 WHERE appliance_id = $1 AND time >= $2 AND time <= $3
		 ORDER BY time ASC
	`, id, from, to)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "query metrics failed")
		return
	}
	defer rows.Close()

	out := []applianceMetricSample{}
	for rows.Next() {
		var s applianceMetricSample
		if err := rows.Scan(&s.Time, &s.CPUPct, &s.MemUsedBytes, &s.MemTotalBytes); err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "scan failed")
			return
		}
		out = append(out, s)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"applianceId":  id.String(),
		"range":        rangeStr,
		"capturedAtTo": to,
		"samples":      out,
	})
}

// applianceIfaceSample is one per-port row.
type applianceIfaceSample struct {
	Time        time.Time `json:"time"`
	InBps       *int64    `json:"inBps,omitempty"`
	OutBps      *int64    `json:"outBps,omitempty"`
	InErrors    *int64    `json:"inErrors,omitempty"`
	OutErrors   *int64    `json:"outErrors,omitempty"`
	InDiscards  *int64    `json:"inDiscards,omitempty"`
	OutDiscards *int64    `json:"outDiscards,omitempty"`
}

func (s *Server) handleApplianceIfaceMetrics(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be a UUID")
		return
	}
	ifIdx, err := strconv.Atoi(chi.URLParam(r, "ifIndex"))
	if err != nil || ifIdx <= 0 {
		writeErr(w, http.StatusBadRequest, "bad_request", "ifIndex must be a positive integer")
		return
	}
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

	rows, err := s.pool.Query(r.Context(), `
		SELECT time, in_bps, out_bps, in_errors, out_errors, in_discards, out_discards
		  FROM appliance_iface_samples
		 WHERE appliance_id = $1 AND if_index = $2
		   AND time >= $3 AND time <= $4
		 ORDER BY time ASC
	`, id, ifIdx, from, to)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "query iface metrics failed")
		return
	}
	defer rows.Close()

	out := []applianceIfaceSample{}
	for rows.Next() {
		var smp applianceIfaceSample
		if err := rows.Scan(&smp.Time, &smp.InBps, &smp.OutBps,
			&smp.InErrors, &smp.OutErrors, &smp.InDiscards, &smp.OutDiscards); err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "scan failed")
			return
		}
		out = append(out, smp)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"applianceId":  id.String(),
		"ifIndex":      ifIdx,
		"range":        rangeStr,
		"capturedAtTo": to,
		"samples":      out,
	})
}

// parseRangeDuration is defined alongside the agent metrics handler
// in handlers_agent_telemetry.go — same semantics, shared here.
