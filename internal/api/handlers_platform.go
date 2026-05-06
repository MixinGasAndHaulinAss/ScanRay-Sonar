// Package api — settings, audit log, discovery aggregation, site network map alias.
package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Server) handleSiteNetworkMap(w http.ResponseWriter, r *http.Request) {
	sid := chi.URLParam(r, "id")
	if _, err := uuid.Parse(sid); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be a UUID")
		return
	}
	q := r.URL.Query()
	q.Set("siteId", sid)
	r = r.Clone(r.Context())
	r.URL.RawQuery = q.Encode()
	s.handleTopology(w, r)
}

func (s *Server) handleDiscoveryDevices(w http.ResponseWriter, r *http.Request) {
	q := `
		SELECT d.id::text, d.site_id::text, s.name, d.ip::text, d.hostname, d.vendor,
		       d.identified, d.protocols, d.metadata, d.last_seen_at, d.first_seen_at,
		       d.collector_id::text, c.name
		  FROM discovered_devices d
		  JOIN sites s ON s.id = d.site_id
		  LEFT JOIN collectors c ON c.id = d.collector_id
		 WHERE 1=1`
	args := []any{}
	if v := r.URL.Query().Get("siteId"); v != "" {
		if _, err := uuid.Parse(v); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid siteId")
			return
		}
		args = append(args, v)
		q += ` AND d.site_id = $` + strconv.Itoa(len(args))
	}
	if v := r.URL.Query().Get("collectorId"); v != "" {
		if _, err := uuid.Parse(v); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid collectorId")
			return
		}
		args = append(args, v)
		q += ` AND d.collector_id = $` + strconv.Itoa(len(args))
	}
	if v := r.URL.Query().Get("vendor"); v != "" {
		args = append(args, "%"+v+"%")
		q += ` AND d.vendor ILIKE $` + strconv.Itoa(len(args))
	}
	if v := r.URL.Query().Get("identified"); v == "true" || v == "false" {
		args = append(args, v == "true")
		q += ` AND d.identified = $` + strconv.Itoa(len(args))
	}
	if v := r.URL.Query().Get("protocol"); v != "" {
		args = append(args, v)
		q += ` AND $` + strconv.Itoa(len(args)) + ` = ANY(d.protocols)`
	}
	q += ` ORDER BY d.last_seen_at DESC NULLS LAST LIMIT 500`

	rows, err := s.pool.Query(r.Context(), q, args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "query failed")
		return
	}
	defer rows.Close()

	out := []map[string]any{}
	for rows.Next() {
		var (
			id, siteID, siteName, ip, hostname, vendor string
			identified                                 bool
			protocols                                  []string
			meta                                       []byte
			lastSeen, firstSeen                        *time.Time
			collID                                     *string
			collName                                   *string
		)
		if err := rows.Scan(&id, &siteID, &siteName, &ip, &hostname, &vendor,
			&identified, &protocols, &meta, &lastSeen, &firstSeen, &collID, &collName); err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "scan failed")
			return
		}
		m := map[string]any{
			"id": id, "siteId": siteID, "siteName": siteName, "ip": ip,
			"hostname": hostname, "vendor": vendor, "identified": identified,
			"protocols": protocols, "lastSeenAt": lastSeen, "firstSeenAt": firstSeen,
		}
		if collID != nil && *collID != "" {
			m["collectorId"] = *collID
			m["collectorName"] = collName
		}
		if len(meta) > 0 {
			var mj map[string]any
			if json.Unmarshal(meta, &mj) == nil {
				m["metadata"] = mj
			}
		}
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDiscoveryNetworks(w http.ResponseWriter, r *http.Request) {
	q := `
		SELECT s.id::text,
		       s.name,
		       COALESCE(ds.subnets, '[]'::jsonb),
		       COALESCE(ds.scan_interval_seconds, 3600),
		       (SELECT COUNT(*) FROM discovered_devices d WHERE d.site_id = s.id),
		       (SELECT MAX(d.last_seen_at) FROM discovered_devices d WHERE d.site_id = s.id),
		       (SELECT string_agg(c.id::text, ',' ORDER BY c.name)
		          FROM collectors c WHERE c.site_id = s.id AND c.is_active)
		  FROM sites s
		  LEFT JOIN site_discovery_settings ds ON ds.site_id = s.id
		 WHERE 1=1`
	args := []any{}
	if v := r.URL.Query().Get("siteId"); v != "" {
		if _, err := uuid.Parse(v); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid siteId")
			return
		}
		args = append(args, v)
		q += ` AND s.id = $` + strconv.Itoa(len(args))
	}
	if v := r.URL.Query().Get("collectorId"); v != "" {
		if _, err := uuid.Parse(v); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid collectorId")
			return
		}
		args = append(args, v)
		q += ` AND EXISTS (SELECT 1 FROM collectors c WHERE c.site_id = s.id AND c.id = $` + strconv.Itoa(len(args)) + `)`
	}
	q += ` ORDER BY s.name`

	rows, err := s.pool.Query(r.Context(), q, args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "query failed")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var sid, name string
		var subnets []byte
		var scanInt int
		var devCount int
		var lastSeen *time.Time
		var collAgg *string
		if err := rows.Scan(&sid, &name, &subnets, &scanInt, &devCount, &lastSeen, &collAgg); err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "scan failed")
			return
		}
		row := map[string]any{
			"siteId":              sid,
			"siteName":            name,
			"subnets":             json.RawMessage(subnets),
			"scanIntervalSeconds": scanInt,
			"deviceCount":         devCount,
			"lastScanAt":          lastSeen,
		}
		if collAgg != nil && *collAgg != "" {
			row["collectorIds"] = strings.Split(*collAgg, ",")
		} else {
			row["collectorIds"] = []string{}
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAuditLog(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 2000 {
			limit = n
		}
	}
	rows, err := s.pool.Query(r.Context(), `
		SELECT id, occurred_at, actor_kind, actor_id::text, action, target_kind, target_id, ip::text, metadata
		  FROM audit_log
		 ORDER BY occurred_at DESC
		 LIMIT $1`, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "query failed")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var occurred time.Time
		var actorKind, action string
		var actorID, targetKind, targetID, ip *string
		var meta []byte
		if err := rows.Scan(&id, &occurred, &actorKind, &actorID, &action, &targetKind, &targetID, &ip, &meta); err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "scan failed")
			return
		}
		row := map[string]any{
			"id": id, "occurredAt": occurred, "actorKind": actorKind,
			"action": action, "metadata": json.RawMessage(meta),
		}
		if actorID != nil {
			row["actorId"] = *actorID
		}
		if targetKind != nil {
			row["targetKind"] = *targetKind
		}
		if targetID != nil {
			row["targetId"] = *targetID
		}
		if ip != nil {
			row["ip"] = *ip
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetSMTPSettings(w http.ResponseWriter, r *http.Request) {
	var host, user, fromAddr string
	var port int
	var encPass []byte
	var useTLS bool
	err := s.pool.QueryRow(r.Context(), `
		SELECT host, port, "user", enc_password, from_addr, use_tls FROM smtp_settings WHERE id = 1`).
		Scan(&host, &port, &user, &encPass, &fromAddr, &useTLS)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{
			"host": s.cfg.SMTP.Host, "port": s.cfg.SMTP.Port, "user": s.cfg.SMTP.User,
			"fromAddr": s.cfg.SMTP.From, "useTls": s.cfg.SMTP.TLS, "passwordSet": s.cfg.SMTP.Password != "",
		})
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "load failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"host": host, "port": port, "user": user, "fromAddr": fromAddr,
		"useTls": useTLS, "passwordSet": len(encPass) > 0 || s.cfg.SMTP.Password != "",
	})
}

