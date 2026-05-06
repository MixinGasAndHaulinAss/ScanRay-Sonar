package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Server) handleGetSiteDiscoverySettings(w http.ResponseWriter, r *http.Request) {
	sid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "site id must be UUID")
		return
	}
	var subnets []byte
	var scanInt, spp, dod, udd, cbInt, icmp int
	var cliFeat, filt []byte
	qerr := s.pool.QueryRow(r.Context(), `
		SELECT subnets, scan_interval_seconds, subnets_per_period,
		       device_offline_delete_days, unidentified_delete_days,
		       config_backup_interval_seconds, icmp_timeout_ms,
		       cli_features, filter_rules
		  FROM site_discovery_settings WHERE site_id = $1`, sid).
		Scan(&subnets, &scanInt, &spp, &dod, &udd, &cbInt, &icmp, &cliFeat, &filt)
	if errors.Is(qerr, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{
			"siteId":                       sid.String(),
			"subnets":                      json.RawMessage(`[]`),
			"scanIntervalSeconds":          3600,
			"subnetsPerPeriod":             4,
			"deviceOfflineDeleteDays":      30,
			"unidentifiedDeleteDays":       7,
			"configBackupIntervalSeconds": 86400,
			"icmpTimeoutMs":                2000,
			"cliFeatures":                  map[string]any{},
			"filterRules":                  json.RawMessage(`{"include":[],"exclude":[]}`),
		})
		return
	}
	if qerr != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"siteId":                      sid.String(),
		"subnets":                     json.RawMessage(subnets),
		"scanIntervalSeconds":         scanInt,
		"subnetsPerPeriod":            spp,
		"deviceOfflineDeleteDays":     dod,
		"unidentifiedDeleteDays":      udd,
		"configBackupIntervalSeconds": cbInt,
		"icmpTimeoutMs":               icmp,
		"cliFeatures":                 json.RawMessage(cliFeat),
		"filterRules":                 json.RawMessage(filt),
	})
}

type putDiscoverySettingsReq struct {
	Subnets                      json.RawMessage `json:"subnets"`
	ScanIntervalSeconds          *int            `json:"scanIntervalSeconds,omitempty"`
	SubnetsPerPeriod             *int            `json:"subnetsPerPeriod,omitempty"`
	DeviceOfflineDeleteDays      *int            `json:"deviceOfflineDeleteDays,omitempty"`
	UnidentifiedDeleteDays       *int            `json:"unidentifiedDeleteDays,omitempty"`
	ConfigBackupIntervalSeconds *int            `json:"configBackupIntervalSeconds,omitempty"`
	IcmpTimeoutMs                *int            `json:"icmpTimeoutMs,omitempty"`
	CliFeatures                  json.RawMessage `json:"cliFeatures,omitempty"`
	FilterRules                  json.RawMessage `json:"filterRules,omitempty"`
}

func (s *Server) handlePutSiteDiscoverySettings(w http.ResponseWriter, r *http.Request) {
	sid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "site id must be UUID")
		return
	}
	var req putDiscoverySettingsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if len(req.Subnets) == 0 {
		req.Subnets = json.RawMessage(`[]`)
	}
	scanInt := 3600
	if req.ScanIntervalSeconds != nil {
		scanInt = *req.ScanIntervalSeconds
	}
	spp := 4
	if req.SubnetsPerPeriod != nil {
		spp = *req.SubnetsPerPeriod
	}
	dod := 30
	if req.DeviceOfflineDeleteDays != nil {
		dod = *req.DeviceOfflineDeleteDays
	}
	udd := 7
	if req.UnidentifiedDeleteDays != nil {
		udd = *req.UnidentifiedDeleteDays
	}
	cbInt := 86400
	if req.ConfigBackupIntervalSeconds != nil {
		cbInt = *req.ConfigBackupIntervalSeconds
	}
	icmp := 2000
	if req.IcmpTimeoutMs != nil {
		icmp = *req.IcmpTimeoutMs
	}
	cliFeat := req.CliFeatures
	if len(cliFeat) == 0 {
		cliFeat = json.RawMessage(`{}`)
	}
	filt := req.FilterRules
	if len(filt) == 0 {
		filt = json.RawMessage(`{"include":[],"exclude":[]}`)
	}
	if scanInt < 60 || spp < 1 || dod < 1 || udd < 1 || cbInt < 300 || icmp < 1 {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid numeric bounds")
		return
	}
	_, err = s.pool.Exec(r.Context(), `
		INSERT INTO site_discovery_settings (
		  site_id, subnets, scan_interval_seconds, subnets_per_period,
		  device_offline_delete_days, unidentified_delete_days,
		  config_backup_interval_seconds, icmp_timeout_ms, cli_features, filter_rules
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (site_id) DO UPDATE SET
		  subnets = EXCLUDED.subnets,
		  scan_interval_seconds = EXCLUDED.scan_interval_seconds,
		  subnets_per_period = EXCLUDED.subnets_per_period,
		  device_offline_delete_days = EXCLUDED.device_offline_delete_days,
		  unidentified_delete_days = EXCLUDED.unidentified_delete_days,
		  config_backup_interval_seconds = EXCLUDED.config_backup_interval_seconds,
		  icmp_timeout_ms = EXCLUDED.icmp_timeout_ms,
		  cli_features = EXCLUDED.cli_features,
		  filter_rules = EXCLUDED.filter_rules,
		  updated_at = NOW()`,
		sid, req.Subnets, scanInt, spp, dod, udd, cbInt, icmp, cliFeat, filt)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "save failed")
		return
	}
	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "discovery.settings.update", &uid, clientIP(r),
		map[string]any{"site_id": sid.String()})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListSiteCredentials(w http.ResponseWriter, r *http.Request) {
	sid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "site id must be UUID")
		return
	}
	rows, err := s.pool.Query(r.Context(), `
		SELECT id::text, kind, name, created_at FROM site_credentials WHERE site_id = $1 ORDER BY kind, name`, sid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "query failed")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, kind, name string
		var created interface{}
		if rows.Scan(&id, &kind, &name, &created) != nil {
			continue
		}
		out = append(out, map[string]any{"id": id, "kind": kind, "name": name, "createdAt": created})
	}
	writeJSON(w, http.StatusOK, out)
}

