package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// handleCollectorSiteCredentials returns the site's stored credentials with
// the secret unsealed in plaintext. The collector-auth bearer JWT proves
// authorization; transit is HTTPS. Audited so a misuse is investigable.
func (s *Server) handleCollectorSiteCredentials(w http.ResponseWriter, r *http.Request) {
	cid := collectorIDFromCtx(r.Context())
	var siteID uuid.UUID
	err := s.pool.QueryRow(r.Context(),
		`SELECT site_id FROM collectors WHERE id = $1 AND is_active`, cid).Scan(&siteID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not_found", "collector not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "lookup failed")
		return
	}
	rows, err := s.pool.Query(r.Context(), `
		SELECT id::text, kind, name, enc_secret FROM site_credentials WHERE site_id = $1 ORDER BY kind, name`, siteID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "query failed")
		return
	}
	defer rows.Close()
	type row struct {
		ID     string `json:"id"`
		Kind   string `json:"kind"`
		Name   string `json:"name"`
		Secret string `json:"secret"`
	}
	out := []row{}
	delivered := []string{}
	for rows.Next() {
		var id, kind, name string
		var sealed []byte
		if rows.Scan(&id, &kind, &name, &sealed) != nil {
			continue
		}
		plain, err := s.sealer.Open(sealed, []byte("credential:"+id))
		if err != nil {
			s.log.Warn("collector cred unseal failed", "credId", id, "err", err)
			continue
		}
		out = append(out, row{ID: id, Kind: kind, Name: name, Secret: string(plain)})
		delivered = append(delivered, id)
	}
	if len(delivered) > 0 {
		s.store.Audit(r.Context(), "collector", "site_credential.delivered", &cid, clientIP(r),
			map[string]any{"site_id": siteID.String(), "credential_ids": delivered})
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentials": out})
}

type discoveryDeviceIngest struct {
	IP          string         `json:"ip"`
	MAC         string         `json:"mac,omitempty"`
	Hostname    string         `json:"hostname,omitempty"`
	Vendor      string         `json:"vendor,omitempty"`
	SysObjectID string         `json:"sysObjectId,omitempty"`
	Identified  bool           `json:"identified"`
	Protocols   []string       `json:"protocols"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Criticality string         `json:"criticality,omitempty"`
}

type collectorDiscoveryResultsReq struct {
	Devices []discoveryDeviceIngest `json:"devices"`
}

func (s *Server) handleCollectorDiscoveryResults(w http.ResponseWriter, r *http.Request) {
	cid := collectorIDFromCtx(r.Context())
	var siteID uuid.UUID
	err := s.pool.QueryRow(r.Context(),
		`SELECT site_id FROM collectors WHERE id = $1 AND is_active`, cid).Scan(&siteID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not_found", "collector not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "lookup failed")
		return
	}
	var req collectorDiscoveryResultsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Devices) == 0 {
		writeErr(w, http.StatusBadRequest, "bad_request", "devices array required")
		return
	}
	for _, d := range req.Devices {
		if net.ParseIP(d.IP) == nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid ip in batch")
			return
		}
		protos := d.Protocols
		if len(protos) == 0 {
			protos = []string{"icmp"}
		}
		crit := d.Criticality
		if crit == "" {
			crit = "normal"
		}
		meta := map[string]any{}
		if d.Metadata != nil {
			meta = d.Metadata
		}
		mb, _ := json.Marshal(meta)
		_, err = s.pool.Exec(r.Context(), `
			INSERT INTO discovered_devices (
			  site_id, collector_id, ip, mac, hostname, vendor, sys_object_id,
			  identified, protocols, metadata, last_seen_at, criticality
			) VALUES ($1, $2, $3::inet, NULLIF($4,''), NULLIF($5,''), NULLIF($6,''), NULLIF($7,''),
			           $8, $9, $10::jsonb, NOW(), $11)
			ON CONFLICT (site_id, ip) DO UPDATE SET
			  collector_id = EXCLUDED.collector_id,
			  mac = COALESCE(EXCLUDED.mac, discovered_devices.mac),
			  hostname = COALESCE(EXCLUDED.hostname, discovered_devices.hostname),
			  vendor = COALESCE(EXCLUDED.vendor, discovered_devices.vendor),
			  sys_object_id = COALESCE(EXCLUDED.sys_object_id, discovered_devices.sys_object_id),
			  identified = discovered_devices.identified OR EXCLUDED.identified,
			  protocols = CASE WHEN cardinality(EXCLUDED.protocols) > 0 THEN EXCLUDED.protocols ELSE discovered_devices.protocols END,
			  metadata = discovered_devices.metadata || EXCLUDED.metadata,
			  last_seen_at = NOW(),
			  criticality = EXCLUDED.criticality`,
			siteID, cid, d.IP, d.MAC, d.Hostname, d.Vendor, d.SysObjectID, d.Identified, protos, mb, crit)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "upsert failed")
			return
		}
		if s.nats != nil && s.nats.IsConnected() {
			_ = s.nats.Publish("discovery.device.found", []byte(`{"siteId":"`+siteID.String()+`","ip":"`+d.IP+`"}`))
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
