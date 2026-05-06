//go:build !windows

package winagent

import (
	"context"
	"errors"
)

// Config keeps the same shape on non-Windows so cross-platform tests compile.
type Config struct {
	SharedToken string
	CertFile    string
	KeyFile     string
	ListenAddr  string
}

// ErrNotWindows fires when a caller tries to actually serve on a platform
// other than Windows. The collector-side client (client.go) is platform-
// agnostic and will work fine.
var ErrNotWindows = errors.New("winagent: server only runs on Windows")

func ServeHTTPS(_ context.Context, _ Config) error { return ErrNotWindows }

func Collect(_ context.Context) (*Inventory, error) { return nil, ErrNotWindows }
