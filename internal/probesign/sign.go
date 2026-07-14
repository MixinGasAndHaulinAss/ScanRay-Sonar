// Package probesign implements ed25519 signing/verification for the
// probe self-update channel. The public key is embedded in every
// sonar-probe binary; the private key signs artifacts at image build
// time (or at API startup when SONAR_PROBE_SIGN_PRIVATE_KEY is set).
package probesign

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
)

// DefaultPublicKeyB64 is the production/dev public key for probe
// update verification. Override at build time with
// -ldflags "-X …DefaultPublicKeyB64=…" when rotating keys.
var DefaultPublicKeyB64 = "06lBVEH3DBvnDVrZpd81qN4RxmLQozfwbvNJpkI8bHI="

// DefaultPrivateKeyB64 is the matching private key used when CI/env
// does not supply SONAR_PROBE_SIGN_PRIVATE_KEY. Rotate both together
// for production fleets that already have the public key burned in.
var DefaultPrivateKeyB64 = "/GcdE4MMkjcp9x1QXhikoVSzd39ECn4P4ObRDYQb13E="

// Manifest is the JSON shape returned by GET /api/v1/probe/latest.
type Manifest struct {
	Version string `json:"version"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	SHA256  string `json:"sha256"`
	Sig     string `json:"sig"` // base64.RawURLEncoding of ed25519 signature over sha256 hex
	URL     string `json:"url"` // relative or absolute download path
}

// PublicKey returns the embedded verifier key.
func PublicKey() (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(DefaultPublicKeyB64)
	if err != nil {
		return nil, fmt.Errorf("probesign: decode public key: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("probesign: public key length %d", len(raw))
	}
	return ed25519.PublicKey(raw), nil
}

// PrivateKeyFromEnv loads the signer from SONAR_PROBE_SIGN_PRIVATE_KEY
// (std base64 of 32-byte seed) or falls back to DefaultPrivateKeyB64.
func PrivateKeyFromEnv() (ed25519.PrivateKey, error) {
	b64 := os.Getenv("SONAR_PROBE_SIGN_PRIVATE_KEY")
	if b64 == "" {
		b64 = DefaultPrivateKeyB64
	}
	seed, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("probesign: decode private key: %w", err)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("probesign: private seed length %d (want %d)", len(seed), ed25519.SeedSize)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// HashSHA256Hex returns the lowercase hex SHA-256 of data.
func HashSHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// SignHash signs the lowercase hex SHA-256 string (not the raw bytes)
// so manifests stay readable and match the JSON field.
func SignHash(priv ed25519.PrivateKey, sha256Hex string) string {
	sig := ed25519.Sign(priv, []byte(sha256Hex))
	return base64.RawURLEncoding.EncodeToString(sig)
}

// Verify checks sig (base64url) over sha256Hex using the embedded public key.
func Verify(sha256Hex, sigB64 string) error {
	pub, err := PublicKey()
	if err != nil {
		return err
	}
	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return fmt.Errorf("probesign: decode sig: %w", err)
	}
	if !ed25519.Verify(pub, []byte(sha256Hex), sig) {
		return errors.New("probesign: signature mismatch")
	}
	return nil
}

// CompareCalVer returns true when remote is newer than local.
// CalVer shape: YYYY.M.D.patch — compared as dotted integers.
func CompareCalVer(local, remote string) bool {
	if local == "" || local == "dev" {
		return remote != "" && remote != "dev" && remote != local
	}
	if remote == "" || remote == "dev" {
		return false
	}
	if local == remote {
		return false
	}
	lp := splitVer(local)
	rp := splitVer(remote)
	n := len(lp)
	if len(rp) < n {
		n = len(rp)
	}
	for i := 0; i < n; i++ {
		if rp[i] > lp[i] {
			return true
		}
		if rp[i] < lp[i] {
			return false
		}
	}
	return len(rp) > len(lp)
}

func splitVer(s string) []int {
	out := make([]int, 0, 4)
	cur := 0
	have := false
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '.' {
			if have {
				out = append(out, cur)
			}
			cur = 0
			have = false
			continue
		}
		if s[i] >= '0' && s[i] <= '9' {
			cur = cur*10 + int(s[i]-'0')
			have = true
		}
	}
	return out
}
