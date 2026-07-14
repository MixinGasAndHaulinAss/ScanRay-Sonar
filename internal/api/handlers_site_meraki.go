package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/NCLGISA/ScanRay-Sonar/internal/poller"
)

type merakiSyncSettingsView struct {
	SiteID              string   `json:"siteId"`
	Enabled             bool     `json:"enabled"`
	OrgIDs              []string `json:"orgIds"`
	SyncIntervalSeconds int      `json:"syncIntervalSeconds"`
	APIKeySet           bool     `json:"apiKeySet"`
	LastSyncAt          *string  `json:"lastSyncAt,omitempty"`
	LastSyncError       *string  `json:"lastSyncError,omitempty"`
}

type putMerakiSyncReq struct {
	Enabled             *bool    `json:"enabled,omitempty"`
	OrgIDs              []string `json:"orgIds,omitempty"`
	SyncIntervalSeconds *int     `json:"syncIntervalSeconds,omitempty"`
	APIKey              *string  `json:"apiKey,omitempty"`
}

func (s *Server) handleGetSiteMerakiSync(w http.ResponseWriter, r *http.Request) {
	sid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "site id must be UUID")
		return
	}
	view := merakiSyncSettingsView{
		SiteID:              sid.String(),
		OrgIDs:              []string{},
		SyncIntervalSeconds: 900,
	}
	var orgRaw []byte
	var lastAt *time.Time
	var lastErr *string
	qerr := s.pool.QueryRow(r.Context(), `
		SELECT meraki_sync_enabled, meraki_org_ids, meraki_sync_interval_seconds,
		       meraki_last_sync_at, meraki_last_sync_error
		  FROM site_discovery_settings WHERE site_id = $1`, sid).
		Scan(&view.Enabled, &orgRaw, &view.SyncIntervalSeconds, &lastAt, &lastErr)
	if qerr != nil && !errors.Is(qerr, pgx.ErrNoRows) {
		writeErr(w, http.StatusInternalServerError, "server_error", "query failed")
		return
	}
	if len(orgRaw) > 0 {
		_ = json.Unmarshal(orgRaw, &view.OrgIDs)
	}
	if view.OrgIDs == nil {
		view.OrgIDs = []string{}
	}
	if lastAt != nil {
		s := lastAt.UTC().Format(time.RFC3339)
		view.LastSyncAt = &s
	}
	view.LastSyncError = lastErr

	var credID uuid.UUID
	cerr := s.pool.QueryRow(r.Context(), `
		SELECT id FROM site_credentials
		 WHERE site_id = $1 AND kind = 'meraki'
		 ORDER BY created_at LIMIT 1`, sid).Scan(&credID)
	view.APIKeySet = cerr == nil
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handlePutSiteMerakiSync(w http.ResponseWriter, r *http.Request) {
	sid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "site id must be UUID")
		return
	}
	var req putMerakiSyncReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}

	enabled := false
	if req.Enabled != nil {
		enabled = *req.Enabled
	} else {
		_ = s.pool.QueryRow(r.Context(), `
			SELECT meraki_sync_enabled FROM site_discovery_settings WHERE site_id = $1`, sid).Scan(&enabled)
	}
	interval := 900
	if req.SyncIntervalSeconds != nil {
		interval = *req.SyncIntervalSeconds
	} else {
		_ = s.pool.QueryRow(r.Context(), `
			SELECT meraki_sync_interval_seconds FROM site_discovery_settings WHERE site_id = $1`, sid).Scan(&interval)
	}
	if interval < 300 {
		writeErr(w, http.StatusBadRequest, "bad_request", "syncIntervalSeconds must be >= 300")
		return
	}
	orgIDs := req.OrgIDs
	if orgIDs == nil {
		orgIDs = []string{}
		var orgRaw []byte
		if s.pool.QueryRow(r.Context(), `
			SELECT meraki_org_ids FROM site_discovery_settings WHERE site_id = $1`, sid).Scan(&orgRaw) == nil {
			_ = json.Unmarshal(orgRaw, &orgIDs)
		}
	}
	cleaned := make([]string, 0, len(orgIDs))
	for _, id := range orgIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			cleaned = append(cleaned, id)
		}
	}
	orgJSON, _ := json.Marshal(cleaned)

	_, err = s.pool.Exec(r.Context(), `
		INSERT INTO site_discovery_settings (
		  site_id, meraki_sync_enabled, meraki_org_ids, meraki_sync_interval_seconds
		) VALUES ($1, $2, $3::jsonb, $4)
		ON CONFLICT (site_id) DO UPDATE SET
		  meraki_sync_enabled = EXCLUDED.meraki_sync_enabled,
		  meraki_org_ids = EXCLUDED.meraki_org_ids,
		  meraki_sync_interval_seconds = EXCLUDED.meraki_sync_interval_seconds`,
		sid, enabled, orgJSON, interval)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "settings save failed")
		return
	}

	if req.APIKey != nil && strings.TrimSpace(*req.APIKey) != "" {
		key := strings.TrimSpace(*req.APIKey)
		var credID uuid.UUID
		cerr := s.pool.QueryRow(r.Context(), `
			SELECT id FROM site_credentials
			 WHERE site_id = $1 AND kind = 'meraki'
			 ORDER BY created_at LIMIT 1`, sid).Scan(&credID)
		if errors.Is(cerr, pgx.ErrNoRows) {
			credID = uuid.New()
			sealed, serr := s.sealer.Seal([]byte(key), []byte("credential:"+credID.String()))
			if serr != nil {
				writeErr(w, http.StatusInternalServerError, "server_error", "seal failed")
				return
			}
			_, err = s.pool.Exec(r.Context(), `
				INSERT INTO site_credentials (id, site_id, kind, name, enc_secret)
				VALUES ($1, $2, 'meraki', 'Dashboard API', $3)`, credID, sid, sealed)
			if err != nil {
				writeErr(w, http.StatusBadRequest, "bad_request", "credential insert failed")
				return
			}
		} else if cerr != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "credential lookup failed")
			return
		} else {
			sealed, serr := s.sealer.Seal([]byte(key), []byte("credential:"+credID.String()))
			if serr != nil {
				writeErr(w, http.StatusInternalServerError, "server_error", "seal failed")
				return
			}
			_, err = s.pool.Exec(r.Context(), `
				UPDATE site_credentials SET enc_secret = $1 WHERE id = $2 AND site_id = $3`,
				sealed, credID, sid)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "server_error", "credential update failed")
				return
			}
		}
	}

	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "meraki.settings.update", &uid, clientIP(r),
		map[string]any{"site_id": sid.String(), "enabled": enabled})
	s.handleGetSiteMerakiSync(w, r)
}

