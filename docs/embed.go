// Package docs embeds the built MkDocs site so sonar-api can serve
// operator documentation at /docs without a separate web container.
//
// Build pipeline:
//
//	mkdocs build                          -> writes docs/site/
//	go build ./cmd/sonar-api              -> bakes docs/site/ into the binary
//
// During Go-only development (no MkDocs toolchain), the placeholder
// site/index.html keeps go:embed happy.
package docs

import "embed"

//go:embed all:site
var SiteFS embed.FS
