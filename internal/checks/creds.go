package checks

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// ResolvedCred is plaintext vault material injected only for the duration of a run.
// Never log, persist, or publish this struct.
type ResolvedCred struct {
	Kind     string
	Username string
	Password string
	Driver   string // sql: postgres|sqlserver|mysql
	Database string
	SSLMode  string
	UseTLS   bool
}

// ExpectedCredKind maps a check type_id to the site_credentials.kind it must use.
func ExpectedCredKind(typeID string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(typeID)) {
	case "sql_query":
		return "sql", true
	case "smtp":
		return "smtp", true
	case "imap":
		return "imap", true
	case "ldap_bind":
		return "ldap", true
	default:
		return "", false
	}
}

// RequiresCredential reports whether the check type must reference a vault credential.
func RequiresCredential(typeID string) bool {
	_, ok := ExpectedCredKind(typeID)
	return ok
}

// IsCentralOnly reports types that must never run on agent or collector (secrets stay on poller).
func IsCentralOnly(typeID string) bool {
	return RequiresCredential(typeID)
}

// forbiddenParamKeys must never appear in checks.params JSONB.
var forbiddenParamKeys = map[string]struct{}{
	"password": {}, "passwd": {}, "secret": {}, "apikey": {}, "api_key": {},
	"privatekey": {}, "private_key": {}, "community": {}, "token": {},
	"authpass": {}, "privpass": {}, "enc_secret": {}, "rawsecret": {},
}

// RejectSecretParams returns an error if params contain forbidden secret-like keys.
func RejectSecretParams(params map[string]any) error {
	for k := range params {
		norm := strings.ToLower(k)
		norm = strings.ReplaceAll(norm, "_", "")
		norm = strings.ReplaceAll(norm, "-", "")
		if _, bad := forbiddenParamKeys[norm]; bad {
			return fmt.Errorf("params must not contain secret field %q; use credentialId", k)
		}
	}
	return nil
}

// CredentialIDFromParams extracts credentialId from check params.
func CredentialIDFromParams(params map[string]any) (uuid.UUID, bool) {
	if params == nil {
		return uuid.Nil, false
	}
	raw, ok := params["credentialId"]
	if !ok || raw == nil {
		return uuid.Nil, false
	}
	switch t := raw.(type) {
	case string:
		id, err := uuid.Parse(t)
		if err != nil {
			return uuid.Nil, false
		}
		return id, true
	default:
		s := fmt.Sprint(t)
		id, err := uuid.Parse(s)
		if err != nil {
			return uuid.Nil, false
		}
		return id, true
	}
}

// ParseResolvedCred unmarshals a vault secret payload for the given kind.
func ParseResolvedCred(kind string, plaintext []byte) (*ResolvedCred, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	var m map[string]any
	if err := json.Unmarshal(plaintext, &m); err != nil {
		// Allow raw password-only string for simple kinds (unlikely); require JSON.
		return nil, fmt.Errorf("credential secret must be JSON: %w", err)
	}
	c := &ResolvedCred{Kind: kind}
	c.Username = stringField(m, "username")
	c.Password = stringField(m, "password")
	c.Driver = strings.ToLower(stringField(m, "driver"))
	c.Database = stringField(m, "database")
	c.SSLMode = stringField(m, "sslmode")
	if c.SSLMode == "" {
		c.SSLMode = stringField(m, "sslMode")
	}
	if v, ok := m["useTLS"].(bool); ok {
		c.UseTLS = v
	}
	switch kind {
	case "sql":
		if c.Username == "" || c.Password == "" {
			return nil, fmt.Errorf("sql credential requires username and password")
		}
		if c.Driver == "" {
			c.Driver = "postgres"
		}
	case "ldap", "smtp", "imap":
		if c.Username == "" || c.Password == "" {
			return nil, fmt.Errorf("%s credential requires username and password", kind)
		}
	default:
		return nil, fmt.Errorf("unsupported check credential kind %q", kind)
	}
	return c, nil
}

func stringField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}
