// Package probebins embeds the cross-compiled sonar-probe binaries so
// the API container can serve them from /api/v1/probe/download/{os}/{arch}.
//
// The bin/ directory is populated by the Docker build (see
// docker/api.Dockerfile) at image-assembly time. A .gitkeep makes go:embed
// happy in source-only checkouts; in that case the served filesystem
// contains nothing but the .gitkeep, and the API returns 404 on
// download. Cross-compile locally with `make probe-all` to populate it
// for `go run`-style development.
package probebins

import "embed"

//go:embed all:bin
var FS embed.FS