func (s *Server) handleSyncSiteMerakiNow(w http.ResponseWriter, r *http.Request) {
	sid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "site id must be UUID")
		return
	}
	var credID uuid.UUID
	var sealed []byte
	err = s.pool.QueryRow(r.Context(), `
		SELECT id, enc_secret FROM site_credentials
		 WHERE site_id = $1 AND kind = 'meraki'
		 ORDER BY created_at LIMIT 1`, sid).Scan(&credID, &sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusBadRequest, "bad_request", "save a Meraki Dashboard API key first")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "credential lookup failed")
		return
	}
	plain, err := s.sealer.Open(sealed, []byte("credential:"+credID.String()))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "unseal failed")
		return
	}
	apiKey := strings.TrimSpace(string(plain))
	if strings.HasPrefix(apiKey, "{") {
		var m map[string]string
		if json.Unmarshal(plain, &m) == nil {
			if k := strings.TrimSpace(m["apiKey"]); k != "" {
				apiKey = k
			} else if k := strings.TrimSpace(m["api_key"]); k != "" {
				apiKey = k
			}
		}
	}
	if apiKey == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "empty Meraki API key")
		return
	}

	var orgRaw []byte
	_ = s.pool.QueryRow(r.Context(), `
		SELECT meraki_org_ids FROM site_discovery_settings WHERE site_id = $1`, sid).Scan(&orgRaw)
	var orgIDs []string
	_ = json.Unmarshal(orgRaw, &orgIDs)

	res, syncErr := poller.SyncSiteMeraki(r.Context(), s.pool, apiKey, sid, orgIDs)
	poller.RecordMerakiSyncStatus(r.Context(), s.pool, sid, syncErr)
	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "meraki.sync", &uid, clientIP(r),
		map[string]any{"site_id": sid.String(), "upserted": res.Upserted, "ok": syncErr == nil})
	if syncErr != nil {
		writeErr(w, http.StatusBadGateway, "meraki_error", syncErr.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"upserted": res.Upserted,
		"orgs":     res.Orgs,
		"devices":  res.Devices,
	})
}
