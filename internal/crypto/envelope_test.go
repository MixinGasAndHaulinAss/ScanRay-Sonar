package crypto

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func newTestSealer(t *testing.T) *Sealer {
	t.Helper()
	k, err := GenerateMasterKey()
	if err != nil {
		t.Fatalf("generate master key: %v", err)
	}
	s, err := NewSealer(k)
	if err != nil {
		t.Fatalf("new sealer: %v", err)
	}
	return s
}

func TestSealOpenRoundTrip(t *testing.T) {
	s := newTestSealer(t)
	plain := []byte("super-secret-snmpv3-priv-pass")
	ad := []byte("appliance:abc-123")

	sealed, err := s.Seal(plain, ad)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if bytes.Contains(sealed, plain) {
		t.Fatalf("plaintext leaked into sealed blob")
	}
	got, err := s.Open(sealed, ad)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("roundtrip mismatch: got %q want %q", got, plain)
	}
}

func TestOpenWrongAssociatedData(t *testing.T) {
	s := newTestSealer(t)
	sealed, err := s.Seal([]byte("x"), []byte("row-1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(sealed, []byte("row-2")); err == nil {
		t.Fatalf("expected error on wrong associated data")
	}
}

func TestOpenWrongMasterKey(t *testing.T) {
	a := newTestSealer(t)
	b := newTestSealer(t)
	sealed, err := a.Seal([]byte("x"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Open(sealed, nil); err == nil {
		t.Fatalf("expected error opening with wrong master key")
	}
}

func TestOpenTampered(t *testing.T) {
	s := newTestSealer(t)
	sealed, err := s.Seal([]byte("hello"), nil)
	if err != nil {
		t.Fatal(err)
	}
	sealed[len(sealed)-1] ^= 0x01
	if _, err := s.Open(sealed, nil); err == nil {
		t.Fatalf("expected error on tampered ciphertext")
	}
}

func TestUniqueDataKeysPerSeal(t *testing.T) {
	s := newTestSealer(t)
	a, _ := s.Seal([]byte("same"), nil)
	b, _ := s.Seal([]byte("same"), nil)
	if bytes.Equal(a, b) {
		t.Fatalf("two seals of identical plaintext produced identical ciphertext (data key is not per-row random)")
	}
}

func TestNewSealerInvalidKey(t *testing.T) {
	if _, err := NewSealer("not-base64!!"); err == nil {
		t.Fatal("expected error on invalid base64")
	}
	short := base64.StdEncoding.EncodeToString([]byte("short"))
	if _, err := NewSealer(short); err == nil {
		t.Fatal("expected error on short master key")
	}
}
