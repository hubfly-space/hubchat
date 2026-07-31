// Package embedded holds everything the binary carries with it: the compiled
// browser bundles, the SQL migrations, and the default email and page
// templates.
//
// This package is the single place `go:embed` appears. Keeping it here means
// the rest of the codebase depends on an interface (an fs.FS) rather than on
// where the bytes came from, which is what lets the server read from disk in
// development and from the binary in production without any other package
// knowing the difference.
package embedded

import (
	"embed"
	"io/fs"
)

// Browser bundles. Each directory is produced by `pnpm build` in the matching
// web/ workspace; the all: prefix keeps files that begin with _ or . which
// asset hashing occasionally produces.
//
//go:embed all:assets
var assetsFS embed.FS

// SQL migrations, applied in filename order. Immutable once released — a
// migration that has shipped is never edited, only superseded.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// Default email bodies and standalone HTML pages (error pages, the
// unsubscribe confirmation, the OAuth callback shim). Workspaces may override
// the email templates; these are the fallbacks.
//
//go:embed templates
var templatesFS embed.FS

// The checked-in OpenAPI document is served by the binary so the contract
// users download is always from the same build as the handlers.
//
//go:embed openapi.json
var openAPIFS embed.FS

// Dashboard returns the agent and admin bundle, rooted at its index.html.
func Dashboard() (fs.FS, error) { return fs.Sub(assetsFS, "assets/dashboard") }

// Portal returns the customer-facing bundle.
func Portal() (fs.FS, error) { return fs.Sub(assetsFS, "assets/portal") }

// Widget returns the loader and the lazily-fetched interface.
func Widget() (fs.FS, error) { return fs.Sub(assetsFS, "assets/widget") }

// Migrations returns the embedded SQL files.
func Migrations() (fs.FS, error) { return fs.Sub(migrationsFS, "migrations") }

// Templates returns the default email and page templates.
func Templates() (fs.FS, error) { return fs.Sub(templatesFS, "templates") }

// OpenAPI returns the versioned public API contract.
func OpenAPI() []byte {
	data, err := fs.ReadFile(openAPIFS, "openapi.json")
	if err != nil {
		return nil
	}
	return data
}
