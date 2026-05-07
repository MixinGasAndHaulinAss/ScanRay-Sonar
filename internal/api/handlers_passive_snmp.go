// Passive-SNMP discovery server-side handlers: ingest from collectors,
// list inventory + change feed for the UI.
//
// Server-side merge is deliberately authoritative — the collector
// posts a flat batch of "what I saw on this run", and the API layer
// computes added/retired/changed/reactivated transitions against the
// existing inventory + bumps miss_count for IPs that were absent.
// This keeps the collector dumb (re-running the capture from scratch
// is fine) and lets the operator see the same change feed shape
// regardless of which collector produced it.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type passiveSNMPDeviceWire struct {
	IP          string `json:"ip"`
	Vendor      string `json:"vendor,omitempty"`
	Type        string `json:"type,omitempty"`
	SubType     string `json:"subType,omitempty"`
	SysDescr    string `json:"sysDescr,omitempty"`
	SysObjectID string `json:"sysObjectId,omitempty"`
	SysName     string `json:"sysName,omitempty"`
	SysLocation string `json:"sysLocation,omitempty"`
}

type passiveSNMPIngestReq struct {
	CapturedAt time.Time               `json:"capturedAt"`
	Devices    []passiveSNMPDeviceWire `json:"devices"`
	// RetireAfter overrides the per-site setting for this run. The
	// collector forwards the value it pulled from settings so the
	// merge is consistent even if settings change mid-run.
	RetireAfter int `json:"retireAfter,omitempty"`
}

