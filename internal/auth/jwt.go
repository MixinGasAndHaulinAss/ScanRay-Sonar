package auth

import (
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// TokenKind disambiguates access vs refresh tokens. Mixing them up is a
// classic bug — encoding the kind into the token and verifying it on
// every check prevents an attacker from using a refresh token as an
// access token (or vice versa).
type TokenKind string

const (
	KindAccess  TokenKind = "access"
	KindRefresh TokenKind = "refresh"
	// KindAgent identifies a long-lived JWT held by a sonar-probe.
	// Subject = agent UUID; Role is empty. Issued at enrollment time.
	KindAgent TokenKind = "agent"
	// KindCollector identifies a long-lived JWT held by sonar-collector.
	// Subject = collector UUID.
	KindCollector TokenKind = "collector"
)

// AgentTTL is the lifetime of an agent JWT. Probes refresh it
// proactively (today: rotate at start of every reconnect window) so a
// long TTL is fine — losing one means a single re-enroll, not a
// gradual fleet outage.
const AgentTTL = 30 * 24 * time.Hour

// CollectorTTL matches AgentTTL — collectors reconnect with the same JWT.
const CollectorTTL = 30 * 24 * time.Hour

// Claims is the Sonar-specific JWT body. We deliberately keep this
// small — heavy data (full role list, site memberships) is loaded from
// the database on each request keyed by UserID.
type Claims struct {
	jwt.RegisteredClaims
	UserID uuid.UUID `json:"uid"`
	Kind   TokenKind `json:"knd"`
	Role   string    `json:"rol,omitempty"`
}

// Issuer signs and validates Sonar JWTs.
type Issuer struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewIssuer parses the base64 secret and returns an Issuer.
func NewIssuer(secretB64 string, accessTTL, refreshTTL time.Duration) (*Issuer, error) {
	key, err := base64.StdEncoding.DecodeString(secretB64)
	if err != nil {
		return nil, fmt.Errorf("auth: decode jwt secret: %w", err)
	}
	if len(key) < 32 {
		return nil, errors.New("auth: jwt secret must be >= 32 bytes")
	}
	return &Issuer{secret: key, accessTTL: accessTTL, refreshTTL: refreshTTL}, nil
}

// Issue mints a fresh token of the given kind for the user. role is
// included in access tokens so middleware can enforce coarse-grained
// permissions without a DB hit; finer authorization still loads the
// full record.
func (i *Issuer) Issue(userID uuid.UUID, role string, kind TokenKind) (string, time.Time, error) {
	now := time.Now().UTC()
	ttl := i.accessTTL
	switch kind {
	case KindRefresh:
		ttl = i.refreshTTL
	case KindAgent:
		ttl = AgentTTL
	case KindCollector:
		ttl = CollectorTTL
	default:
	}
	exp := now.Add(ttl)
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "sonar",
			Subject:   userID.String(),
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ID:        uuid.NewString(),
		},
		UserID: userID,
		Kind:   kind,
		Role:   role,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(i.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: sign: %w", err)
	}
	return signed, exp, nil
}

// Parse validates a token's signature, expiration, and kind. Returns
// ErrTokenInvalid on any failure (rather than leaking the specific
// reason via error wrapping) so callers can't accidentally surface
// timing-sensitive details to clients.
func (i *Issuer) Parse(token string, expectedKind TokenKind) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(token, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrTokenInvalid
		}
		return i.secret, nil
	})
	if err != nil || !parsed.Valid {
		return nil, ErrTokenInvalid
	}
	c, ok := parsed.Claims.(*Claims)
	if !ok {
		return nil, ErrTokenInvalid
	}
	if c.Kind != expectedKind {
		return nil, ErrTokenInvalid
	}
	if c.UserID == uuid.Nil {
		return nil, ErrTokenInvalid
	}
	return c, nil
}

// ErrTokenInvalid is the single error returned for any JWT failure.
var ErrTokenInvalid = errors.New("auth: token invalid")
