package api

import (
	"bytes"
	"context"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/NCLGISA/ScanRay-Sonar/internal/auth"
)

const docsSessionCookie = "sonar_docs_session"

// handleDocsSession mints a short-lived docs JWT and sets it as an HttpOnly
// cookie so subsequent navigations to /docs/* succeed without a Bearer header.
func (s *Server) handleDocsSession(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r.Context())
	role, _ := r.Context().Value(ctxUserRole).(auth.Role)
	tok, exp, err := s.iss.Issue(uid, string(role), auth.KindDocs)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "could not issue docs session")
		return
	}
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name:     docsSessionCookie,
		Value:    tok,
		Path:     "/docs",
		Expires:  exp,
		MaxAge:   int(time.Until(exp).Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

// docsAuthRequired accepts a Bearer access JWT or a valid docs session cookie.
// Browser navigations without credentials redirect to /login; others get 401.
func (s *Server) docsAuthRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if raw := bearerToken(r); raw != "" && !strings.HasPrefix(raw, apiKeyBearerPrefix) {
			claims, err := s.iss.Parse(raw, auth.KindAccess)
			if err == nil {
				ctx := context.WithValue(r.Context(), ctxUserID, claims.UserID)
				ctx = context.WithValue(ctx, ctxUserRole, auth.Role(claims.Role))
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}
		if c, err := r.Cookie(docsSessionCookie); err == nil && c.Value != "" {
			claims, err := s.iss.Parse(c.Value, auth.KindDocs)
			if err == nil {
				ctx := context.WithValue(r.Context(), ctxUserID, claims.UserID)
				ctx = context.WithValue(ctx, ctxUserRole, auth.Role(claims.Role))
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}
		accept := r.Header.Get("Accept")
		if strings.Contains(accept, "text/html") {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		writeErr(w, http.StatusUnauthorized, "unauthorized", "docs session required")
	})
}

// serveDocs serves the embedded MkDocs site under /docs.
func (s *Server) serveDocs(w http.ResponseWriter, r *http.Request) {
	if s.docsFS == nil {
		writeErr(w, http.StatusNotFound, "not_found", "no embedded docs build")
		return
	}

	rel := strings.TrimPrefix(r.URL.Path, "/docs")
	name := strings.TrimPrefix(path.Clean("/"+rel), "/")
	if name == "" || name == "." {
		name = "index.html"
	}

	data, info, err := readAsset(s.docsFS, name)
	if err != nil {
		alt := path.Join(name, "index.html")
		data, info, err = readAsset(s.docsFS, alt)
		if err == nil {
			name = alt
		}
	}
	if err != nil {
		writeErr(w, http.StatusNotFound, "not_found", "docs page not found")
		return
	}

	if strings.Contains(name, "assets/") || strings.HasSuffix(name, ".css") ||
		strings.HasSuffix(name, ".js") || strings.HasSuffix(name, ".woff2") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	}
	http.ServeContent(w, r, name, info.ModTime(), bytes.NewReader(data))
}
