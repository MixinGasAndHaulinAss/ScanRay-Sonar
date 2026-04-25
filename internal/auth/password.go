// Package auth implements Sonar's authentication primitives:
//
//   - Argon2id password hashing with PHC-string output (this file)
//   - HS256 JWT issuance + verification (jwt.go)
//   - TOTP enrollment + verification (totp.go)
//   - RBAC role + permission helpers (rbac.go)
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Conservative Argon2id parameters tuned for ~50ms on a modest CPU.
// They live here so the cost can be raised over time without breaking
// existing hashes — every hash carries its parameters in the PHC string.
var argonParams = struct {
	Time, Memory uint32
	Threads      uint8
	KeyLen       uint32
	SaltLen      uint32
}{
	Time:    3,
	Memory:  64 * 1024,
	Threads: max8(uint8(runtime.NumCPU()), 1),
	KeyLen:  32,
	SaltLen: 16,
}

var (
	ErrInvalidHash         = errors.New("auth: invalid password hash")
	ErrIncompatibleVersion = errors.New("auth: incompatible argon2 version")
	ErrPasswordMismatch    = errors.New("auth: password mismatch")
)

// HashPassword returns a PHC-formatted argon2id string suitable for
// storing in a TEXT column. Example output:
//
//	$argon2id$v=19$m=65536,t=3,p=4$<saltB64>$<hashB64>
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonParams.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: read salt: %w", err)
	}
	hash := argon2.IDKey(
		[]byte(password),
		salt,
		argonParams.Time,
		argonParams.Memory,
		argonParams.Threads,
		argonParams.KeyLen,
	)
	enc := base64.RawStdEncoding.EncodeToString
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonParams.Memory,
		argonParams.Time,
		argonParams.Threads,
		enc(salt),
		enc(hash),
	), nil
}

// VerifyPassword constant-time compares a provided password against a
// stored PHC string. Returns nil on match, ErrPasswordMismatch on a
// well-formed but wrong password, and a parse error otherwise.
func VerifyPassword(password, encoded string) error {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return ErrInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return ErrInvalidHash
	}
	if version != argon2.Version {
		return ErrIncompatibleVersion
	}
	var memory uint32
	var time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return ErrInvalidHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return ErrInvalidHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return ErrInvalidHash
	}
	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrPasswordMismatch
	}
	return nil
}

func max8(a, b uint8) uint8 {
	if a > b {
		return a
	}
	return b
}
