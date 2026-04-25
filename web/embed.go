// Package web embeds the built React UI so sonar-api can serve it
// without a separate filesystem mount or web container.
//
// Build pipeline:
//
//	cd web && npm install && npm run build  -> writes web/dist/
//	go build ./cmd/sonar-api                 -> bakes web/dist/ into the binary
//
// During Go-only development (no Node toolchain), the placeholder
// dist/index.html keeps go:embed happy and the API still serves a
// "build the UI" landing page.
package web

import "embed"

//go:embed all:dist
var DistFS embed.FS
