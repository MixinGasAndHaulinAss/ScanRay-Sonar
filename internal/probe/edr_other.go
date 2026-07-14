//go:build !windows

package probe

import "context"

// CollectEDR is a no-op on non-Windows platforms.
func CollectEDR(_ context.Context) ([]string, *bool) { return nil, nil }
