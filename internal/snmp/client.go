// Package snmp wraps gosnmp with the credential shapes Sonar persists.
//
// Why a wrapper at all: gosnmp's GoSNMP struct exposes ~30 fields and a
// handful of subtly-typed enums (SnmpVersion, SnmpV3MsgFlags, etc.).
// Letting collectors construct that directly would scatter "translate
// our v3 SHA256 string to gosnmp.SHA256" logic across every call site.
// This package owns that translation in one place and hands collectors
// a tiny `Client` that exposes only the SNMP operations we actually
// use (Get, BulkWalk).
//
// Concurrency: a Client is single-conversation. Don't share one across
// goroutines — the underlying gosnmp.GoSNMP is documented as not
// goroutine-safe. Spawn one Client per appliance per poll cycle.
package snmp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
)

// Creds is the in-memory form of the JSON we seal into
// appliances.enc_snmp_creds. Only one of {Community} (v1/v2c) or the
// V3* fields is used, depending on Version.
type Creds struct {
	Version   string `json:"version"`             // "v1" | "v2c" | "v3"
	Community string `json:"community,omitempty"` // v1/v2c

	V3User      string `json:"v3User,omitempty"`
	V3AuthProto string `json:"v3AuthProto,omitempty"` // "" | SHA | SHA256 | SHA512 | MD5
	V3AuthPass  string `json:"v3AuthPass,omitempty"`
	V3PrivProto string `json:"v3PrivProto,omitempty"` // "" | AES | AES256 | DES
	V3PrivPass  string `json:"v3PrivPass,omitempty"`
}

// Target is the network destination + transport tuning. Kept separate
// from Creds so a credential rotation doesn't require re-typing the IP
// and a host change doesn't require re-typing the password.
type Target struct {
	Host    string        // hostname or IP, no port
	Port    uint16        // 0 → default 161
	Timeout time.Duration // 0 → default 4s per request
	Retries int           // 0 → default 2
}

// Client is a high-level SNMP session. Build with Dial; call Close when
// done. Methods return typed values rather than gosnmp.SnmpPDU so
// collectors don't have to repeat type-assertion ladders.
type Client struct {
	g       *gosnmp.GoSNMP
	target  Target
	version string // copy of Creds.Version for diagnostics
}

// Dial validates Creds, builds the gosnmp config, and opens the UDP
// "connection" (it's connectionless; this just resolves the host and
// allocates buffers). Returns an error if the credentials are
// internally inconsistent (e.g. v3 with no user) or the target is
// unreachable.
func Dial(ctx context.Context, t Target, c Creds) (*Client, error) {
	if t.Host == "" {
		return nil, errors.New("snmp: target host required")
	}
	port := t.Port
	if port == 0 {
		port = 161
	}
	timeout := t.Timeout
	if timeout <= 0 {
		timeout = 4 * time.Second
	}
	retries := t.Retries
	if retries <= 0 {
		retries = 2
	}

	g := &gosnmp.GoSNMP{
		Target:             t.Host,
		Port:               port,
		Transport:          "udp",
		Community:          c.Community,
		Timeout:            timeout,
		Retries:            retries,
		ExponentialTimeout: true,
		MaxOids:            gosnmp.MaxOids,
		Context:            ctx,
	}

	switch c.Version {
	case "v1":
		g.Version = gosnmp.Version1
		if c.Community == "" {
			return nil, errors.New("snmp: v1 requires community")
		}
	case "", "v2c":
		g.Version = gosnmp.Version2c
		if c.Community == "" {
			return nil, errors.New("snmp: v2c requires community")
		}
	case "v3":
		g.Version = gosnmp.Version3
		if c.V3User == "" {
			return nil, errors.New("snmp: v3 requires user")
		}
		g.SecurityModel = gosnmp.UserSecurityModel
		auth, err := mapAuthProto(c.V3AuthProto)
		if err != nil {
			return nil, err
		}
		priv, err := mapPrivProto(c.V3PrivProto)
		if err != nil {
			return nil, err
		}
		flags := gosnmp.NoAuthNoPriv
		switch {
		case auth != gosnmp.NoAuth && priv != gosnmp.NoPriv:
			flags = gosnmp.AuthPriv
		case auth != gosnmp.NoAuth:
			flags = gosnmp.AuthNoPriv
		}
		g.MsgFlags = flags
		g.SecurityParameters = &gosnmp.UsmSecurityParameters{
			UserName:                 c.V3User,
			AuthenticationProtocol:   auth,
			AuthenticationPassphrase: c.V3AuthPass,
			PrivacyProtocol:          priv,
			PrivacyPassphrase:        c.V3PrivPass,
		}
	default:
		return nil, fmt.Errorf("snmp: unsupported version %q", c.Version)
	}

	if err := g.Connect(); err != nil {
		return nil, fmt.Errorf("snmp: connect %s: %w", t.Host, err)
	}
	return &Client{g: g, target: t, version: string(g.Version)}, nil
}

// Close releases the underlying UDP socket.
func (c *Client) Close() error {
	if c == nil || c.g == nil || c.g.Conn == nil {
		return nil
	}
	return c.g.Conn.Close()
}

