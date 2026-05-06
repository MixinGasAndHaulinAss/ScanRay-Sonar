package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SNMPCreds is the structured form of `appliances.enc_snmp_creds`.
// Only one of {Community} (v1/v2c) or the v3 trio is populated, depending
// on Version. Marshalled to JSON before sealing.
type snmpCreds struct {
	Version   string `json:"version"`
	Community string `json:"community,omitempty"`

	V3User      string `json:"v3User,omitempty"`
	V3AuthProto string `json:"v3AuthProto,omitempty"` // SHA, SHA256, SHA512
	V3AuthPass  string `json:"v3AuthPass,omitempty"`
	V3PrivProto string `json:"v3PrivProto,omitempty"` // AES, AES256, DES
	V3PrivPass  string `json:"v3PrivPass,omitempty"`
}

type createApplianceReq struct {
	SiteID              string   `json:"siteId"`
	Name                string   `json:"name"`
	Vendor              string   `json:"vendor"`
	Model               string   `json:"model"`
	Serial              string   `json:"serial"`
	MgmtIP              string   `json:"mgmtIp"`
	SNMPVersion         string   `json:"snmpVersion"`
	Community           string   `json:"community,omitempty"`
	V3User              string   `json:"v3User,omitempty"`
	V3AuthProto         string   `json:"v3AuthProto,omitempty"`
	V3AuthPass          string   `json:"v3AuthPass,omitempty"`
	V3PrivProto         string   `json:"v3PrivProto,omitempty"`
	V3PrivPass          string   `json:"v3PrivPass,omitempty"`
	PollIntervalSeconds int      `json:"pollIntervalSeconds"`
	Tags                []string `json:"tags,omitempty"`
	CollectorID         string   `json:"collectorId,omitempty"`
	Criticality         string   `json:"criticality,omitempty"`
}

var validVendors = map[string]bool{
	"meraki": true, "cisco": true, "aruba": true, "ubiquiti": true,
	"mikrotik": true, "generic": true,
}

