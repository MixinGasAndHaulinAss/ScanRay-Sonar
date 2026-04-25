package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/NCLGISA/ScanRay-Sonar/internal/auth"
)

type ctxKey int

const (
	ctxUserID ctxKey = iota
	ctxUserRole
)

// authRequired pulls a Bearer access token, validates it, and stows the
// user id + role on the request context for downstream handlers.
func (s *Server) authRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := bearerToken(r)
		if raw == "" {
			writeErr(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
			return
		}
		claims, err := s.iss.Parse(raw, auth.KindAccess)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized", "invalid token")
			return
		}
		ctx := context.WithValue(r.Context(), ctxUserID, claims.UserID)
		ctx = context.WithValue(ctx, ctxUserRole, auth.Role(claims.Role))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireRole is a chi middleware factory that enforces a minimum role.
// Must be placed *after* authRequired in the middleware chain.
func requireRole(min auth.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, _ := r.Context().Value(ctxUserRole).(auth.Role)
			if !role.AtLeast(min) {
				writeErr(w, http.StatusForbidden, "forbidden", "insufficient role")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	const p = "Bearer "
	if !strings.HasPrefix(h, p) {
		return ""
	}
	return strings.TrimSpace(h[len(p):])
}

func userIDFromCtx(ctx context.Context) uuid.UUID {
	if v, ok := ctx.Value(ctxUserID).(uuid.UUID); ok {
		return v
	}
	return uuid.Nil
}
