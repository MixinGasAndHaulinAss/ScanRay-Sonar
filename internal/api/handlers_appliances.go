package api

import (
	"encoding/json"
	"net"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
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
	if req.Tags == nil {
		req.Tags = []string{}
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
		  snmp_version, enc_snmp_creds, poll_interval_s, tags
		)
		VALUES ($1, $2, $3, $4, NULLIF($5,''), NULLIF($6,''), $7::inet,
		        $8, $9, $10, $11)
		RETURNING id
	`
	var insertedID uuid.UUID
	if err := s.pool.QueryRow(r.Context(), q,
		id, siteID, req.Name, req.Vendor, req.Model, req.Serial, req.MgmtIP,
		req.SNMPVersion, sealed, req.PollIntervalSeconds, req.Tags,
	).Scan(&insertedID); err != nil {
		s.log.Warn("create appliance failed", "err", err, "name", req.Name)
		writeErr(w, http.StatusBadRequest, "bad_request", "appliance exists or invalid")
		return
	}

	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "appliance.create", &uid, clientIP(r),
		map[string]any{"appliance_id": insertedID.String(), "name": req.Name, "site_id": siteID.String()})

	writeJSON(w, http.StatusCreated, map[string]any{
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
		"isActive":            true,
	})
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

