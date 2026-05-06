package api

import (
	"context"
	"crypto/sha256"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/NCLGISA/ScanRay-Sonar/internal/auth"
)

type ctxKey int

const (
	ctxUserID ctxKey = iota
	ctxUserRole
	ctxCollectorID
	ctxAPIKeyID
	ctxAPIKeySiteScope
)

const apiKeyBearerPrefix = "scr_"

// authRequired validates Bearer JWT access tokens or API keys (`scr_…`).
func (s *Server) authRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := bearerToken(r)
		if raw == "" {
			writeErr(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
			return
		}
		if strings.HasPrefix(raw, apiKeyBearerPrefix) {
			s.authAPIKey(w, r, raw, next)
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

func (s *Server) authAPIKey(w http.ResponseWriter, r *http.Request, raw string, next http.Handler) {
	sum := sha256.Sum256([]byte(raw))
	var keyID, uid uuid.UUID
	err := s.pool.QueryRow(r.Context(), `
		SELECT id, user_id FROM api_keys
		WHERE token_hash = $1 AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > NOW())`, sum[:]).Scan(&keyID, &uid)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized", "invalid api key")
		return
	}
	var role string
	err = s.pool.QueryRow(r.Context(),
		`SELECT role FROM users WHERE id = $1 AND is_active`, uid).Scan(&role)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized", "api key user inactive")
		return
	}
	go func() {
		bg := context.Background()
		_, _ = s.pool.Exec(bg, `UPDATE api_keys SET last_used_at = NOW() WHERE id = $1`, keyID)
		u := uid
		s.store.Audit(bg, "api_key", "api_key.use", &u, clientIP(r),
			map[string]any{"method": r.Method, "path": r.URL.Path})
	}()

	scope := &apiKeySiteScope{Restrict: false}
	srows, qerr := s.pool.Query(r.Context(),
		`SELECT site_id FROM api_key_sites WHERE api_key_id = $1`, keyID)
	if qerr == nil {
		defer srows.Close()
		for srows.Next() {
			var sid uuid.UUID
			if srows.Scan(&sid) != nil {
				continue
			}
			if scope.Sites == nil {
				scope.Sites = map[uuid.UUID]struct{}{}
				scope.Restrict = true
			}
			scope.Sites[sid] = struct{}{}
		}
	}

	ctx := context.WithValue(r.Context(), ctxUserID, uid)
	ctx = context.WithValue(ctx, ctxUserRole, auth.Role(role))
	ctx = context.WithValue(ctx, ctxAPIKeyID, keyID)
	ctx = context.WithValue(ctx, ctxAPIKeySiteScope, scope)
	next.ServeHTTP(w, r.WithContext(ctx))
}

func apiKeyIDFromCtx(ctx context.Context) uuid.UUID {
	if v, ok := ctx.Value(ctxAPIKeyID).(uuid.UUID); ok {
		return v
	}
	return uuid.Nil
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

// collectorAuthRequired validates a Bearer token issued at collector
// enrollment (KindCollector). Downstream handlers read the collector id via
// collectorIDFromCtx.
func (s *Server) collectorAuthRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := bearerToken(r)
		if raw == "" {
			writeErr(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
			return
		}
		claims, err := s.iss.Parse(raw, auth.KindCollector)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized", "invalid collector token")
			return
		}
		ctx := context.WithValue(r.Context(), ctxCollectorID, claims.UserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func collectorIDFromCtx(ctx context.Context) uuid.UUID {
	if v, ok := ctx.Value(ctxCollectorID).(uuid.UUID); ok {
		return v
	}
	return uuid.Nil
}