type putSMTPReq struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	FromAddr string `json:"fromAddr"`
	UseTLS   bool   `json:"useTls"`
}

func (s *Server) handlePutSMTPSettings(w http.ResponseWriter, r *http.Request) {
	var req putSMTPReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if req.Port <= 0 {
		req.Port = 587
	}
	var encPass []byte
	var err error
	if req.Password != "" {
		encPass, err = s.sealer.Seal([]byte(req.Password), []byte("smtp:password"))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "seal failed")
			return
		}
	} else {
		_ = s.pool.QueryRow(r.Context(), `SELECT enc_password FROM smtp_settings WHERE id = 1`).Scan(&encPass)
	}
	_, err = s.pool.Exec(r.Context(), `
		INSERT INTO smtp_settings (id, host, port, "user", enc_password, from_addr, use_tls)
		VALUES (1, $1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET
		  host = EXCLUDED.host, port = EXCLUDED.port, "user" = EXCLUDED."user",
		  enc_password = COALESCE(EXCLUDED.enc_password, smtp_settings.enc_password),
		  from_addr = EXCLUDED.from_addr, use_tls = EXCLUDED.use_tls,
		  updated_at = NOW()`,
		req.Host, req.Port, req.User, encPass, req.FromAddr, req.UseTLS)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "save failed")
		return
	}
	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "settings.smtp.update", &uid, clientIP(r), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(),
		`SELECT id, name, url, is_active, created_at FROM webhook_endpoints ORDER BY created_at DESC`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "query failed")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var name, url string
		var active bool
		var created time.Time
		if err := rows.Scan(&id, &name, &url, &active, &created); err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "scan failed")
			return
		}
		out = append(out, map[string]any{
			"id": id.String(), "name": name, "url": url, "isActive": active, "createdAt": created,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

type createWebhookReq struct {
	Name          string `json:"name"`
	URL           string `json:"url"`
	SigningSecret string `json:"signingSecret"`
}

func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	var req createWebhookReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.URL == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "name and url required")
		return
	}
	var enc []byte
	var err error
	if req.SigningSecret != "" {
		enc, err = s.sealer.Seal([]byte(req.SigningSecret), []byte("webhook:secret"))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "seal failed")
			return
		}
	}
	var id uuid.UUID
	err = s.pool.QueryRow(r.Context(), `
		INSERT INTO webhook_endpoints (name, url, enc_signing_secret)
		VALUES ($1, $2, $3) RETURNING id`, req.Name, req.URL, enc).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "create failed")
		return
	}
	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "webhook.create", &uid, clientIP(r), map[string]any{"id": id.String()})
	writeJSON(w, http.StatusCreated, map[string]any{"id": id.String()})
}

