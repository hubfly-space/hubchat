//go:build integration

package portability

import (
	"bytes"
	"testing"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/feedback"
	filemodule "github.com/hubchat/hubchat/internal/file"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/workspace"
)

func TestFeedbackCSVImportIsIdempotentAndRestoresStatus(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	userID := ids.New(ids.PrefixUser)
	if _, err := pool.Exec(ctx, `INSERT INTO users (id,name,email,password_hash,email_verified_at) VALUES ($1,'Feedback import owner',$2,'x',now())`, userID, userID+"@example.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	eventLog := events.New(pool)
	workspaceService := workspace.New(pool, eventLog, audit.New(pool))
	createdWorkspace, err := workspaceService.Bootstrap(ctx, userID, "Feedback import target", "feedback-import-"+userID[len(userID)-10:])
	if err != nil {
		t.Fatalf("bootstrap workspace: %v", err)
	}
	actor, err := workspaceService.ActorForUser(ctx, createdWorkspace.ID, userID)
	if err != nil {
		t.Fatalf("resolve actor: %v", err)
	}
	feedbackService := feedback.New(pool, eventLog, audit.New(pool))
	board, err := feedbackService.CreateBoard(ctx, createdWorkspace.ID, feedback.BoardInput{Name: "Roadmap", Slug: "roadmap"})
	if err != nil {
		t.Fatalf("create board: %v", err)
	}
	store, err := filemodule.NewLocalStore(t.TempDir(), 1<<20, []string{"text/csv"})
	if err != nil {
		t.Fatalf("create file store: %v", err)
	}
	files := filemodule.New(pool, store)
	content := []byte("external_id,board_id,title,description,status,type\nfeedback-1," + board.ID + ",Export request,Allow archive exports,planned,feature_request\n")
	fileRecord, err := files.Create(ctx, createdWorkspace.ID, filemodule.UploadInput{
		Name: "feedback.csv", MIMEType: "text/csv", SizeBytes: int64(len(content)), Body: bytes.NewReader(content),
		OwnerType: "workspace", OwnerID: createdWorkspace.ID, UploadedByType: "user", UploadedByID: actor.MemberID,
	})
	if err != nil {
		t.Fatalf("upload CSV: %v", err)
	}

	service := New(pool, files, nil)
	service.SetFeedbackImporter(feedbackService)
	request, err := service.CreateImport(ctx, createdWorkspace.ID, actor.MemberID, fileRecord.ID, KindFeedbackCSV, nil, false)
	if err != nil {
		t.Fatalf("create feedback import: %v", err)
	}
	preview, err := service.PreviewImport(ctx, createdWorkspace.ID, request.ID)
	if err != nil {
		t.Fatalf("preview feedback import: %v", err)
	}
	if len(preview) != 1 || preview[0].New != 1 {
		t.Fatalf("unexpected feedback preview: %+v", preview)
	}
	if err := service.RunImport(ctx, request.ID); err != nil {
		t.Fatalf("run feedback import: %v", err)
	}
	if err := service.RunImport(ctx, request.ID); err != nil {
		t.Fatalf("retry feedback import: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM feedback_items WHERE workspace_id=$1`, createdWorkspace.ID).Scan(&count); err != nil {
		t.Fatalf("count imported feedback: %v", err)
	}
	if count != 1 {
		t.Fatalf("imported feedback count = %d, want 1", count)
	}
	var status, importKey string
	if err := pool.QueryRow(ctx, `SELECT status,import_key FROM feedback_items WHERE workspace_id=$1`, createdWorkspace.ID).Scan(&status, &importKey); err != nil {
		t.Fatalf("read imported feedback: %v", err)
	}
	if status != "planned" || importKey != "feedback-1" {
		t.Fatalf("imported feedback = status %q key %q", status, importKey)
	}
}
