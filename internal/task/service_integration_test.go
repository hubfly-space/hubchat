//go:build integration

package task

import (
	"errors"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/database/dbtest"
)

func TestTaskServiceScopesPagesAndTransitions(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES
			('wrk_task_a','Task A','task-a'),
			('wrk_task_b','Task B','task-b');
		INSERT INTO users (id,name,email) VALUES
			('usr_task_a','Task Agent A','task-a@example.test'),
			('usr_task_b','Task Agent B','task-b@example.test');
		INSERT INTO workspace_members (id,workspace_id,user_id,role) VALUES
			('mem_task_a','wrk_task_a','usr_task_a','agent'),
			('mem_task_b','wrk_task_b','usr_task_b','agent')
	`); err != nil {
		t.Fatal(err)
	}

	service := New(pool)
	first, err := service.Create(ctx, "wrk_task_a", "mem_task_a", Input{Title: "First follow-up", AssigneeID: "mem_task_a"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Create(ctx, "wrk_task_a", "mem_task_a", Input{Title: "Second follow-up", DueAt: timePointer(time.Now().UTC().Add(-time.Hour))})
	if err != nil {
		t.Fatal(err)
	}
	other, err := service.Create(ctx, "wrk_task_b", "mem_task_b", Input{Title: "Other workspace"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Get(ctx, "wrk_task_a", other.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-workspace get error = %v, want ErrNotFound", err)
	}
	if _, err := service.Create(ctx, "wrk_task_a", "mem_task_a", Input{Title: "Invalid assignee", AssigneeID: "mem_task_b"}); !errors.Is(err, ErrInvalidAssignee) {
		t.Fatalf("cross-workspace assignee error = %v, want ErrInvalidAssignee", err)
	}

	if _, err := service.Update(ctx, "wrk_task_a", first.ID, UpdateInput{State: stringPointer(StateCompleted)}); err != nil {
		t.Fatalf("complete task: %v", err)
	}
	if _, err := service.Update(ctx, "wrk_task_a", first.ID, UpdateInput{State: stringPointer(StateCancelled)}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("completed to cancelled error = %v, want ErrInvalidState", err)
	}
	reopened, err := service.Update(ctx, "wrk_task_a", first.ID, UpdateInput{State: stringPointer(StateOpen)})
	if err != nil || reopened.State != StateOpen {
		t.Fatalf("reopen task = %#v, err=%v", reopened, err)
	}

	items, err := service.ListPage(ctx, "wrk_task_a", "", "", "", false, time.Time{}, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].WorkspaceID != "wrk_task_a" || items[1].WorkspaceID != "wrk_task_a" {
		t.Fatalf("workspace task list = %#v", items)
	}
	for _, item := range items {
		if item.ID == first.ID && item.AssigneeName != "Task Agent A" {
			t.Fatalf("task assignee name = %q, items = %#v", item.AssigneeName, items)
		}
	}
	overdue, err := service.ListPage(ctx, "wrk_task_a", "", "", "", true, time.Time{}, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(overdue) != 1 || overdue[0].ID != second.ID {
		t.Fatalf("overdue task list = %#v", overdue)
	}

	// Equal timestamps still paginate without repeating or skipping because id
	// is the deterministic tiebreaker.
	if _, err := pool.Exec(ctx, `UPDATE tasks SET created_at='2026-08-01T12:00:00Z' WHERE id IN ($1,$2)`, first.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	page, err := service.ListPage(ctx, "wrk_task_a", "", "", "", false, time.Time{}, "", 1)
	if err != nil || len(page) != 1 {
		t.Fatalf("first task page = %#v, err=%v", page, err)
	}
	pageTwo, err := service.ListPage(ctx, "wrk_task_a", "", "", "", false, page[0].CreatedAt, page[0].ID, 1)
	if err != nil || len(pageTwo) != 1 || pageTwo[0].ID == page[0].ID {
		t.Fatalf("second task page = %#v, err=%v", pageTwo, err)
	}
}

func stringPointer(value string) *string { return &value }

func timePointer(value time.Time) *time.Time { return &value }
