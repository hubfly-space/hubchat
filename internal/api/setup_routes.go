package api

import (
	"io/fs"
	"net/http"

	"github.com/hubchat/hubchat/embedded"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/httpserver"
)

// migrationFileCount counts embedded migration files.
//
// Doesn't reuse cmd/hubchat's readDirNames — that lives in package main,
// which nothing outside the binary's own entrypoint may import — but the
// count is all this endpoint needs, so a full sorted listing is more than
// the job calls for.
func migrationFileCount() (int, error) {
	migrations, err := embedded.Migrations()
	if err != nil {
		return 0, err
	}
	entries, err := fs.ReadDir(migrations, ".")
	if err != nil {
		return 0, err
	}

	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			count++
		}
	}
	return count, nil
}

// registerSetupRoutes mounts the first-run installation check (§7.1).
//
// Unauthenticated by necessity: it exists to answer "has anyone signed in
// yet", which is precisely the question a signed-in check cannot ask.
func registerSetupRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/setup/state", handleSetupState(deps))
}

// setupState mirrors what the wizard actually needs to decide what to show —
// not a general config dump. Nothing here is a secret: it is the same
// information `hubchat doctor` prints to a terminal, now answerable from a
// browser before any account exists to authenticate the request.
type setupState struct {
	// Installed is true once any user exists. The wizard is a first-run
	// experience; once this flips, its only job is to send someone to
	// /login rather than let a stranger create a second "first" owner.
	Installed bool `json:"installed"`

	PublicURL       string `json:"public_url"`
	SecretKeyOK     bool   `json:"secret_key_ok"`
	EmailConfigured bool   `json:"email_configured"`
	StorageBackend  string `json:"storage_backend"`

	MigrationsTotal   int `json:"migrations_total"`
	MigrationsApplied int `json:"migrations_applied"`
}

func handleSetupState(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		installed, err := deps.Auth.AnyUserExists(r.Context())
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError,
				"Could not read installation state.")
			return
		}

		state := setupState{
			Installed: installed,
			PublicURL: publicURLString(deps),
			// The server would not have started with a short key (§11.5,
			// enforced by config.Validate at boot), so by the time this
			// endpoint answers, it is definitionally true. Reported anyway so
			// the wizard shows the same check `hubchat doctor` would.
			SecretKeyOK:     len(deps.Config.Security.SecretKey) >= 32,
			EmailConfigured: deps.Config.Email.Enabled,
			StorageBackend:  deps.Config.Storage.Backend,
		}

		if total, applied, err := migrationCounts(r, deps); err == nil {
			state.MigrationsTotal = total
			state.MigrationsApplied = applied
		}

		httpserver.WriteJSON(w, http.StatusOK, state)
	}
}

func publicURLString(deps Deps) string {
	if deps.PublicURL == nil {
		return ""
	}
	return deps.PublicURL.String()
}

// migrationCounts reports how many embedded migrations exist versus how many
// have been applied.
//
// By the time this handler runs the server has already started, which under
// every MigratePolicy means there are no pending migrations — Apply already
// ran them and Verify would have refused to boot otherwise. The two numbers
// are still read separately and shown rather than collapsed into a boolean,
// because "up to date" is a claim worth letting the operator verify rather
// than take on faith.
func migrationCounts(r *http.Request, deps Deps) (total, applied int, err error) {
	count, err := migrationFileCount()
	if err != nil {
		return 0, 0, err
	}

	appliedSet, err := database.AppliedMigrations(r.Context(), deps.Pool)
	if err != nil {
		return 0, 0, err
	}

	return count, len(appliedSet), nil
}
