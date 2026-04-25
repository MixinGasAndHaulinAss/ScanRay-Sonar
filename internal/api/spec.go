package api

import _ "embed"

//go:embed openapi.yaml
var openAPISpecBytes []byte

// Spec returns the embedded OpenAPI 3.1 source-of-truth document.
func Spec() []byte { return openAPISpecBytes }
