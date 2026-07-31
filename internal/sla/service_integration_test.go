//go:build integration

package sla

import (
	"testing"

	"github.com/hubchat/hubchat/internal/database/dbtest"
)

func TestUpdateCalendarReplacesScheduleAndIsWorkspaceScoped(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id, name, slug) VALUES
			('wrk_sla_a', 'SLA A', 'sla-a'),
			('wrk_sla_b', 'SLA B', 'sla-b')
	`); err != nil {
		t.Fatal(err)
	}

	service := New(pool)
	weekly := [7][]Window{}
	weekly[0] = []Window{{Start: "09:00", End: "17:00"}}
	calendar, err := service.CreateCalendar(ctx, "wrk_sla_a", CalendarInput{
		Name: "Support hours", Timezone: "UTC", Weekly: weekly, IsDefault: true,
		Holidays: []Holiday{{Date: "2026-08-03", Name: "Founders day"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	weekly[0] = []Window{{Start: "10:00", End: "18:00"}}
	weekly[1] = []Window{{Start: "10:00", End: "14:00"}}
	updated, err := service.UpdateCalendar(ctx, "wrk_sla_a", calendar.ID, CalendarInput{
		Name: "Updated support hours", Timezone: "Africa/Kigali", Weekly: weekly, IsDefault: true,
		Holidays: []Holiday{{Date: "2026-12-25", Name: "Holiday"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Updated support hours" || updated.Timezone != "Africa/Kigali" || len(updated.Holidays) != 1 || updated.Holidays[0].Date != "2026-12-25" {
		t.Fatalf("updated calendar = %+v", updated)
	}
	if len(updated.Weekly[0]) != 1 || updated.Weekly[0][0].Start != "10:00" || len(updated.Weekly[1]) != 1 {
		t.Fatalf("updated schedule = %+v", updated.Weekly)
	}
	if _, err := service.GetCalendar(ctx, "wrk_sla_b", calendar.ID); err != ErrNotFound {
		t.Fatalf("cross-workspace calendar lookup error = %v", err)
	}
}
