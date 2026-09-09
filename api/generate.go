// Package api holds the specification this service is built from, and the
// types and server interface generated from it.
//
// The specification is written by hand and the code is generated, never the
// other way round: the document is what a caller reads and what the service
// answers to, so it cannot be a by-product of the handlers. The handlers are
// written by hand against the generated interface, so a route the spec
// describes and the service does not serve fails to compile.
//
// CI regenerates and diffs, so a spec edited without regenerating fails the
// build rather than drifting quietly.
package api

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1 -config cfg.yaml openapi.yaml
