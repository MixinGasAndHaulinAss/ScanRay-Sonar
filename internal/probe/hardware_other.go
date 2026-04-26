//go:build !linux && !windows

// Hardware collector stub for platforms we don't actively target
// (notably macOS and the BSDs). The probe still runs there during
// dev work; we just don't promise inventory.
//
// Returning nil tells the snapshot pipeline "nothing to attach"; the
// `hardware` field is omitted from the JSON instead of shipping an
// empty {} that would imply we tried and got back nothing useful.

package probe

import "context"

func collectHardware(_ context.Context) *Hardware {
	return nil
}
