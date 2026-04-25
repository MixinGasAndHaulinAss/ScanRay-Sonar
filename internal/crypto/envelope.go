// Package crypto implements envelope encryption for secrets stored in
// Postgres. Every secret value (switch credentials, API keys, agent
// enrollment tokens, etc.) is encrypted with a one-shot AES-256-GCM
// data key, which is itself encrypted ("wrapped") by the long-lived
// master key loaded from SONAR_MASTER_KEY.
//
// Wire format for a sealed value (the bytes stored in Postgres):
//
//	+----+--------+------+-----------+--------+-----------+
//	| v1 | wrapNonce | wrappedDataKey | dataNonce | ciphertext+tag |
//	+----+--------+------+-----------+--------+-----------+
//	  1B    12B            48B            12B           variable
//
// where wrappedDataKey = AES-GCM(masterKey, wrapNonce, dataKey) and is
// always exactly 32 (data key) + 16 (GCM tag) = 48 bytes.
//
// This layout is self-describing: the version byte lets us rotate
// algorithms in the future without breaking older rows. Per-row data
// keys mean compromising one secret never reveals another.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

const (
	versionV1 byte = 0x01

	masterKeyLen = 32 // AES-256
	dataKeyLen   = 32 // AES-256
	gcmNonceLen  = 12
	gcmTagLen    = 16
	wrappedLen   = dataKeyLen + gcmTagLen // 48
)

var (
	ErrInvalidMasterKey = errors.New("crypto: master key must decode to exactly 32 bytes")
	ErrShortCiphertext  = errors.New("crypto: ciphertext too short")
	ErrUnknownVersion   = errors.New("crypto: unknown sealed value version")
)

// Sealer encrypts and decrypts secret values using a master key.
// A single Sealer is safe for concurrent use by multiple goroutines.
type Sealer struct {
	masterAEAD cipher.AEAD
}

// NewSealer parses a base64-encoded 32-byte master key and returns a
// Sealer ready for use. Pass cfg.MasterKeyB64.
func NewSealer(masterKeyB64 string) (*Sealer, error) {
	key, err := base64.StdEncoding.DecodeString(masterKeyB64)
	if err != nil {
		return nil, fmt.Errorf("crypto: decode master key: %w", err)
	}
	if len(key) != masterKeyLen {
		return nil, ErrInvalidMasterKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: new aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: new gcm: %w", err)
	}
	return &Sealer{masterAEAD: aead}, nil
}

// Seal encrypts plaintext and returns a self-describing sealed blob safe
// to store in a `bytea` column. associatedData (optional) is bound to the
// ciphertext via AEAD; pass the same value to Open or decryption fails.
// A common choice is the row's stable ID, so a sealed value can't be
// silently moved between rows.
func (s *Sealer) Seal(plaintext, associatedData []byte) ([]byte, error) {
	dataKey := make([]byte, dataKeyLen)
	if _, err := io.ReadFull(rand.Reader, dataKey); err != nil {
		return nil, fmt.Errorf("crypto: read data key: %w", err)
	}

	wrapNonce := make([]byte, gcmNonceLen)
	if _, err := io.ReadFull(rand.Reader, wrapNonce); err != nil {
		return nil, fmt.Errorf("crypto: read wrap nonce: %w", err)
	}
	wrapped := s.masterAEAD.Seal(nil, wrapNonce, dataKey, []byte("sonar:dk:v1"))
	if len(wrapped) != wrappedLen {
		return nil, fmt.Errorf("crypto: unexpected wrapped length %d", len(wrapped))
	}

	dataBlock, err := aes.NewCipher(dataKey)
	if err != nil {
		return nil, fmt.Errorf("crypto: data cipher: %w", err)
	}
	dataAEAD, err := cipher.NewGCM(dataBlock)
	if err != nil {
		return nil, fmt.Errorf("crypto: data gcm: %w", err)
	}
	dataNonce := make([]byte, gcmNonceLen)
	if _, err := io.ReadFull(rand.Reader, dataNonce); err != nil {
		return nil, fmt.Errorf("crypto: read data nonce: %w", err)
	}
	ciphertext := dataAEAD.Seal(nil, dataNonce, plaintext, associatedData)

	out := make([]byte, 0, 1+gcmNonceLen+wrappedLen+gcmNonceLen+len(ciphertext))
	out = append(out, versionV1)
	out = append(out, wrapNonce...)
	out = append(out, wrapped...)
	out = append(out, dataNonce...)
	out = append(out, ciphertext...)
	return out, nil
}

// Open decrypts a blob produced by Seal. associatedData must match the
// value that was passed to Seal or this returns an authentication error.
func (s *Sealer) Open(sealed, associatedData []byte) ([]byte, error) {
	if len(sealed) < 1+gcmNonceLen+wrappedLen+gcmNonceLen+gcmTagLen {
		return nil, ErrShortCiphertext
	}
	if sealed[0] != versionV1 {
		return nil, ErrUnknownVersion
	}
	off := 1
	wrapNonce := sealed[off : off+gcmNonceLen]
	off += gcmNonceLen
	wrapped := sealed[off : off+wrappedLen]
	off += wrappedLen
	dataNonce := sealed[off : off+gcmNonceLen]
	off += gcmNonceLen
	ciphertext := sealed[off:]

	dataKey, err := s.masterAEAD.Open(nil, wrapNonce, wrapped, []byte("sonar:dk:v1"))
	if err != nil {
		return nil, fmt.Errorf("crypto: unwrap data key: %w", err)
	}
	dataBlock, err := aes.NewCipher(dataKey)
	if err != nil {
		return nil, fmt.Errorf("crypto: data cipher: %w", err)
	}
	dataAEAD, err := cipher.NewGCM(dataBlock)
	if err != nil {
		return nil, fmt.Errorf("crypto: data gcm: %w", err)
	}
	plaintext, err := dataAEAD.Open(nil, dataNonce, ciphertext, associatedData)
	if err != nil {
		return nil, fmt.Errorf("crypto: open ciphertext: %w", err)
	}
	return plaintext, nil
}

// GenerateMasterKey returns a fresh 32-byte key, base64 encoded — handy for
// the first-run bootstrap helper.
func GenerateMasterKey() (string, error) {
	key := make([]byte, masterKeyLen)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key), nil
}