func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be a UUID")
		return
	}
	if _, err := s.pool.Exec(r.Context(), `DELETE FROM webhook_endpoints WHERE id = $1`, id); err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "delete failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListAlarmRules(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `
		SELECT id, COALESCE(site_id::text,''), name, severity, expression, enabled, created_at
		  FROM alarm_rules ORDER BY created_at DESC`)
	if err != nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var siteID, name, sev, expr string
		var enabled bool
		var created time.Time
		if err := rows.Scan(&id, &siteID, &name, &sev, &expr, &enabled, &created); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id.String(), "siteId": nullIfEmpty(siteID), "name": name,
			"severity": sev, "expression": expr, "enabled": enabled, "createdAt": created,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Server) handleListAlarms(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `
		SELECT id, COALESCE(rule_id::text,''), COALESCE(site_id::text,''), target_kind, target_id::text,
		       severity, title, opened_at, cleared_at, acked_at,
		       COALESCE(acked_by::text,''), COALESCE(cleared_by::text,''),
		       COALESCE(auto_cleared, FALSE)
		  FROM alarms ORDER BY opened_at DESC LIMIT 500`)
	if err != nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var ruleID, siteID, tgtKind, tgtID, sev, title, ackedBy, clearedBy string
		var opened time.Time
		var cleared, acked *time.Time
		var autoCleared bool
		if err := rows.Scan(&id, &ruleID, &siteID, &tgtKind, &tgtID, &sev, &title, &opened, &cleared, &acked, &ackedBy, &clearedBy, &autoCleared); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "ruleId": nullIfEmpty(ruleID), "siteId": nullIfEmpty(siteID),
			"targetKind": tgtKind, "targetId": tgtID, "severity": sev, "title": title,
			"openedAt":    opened,
			"clearedAt":   cleared,
			"ackedAt":     acked,
			"ackedBy":     nullIfEmpty(ackedBy),
			"clearedBy":   nullIfEmpty(clearedBy),
			"autoCleared": autoCleared,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	siteID := r.URL.Query().Get("siteId")
	q := `SELECT id, site_id::text, title, mime_type, sha256, size_bytes, created_at FROM documents WHERE 1=1`
	args := []any{}
	if siteID != "" {
		sid, err := uuid.Parse(siteID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid siteId")
			return
		}
		if !apiKeyAllowsSite(r.Context(), sid) {
			writeErr(w, http.StatusForbidden, "forbidden", "API key not scoped to this site")
			return
		}
		args = append(args, siteID)
		q += ` AND site_id = $` + strconv.Itoa(len(args))
	} else {
		q = appendAPISiteFilter(q, &args, r.Context(), "site_id")
	}
	q += ` ORDER BY created_at DESC LIMIT 200`
	rows, err := s.pool.Query(r.Context(), q, args...)
	if err != nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var sid, title, mime, sha string
		var sz int64
		var created time.Time
		if err := rows.Scan(&id, &sid, &title, &mime, &sha, &sz, &created); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id.String(), "siteId": sid, "title": title, "mimeType": mime,
			"sha256": sha, "sizeBytes": sz, "createdAt": created,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

type uploadDocReq struct {
	Title      string `json:"title"`
	MimeType   string `json:"mimeType"`
	ContentB64 string `json:"contentBase64"`
}

func (s *Server) handleUploadDocument(w http.ResponseWriter, r *http.Request) {
	siteID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "site id must be UUID")
		return
	}
	if !apiKeyAllowsSite(r.Context(), siteID) {
		writeErr(w, http.StatusForbidden, "forbidden", "API key not scoped to this site")
		return
	}
	var req uploadDocReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if req.Title == "" || req.ContentB64 == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "title and contentBase64 required")
		return
	}
	raw, err := base64.StdEncoding.DecodeString(req.ContentB64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid base64")
		return
	}
	if req.MimeType == "" {
		req.MimeType = "application/octet-stream"
	}
	docID := uuid.New()
	sealed, err := s.sealer.Seal(raw, []byte("document:"+docID.String()))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "seal failed")
		return
	}
	uid := userIDFromCtx(r.Context())
	const shaPlaceholder = "sha256-pending"
	_, err = s.pool.Exec(r.Context(), `
		INSERT INTO documents (id, site_id, title, mime_type, enc_body, sha256, size_bytes, uploaded_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		docID, siteID, req.Title, req.MimeType, sealed, shaPlaceholder, len(raw), nullableUID(uid))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "insert failed")
		return
	}
	_, _ = s.pool.Exec(r.Context(), `
		INSERT INTO document_versions (document_id, enc_body, sha256, size_bytes, uploaded_by)
		VALUES ($1, $2, $3, $4, $5)`,
		docID, sealed, shaPlaceholder, len(raw), nullableUID(uid))
	s.store.Audit(r.Context(), "user", "document.upload", &uid, clientIP(r),
		map[string]any{"document_id": docID.String(), "site_id": siteID.String()})
	writeJSON(w, http.StatusCreated, map[string]any{"id": docID.String()})
}

