package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/NCLGISA/ScanRay-Sonar/internal/poller"
	"github.com/NCLGISA/ScanRay-Sonar/internal/snmp"
)

func (s *Server) handleCollectorSNMPTargets(w http.ResponseWriter, r *http.Request) {
	cid := collectorIDFromCtx(r.Context())
	rows, err := s.pool.Query(r.Context(), `
		SELECT id, host(mgmt_ip), snmp_version, vendor, poll_interval_s, enc_snmp_creds
		  FROM appliances
		 WHERE collector_id = $1 AND is_active AND enc_snmp_creds IS NOT NULL
		 ORDER BY name`, cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "query failed")
		return
	}
	defer rows.Close()

	type row struct {
		ID                  uuid.UUID `json:"id"`
		MgmtIP              string    `json:"mgmtIp"`
		SNMPVersion         string    `json:"snmpVersion"`
		Vendor              string    `json:"vendor"`
		PollIntervalSeconds int       `json:"pollIntervalSeconds"`
		EncSNMPCreds        string    `json:"encSnmpCreds"`
	}
	var targets []row
	for rows.Next() {
		var t row
		var sealed []byte
		if err := rows.Scan(&t.ID, &t.MgmtIP, &t.SNMPVersion, &t.Vendor, &t.PollIntervalSeconds, &sealed); err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "scan failed")
			return
		}
		t.EncSNMPCreds = base64.StdEncoding.EncodeToString(sealed)
		targets = append(targets, t)
	}
	writeJSON(w, http.StatusOK, map[string]any{"targets": targets})
}

type collectorSNMPResultReq struct {
	ApplianceID string          `json:"applianceId"`
	Snapshot    json.RawMessage `json:"snapshot"`
}

func (s *Server) handleCollectorSNMPResult(w http.ResponseWriter, r *http.Request) {
	cid := collectorIDFromCtx(r.Context())
	var req collectorSNMPResultReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	aid, err := uuid.Parse(req.ApplianceID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "applianceId must be a UUID")
		return
	}

	var owner *uuid.UUID
	var siteID uuid.UUID
	var vendor, crit string
	err = s.pool.QueryRow(r.Context(),
		`SELECT collector_id, site_id, vendor, COALESCE(criticality::text,'normal')
		   FROM appliances WHERE id = $1 AND is_active`, aid).
		Scan(&owner, &siteID, &vendor, &crit)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "not_found", "appliance not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "server_error", "lookup failed")
		return
	}
	if owner == nil || *owner != cid {
		writeErr(w, http.StatusForbidden, "forbidden", "appliance not delegated to this collector")
		return
	}

	var snap snmp.Snapshot
	if err := json.Unmarshal(req.Snapshot, &snap); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid snapshot")
		return
	}

	if err := poller.PersistSnapshot(r.Context(), s.pool, aid, snap); err != nil {
		s.log.Warn("collector persist snapshot failed", "err", err, "appliance_id", aid)
		writeErr(w, http.StatusInternalServerError, "server_error", "persist failed")
		return
	}
	var cpu float64
	var memRatio float64
	if snap.Chassis.CPUPct != nil {
		cpu = *snap.Chassis.CPUPct
	}
	if snap.Chassis.MemTotalBytes != nil && *snap.Chassis.MemTotalBytes > 0 && snap.Chassis.MemUsedBytes != nil {
		memRatio = float64(*snap.Chassis.MemUsedBytes) / float64(*snap.Chassis.MemTotalBytes)
	}
	payloadMap := map[string]any{
		"applianceId":  aid.String(),
		"siteId":       siteID.String(),
		"vendor":       vendor,
		"criticality":  crit,
		"cpuPct":       cpu,
		"memUsedRatio": memRatio,
	}
	addVendorMetricsToPayload(payloadMap, &snap)
	payload, _ := json.Marshal(payloadMap)
	if s.nats != nil && s.nats.IsConnected() {
		_ = s.nats.Publish("metrics.appliance", payload)
	}
	w.WriteHeader(http.StatusNoContent)
}

type collectorSNMPErrorReq struct {
	ApplianceID string `json:"applianceId"`
	Error       string `json:"error"`
}

func (s *Server) handleCollectorSNMPError(w http.ResponseWriter, r *http.Request) {
	cid := collectorIDFromCtx(r.Context())
	var req collectorSNMPErrorReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	aid, err := uuid.Parse(req.ApplianceID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "applianceId must be a UUID")
		return
	}
	var owner *uuid.UUID
	err = s.pool.QueryRow(r.Context(),
		`SELECT collector_id FROM appliances WHERE id = $1`, aid).Scan(&owner)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "not_found", "appliance not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "server_error", "lookup failed")
		return
	}
	if owner == nil || *owner != cid {
		writeErr(w, http.StatusForbidden, "forbidden", "appliance not delegated to this collector")
		return
	}

	poller.RecordPollError(r.Context(), s.pool, s.log, aid, errFromMsg(req.Error))
	w.WriteHeader(http.StatusNoContent)
}

type stringErr string

func (e stringErr) Error() string { return string(e) }

func errFromMsg(s string) error {
	if s == "" {
		return stringErr("collector poll error")
	}
	return stringErr(s)
}
