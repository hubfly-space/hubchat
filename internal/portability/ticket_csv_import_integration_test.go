//go:build integration

package portability

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
	"github.com/hubchat/hubchat/internal/ticket"
	"github.com/hubchat/hubchat/internal/workspace"
)

func TestTicketCSVImportIsIdempotentAcrossRetries(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	userID := ids.New(ids.PrefixUser)
	if _, err := pool.Exec(ctx, `INSERT INTO users (id,name,email,password_hash,email_verified_at) VALUES ($1,'Ticket import owner',$2,'x',now())`, userID, userID+"@example.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	workspaceService := workspace.New(pool, events.New(pool), audit.New(pool))
	createdWorkspace, err := workspaceService.Bootstrap(ctx, userID, "Ticket import target", "ticket-import-"+userID[len(userID)-10:])
	if err != nil {
		t.Fatalf("bootstrap workspace: %v", err)
	}
	actor, err := workspaceService.ActorForUser(ctx, createdWorkspace.ID, userID)
	if err != nil {
		t.Fatalf("resolve actor: %v", err)
	}
	var inboxID string
	if err := pool.QueryRow(ctx, `SELECT id FROM inboxes WHERE workspace_id=$1 LIMIT 1`, createdWorkspace.ID).Scan(&inboxID); err != nil {
		t.Fatalf("find inbox: %v", err)
	}

	store, err := filemodule.NewLocalStore(t.TempDir(), 1<<20, []string{"text/csv"})
	if err != nil {
		t.Fatalf("create file store: %v", err)
	}
	files := filemodule.New(pool, store)
	content := []byte("external_id,title,description,status,priority,inbox_id,channel\nticket-1,Imported issue,Imported description,open,high," + inboxID + ",api\n")
	fileRecord, err := files.Create(ctx, createdWorkspace.ID, filemodule.UploadInput{
		Name: "tickets.csv", MIMEType: "text/csv", SizeBytes: int64(len(content)), Body: bytes.NewReader(content),
		OwnerType: "workspace", OwnerID: createdWorkspace.ID, UploadedByType: "user", UploadedByID: actor.MemberID,
	})
	if err != nil {
		t.Fatalf("upload CSV: %v", err)
	}

	customers := customer.New(pool, events.New(pool), audit.New(pool), config.Default().Limits)
	tickets := ticket.New(pool, workspaceService, events.New(pool), audit.New(pool))
	service := New(pool, files, nil)
	service.SetCustomerImporter(customers)
	service.SetTicketImporter(tickets)
	request, err := service.CreateImport(ctx, createdWorkspace.ID, actor.MemberID, fileRecord.ID, KindTicketsCSV, nil, false)
	if err != nil {
		t.Fatalf("create ticket import: %v", err)
	}
	preview, err := service.PreviewImport(ctx, createdWorkspace.ID, request.ID)
	if err != nil {
		t.Fatalf("preview ticket import: %v", err)
	}
	if len(preview) != 1 || preview[0].Rows != 1 || preview[0].Existing != 0 || preview[0].New != 1 {
		t.Fatalf("unexpected ticket preview: %+v", preview)
	}
	if err := service.RunImport(ctx, request.ID); err != nil {
		t.Fatalf("run ticket import: %v", err)
	}
	if err := service.RunImport(ctx, request.ID); err != nil {
		t.Fatalf("retry ticket import: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tickets WHERE workspace_id=$1`, createdWorkspace.ID).Scan(&count); err != nil {
		t.Fatalf("count imported tickets: %v", err)
	}
	if count != 1 {
		t.Fatalf("imported ticket count = %d, want 1", count)
	}
	var status, priority, importKey string
	if err := pool.QueryRow(ctx, `SELECT status,priority,import_key FROM tickets WHERE workspace_id=$1`, createdWorkspace.ID).Scan(&status, &priority, &importKey); err != nil {
		t.Fatalf("read imported ticket: %v", err)
	}
	if status != "open" || priority != "high" || importKey != "ticket-1" {
		t.Fatalf("imported ticket = status %q priority %q key %q", status, priority, importKey)
	}
}
