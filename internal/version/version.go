// Package version exposes the build version embedded at compile time via
// -ldflags "-X github.com/NCLGISA/ScanRay-Sonar/internal/version.Version=..."
//
// The single source of truth for the version is the top-level VERSION file.
// CI reads it and injects it into the binaries; locally, builds without the
// ldflag fall back to "dev".
package version

import "runtime/debug"

var (
	// Version follows CalVer: YYYY.M.D.patch
	Version = "dev"

	// Commit is the short git SHA (set by ldflags in CI).
	Commit = "unknown"

	// BuildTime is RFC3339 build time (set by ldflags in CI).
	BuildTime = "unknown"
)

// Info is the structured form returned by the /version endpoint.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"buildTime"`
	GoVersion string `json:"goVersion"`
}

// Get returns a populated Info, falling back to debug.BuildInfo for fields
// the ldflags didn't set.
func Get() Info {
	info := Info{
		Version:   Version,
		Commit:    Commit,
		BuildTime: BuildTime,
		GoVersion: "unknown",
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		info.GoVersion = bi.GoVersion
		if Commit == "unknown" {
			for _, s := range bi.Settings {
				if s.Key == "vcs.revision" && s.Value != "" {
					info.Commit = s.Value
				}
				if s.Key == "vcs.time" && s.Value != "" {
					info.BuildTime = s.Value
				}
			}
		}
	}
	return info
}
