package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/NCLGISA/ScanRay-Sonar/internal/auth"
	"github.com/NCLGISA/ScanRay-Sonar/internal/db"
)

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	TOTP     string `json:"totp"`
}

type loginResp struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
	MFARequired  bool      `json:"mfaRequired,omitempty"`
	User         userView  `json:"user"`
}

type userView struct {
	ID           string     `json:"id"`
	Email        string     `json:"email"`
	DisplayName  string     `json:"displayName"`
	Role         string     `json:"role"`
	TOTPEnrolled bool       `json:"totpEnrolled"`
	IsActive     bool       `json:"isActive"`
	LastLoginAt  *time.Time `json:"lastLoginAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
}

func toUserView(u *db.User) userView {
	return userView{
		ID:           u.ID.String(),
		Email:        u.Email,
		DisplayName:  u.DisplayName,
		Role:         u.Role,
		TOTPEnrolled: u.TOTPEnrolled,
		IsActive:     u.IsActive,
		LastLoginAt:  u.LastLoginAt,
		CreatedAt:    u.CreatedAt,
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	user, err := s.store.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, db.ErrNoRows) {
			s.store.Audit(r.Context(), "system", "user.login.deny", nil, clientIP(r),
				map[string]any{"email": req.Email, "reason": "unknown_user"})
			writeErr(w, http.StatusUnauthorized, "unauthorized", "invalid credentials")
			return
		}
		writeErr(w, http.StatusInternalServerError, "server_error", "lookup failed")
		return
	}
	if !user.IsActive {
		writeErr(w, http.StatusUnauthorized, "unauthorized", "account disabled")
		return
	}
	if err := auth.VerifyPassword(req.Password, user.PasswordHash); err != nil {
		s.store.Audit(r.Context(), "user", "user.login.deny", &user.ID, clientIP(r),
			map[string]any{"reason": "bad_password"})
		writeErr(w, http.StatusUnauthorized, "unauthorized", "invalid credentials")
		return
	}

	if user.TOTPEnrolled && req.TOTP == "" {
		writeJSON(w, http.StatusOK, loginResp{MFARequired: true, User: toUserView(user)})
		return
	}

	access, exp, err := s.iss.Issue(user.ID, user.Role, auth.KindAccess)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "token issue failed")
		return
	}
	refresh, _, err := s.iss.Issue(user.ID, user.Role, auth.KindRefresh)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "token issue failed")
		return
	}
	_ = s.store.TouchUserLogin(r.Context(), user.ID)
	s.store.Audit(r.Context(), "user", "user.login.ok", &user.ID, clientIP(r), nil)

	writeJSON(w, http.StatusOK, loginResp{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresAt:    exp,
		User:         toUserView(user),
	})
}

type refreshReq struct {
	RefreshToken string `json:"refreshToken"`
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req refreshReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	claims, err := s.iss.Parse(req.RefreshToken, auth.KindRefresh)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized", "invalid refresh token")
		return
	}
	access, exp, err := s.iss.Issue(claims.UserID, claims.Role, auth.KindAccess)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "token issue failed")
		return
	}
	writeJSON(w, http.StatusOK, loginResp{
		AccessToken:  access,
		RefreshToken: req.RefreshToken, // reuse until expiry
		ExpiresAt:    exp,
	})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r.Context())
	if uid == uuid.Nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized", "missing user")
		return
	}

	const q = `SELECT id, email, display_name, password_hash, role,
	                  totp_enrolled, is_active, last_login_at, created_at
	           FROM users WHERE id = $1`
	u := &db.User{}
	if err := s.pool.QueryRow(r.Context(), q, uid).Scan(
		&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.Role,
		&u.TOTPEnrolled, &u.IsActive, &u.LastLoginAt, &u.CreatedAt,
	); err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized", "user not found")
		return
	}
	writeJSON(w, http.StatusOK, toUserView(u))
}

func clientIP(r *http.Request) string {
	if xf := r.Header.Get("CF-Connecting-IP"); xf != "" {
		return xf
	}
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		// Take the first IP in the list.
		for i := 0; i < len(xf); i++ {
			if xf[i] == ',' {
				return xf[:i]
			}
		}
		return xf
	}
	return r.RemoteAddr
}
