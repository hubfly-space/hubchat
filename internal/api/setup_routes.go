package api

import (
	"io/fs"
	"net/http"
	"os"
	"strings"

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

	PublicURL       string       `json:"public_url"`
	SecretKeyOK     bool         `json:"secret_key_ok"`
	EmailConfigured bool         `json:"email_configured"`
	StorageBackend  string       `json:"storage_backend"`
	StorageReady    bool         `json:"storage_ready"`
	StorageDetail   string       `json:"storage_detail"`
	MigrationsReady bool         `json:"migrations_ready"`
	Checks          []setupCheck `json:"checks"`

	MigrationsTotal   int `json:"migrations_total"`
	MigrationsApplied int `json:"migrations_applied"`
}

type setupCheck struct {
	ID     string `json:"id"`
	Status string `json:"status"` // pass, warn, or fail
	Detail string `json:"detail"`
}

func handleSetupState(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		installed, err := deps.Auth.AnyUserExists(r.Context())
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError,
				"Could not read installation state.")
			return
		}

		storageReady, storageDetail := storageReadiness(deps)
		emailConfigured := deps.Config.Email.Enabled &&
			strings.TrimSpace(deps.Config.Email.SMTPHost) != "" &&
			strings.TrimSpace(deps.Config.Email.FromAddress) != ""
		state := setupState{
			Installed: installed,
			PublicURL: publicURLString(deps),
			// The server would not have started with a short key (§11.5,
			// enforced by config.Validate at boot), so by the time this
			// endpoint answers, it is definitionally true. Reported anyway so
			// the wizard shows the same check `hubchat doctor` would.
			SecretKeyOK:     len(deps.Config.Security.SecretKey) >= 32,
			EmailConfigured: emailConfigured,
			StorageBackend:  deps.Config.Storage.Backend,
			StorageReady:    storageReady,
			StorageDetail:   storageDetail,
		}

		migrationError := ""
		if total, applied, err := migrationCounts(r, deps); err == nil {
			state.MigrationsTotal = total
			state.MigrationsApplied = applied
			state.MigrationsReady = total > 0 && applied >= total
		} else {
			migrationError = "Could not verify the database schema. Check PostgreSQL connectivity and migration state."
		}
		publicURLReady := state.PublicURL != ""
		secretReady := state.SecretKeyOK
		migrationDetail := migrationError
		if migrationDetail == "" {
			migrationDetail = "All embedded migrations are applied."
			if !state.MigrationsReady {
				migrationDetail = "The database schema is behind the embedded migrations."
			}
		}
		state.Checks = []setupCheck{
			{ID: "database", Status: "pass", Detail: "PostgreSQL responded to the installation check."},
			{ID: "migrations", Status: checkStatus(state.MigrationsReady), Detail: migrationDetail},
			{ID: "public_url", Status: checkStatus(publicURLReady), Detail: publicURLDetail(state.PublicURL)},
			{ID: "storage", Status: checkStatus(storageReady), Detail: storageDetail},
			{ID: "secret_key", Status: checkStatus(secretReady), Detail: secretDetail(secretReady)},
		}
		if emailConfigured {
			state.Checks = append(state.Checks, setupCheck{ID: "email", Status: "pass", Detail: "SMTP host and sender address are configured."})
		} else {
			state.Checks = append(state.Checks, setupCheck{ID: "email", Status: "warn", Detail: "SMTP is optional, but customer notifications will remain queued until it is configured."})
		}

		httpserver.WriteJSON(w, http.StatusOK, state)
	}
}

func checkStatus(ok bool) string {
	if ok {
		return "pass"
	}
	return "fail"
}

func publicURLDetail(value string) string {
	if value == "" {
		return "HUBCHAT_PUBLIC_URL is not configured."
	}
	return "Browser, widget, and outbound links use " + value + "."
}

func secretDetail(ok bool) string {
	if ok {
		return "The configured key is present and meets the minimum length."
	}
	return "The configured secret key is missing or too short."
}

func storageReadiness(deps Deps) (bool, string) {
	switch strings.ToLower(strings.TrimSpace(deps.Config.Storage.Backend)) {
	case "local":
		root := strings.TrimSpace(deps.Config.Storage.LocalPath)
		if root == "" {
			return false, "Local attachment storage has no directory configured."
		}
		info, err := os.Stat(root)
		if err == nil {
			if !info.IsDir() {
				return false, "The configured local attachment path is not a directory."
			}
			return true, "Local attachment storage directory is available."
		}
		if os.IsNotExist(err) {
			// LocalStore creates the workspace subdirectories on the first
			// upload. A missing root is therefore usable, but deserves a
			// visible warning so an operator can pre-create it with the
			// intended ownership and permissions.
			return true, "Local attachment directory will be created on the first upload."
		}
		return false, "The local attachment directory could not be inspected."
	case "s3":
		if strings.TrimSpace(deps.Config.Storage.S3Bucket) == "" ||
			strings.TrimSpace(deps.Config.Storage.S3AccessKey) == "" ||
			strings.TrimSpace(deps.Config.Storage.S3SecretKey) == "" {
			return false, "S3-compatible storage is missing its bucket or credentials."
		}
		return true, "S3-compatible attachment storage is configured."
	default:
		return false, "The attachment storage backend is not supported."
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