func (s *Server) handleCreateAppliance(w http.ResponseWriter, r *http.Request) {
	var req createApplianceReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	siteID, err := uuid.Parse(req.SiteID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "siteId must be a UUID")
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "name is required")
		return
	}
	if req.Vendor == "" {
		req.Vendor = "generic"
	}
	if !validVendors[req.Vendor] {
		writeErr(w, http.StatusBadRequest, "bad_request", "unknown vendor")
		return
	}
	if net.ParseIP(req.MgmtIP) == nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "mgmtIp must be a valid IP")
		return
	}
	if req.SNMPVersion == "" {
		req.SNMPVersion = "v2c"
	}
	switch req.SNMPVersion {
	case "v1", "v2c":
		if req.Community == "" {
			writeErr(w, http.StatusBadRequest, "bad_request", "community is required for SNMP v1/v2c")
			return
		}
	case "v3":
		if req.V3User == "" || req.V3AuthPass == "" {
			writeErr(w, http.StatusBadRequest, "bad_request", "v3User and v3AuthPass are required for SNMP v3")
			return
		}
	default:
		writeErr(w, http.StatusBadRequest, "bad_request", "snmpVersion must be v1, v2c, or v3")
		return
	}
	if req.PollIntervalSeconds <= 0 {
		req.PollIntervalSeconds = 60
	}
	if req.PollIntervalSeconds < 15 {
		writeErr(w, http.StatusBadRequest, "bad_request", "pollIntervalSeconds must be >= 15")
		return
	}

	var collectorUUID *uuid.UUID
	if req.CollectorID != "" {
		cid, perr := uuid.Parse(req.CollectorID)
		if perr != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "collectorId must be a UUID")
			return
		}
		var collSite uuid.UUID
		err := s.pool.QueryRow(r.Context(),
			`SELECT site_id FROM collectors WHERE id = $1 AND is_active`, cid).Scan(&collSite)
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusBadRequest, "bad_request", "collector not found or inactive")
			return
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "collector lookup failed")
			return
		}
		if collSite != siteID {
			writeErr(w, http.StatusBadRequest, "bad_request", "collector belongs to a different site")
			return
		}
		collectorUUID = &cid
	}

	if req.Tags == nil {
		req.Tags = []string{}
	}

	crit := "normal"
	if req.Criticality != "" {
		switch req.Criticality {
		case "low", "normal", "high", "critical":
			crit = req.Criticality
		default:
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid criticality")
			return
		}
	}

	creds := snmpCreds{
		Version:     req.SNMPVersion,
		Community:   req.Community,
		V3User:      req.V3User,
		V3AuthProto: req.V3AuthProto,
		V3AuthPass:  req.V3AuthPass,
		V3PrivProto: req.V3PrivProto,
		V3PrivPass:  req.V3PrivPass,
	}
	credsJSON, err := json.Marshal(creds)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "marshal creds")
		return
	}

	// Pre-allocate the row id so it can be used as the AEAD associated
	// data — binding the sealed creds to the row prevents anyone with
	// db write access from copy-pasting a sealed value onto a different
	// appliance and having it decrypt cleanly.
	id := uuid.New()
	sealed, err := s.sealer.Seal(credsJSON, []byte("appliance:"+id.String()))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "seal creds")
		return
	}

	const q = `
		INSERT INTO appliances (
		  id, site_id, name, vendor, model, serial, mgmt_ip,
		  snmp_version, enc_snmp_creds, poll_interval_s, tags, collector_id, criticality
		)
		VALUES ($1, $2, $3, $4, NULLIF($5,''), NULLIF($6,''), $7::inet,
		        $8, $9, $10, $11, $12, $13)
		RETURNING id
	`
	var insertedID uuid.UUID
	if err := s.pool.QueryRow(r.Context(), q,
		id, siteID, req.Name, req.Vendor, req.Model, req.Serial, req.MgmtIP,
		req.SNMPVersion, sealed, req.PollIntervalSeconds, req.Tags, collectorUUID, crit,
	).Scan(&insertedID); err != nil {
		s.log.Warn("create appliance failed", "err", err, "name", req.Name)
		writeErr(w, http.StatusBadRequest, "bad_request", "appliance exists or invalid")
		return
	}

	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "appliance.create", &uid, clientIP(r),
		map[string]any{"appliance_id": insertedID.String(), "name": req.Name, "site_id": siteID.String()})

	resp := map[string]any{
		"id":                  insertedID.String(),
		"siteId":              siteID.String(),
		"name":                req.Name,
		"vendor":              req.Vendor,
		"model":               req.Model,
		"serial":              req.Serial,
		"mgmtIp":              req.MgmtIP,
		"snmpVersion":         req.SNMPVersion,
		"pollIntervalSeconds": req.PollIntervalSeconds,
		"tags":                req.Tags,
		"criticality":         crit,
		"isActive":            true,
	}
	if collectorUUID != nil {
		resp["collectorId"] = collectorUUID.String()
	}
	writeJSON(w, http.StatusCreated, resp)
}

// updateApplianceReq is a sparse PATCH body: every field is optional
// and absent fields leave the column untouched. Credential fields are
// re-sealed only when the operator is rotating them — sending no
// credential fields keeps the existing sealed value intact, which is
// the right default for "rename this appliance" workflows.
type updateApplianceReq struct {
	SiteID              *string   `json:"siteId,omitempty"`
	Name                *string   `json:"name,omitempty"`
	Vendor              *string   `json:"vendor,omitempty"`
	Model               *string   `json:"model,omitempty"`
	Serial              *string   `json:"serial,omitempty"`
	MgmtIP              *string   `json:"mgmtIp,omitempty"`
	PollIntervalSeconds *int      `json:"pollIntervalSeconds,omitempty"`
	IsActive            *bool     `json:"isActive,omitempty"`
	Tags                *[]string `json:"tags,omitempty"`

	// Credential rotation. SNMPVersion alone is not enough to trigger
	// a re-seal — the caller must also send the matching secret(s).
	SNMPVersion *string `json:"snmpVersion,omitempty"`
	Community   *string `json:"community,omitempty"`
	V3User      *string `json:"v3User,omitempty"`
	V3AuthProto *string `json:"v3AuthProto,omitempty"`
	V3AuthPass  *string `json:"v3AuthPass,omitempty"`
	V3PrivProto *string `json:"v3PrivProto,omitempty"`
	V3PrivPass  *string `json:"v3PrivPass,omitempty"`
	CollectorID *string `json:"collectorId,omitempty"`
}

