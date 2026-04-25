//go:build windows

package main

import (
	"os"
	"path/filepath"
)

// On Windows we drop config under %ProgramData%\Sonar — readable by
// SYSTEM (the service account) and writable only by Administrators.
var defaultConfigPath = func() string {
	pd := os.Getenv("ProgramData")
	if pd == "" {
		pd = `C:\ProgramData`
	}
	return filepath.Join(pd, "Sonar", "agent.json")
}()