// handleCollectorPassiveSNMP receives a passive-discovery batch from
// a collector and merges it into passive_snmp_inventory + the change
// feed. Auth: collector bearer token.
func (s *Server) handleCollectorPassiveSNMP(w http.ResponseWriter, r *http.Request) {
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

	var req passiveSNMPIngestReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if req.RetireAfter <= 0 {
		req.RetireAfter = 3
	}
	for _, d := range req.Devices {
		if net.ParseIP(d.IP) == nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid IP in batch")
			return
		}
	}

	if err := mergePassiveSNMPBatch(r.Context(), s.pool, siteID, req); err != nil {
		s.log.Warn("passive snmp merge failed", "err", err, "site", siteID)
		writeErr(w, http.StatusInternalServerError, "server_error", "merge failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// mergePassiveSNMPBatch is the heart of the additive inventory +
// change-feed logic. Runs in a single transaction so a partial
// failure leaves the inventory unchanged.
//
//  1. Insert/update each captured IP. New row → "added" event;
//     existing row with changed sys_descr/vendor/type → "changed";
//     previously retired row → "reactivated".
//  2. For IPs that were active before this run but absent in the
//     batch, increment miss_count. When miss_count reaches the
//     configured retire_after, flip status='retired' + emit a
//     "retired" event.
//  3. After the merge, trim passive_snmp_changes to the most recent
//     500 entries per site.
func mergePassiveSNMPBatch(ctx context.Context, pool *pgxpool.Pool, siteID uuid.UUID, req passiveSNMPIngestReq) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	seen := make(map[string]passiveSNMPDeviceWire, len(req.Devices))
	for _, d := range req.Devices {
		seen[d.IP] = d
	}

	for ip, d := range seen {
		// Read prior row (if any) for change-detection.
		var prevVendor, prevType, prevDescr, prevStatus string
		var prevExists bool
		err := tx.QueryRow(ctx, `
			SELECT COALESCE(vendor,''), COALESCE(type,''), COALESCE(sys_descr,''), status
			  FROM passive_snmp_inventory WHERE site_id = $1 AND ip = $2`,
			siteID, ip).Scan(&prevVendor, &prevType, &prevDescr, &prevStatus)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			prevExists = false
		case err != nil:
			return err
		default:
			prevExists = true
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO passive_snmp_inventory (
			  site_id, ip, vendor, type, sub_type,
			  sys_descr, sys_object_id, sys_name, sys_location,
			  status, first_seen_at, last_seen_at, miss_count
			) VALUES ($1, $2::inet, NULLIF($3,''), NULLIF($4,''), NULLIF($5,''),
			          NULLIF($6,''), NULLIF($7,''), NULLIF($8,''), NULLIF($9,''),
			          'active', NOW(), NOW(), 0)
			ON CONFLICT (site_id, ip) DO UPDATE SET
			  vendor        = COALESCE(NULLIF(EXCLUDED.vendor,''), passive_snmp_inventory.vendor),
			  type          = COALESCE(NULLIF(EXCLUDED.type,''),   passive_snmp_inventory.type),
			  sub_type      = COALESCE(NULLIF(EXCLUDED.sub_type,''), passive_snmp_inventory.sub_type),
			  sys_descr     = COALESCE(NULLIF(EXCLUDED.sys_descr,''), passive_snmp_inventory.sys_descr),
			  sys_object_id = COALESCE(NULLIF(EXCLUDED.sys_object_id,''), passive_snmp_inventory.sys_object_id),
			  sys_name      = COALESCE(NULLIF(EXCLUDED.sys_name,''), passive_snmp_inventory.sys_name),
			  sys_location  = COALESCE(NULLIF(EXCLUDED.sys_location,''), passive_snmp_inventory.sys_location),
			  status        = 'active',
			  last_seen_at  = NOW(),
			  miss_count    = 0`,
			siteID, ip, d.Vendor, d.Type, d.SubType,
			d.SysDescr, d.SysObjectID, d.SysName, d.SysLocation)
		if err != nil {
			return err
		}

		// Emit the appropriate change event.
		newJSON, _ := json.Marshal(d)
		switch {
		case !prevExists:
			_, _ = tx.Exec(ctx, `
				INSERT INTO passive_snmp_changes (site_id, ip, kind, new_json)
				VALUES ($1, $2::inet, 'added', $3::jsonb)`,
				siteID, ip, string(newJSON))
		case prevStatus == "retired":
			_, _ = tx.Exec(ctx, `
				INSERT INTO passive_snmp_changes (site_id, ip, kind, new_json)
				VALUES ($1, $2::inet, 'reactivated', $3::jsonb)`,
				siteID, ip, string(newJSON))
		case (d.Vendor != "" && d.Vendor != prevVendor) ||
			(d.Type != "" && d.Type != prevType) ||
			(d.SysDescr != "" && d.SysDescr != prevDescr):
			oldJSON, _ := json.Marshal(map[string]string{
				"vendor": prevVendor, "type": prevType, "sysDescr": prevDescr,
			})
			_, _ = tx.Exec(ctx, `
				INSERT INTO passive_snmp_changes (site_id, ip, kind, old_json, new_json)
				VALUES ($1, $2::inet, 'changed', $3::jsonb, $4::jsonb)`,
				siteID, ip, string(oldJSON), string(newJSON))
		}
	}

	// Bump miss_count for active IPs we did NOT see this round.
	// Then retire those that crossed the threshold.
	_, err = tx.Exec(ctx, `
		UPDATE passive_snmp_inventory
		   SET miss_count = miss_count + 1
		 WHERE site_id = $1 AND status = 'active'
		   AND NOT (ip = ANY ($2::inet[]))`,
		siteID, ipsArray(seen))
	if err != nil {
		return err
	}

	rows, err := tx.Query(ctx, `
		SELECT host(ip), vendor, type, sys_descr
		  FROM passive_snmp_inventory
		 WHERE site_id = $1 AND status = 'active' AND miss_count >= $2`,
		siteID, req.RetireAfter)
	if err != nil {
		return err
	}
	type retiringRow struct{ ip, vendor, typ, descr string }
	var retiring []retiringRow
	for rows.Next() {
		var r retiringRow
		var v, t, d *string
		if err := rows.Scan(&r.ip, &v, &t, &d); err != nil {
			rows.Close()
			return err
		}
		if v != nil {
			r.vendor = *v
		}
		if t != nil {
			r.typ = *t
		}
		if d != nil {
			r.descr = *d
		}
		retiring = append(retiring, r)
	}
	rows.Close()

	for _, r := range retiring {
		_, err = tx.Exec(ctx, `
			UPDATE passive_snmp_inventory
			   SET status = 'retired'
			 WHERE site_id = $1 AND ip = $2::inet`, siteID, r.ip)
		if err != nil {
			return err
		}
		oldJSON, _ := json.Marshal(map[string]string{
			"vendor": r.vendor, "type": r.typ, "sysDescr": r.descr,
		})
		_, _ = tx.Exec(ctx, `
			INSERT INTO passive_snmp_changes (site_id, ip, kind, old_json)
			VALUES ($1, $2::inet, 'retired', $3::jsonb)`,
			siteID, r.ip, string(oldJSON))
	}

	// Cap the change feed to ~500 most-recent entries per site so
	// a noisy network can't unbounded-grow the table.
	_, _ = tx.Exec(ctx, `
		DELETE FROM passive_snmp_changes
		 WHERE site_id = $1
		   AND id NOT IN (
		     SELECT id FROM passive_snmp_changes
		      WHERE site_id = $1
		      ORDER BY time DESC LIMIT 500
		   )`, siteID)

	return tx.Commit(ctx)
}

// ipsArray returns the keys of seen as a string slice in INET text
// form, suitable for $::inet[] parameter binding.
func ipsArray(seen map[string]passiveSNMPDeviceWire) []string {
	out := make([]string, 0, len(seen))
	for ip := range seen {
		out = append(out, ip)
	}
	return out
}

// ----- Read-side handlers (operator / UI) -----

func (s *Server) handleListPassiveSNMP(w http.ResponseWriter, r *http.Request) {
	siteID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "site id must be UUID")
		return
	}
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "active"
	}
	rows, err := s.pool.Query(r.Context(), `
		SELECT host(ip), COALESCE(vendor,''), COALESCE(type,''),
		       COALESCE(sub_type,''), COALESCE(sys_descr,''),
		       COALESCE(sys_object_id,''), COALESCE(sys_name,''),
		       COALESCE(sys_location,''), status, first_seen_at, last_seen_at, miss_count
		  FROM passive_snmp_inventory
		 WHERE site_id = $1
		   AND ($2 = 'all' OR status = $2)
		 ORDER BY last_seen_at DESC`, siteID, status)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "query failed")
		return
	}
	defer rows.Close()

	type row struct {
		IP          string    `json:"ip"`
		Vendor      string    `json:"vendor"`
		Type        string    `json:"type"`
		SubType     string    `json:"subType"`
		SysDescr    string    `json:"sysDescr"`
		SysObjectID string    `json:"sysObjectId"`
		SysName     string    `json:"sysName"`
		SysLocation string    `json:"sysLocation"`
		Status      string    `json:"status"`
		FirstSeen   time.Time `json:"firstSeenAt"`
		LastSeen    time.Time `json:"lastSeenAt"`
		MissCount   int       `json:"missCount"`
	}
	out := []row{}
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.IP, &x.Vendor, &x.Type, &x.SubType,
			&x.SysDescr, &x.SysObjectID, &x.SysName, &x.SysLocation,
			&x.Status, &x.FirstSeen, &x.LastSeen, &x.MissCount); err != nil {
			continue
		}
		out = append(out, x)
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": out})
}

func (s *Server) handleListPassiveSNMPChanges(w http.ResponseWriter, r *http.Request) {
	siteID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "site id must be UUID")
		return
	}
	rows, err := s.pool.Query(r.Context(), `
		SELECT id, time, host(ip), kind,
		       COALESCE(old_json::text,''), COALESCE(new_json::text,'')
		  FROM passive_snmp_changes
		 WHERE site_id = $1
		 ORDER BY time DESC LIMIT 500`, siteID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "query failed")
		return
	}
	defer rows.Close()

	type row struct {
		ID      int64           `json:"id"`
		Time    time.Time       `json:"time"`
		IP      string          `json:"ip"`
		Kind    string          `json:"kind"`
		OldJSON json.RawMessage `json:"old,omitempty"`
		NewJSON json.RawMessage `json:"new,omitempty"`
	}
	out := []row{}
	for rows.Next() {
		var x row
		var oldS, newS string
		if err := rows.Scan(&x.ID, &x.Time, &x.IP, &x.Kind, &oldS, &newS); err != nil {
			continue
		}
		if oldS != "" {
			x.OldJSON = json.RawMessage(oldS)
		}
		if newS != "" {
			x.NewJSON = json.RawMessage(newS)
		}
		out = append(out, x)
	}
	writeJSON(w, http.StatusOK, map[string]any{"changes": out})
}
