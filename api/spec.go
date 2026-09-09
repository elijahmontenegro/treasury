package api

import _ "embed"

// Spec is api/openapi.yaml, exactly as written. It is embedded rather
// than re-marshalled from a parsed model so that what a caller fetches
// from /openapi.yaml is the document this service was generated from,
// character for character, including its comments.
//
//go:embed openapi.yaml
var Spec []byte
