package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OIDCConfig holds OpenID Connect provider settings.
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

// Enabled reports whether OIDC login is configured.
func (c OIDCConfig) Enabled() bool {
	return c.Issuer != "" && c.ClientID != "" && c.RedirectURL != ""
}

// OIDCProvider performs authorization-code login stubs.
type OIDCProvider struct {
	cfg OIDCConfig
	http *http.Client
}

// NewOIDCProvider returns a provider when configured.
func NewOIDCProvider(cfg OIDCConfig) *OIDCProvider {
	return &OIDCProvider{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}}
}

// Enabled reports whether OIDC login is configured.
func (p *OIDCProvider) Enabled() bool {
	return p != nil && p.cfg.Enabled()
}

type oidcDiscovery struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
}

// LoginURL builds the authorize redirect URL and state nonce.
func (p *OIDCProvider) LoginURL() (redirect string, state string, err error) {
	if !p.cfg.Enabled() {
		return "", "", errors.New("oidc: not configured")
	}
	disc, err := p.discover(context.Background())
	if err != nil {
		return "", "", err
	}
	state, err = randomState()
	if err != nil {
		return "", "", err
	}
	q := url.Values{}
	q.Set("client_id", p.cfg.ClientID)
	q.Set("response_type", "code")
	q.Set("scope", "openid email profile")
	q.Set("redirect_uri", p.cfg.RedirectURL)
	q.Set("state", state)
	return disc.AuthorizationEndpoint + "?" + q.Encode(), state, nil
}

// ExchangeCode trades an authorization code for tokens (stub: returns raw token JSON).
func (p *OIDCProvider) ExchangeCode(ctx context.Context, code string) (map[string]any, error) {
	if !p.cfg.Enabled() {
		return nil, errors.New("oidc: not configured")
	}
	disc, err := p.discover(ctx)
	if err != nil {
		return nil, err
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", p.cfg.RedirectURL)
	form.Set("client_id", p.cfg.ClientID)
	form.Set("client_secret", p.cfg.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, disc.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("oidc token: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *OIDCProvider) discover(ctx context.Context) (*oidcDiscovery, error) {
	wellKnown := strings.TrimRight(p.cfg.Issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnown, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc discovery: %s", resp.Status)
	}
	var disc oidcDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&disc); err != nil {
		return nil, err
	}
	if disc.AuthorizationEndpoint == "" || disc.TokenEndpoint == "" {
		return nil, errors.New("oidc discovery: missing endpoints")
	}
	return &disc, nil
}

func randomState() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
