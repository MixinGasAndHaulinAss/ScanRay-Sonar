//go:build !windows

package main

import (
	"fmt"
	"os"
)

// The Windows agent ships only as a Windows binary; on other platforms we
// keep this stub so `go build ./...` and `go vet ./...` succeed in CI
// without conditional path filtering.
func main() {
	fmt.Fprintln(os.Stderr, "sonar-collector-winagent only builds on Windows")
	os.Exit(2)
}
