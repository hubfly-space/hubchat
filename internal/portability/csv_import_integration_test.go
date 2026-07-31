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
	"github.com/hubchat/hubchat/internal/workspace"
)

func TestCSVImportPreviewsAndResumesWithoutDuplicates(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	userID := ids.New(ids.PrefixUser)
	if _, err := pool.Exec(ctx, `INSERT INTO users (id,name,email,password_hash,email_verified_at) VALUES ($1,'Import owner',$2,'x',now())`, userID, userID+"@example.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	workspaceService := workspace.New(pool, events.New(pool), audit.New(pool))
	createdWorkspace, err := workspaceService.Bootstrap(ctx, userID, "Import target", "import-"+userID[len(userID)-10:])
	if err != nil {
		t.Fatalf("bootstrap workspace: %v", err)
	}
	actor, err := workspaceService.ActorForUser(ctx, createdWorkspace.ID, userID)
	if err != nil {
		t.Fatalf("resolve actor: %v", err)
	}

	store, err := filemodule.NewLocalStore(t.TempDir(), 1<<20, []string{"text/csv"})
	if err != nil {
		t.Fatalf("create file store: %v", err)
	}
	files := filemodule.New(pool, store)
	content := []byte("external_id,name,email,phone\nuser-1,Alice,alice@example.com,+250780000001\nuser-2,Bob,bob@example.com,\n")
	fileRecord, err := files.Create(ctx, createdWorkspace.ID, filemodule.UploadInput{
		Name: "customers.csv", MIMEType: "text/csv", SizeBytes: int64(len(content)), Body: bytes.NewReader(content),
		OwnerType: "workspace", OwnerID: createdWorkspace.ID, UploadedByType: "user", UploadedByID: actor.MemberID,
	})
	if err != nil {
		t.Fatalf("upload CSV: %v", err)
	}

	customers := customer.New(pool, events.New(pool), audit.New(pool), config.Default().Limits)
	service := New(pool, files, nil)
	service.SetCustomerImporter(customers)
	request, err := service.CreateImport(ctx, createdWorkspace.ID, actor.MemberID, fileRecord.ID, KindCustomersCSV, nil, false)
	if err != nil {
		t.Fatalf("create CSV import: %v", err)
	}
	preview, err := service.PreviewImport(ctx, createdWorkspace.ID, request.ID)
	if err != nil {
		t.Fatalf("preview CSV import: %v", err)
	}
	if len(preview) != 1 || preview[0].Rows != 2 || preview[0].Existing != 0 || preview[0].New != 2 {
		t.Fatalf("unexpected CSV preview: %+v", preview)
	}
	if err := service.RunImport(ctx, request.ID); err != nil {
		t.Fatalf("run CSV import: %v", err)
	}
	if err := service.RunImport(ctx, request.ID); err != nil {
		t.Fatalf("retry CSV import: %v", err)
	}

	var customerCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM customers WHERE workspace_id=$1`, createdWorkspace.ID).Scan(&customerCount); err != nil {
		t.Fatalf("count imported customers: %v", err)
	}
	if customerCount != 2 {
		t.Fatalf("imported customer count = %d, want 2", customerCount)
	}
	var state string
	if err := pool.QueryRow(ctx, `SELECT state FROM import_requests WHERE workspace_id=$1 AND id=$2`, createdWorkspace.ID, request.ID).Scan(&state); err != nil {
		t.Fatalf("read import state: %v", err)
	}
	if state != "completed" {
		t.Fatalf("import state = %q, want completed", state)
	}

	updateContent := []byte("external_id,name,email\nuser-1,Alice Updated,alice@example.com\n")
	updateFile, err := files.Create(ctx, createdWorkspace.ID, filemodule.UploadInput{
		Name: "customers-update.csv", MIMEType: "text/csv", SizeBytes: int64(len(updateContent)), Body: bytes.NewReader(updateContent),
		OwnerType: "workspace", OwnerID: createdWorkspace.ID, UploadedByType: "user", UploadedByID: actor.MemberID,
	})
	if err != nil {
		t.Fatalf("upload customer update CSV: %v", err)
	}
	updateRequest, err := service.CreateImport(ctx, createdWorkspace.ID, actor.MemberID, updateFile.ID, KindCustomersCSV, nil, false)
	if err != nil {
		t.Fatalf("create customer update import: %v", err)
	}
	updatePreview, err := service.PreviewImport(ctx, createdWorkspace.ID, updateRequest.ID)
	if err != nil {
		t.Fatalf("preview customer update import: %v", err)
	}
	if len(updatePreview) != 1 || updatePreview[0].Existing != 1 || updatePreview[0].New != 0 {
		t.Fatalf("unexpected customer update preview: %+v", updatePreview)
	}
	if err := service.RunImport(ctx, updateRequest.ID); err != nil {
		t.Fatalf("run customer update import: %v", err)
	}

	companyContent := []byte("external_id,name,domain,tier\ncompany-1,Acme,acme.example,pro\ncompany-2,Globex,globex.example,team\n")
	companyFile, err := files.Create(ctx, createdWorkspace.ID, filemodule.UploadInput{
		Name: "companies.csv", MIMEType: "text/csv", SizeBytes: int64(len(companyContent)), Body: bytes.NewReader(companyContent),
		OwnerType: "workspace", OwnerID: createdWorkspace.ID, UploadedByType: "user", UploadedByID: actor.MemberID,
	})
	if err != nil {
		t.Fatalf("upload company CSV: %v", err)
	}
	companyRequest, err := service.CreateImport(ctx, createdWorkspace.ID, actor.MemberID, companyFile.ID, KindCompaniesCSV, nil, false)
	if err != nil {
		t.Fatalf("create company import: %v", err)
	}
	if err := service.RunImport(ctx, companyRequest.ID); err != nil {
		t.Fatalf("run company import: %v", err)
	}
	var companyCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM companies WHERE workspace_id=$1`, createdWorkspace.ID).Scan(&companyCount); err != nil {
		t.Fatalf("count imported companies: %v", err)
	}
	if companyCount != 2 {
		t.Fatalf("imported company count = %d, want 2", companyCount)
	}
}
