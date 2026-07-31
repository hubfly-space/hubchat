//go:build integration

package analytics

import (
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
)

func TestFoldWorkspacePromotesWidgetSurfaceEvents(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id,name,slug) VALUES ('wrk_analytics_surface','Analytics surface','analytics-surface')`); err != nil {
		t.Fatal(err)
	}

	log := events.New(pool)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.Append(ctx, tx, events.Event{
		WorkspaceID: "wrk_analytics_surface",
		Type:        events.EventReceived,
		EntityType:  "customer",
		ActorType:   events.ActorVisitor,
		Data:        map[string]any{"type": "widget.impression", "source": "js_sdk"},
	}); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	service := New(pool)
	if folded, err := service.FoldWorkspace(ctx, "wrk_analytics_surface", time.Now().UTC()); err != nil {
		t.Fatal(err)
	} else if folded != 1 {
		t.Fatalf("folded event count = %d, want 1", folded)
	}

	rollups, err := service.Rollups(ctx, "wrk_analytics_surface", "surfaces.widget.impressions", "day", time.Now().UTC().AddDate(0, 0, -1), time.Now().UTC().AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(rollups) != 1 || rollups[0].Value != 1 {
		t.Fatalf("surface rollups = %+v, want one impression", rollups)
	}
}
