//go:build integration

package audit

import (
	"bytes"
	"encoding/csv"
	"testing"

	"github.com/hubchat/hubchat/internal/database/dbtest"
)

func TestWriteCSVIsWorkspaceScopedAndFilterable(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id,name,slug) VALUES ('wrk_audit_a','Audit A','audit-a'),('wrk_audit_b','Audit B','audit-b')`); err != nil {
		t.Fatalf("seed workspaces: %v", err)
	}
	log := New(pool)
	for _, entry := range []Entry{
		{WorkspaceID: "wrk_audit_a", ActorType: ActorUser, ActorID: "mem_a", ActorName: "Alice", Action: WorkspaceUpdated, EntityType: "workspace", EntityID: "wrk_audit_a", Metadata: map[string]any{"field": "name"}},
		{WorkspaceID: "wrk_audit_a", ActorType: ActorSystem, ActorName: "System", Action: DataExported, EntityType: "export", EntityID: "exp_a"},
		{WorkspaceID: "wrk_audit_b", ActorType: ActorUser, ActorID: "mem_b", ActorName: "Bob", Action: WorkspaceUpdated, EntityType: "workspace", EntityID: "wrk_audit_b"},
	} {
		if err := log.Record(ctx, entry); err != nil {
			t.Fatalf("record audit entry: %v", err)
		}
	}

	var output bytes.Buffer
	if err := log.WriteCSV(ctx, "wrk_audit_a", Filter{Action: WorkspaceUpdated}, &output); err != nil {
		t.Fatalf("write audit CSV: %v", err)
	}
	rows, err := csv.NewReader(&output).ReadAll()
	if err != nil {
		t.Fatalf("read audit CSV: %v", err)
	}
	if len(rows) != 2 || rows[0][0] != "id" || rows[1][3] != string(ActorUser) || rows[1][5] != "Alice" {
		t.Fatalf("unexpected audit CSV: %+v", rows)
	}

	first, err := log.List(ctx, "wrk_audit_a", Filter{Limit: 1})
	if err != nil {
		t.Fatalf("list first audit page: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("expected one audit row on first page, got %d", len(first))
	}
	second, err := log.List(ctx, "wrk_audit_a", Filter{Before: first[0].OccurredAt, BeforeID: first[0].ID, Limit: 1})
	if err != nil {
		t.Fatalf("list second audit page: %v", err)
	}
	if len(second) != 1 || second[0].WorkspaceID != "wrk_audit_a" || second[0].ID == first[0].ID {
		t.Fatalf("expected the next workspace-scoped audit row, got %+v", second)
	}
}