func (s *Server) handleUpdateAppliance(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be a UUID")
		return
	}
	var req updateApplianceReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	var curSite uuid.UUID
	if err := s.pool.QueryRow(r.Context(), `SELECT site_id FROM appliances WHERE id = $1`, id).Scan(&curSite); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "not_found", "appliance not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "server_error", "load appliance failed")
		return
	}

	sets := []string{}
	args := []any{}
	add := func(col string, val any) {
		args = append(args, val)
		sets = append(sets, col+" = $"+itoa(len(args)))
	}

	if req.SiteID != nil {
		if _, err := uuid.Parse(*req.SiteID); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "siteId must be a UUID")
			return
		}
		add("site_id", *req.SiteID)
	}
	if req.Name != nil {
		if *req.Name == "" {
			writeErr(w, http.StatusBadRequest, "bad_request", "name cannot be empty")
			return
		}
		add("name", *req.Name)
	}
	if req.Vendor != nil {
		if !validVendors[*req.Vendor] {
			writeErr(w, http.StatusBadRequest, "bad_request", "unknown vendor")
			return
		}
		add("vendor", *req.Vendor)
	}
	if req.Model != nil {
		if *req.Model == "" {
			sets = append(sets, "model = NULL")
		} else {
			add("model", *req.Model)
		}
	}
	if req.Serial != nil {
		if *req.Serial == "" {
			sets = append(sets, "serial = NULL")
		} else {
			add("serial", *req.Serial)
		}
	}
	if req.MgmtIP != nil {
		if net.ParseIP(*req.MgmtIP) == nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "mgmtIp must be a valid IP")
			return
		}
		args = append(args, *req.MgmtIP)
		sets = append(sets, "mgmt_ip = $"+itoa(len(args))+"::inet")
	}
	if req.PollIntervalSeconds != nil {
		if *req.PollIntervalSeconds < 15 {
			writeErr(w, http.StatusBadRequest, "bad_request", "pollIntervalSeconds must be >= 15")
			return
		}
		add("poll_interval_s", *req.PollIntervalSeconds)
	}
	if req.IsActive != nil {
		add("is_active", *req.IsActive)
	}
	if req.Tags != nil {
		add("tags", *req.Tags)
	}
	if req.Criticality != nil {
		switch *req.Criticality {
		case "low", "normal", "high", "critical":
			add("criticality", *req.Criticality)
		default:
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid criticality")
			return
		}
	}

	if req.CollectorID != nil {
		effSite := curSite
		if req.SiteID != nil {
			sid, perr := uuid.Parse(*req.SiteID)
			if perr != nil {
				writeErr(w, http.StatusBadRequest, "bad_request", "siteId must be a UUID")
				return
			}
			effSite = sid
		}
		if *req.CollectorID == "" {
			sets = append(sets, "collector_id = NULL")
		} else {
			cid, perr := uuid.Parse(*req.CollectorID)
			if perr != nil {
				writeErr(w, http.StatusBadRequest, "bad_request", "collectorId must be a UUID")
				return
			}
			var collSite uuid.UUID
			err := s.pool.QueryRow(r.Context(),
				`SELECT site_id FROM collectors WHERE id = $1 AND is_active`, cid).Scan(&collSite)
			if errors.Is(err, pgx.ErrNoRows) {
				writeErr(w, http.StatusBadRequest, "bad_request", "collector not found or inactive")
				return
			}
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "server_error", "collector lookup failed")
				return
			}
			if collSite != effSite {
				writeErr(w, http.StatusBadRequest, "bad_request", "collector belongs to a different site")
				return
			}
			add("collector_id", cid)
		}
	}

	// Credential rotation block — only enter if at least one cred
	// field changed. We re-read the current snmp_version so partial
	// updates (e.g. just rotating community on an existing v2c
	// appliance) don't have to repeat snmpVersion in the body.
	credChange := req.SNMPVersion != nil || req.Community != nil ||
		req.V3User != nil || req.V3AuthPass != nil || req.V3PrivPass != nil ||
		req.V3AuthProto != nil || req.V3PrivProto != nil
	if credChange {
		var existingVersion string
		if err := s.pool.QueryRow(r.Context(),
			`SELECT snmp_version FROM appliances WHERE id = $1`, id).Scan(&existingVersion); err != nil {
			writeErr(w, http.StatusNotFound, "not_found", "appliance not found")
			return
		}
		ver := existingVersion
		if req.SNMPVersion != nil {
			ver = *req.SNMPVersion
		}
		creds := snmpCreds{Version: ver}
		switch ver {
		case "v1", "v2c":
			if req.Community == nil || *req.Community == "" {
				writeErr(w, http.StatusBadRequest, "bad_request", "community is required for SNMP v1/v2c rotation")
				return
			}
			creds.Community = *req.Community
		case "v3":
			if req.V3User == nil || req.V3AuthPass == nil {
				writeErr(w, http.StatusBadRequest, "bad_request", "v3User and v3AuthPass are required for SNMP v3 rotation")
				return
			}
			creds.V3User = *req.V3User
			creds.V3AuthPass = *req.V3AuthPass
			if req.V3AuthProto != nil {
				creds.V3AuthProto = *req.V3AuthProto
			}
			if req.V3PrivProto != nil {
				creds.V3PrivProto = *req.V3PrivProto
			}
			if req.V3PrivPass != nil {
				creds.V3PrivPass = *req.V3PrivPass
			}
		default:
			writeErr(w, http.StatusBadRequest, "bad_request", "snmpVersion must be v1, v2c, or v3")
			return
		}
		credsJSON, err := json.Marshal(creds)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "marshal creds")
			return
		}
		sealed, err := s.sealer.Seal(credsJSON, []byte("appliance:"+id.String()))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "seal creds")
			return
		}
		add("snmp_version", ver)
		add("enc_snmp_creds", sealed)
	}

	if len(sets) == 0 {
		writeErr(w, http.StatusBadRequest, "bad_request", "no fields to update")
		return
	}

	args = append(args, id)
	q := "UPDATE appliances SET " + join(sets, ", ") + " WHERE id = $" + itoa(len(args)) +
		` RETURNING id, site_id, name, vendor, model, serial, host(mgmt_ip),
		           snmp_version, poll_interval_s, is_active, tags, last_polled_at,
		           last_error, created_at, collector_id, criticality`
	var (
		oid, sid, name, vendor, ip, snmpv, crit string
		model, serial, lastErr                  *string
		pollSec                                 int
		active                                  bool
		tags                                    []string
		lastPolled                              *time.Time
		created                                 time.Time
		collectorID                             *uuid.UUID
	)
	if err := s.pool.QueryRow(r.Context(), q, args...).Scan(
		&oid, &sid, &name, &vendor, &model, &serial, &ip, &snmpv,
		&pollSec, &active, &tags, &lastPolled, &lastErr, &created,
		&collectorID, &crit,
	); err != nil {
		s.log.Warn("update appliance failed", "err", err, "id", id.String())
		writeErr(w, http.StatusBadRequest, "bad_request", "update failed (name/IP conflict?)")
		return
	}

	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "appliance.update", &uid, clientIP(r),
		map[string]any{"appliance_id": id.String(), "creds_rotated": credChange})

	out := map[string]any{
		"id":                  oid,
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
		"criticality":         crit,
	}
	if collectorID != nil {
		out["collectorId"] = collectorID.String()
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeleteAppliance(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be a UUID")
		return
	}
	tag, err := s.pool.Exec(r.Context(), `DELETE FROM appliances WHERE id = $1`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "delete failed")
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "not_found", "appliance not found")
		return
	}
	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "appliance.delete", &uid, clientIP(r),
		map[string]any{"appliance_id": id.String()})
	w.WriteHeader(http.StatusNoContent)
}