type createSiteCredReq struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Secret string `json:"secret"`
}

func (s *Server) handleCreateSiteCredential(w http.ResponseWriter, r *http.Request) {
	sid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "site id must be UUID")
		return
	}
	var req createSiteCredReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Kind == "" || req.Name == "" || req.Secret == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "kind, name, secret required")
		return
	}
	id := uuid.New()
	sealed, err := s.sealer.Seal([]byte(req.Secret), []byte("credential:"+id.String()))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "seal failed")
		return
	}
	_, err = s.pool.Exec(r.Context(), `
		INSERT INTO site_credentials (id, site_id, kind, name, enc_secret)
		VALUES ($1, $2, $3, $4, $5)`, id, sid, req.Kind, req.Name, sealed)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "insert failed (duplicate kind/name?)")
		return
	}
	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "site_credential.create", &uid, clientIP(r),
		map[string]any{"site_id": sid.String(), "credential_id": id.String(), "kind": req.Kind})
	writeJSON(w, http.StatusCreated, map[string]any{"id": id.String()})
}

type patchSiteCredReq struct {
	Name   *string `json:"name,omitempty"`
	Secret *string `json:"secret,omitempty"`
}

func (s *Server) handlePatchSiteCredential(w http.ResponseWriter, r *http.Request) {
	sid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "site id must be UUID")
		return
	}
	credID, err := uuid.Parse(chi.URLParam(r, "credId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "credential id must be UUID")
		return
	}
	var req patchSiteCredReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	sets := []string{}
	args := []any{}
	add := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, col+" = $"+itoa(len(args)))
	}
	if req.Name != nil {
		if *req.Name == "" {
			writeErr(w, http.StatusBadRequest, "bad_request", "name cannot be empty")
			return
		}
		add("name", *req.Name)
	}
	if req.Secret != nil && *req.Secret != "" {
		sealed, err := s.sealer.Seal([]byte(*req.Secret), []byte("credential:"+credID.String()))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "seal failed")
			return
		}
		add("enc_secret", sealed)
	}
	if len(sets) == 0 {
		writeErr(w, http.StatusBadRequest, "bad_request", "no fields to update")
		return
	}
	args = append(args, credID, sid)
	q := "UPDATE site_credentials SET " + join(sets, ", ") + " WHERE id = $" + itoa(len(args)-1) +
		" AND site_id = $" + itoa(len(args))
	tag, err := s.pool.Exec(r.Context(), q, args...)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "update failed")
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "not_found", "credential not found")
		return
	}
	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "site_credential.update", &uid, clientIP(r),
		map[string]any{"credential_id": credID.String(), "site_id": sid.String()})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteSiteCredential(w http.ResponseWriter, r *http.Request) {
	sid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "site id must be UUID")
		return
	}
	credID, err := uuid.Parse(chi.URLParam(r, "credId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "credential id must be UUID")
		return
	}
	tag, err := s.pool.Exec(r.Context(), `DELETE FROM site_credentials WHERE id = $1 AND site_id = $2`, credID, sid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "delete failed")
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "not_found", "credential not found")
		return
	}
	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "site_credential.delete", &uid, clientIP(r),
		map[string]any{"credential_id": credID.String(), "site_id": sid.String()})
	w.WriteHeader(http.StatusNoContent)
}
