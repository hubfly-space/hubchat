package sla

import (
	"testing"
	"time"
)

func weekdayWindows() [7][]Window {
	var weekly [7][]Window
	for day := 0; day < 5; day++ {
		weekly[day] = []Window{{Start: "09:00", End: "17:00"}}
	}
	return weekly
}

func TestCalendarAddSkipsWeekendAndGaps(t *testing.T) {
	calendar, err := NewCalendar("UTC", weekdayWindows(), nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, time.July, 31, 16, 0, 0, 0, time.UTC) // Friday
	got, err := calendar.Add(start, 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, time.August, 3, 10, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("deadline = %s, want %s", got, want)
	}
}

func TestCalendarElapsedHonoursHoliday(t *testing.T) {
	calendar, err := NewCalendar("UTC", weekdayWindows(), []string{"2026-08-03"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, time.July, 31, 16, 0, 0, 0, time.UTC)
	end := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)
	got, err := calendar.Elapsed(start, end)
	if err != nil {
		t.Fatal(err)
	}
	if got != 2*time.Hour {
		t.Fatalf("elapsed = %s, want 2h", got)
	}
}

func TestCalendarUsesLocalWallClockAcrossDST(t *testing.T) {
	var weekly [7][]Window
	for day := 0; day < 5; day++ {
		weekly[day] = []Window{{Start: "09:00", End: "17:00"}}
	}
	calendar, err := NewCalendar("America/New_York", weekly, nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, time.March, 6, 21, 0, 0, 0, time.UTC) // Friday 16:00 local, before spring-forward
	got, err := calendar.Add(start, 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// Build the expected value in the calendar's zone to make the assertion
	// independent of the machine running the test.
	want := time.Date(2026, time.March, 9, 10, 0, 0, 0, calendar.Location)
	if !got.Equal(want) {
		t.Fatalf("deadline = %s, want %s", got, want)
	}
}

func TestCalendarRejectsMalformedWindowsAndHolidays(t *testing.T) {
	weekly := weekdayWindows()
	weekly[0] = []Window{{Start: "9am", End: "17:00"}}
	if _, err := NewCalendar("UTC", weekly, nil); err == nil {
		t.Fatal("malformed window was accepted")
	}
	if _, err := NewCalendar("UTC", weekdayWindows(), []string{"08/03/2026"}); err == nil {
		t.Fatal("malformed holiday was accepted")
	}
}