func nullableUID(u uuid.UUID) any {
	if u == uuid.Nil {
		return nil
	}
	return u
}

func (s *Server) handleDownloadDocument(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var title, mime string
	var sealed []byte
	var docSite uuid.UUID
	err = s.pool.QueryRow(r.Context(), `
		SELECT title, mime_type, enc_body, site_id FROM documents WHERE id = $1`, id).
		Scan(&title, &mime, &sealed, &docSite)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "load failed")
		return
	}
	if !apiKeyAllowsSite(r.Context(), docSite) {
		writeErr(w, http.StatusForbidden, "forbidden", "API key not scoped to this site")
		return
	}
	plain, err := s.sealer.Open(sealed, []byte("document:"+id.String()))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "decrypt failed")
		return
	}
	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "document.download", &uid, clientIP(r),
		map[string]any{"document_id": id.String()})
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(title))
	_, _ = io.Copy(w, bytes.NewReader(plain))
}

func (s *Server) handleQueryDevices(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	siteID := r.URL.Query().Get("siteId")
	ctx := r.Context()
	if siteID != "" {
		sid, err := uuid.Parse(siteID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid siteId")
			return
		}
		if !apiKeyAllowsSite(ctx, sid) {
			writeErr(w, http.StatusForbidden, "forbidden", "API key not scoped to this site")
			return
		}
	}

	arg := []any{}
	q := `SELECT id::text, site_id::text, hostname, 'agent' AS kind, COALESCE(criticality::text,'normal') FROM agents WHERE is_active`
	if siteID != "" {
		arg = append(arg, siteID)
		q += ` AND site_id = $` + strconv.Itoa(len(arg))
	} else {
		q = appendAPISiteFilter(q, &arg, ctx, "site_id")
	}
	rows, err := s.pool.Query(r.Context(), q, arg...)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id, sid, host, kind, crit string
			if rows.Scan(&id, &sid, &host, &kind, &crit) == nil {
				out = append(out, map[string]any{"id": id, "siteId": sid, "hostname": host, "kind": kind, "criticality": crit})
			}
		}
	}
	arg = []any{}
	q2 := `SELECT id::text, site_id::text, name, 'appliance' AS kind, COALESCE(criticality::text,'normal') FROM appliances WHERE is_active`
	if siteID != "" {
		arg = append(arg, siteID)
		q2 += ` AND site_id = $` + strconv.Itoa(len(arg))
	} else {
		q2 = appendAPISiteFilter(q2, &arg, ctx, "site_id")
	}
	rows2, err := s.pool.Query(r.Context(), q2, arg...)
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var id, sid, name, kind, crit string
			if rows2.Scan(&id, &sid, &name, &kind, &crit) == nil {
				out = append(out, map[string]any{"id": id, "siteId": sid, "name": name, "kind": kind, "criticality": crit})
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": out})
}