// Get returns the named scalar OIDs. Map keys are the requested OIDs
// in their original form (the leading "." that gosnmp prepends to
// returned names is stripped, so callers can look up either "1.3.6…"
// or ".1.3.6…" — we normalize on insert). Missing or NoSuchObject
// results are omitted (not error).
func (c *Client) Get(oids []string) (map[string]Value, error) {
	if len(oids) == 0 {
		return map[string]Value{}, nil
	}
	res, err := c.g.Get(oids)
	if err != nil {
		return nil, fmt.Errorf("snmp: GET %v: %w", oids, err)
	}
	out := make(map[string]Value, len(res.Variables))
	for _, v := range res.Variables {
		if v.Type == gosnmp.NoSuchObject || v.Type == gosnmp.NoSuchInstance || v.Type == gosnmp.EndOfMibView {
			continue
		}
		out[strings.TrimPrefix(v.Name, ".")] = wrap(v)
	}
	return out, nil
}

// BulkWalk walks a subtree and returns every variable as an ordered
// slice. Uses GETBULK (v2c+) where possible; falls back to GETNEXT for
// v1. The walker stops automatically when an OID falls outside the
// requested root.
func (c *Client) BulkWalk(root string) ([]Variable, error) {
	var out []Variable
	cb := func(v gosnmp.SnmpPDU) error {
		out = append(out, Variable{OID: v.Name, Value: wrap(v)})
		return nil
	}
	var err error
	if c.g.Version == gosnmp.Version1 {
		err = c.g.Walk(root, cb)
	} else {
		err = c.g.BulkWalk(root, cb)
	}
	if err != nil {
		return nil, fmt.Errorf("snmp: WALK %s: %w", root, err)
	}
	return out, nil
}

// Variable is one (oid, value) pair returned from a walk.
type Variable struct {
	OID   string
	Value Value
}

// Value is a typed wrapper around gosnmp.SnmpPDU. Only the methods
// matter to collectors; the underlying PDU type is hidden so a future
// switch to an alternative SNMP library doesn't ripple through.
type Value struct {
	pdu gosnmp.SnmpPDU
}

func wrap(v gosnmp.SnmpPDU) Value { return Value{pdu: v} }

// Type returns the SNMP-side type name (helps logging).
func (v Value) Type() string { return v.pdu.Type.String() }

// String renders any value as a printable string. OctetStrings are
// returned as-is; counters/integers are decimal-formatted; OIDs as
// dotted notation; nil as "".
func (v Value) String() string {
	if v.pdu.Value == nil {
		return ""
	}
	switch x := v.pdu.Value.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	case int:
		return strconv.Itoa(x)
	case uint:
		return strconv.FormatUint(uint64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	default:
		return fmt.Sprintf("%v", x)
	}
}

// Bytes returns the raw OctetString bytes if applicable, else nil.
// Useful for MAC addresses and binary IfPhysAddress values.
func (v Value) Bytes() []byte {
	if b, ok := v.pdu.Value.([]byte); ok {
		return b
	}
	return nil
}

// Int64 coerces integer-flavored types to int64. Returns 0, false for
// non-integer values; callers should treat false as "not present".
func (v Value) Int64() (int64, bool) {
	switch x := v.pdu.Value.(type) {
	case int:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case uint:
		return int64(x), true
	case uint32:
		return int64(x), true
	case uint64:
		// Counter64 fits here; values > MaxInt64 are unrealistic for
		// the metrics we collect.
		return int64(x), true
	default:
		return 0, false
	}
}

// Uint64 is the unsigned cousin of Int64, important for Counter64
// (ifHCInOctets etc.) where 64-bit cumulative values can exceed
// MaxInt64 over very long uptimes.
func (v Value) Uint64() (uint64, bool) {
	switch x := v.pdu.Value.(type) {
	case uint64:
		return x, true
	case uint32:
		return uint64(x), true
	case uint:
		return uint64(x), true
	case int:
		if x < 0 {
			return 0, false
		}
		return uint64(x), true
	case int32:
		if x < 0 {
			return 0, false
		}
		return uint64(x), true
	case int64:
		if x < 0 {
			return 0, false
		}
		return uint64(x), true
	default:
		return 0, false
	}
}

// mapAuthProto converts the human-readable string we store with the
// credentials into the gosnmp constant. Empty / "NONE" maps to NoAuth
// so noAuthNoPriv works for v3 lab setups.
func mapAuthProto(s string) (gosnmp.SnmpV3AuthProtocol, error) {
	switch s {
	case "", "NONE":
		return gosnmp.NoAuth, nil
	case "MD5":
		return gosnmp.MD5, nil
	case "SHA":
		return gosnmp.SHA, nil
	case "SHA224":
		return gosnmp.SHA224, nil
	case "SHA256":
		return gosnmp.SHA256, nil
	case "SHA384":
		return gosnmp.SHA384, nil
	case "SHA512":
		return gosnmp.SHA512, nil
	default:
		return 0, fmt.Errorf("snmp: unknown auth proto %q", s)
	}
}

func mapPrivProto(s string) (gosnmp.SnmpV3PrivProtocol, error) {
	switch s {
	case "", "NONE":
		return gosnmp.NoPriv, nil
	case "DES":
		return gosnmp.DES, nil
	case "AES", "AES128":
		return gosnmp.AES, nil
	case "AES192":
		return gosnmp.AES192, nil
	case "AES256":
		return gosnmp.AES256, nil
	default:
		return 0, fmt.Errorf("snmp: unknown priv proto %q", s)
	}
}
