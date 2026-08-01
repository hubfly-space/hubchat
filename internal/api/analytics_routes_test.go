package api

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestAnalyticsWindowValidatesAndPreservesTimezone(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/v1/analytics/summary?timezone=America%2FNew_York", nil)
	from, to, timezone, err := analyticsWindow(r)
	if err != nil {
		t.Fatalf("analytics window error = %v", err)
	}
	if timezone != "America/New_York" {
		t.Fatalf("timezone = %q", timezone)
	}
	if !from.Before(to) || to.Location() != time.UTC {
		t.Fatalf("window = %s..%s, want an ordered UTC window", from, to)
	}

	invalid := httptest.NewRequest("GET", "/api/v1/analytics/summary?timezone=Not%2FATimezone", nil)
	if _, _, _, err := analyticsWindow(invalid); err == nil {
		t.Fatal("invalid timezone was accepted")
	}

	explicit := httptest.NewRequest("GET", "/api/v1/analytics/summary?from=2026-03-03T13:00:00Z&to=2026-03-10T12:00:00Z&timezone=Africa%2FKigali", nil)
	gotFrom, gotTo, gotTimezone, err := analyticsWindow(explicit)
	if err != nil {
		t.Fatalf("explicit analytics window error = %v", err)
	}
	if !gotFrom.Equal(time.Date(2026, time.March, 3, 13, 0, 0, 0, time.UTC)) || !gotTo.Equal(time.Date(2026, time.March, 10, 12, 0, 0, 0, time.UTC)) || gotTimezone != "Africa/Kigali" {
		t.Fatalf("explicit window = %s..%s timezone=%s", gotFrom, gotTo, gotTimezone)
	}
}
