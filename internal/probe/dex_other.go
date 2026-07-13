//go:build !windows

package probe

import "context"

func collectDEXInventoryOS(ctx context.Context) *DexInventory {
	_ = ctx
	return &DexInventory{}
}

func sampleAppFocusOS() (name string, pid int32, ok bool) {
	return "", 0, false
}
