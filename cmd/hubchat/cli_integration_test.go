//go:build integration

package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/hubchat/hubchat/internal/auth"
	"github.com/hubchat/hubchat/internal/database/dbtest"
)

func captureCLIOutput(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create output pipe: %v", err)
	}
	previous := os.Stdout
	os.Stdout = writer
	commandErr := fn()
	_ = writer.Close()
	os.Stdout = previous
	data, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if readErr != nil {
		t.Fatalf("read command output: %v", readErr)
	}
	return string(data), commandErr
}

func TestAdminCLIResetPasswordAndCreateValidation(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	url := os.Getenv(dbtest.URLEnv)
	t.Setenv("HUBCHAT_DATABASE_URL", url)
	t.Setenv("HUBCHAT_PUBLIC_URL", "https://cli-test.example.com")
	t.Setenv("HUBCHAT_SECRET_KEY", "cli-test-secret-key-that-is-at-least-32-bytes")
	t.Setenv("HUBCHAT_MIGRATE", "skip")
	t.Setenv("HUBCHAT_DEV", "0")
	t.Setenv("HUBCHAT_DATA_DIR", t.TempDir())

	out, err := captureCLIOutput(t, func() error {
		return setupCommand([]string{"--name", "CLI Owner", "--email", "cli-owner@example.com", "--password", "owner-password-x", "--slug", "cli-workspace", "--json"})
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	var setupResult map[string]any
	if err := json.Unmarshal([]byte(out), &setupResult); err != nil {
		t.Fatalf("decode setup output %q: %v", out, err)
	}
	ownerID, _ := setupResult["user_id"].(string)
	workspaceID, _ := setupResult["workspace_id"].(string)
	if ownerID == "" || workspaceID == "" || setupResult["workspace_slug"] != "cli-workspace" {
		t.Fatalf("unexpected setup output: %#v", setupResult)
	}

	out, err = captureCLIOutput(t, func() error {
		return adminCommand([]string{"list", "--json"})
	})
	if err != nil {
		t.Fatalf("admin list: %v", err)
	}
	var accounts []map[string]any
	if err := json.Unmarshal([]byte(out), &accounts); err != nil {
		t.Fatalf("decode admin list output %q: %v", out, err)
	}
	if len(accounts) != 1 || accounts[0]["email"] != "cli-owner@example.com" {
		t.Fatalf("unexpected admin list output: %#v", accounts)
	}

	out, err = captureCLIOutput(t, func() error {
		return doctor([]string{"--json"})
	})
	if err != nil {
		t.Fatalf("doctor --json: %v", err)
	}
	var doctorResult struct {
		OK       bool             `json:"ok"`
		Checks   []map[string]any `json:"checks"`
		Failures int              `json:"failures"`
	}
	if err := json.Unmarshal([]byte(out), &doctorResult); err != nil {
		t.Fatalf("decode doctor output %q: %v", out, err)
	}
	if !doctorResult.OK || doctorResult.Failures != 0 || len(doctorResult.Checks) == 0 {
		t.Fatalf("unexpected doctor output: %#v", doctorResult)
	}
	if strings.Contains(out, "cli-test-secret-key") {
		t.Fatal("doctor output leaked the deployment secret")
	}

	archiveFile, err := os.CreateTemp("", "hubchat-cli-*.json.gz")
	if err != nil {
		t.Fatalf("create archive path: %v", err)
	}
	archivePath := archiveFile.Name()
	_ = archiveFile.Close()
	t.Cleanup(func() { _ = os.Remove(archivePath) })
	out, err = captureCLIOutput(t, func() error {
		return workspaceCommand([]string{"export", "--workspace", workspaceID, "--out", archivePath, "--json"})
	})
	if err != nil {
		t.Fatalf("workspace export: %v", err)
	}
	var exportResult map[string]any
	if err := json.Unmarshal([]byte(out), &exportResult); err != nil {
		t.Fatalf("decode workspace export output %q: %v", out, err)
	}
	if exportResult["action"] != "exported" || exportResult["path"] != archivePath {
		t.Fatalf("unexpected workspace export output: %#v", exportResult)
	}
	out, err = captureCLIOutput(t, func() error {
		return workspaceCommand([]string{"import", "--workspace", workspaceID, "--file", archivePath, "--dry-run", "--json"})
	})
	if err != nil {
		t.Fatalf("workspace import dry-run: %v", err)
	}
	var importResult map[string]any
	if err := json.Unmarshal([]byte(out), &importResult); err != nil {
		t.Fatalf("decode workspace import output %q: %v", out, err)
	}
	if importResult["action"] != "validated" {
		t.Fatalf("unexpected workspace import output: %#v", importResult)
	}

	out, err = captureCLIOutput(t, func() error {
		return jobsCommand([]string{"list", "--json"})
	})
	if err != nil {
		t.Fatalf("jobs list: %v", err)
	}
	var jobs []any
	if err := json.Unmarshal([]byte(out), &jobs); err != nil {
		t.Fatalf("decode jobs output %q: %v", out, err)
	}
	if jobs == nil {
		t.Fatal("jobs list returned null instead of a JSON array")
	}

	users := auth.New(pool, auth.Options{})

	session, err := users.CreateSession(ctx, ownerID, "before-reset", "")
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}

	out, err = captureCLIOutput(t, func() error {
		return adminCommand([]string{"reset-password", "--email", "cli-owner@example.com", "--password", "cli-new-password-x", "--json"})
	})
	if err != nil {
		t.Fatalf("admin reset-password: %v", err)
	}
	var resetResult map[string]any
	if err := json.Unmarshal([]byte(out), &resetResult); err != nil {
		t.Fatalf("decode reset output %q: %v", out, err)
	}
	if resetResult["action"] != "reset-password" || resetResult["email"] != "cli-owner@example.com" || resetResult["sessions_revoked"] != true {
		t.Fatalf("unexpected reset output: %#v", resetResult)
	}
	if strings.Contains(out, "cli-new-password-x") {
		t.Fatal("reset command printed the new password")
	}
	if _, err := users.UserForSession(ctx, session.Token); err == nil {
		t.Fatal("admin CLI reset left an old session valid")
	}
	if _, err := users.SignIn(ctx, "cli-owner@example.com", "cli-new-password-x", "after-reset", ""); err != nil {
		t.Fatalf("sign in after CLI reset: %v", err)
	}

	var beforeUsers int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM users`).Scan(&beforeUsers); err != nil {
		t.Fatalf("count users before invalid create: %v", err)
	}
	if _, err := captureCLIOutput(t, func() error {
		return adminCommand([]string{"create", "--name", "Orphan", "--email", "orphan@example.com", "--password", "orphan-password-x", "--workspace", "ws_missing"})
	}); err == nil {
		t.Fatal("admin create accepted a missing workspace")
	}
	var afterUsers int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM users`).Scan(&afterUsers); err != nil {
		t.Fatalf("count users after invalid create: %v", err)
	}
	if afterUsers != beforeUsers {
		t.Fatalf("invalid admin create left an orphan user: before=%d after=%d", beforeUsers, afterUsers)
	}

	out, err = captureCLIOutput(t, func() error {
		return adminCommand([]string{"create", "--name", "CLI Agent", "--email", "cli-agent@example.com", "--password", "agent-password-x", "--workspace", workspaceID, "--role", "agent", "--json"})
	})
	if err != nil {
		t.Fatalf("admin create: %v", err)
	}
	var createResult map[string]any
	if err := json.Unmarshal([]byte(out), &createResult); err != nil {
		t.Fatalf("decode create output %q: %v", out, err)
	}
	if createResult["email"] != "cli-agent@example.com" || createResult["workspace_id"] != workspaceID || createResult["role"] != "agent" {
		t.Fatalf("unexpected create output: %#v", createResult)
	}

}
