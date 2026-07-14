package api

import (
	"net/http"
	"net/url"
)

func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil || !s.oidc.Enabled() {
		writeErr(w, http.StatusNotImplemented, "not_configured", "OIDC is not configured")
		return
	}
	redirect, state, err := s.oidc.LoginURL()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "oidc login failed")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "sonar_oidc_state",
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
	http.Redirect(w, r, redirect, http.StatusFound)
}

func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil || !s.oidc.Enabled() {
		writeErr(w, http.StatusNotImplemented, "not_configured", "OIDC is not configured")
		return
	}
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		writeErr(w, http.StatusBadRequest, "oidc_error", errParam)
		return
	}
	state := r.URL.Query().Get("state")
	cookie, err := r.Cookie("sonar_oidc_state")
	if err != nil || cookie.Value == "" || cookie.Value != state {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid oidc state")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "missing code")
		return
	}
	tokens, err := s.oidc.ExchangeCode(r.Context(), code)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "oidc_error", "token exchange failed")
		return
	}
	// Stub completion: redirect to SPA login with tokens in fragment for now.
	// Full user provisioning (JIT create/link) lands in a follow-up.
	frag := url.Values{}
	if at, ok := tokens["access_token"].(string); ok {
		frag.Set("oidc_access_token", at)
	}
	http.Redirect(w, r, "/login?oidc=1&"+frag.Encode(), http.StatusFound)
}
