// Reports HTTP surface: list available templates, generate one
// on-demand, list previously-generated reports, download a single
// report's body.
//
// Markdown is rendered client-side (react-markdown) so the API
// returns raw template output as text/markdown. PDF export is the
// browser's job — Sonar deliberately doesn't ship a PDF renderer.
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

	"github.com/NCLGISA/ScanRay-Sonar/internal/reports"
)

func (s *Server) handleListReportTemplates(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `
		SELECT slug, title, COALESCE(vendor_scope,''), COALESCE(description,'')
		  FROM report_templates ORDER BY title`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "query failed")
		return
	}
	defer rows.Close()
	type row struct {
		Slug        string `json:"slug"`
		Title       string `json:"title"`
		VendorScope string `json:"vendorScope"`
		Description string `json:"description"`
	}
	out := []row{}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.Slug, &r.Title, &r.VendorScope, &r.Description); err == nil {
			out = append(out, r)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": out})
}

type generateReportReq struct {
	TemplateSlug string `json:"templateSlug"`
	SiteID       string `json:"siteId"`
}

// handleGenerateReport renders the requested template against the
// requested site, persists the result in `reports`, and returns the
// new report's ID. Auth: site_admin (templates can read every
// appliance + alarm in the site).
func (s *Server) handleGenerateReport(w http.ResponseWriter, r *http.Request) {
	var req generateReportReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		req.TemplateSlug == "" || req.SiteID == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "templateSlug and siteId required")
		return
	}
	siteID, err := uuid.Parse(req.SiteID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "siteId must be UUID")
		return
	}
	uid := userIDFromCtx(r.Context())

	g, err := reports.Generate(r.Context(), s.pool, req.TemplateSlug, siteID, uid.String())
	if err != nil {
		s.log.Warn("report generate failed", "err", err, "slug", req.TemplateSlug, "site", siteID)
		writeErr(w, http.StatusBadRequest, "bad_request", "generate failed: "+err.Error())
		return
	}
	metaJSON, _ := json.Marshal(g.Metadata)
	var id int64
	err = s.pool.QueryRow(r.Context(), `
		INSERT INTO reports (template_slug, site_id, generated_by, format, content, size_bytes, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
		RETURNING id`,
		req.TemplateSlug, siteID, uid.String(), g.Format, g.Content, len(g.Content), string(metaJSON)).
		Scan(&id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "save failed")
		return
	}
	s.store.Audit(r.Context(), "user", "report.generate", &uid, clientIP(r),
		map[string]any{"report_id": id, "template": req.TemplateSlug, "site_id": siteID.String()})
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) handleListReports(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	templateSlug := q.Get("templateSlug")
	siteIDStr := q.Get("siteId")
	limit := 100
	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 && l <= 500 {
		limit = l
	}

	args := []any{}
	wheres := []string{}
	if templateSlug != "" {
		args = append(args, templateSlug)
		wheres = append(wheres, "template_slug = $"+strconv.Itoa(len(args)))
	}
	if siteIDStr != "" {
		sid, err := uuid.Parse(siteIDStr)
		if err == nil {
			args = append(args, sid)
			wheres = append(wheres, "site_id = $"+strconv.Itoa(len(args)))
		}
	}
	where := ""
	if len(wheres) > 0 {
		where = "WHERE " + joinStrings(wheres, " AND ")
	}
	args = append(args, limit)
	rows, err := s.pool.Query(r.Context(),
		"SELECT id, template_slug, COALESCE(site_id::text,''), generated_at, generated_by, format, size_bytes, metadata::text "+
			"FROM reports "+where+
			" ORDER BY generated_at DESC LIMIT $"+strconv.Itoa(len(args)),
		args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "query failed")
		return
	}
	defer rows.Close()

	type row struct {
		ID           int64           `json:"id"`
		TemplateSlug string          `json:"templateSlug"`
		SiteID       string          `json:"siteId,omitempty"`
		GeneratedAt  time.Time       `json:"generatedAt"`
		GeneratedBy  string          `json:"generatedBy"`
		Format       string          `json:"format"`
		SizeBytes    int             `json:"sizeBytes"`
		Metadata     json.RawMessage `json:"metadata,omitempty"`
	}
	out := []row{}
	for rows.Next() {
		var x row
		var meta string
		if err := rows.Scan(&x.ID, &x.TemplateSlug, &x.SiteID, &x.GeneratedAt,
			&x.GeneratedBy, &x.Format, &x.SizeBytes, &meta); err != nil {
			continue
		}
		if meta != "" {
			x.Metadata = json.RawMessage(meta)
		}
		out = append(out, x)
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": out})
}

func (s *Server) handleGetReport(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be int")
		return
	}
	var (
		slug, by, format, content string
		siteID                    string
		generatedAt               time.Time
		size                      int
		metaText                  string
	)
	err = s.pool.QueryRow(r.Context(), `
		SELECT template_slug, COALESCE(site_id::text,''), generated_at, generated_by,
		       format, content, size_bytes, COALESCE(metadata::text,'{}')
		  FROM reports WHERE id = $1`, id).
		Scan(&slug, &siteID, &generatedAt, &by, &format, &content, &size, &metaText)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not_found", "report not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "load failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":            id,
		"templateSlug":  slug,
		"siteId":        siteID,
		"generatedAt":   generatedAt,
		"generatedBy":   by,
		"format":        format,
		"sizeBytes":     size,
		"content":       content,
		"metadata":      json.RawMessage(metaText),
	})
}

func (s *Server) handleDownloadReport(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be int")
		return
	}
	var format, content, slug string
	var generatedAt time.Time
	err = s.pool.QueryRow(r.Context(), `
		SELECT format, content, template_slug, generated_at FROM reports WHERE id = $1`, id).
		Scan(&format, &content, &slug, &generatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not_found", "report not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "load failed")
		return
	}
	mime := "text/markdown; charset=utf-8"
	ext := "md"
	switch format {
	case "html":
		mime = "text/html; charset=utf-8"
		ext = "html"
	case "text":
		mime = "text/plain; charset=utf-8"
		ext = "txt"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition",
		`attachment; filename="`+slug+`-`+generatedAt.Format("20060102-150405")+`.`+ext+`"`)
	_, _ = w.Write([]byte(content))
}

// joinStrings is a tiny stdlib-only join used by handleListReports
// to assemble a WHERE clause without importing strings just for it.
func joinStrings(parts []string, sep string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	n := len(sep) * (len(parts) - 1)
	for _, p := range parts {
		n += len(p)
	}
	b := make([]byte, 0, n)
	for i, p := range parts {
		if i > 0 {
			b = append(b, sep...)
		}
		b = append(b, p...)
	}
	return string(b)
}
