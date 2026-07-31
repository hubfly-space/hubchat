//go:build integration

package savedview

import (
	"testing"

	"github.com/hubchat/hubchat/internal/database/dbtest"
)

func TestViewCRUDIsWorkspaceScopedAndNormalizesTargets(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES
			('wrk_savedview_a','Saved views A','savedview-a'), ('wrk_savedview_b','Saved views B','savedview-b');
		INSERT INTO users (id,name,email) VALUES ('usr_savedview_a','Saved view owner','savedview-a@example.com');
		INSERT INTO workspace_members (id,workspace_id,user_id,role) VALUES ('mem_savedview_a','wrk_savedview_a','usr_savedview_a','agent');
	`); err != nil {
		t.Fatal(err)
	}

	service := New(pool, nil, nil)
	created, err := service.Create(ctx, "wrk_savedview_a", "mem_savedview_a", Input{
		Name:    "Waiting queue",
		Filters: map[string]any{"match": "all"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Scope != "personal" || created.OwnerID == nil || *created.OwnerID != "mem_savedview_a" {
		t.Fatalf("created personal view target = %+v", created)
	}

	created, err = service.Update(ctx, "wrk_savedview_a", "mem_savedview_a", "agent", created.ID, Input{
		Name:       "Waiting on support",
		EntityType: "conversation",
		Scope:      "personal",
		Filters:    map[string]any{"conditions": []any{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "Waiting on support" {
		t.Fatalf("updated view = %+v", created)
	}
	if _, err := service.Get(ctx, "wrk_savedview_b", "mem_savedview_a", "agent", created.ID); err != ErrNotFound {
		t.Fatalf("cross-workspace get error = %v, want ErrNotFound", err)
	}
	if err := service.Delete(ctx, "wrk_savedview_a", "mem_savedview_a", "agent", created.ID); err != nil {
		t.Fatal(err)
	}
}
