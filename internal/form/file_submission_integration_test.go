//go:build integration

package form

import (
	"bytes"
	"testing"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/config"
	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	filemodule "github.com/hubchat/hubchat/internal/file"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/workspace"
)

func TestSubmitClaimsWorkspaceStagedFileForSubmission(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	userID := ids.New(ids.PrefixUser)
	if _, err := pool.Exec(ctx, `INSERT INTO users (id,name,email,password_hash,email_verified_at) VALUES ($1,'Form owner',$2,'x',now())`, userID, userID+"@example.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	workspaceService := workspace.New(pool, events.New(pool), audit.New(pool))
	createdWorkspace, err := workspaceService.Bootstrap(ctx, userID, "Forms", "forms-"+userID[len(userID)-10:])
	if err != nil {
		t.Fatalf("bootstrap workspace: %v", err)
	}

	store, err := filemodule.NewLocalStore(t.TempDir(), 1<<20, []string{"text/plain"})
	if err != nil {
		t.Fatalf("create file store: %v", err)
	}
	files := filemodule.New(pool, store)
	staged, err := files.Create(ctx, createdWorkspace.ID, filemodule.UploadInput{
		Name: "screenshot.txt", MIMEType: "text/plain", SizeBytes: 5, Body: bytes.NewReader([]byte("hello")),
		OwnerType: "workspace", OwnerID: createdWorkspace.ID, UploadedByType: "visitor",
	})
	if err != nil {
		t.Fatalf("stage form file: %v", err)
	}

	customers := customer.New(pool, events.New(pool), audit.New(pool), config.Default().Limits)
	service := New(pool, TargetServices{Customer: customers})
	form, err := service.Create(ctx, createdWorkspace.ID, CreateInput{
		Name: "Contact", Slug: "contact", Purpose: "customer", Access: "public", Enabled: true,
		Fields: []FieldInput{
			{Key: "name", Label: "Name", Type: "string", Required: true},
			{Key: "attachment", Label: "Attachment", Type: "file", Required: true},
		},
	})
	if err != nil {
		t.Fatalf("create form: %v", err)
	}

	submissionID, err := service.Submit(ctx, createdWorkspace.ID, form.Slug, SubmissionInput{
		Values:  map[string]any{"name": "Alice"},
		FileIDs: map[string]string{"attachment": staged.ID},
	})
	if err != nil {
		t.Fatalf("submit form with file: %v", err)
	}

	var ownerType, ownerID, fileID string
	if err := pool.QueryRow(ctx, `SELECT owner_type,owner_id FROM files WHERE workspace_id=$1 AND id=$2`, createdWorkspace.ID, staged.ID).Scan(&ownerType, &ownerID); err != nil {
		t.Fatalf("read claimed file: %v", err)
	}
	if ownerType != "form_submission" || ownerID != submissionID {
		t.Fatalf("file owner = %s/%s, want form_submission/%s", ownerType, ownerID, submissionID)
	}
	if err := pool.QueryRow(ctx, `SELECT file_id FROM form_submission_values WHERE submission_id=$1 AND file_id IS NOT NULL`, submissionID).Scan(&fileID); err != nil {
		t.Fatalf("read form file value: %v", err)
	}
	if fileID != staged.ID {
		t.Fatalf("form file id = %s, want %s", fileID, staged.ID)
	}
}
