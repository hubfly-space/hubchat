//go:build integration

package portability

import (
	"bytes"
	"testing"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	filemodule "github.com/hubchat/hubchat/internal/file"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/knowledgebase"
	"github.com/hubchat/hubchat/internal/workspace"
)

func TestMarkdownImportUpsertsTheSameArticleOnRetry(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	userID := ids.New(ids.PrefixUser)
	if _, err := pool.Exec(ctx, `INSERT INTO users (id,name,email,password_hash,email_verified_at) VALUES ($1,'KB import owner',$2,'x',now())`, userID, userID+"@example.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	workspaceService := workspace.New(pool, events.New(pool), audit.New(pool))
	createdWorkspace, err := workspaceService.Bootstrap(ctx, userID, "KB import target", "kb-import-"+userID[len(userID)-10:])
	if err != nil {
		t.Fatalf("bootstrap workspace: %v", err)
	}
	actor, err := workspaceService.ActorForUser(ctx, createdWorkspace.ID, userID)
	if err != nil {
		t.Fatalf("resolve actor: %v", err)
	}
	kbService := knowledgebase.New(pool)
	kb, err := kbService.CreateKnowledgeBase(ctx, createdWorkspace.ID, knowledgebase.KnowledgeBaseInput{Name: "Help", Slug: "help"})
	if err != nil {
		t.Fatalf("create knowledge base: %v", err)
	}

	store, err := filemodule.NewLocalStore(t.TempDir(), 1<<20, []string{"text/markdown"})
	if err != nil {
		t.Fatalf("create file store: %v", err)
	}
	files := filemodule.New(pool, store)
	content := []byte("---\nknowledge_base_id: " + kb.ID + "\nslug: getting-started\nstate: published\n---\n# Getting Started\n\nUse Hubchat to help customers.\n")
	fileRecord, err := files.Create(ctx, createdWorkspace.ID, filemodule.UploadInput{
		Name: "getting-started.md", MIMEType: "text/markdown", SizeBytes: int64(len(content)), Body: bytes.NewReader(content),
		OwnerType: "workspace", OwnerID: createdWorkspace.ID, UploadedByType: "user", UploadedByID: actor.MemberID,
	})
	if err != nil {
		t.Fatalf("upload Markdown: %v", err)
	}

	service := New(pool, files, nil)
	service.SetKnowledgeBaseImporter(kbService)
	request, err := service.CreateImport(ctx, createdWorkspace.ID, actor.MemberID, fileRecord.ID, KindKnowledgeBaseMarkdown, nil, false)
	if err != nil {
		t.Fatalf("create Markdown import: %v", err)
	}
	preview, err := service.PreviewImport(ctx, createdWorkspace.ID, request.ID)
	if err != nil {
		t.Fatalf("preview Markdown import: %v", err)
	}
	if len(preview) != 1 || preview[0].Rows != 1 || preview[0].Existing != 0 || preview[0].New != 1 {
		t.Fatalf("unexpected Markdown preview: %+v", preview)
	}
	if err := service.RunImport(ctx, request.ID); err != nil {
		t.Fatalf("run Markdown import: %v", err)
	}
	if err := service.RunImport(ctx, request.ID); err != nil {
		t.Fatalf("retry Markdown import: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM articles WHERE workspace_id=$1 AND knowledge_base_id=$2`, createdWorkspace.ID, kb.ID).Scan(&count); err != nil {
		t.Fatalf("count imported articles: %v", err)
	}
	if count != 1 {
		t.Fatalf("imported article count = %d, want 1", count)
	}
	var state string
	if err := pool.QueryRow(ctx, `SELECT state FROM articles WHERE workspace_id=$1 AND knowledge_base_id=$2`, createdWorkspace.ID, kb.ID).Scan(&state); err != nil {
		t.Fatalf("read imported article: %v", err)
	}
	if state != "published" {
		t.Fatalf("imported article state = %q, want published", state)
	}
}
