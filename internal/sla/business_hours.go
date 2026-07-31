package sla

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

var (
	ErrInvalidTimezone = errors.New("sla: invalid calendar timezone")
	ErrInvalidWindow   = errors.New("sla: invalid business-hours window")
	ErrCalendarSearch  = errors.New("sla: business-hours search exceeded its safety bound")
)

// Window is a local-time interval in HH:MM notation. Weekly is Monday-first;
// an empty day is a non-working day. Keeping this representation close to the
// migration's JSON shape makes policy loading and arithmetic independently
// testable.
type Window struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// Calendar contains the immutable arithmetic inputs for one SLA calendar.
// Holidays use the calendar's local date format (YYYY-MM-DD).
type Calendar struct {
	Location *time.Location
	Weekly   [7][]Window
	Holidays map[string]struct{}
}

// NewCalendar validates the IANA zone and every weekly interval. The returned
// calendar owns a copy of the schedules, so callers can safely reuse their
// decoded request object.
func NewCalendar(timezone string, weekly [7][]Window, holidays []string) (*Calendar, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidTimezone, timezone)
	}
	copyWeekly := [7][]Window{}
	for day, windows := range weekly {
		copyWeekly[day] = append([]Window(nil), windows...)
		for _, window := range windows {
			if _, _, err := parseWindow(window); err != nil {
				return nil, err
			}
		}
	}
	copyHolidays := make(map[string]struct{}, len(holidays))
	for _, holiday := range holidays {
		parsed, err := time.ParseInLocation("2006-01-02", holiday, location)
		if err != nil || parsed.Format("2006-01-02") != holiday {
			return nil, fmt.Errorf("%w: holiday %q", ErrInvalidWindow, holiday)
		}
		copyHolidays[holiday] = struct{}{}
	}
	return &Calendar{Location: location, Weekly: copyWeekly, Holidays: copyHolidays}, nil
}

// Add advances start by duration spent inside working windows. Non-working
// time—including weekends, holidays, and gaps between windows—is skipped.
func (c *Calendar) Add(start time.Time, duration time.Duration) (time.Time, error) {
	if c == nil || c.Location == nil {
		return time.Time{}, ErrInvalidTimezone
	}
	if duration <= 0 {
		return start, nil
	}
	cursor := start.In(c.Location)
	remaining := duration
	for days := 0; days < 36600; days++ { // bounded at roughly a century
		for _, interval := range c.intervals(cursor) {
			if !cursor.Before(interval.end) {
				continue
			}
			if cursor.Before(interval.start) {
				cursor = interval.start
			}
			available := interval.end.Sub(cursor)
			if available >= remaining {
				return cursor.Add(remaining).In(start.Location()), nil
			}
			remaining -= available
			cursor = interval.end
		}
		cursor = nextLocalDay(cursor)
	}
	return time.Time{}, ErrCalendarSearch
}

// Elapsed returns the working duration between start and end. It is the
// inverse operation used for reporting and for persisting SLA elapsed time.
func (c *Calendar) Elapsed(start, end time.Time) (time.Duration, error) {
	if c == nil || c.Location == nil {
		return 0, ErrInvalidTimezone
	}
	if !end.After(start) {
		return 0, nil
	}
	left := start.In(c.Location)
	right := end.In(c.Location)
	var total time.Duration
	for days := 0; days < 36600; days++ {
		for _, interval := range c.intervals(left) {
			from, to := left, right
			if from.Before(interval.start) {
				from = interval.start
			}
			if to.After(interval.end) {
				to = interval.end
			}
			if to.After(from) {
				total += to.Sub(from)
			}
		}
		if !left.Before(right) || left.AddDate(0, 0, 1).After(right) {
			break
		}
		left = nextLocalDay(left)
	}
	if !right.Before(left) && total == 0 && right.Sub(start.In(c.Location)) > 36600*24*time.Hour {
		return 0, ErrCalendarSearch
	}
	return total, nil
}

type interval struct{ start, end time.Time }

func (c *Calendar) intervals(day time.Time) []interval {
	local := day.In(c.Location)
	if _, holiday := c.Holidays[local.Format("2006-01-02")]; holiday {
		return nil
	}
	windows := append([]Window(nil), c.Weekly[weekdayIndex(local)]...)
	sort.SliceStable(windows, func(i, j int) bool {
		a, _, _ := parseWindow(windows[i])
		b, _, _ := parseWindow(windows[j])
		return a < b
	})
	result := make([]interval, 0, len(windows))
	for _, window := range windows {
		startMinute, endMinute, _ := parseWindow(window)
		year, month, day := local.Date()
		begin := time.Date(year, month, day, startMinute/60, startMinute%60, 0, 0, c.Location)
		end := time.Date(year, month, day, endMinute/60, endMinute%60, 0, 0, c.Location)
		if endMinute <= startMinute {
			next := time.Date(year, month, day+1, endMinute/60, endMinute%60, 0, 0, c.Location)
			end = next
		}
		result = append(result, interval{start: begin, end: end})
	}
	return result
}

func parseWindow(window Window) (start, end int, err error) {
	start, err = parseMinute(window.Start)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: start %q", ErrInvalidWindow, window.Start)
	}
	end, err = parseMinute(window.End)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: end %q", ErrInvalidWindow, window.End)
	}
	if start == end {
		return 0, 0, fmt.Errorf("%w: zero-length interval", ErrInvalidWindow)
	}
	return start, end, nil
}

func parseMinute(value string) (int, error) {
	if value == "24:00" {
		return 24 * 60, nil
	}
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return 0, err
	}
	return parsed.Hour()*60 + parsed.Minute(), nil
}

func localMidnight(value time.Time) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, value.Location())
}

func nextLocalDay(value time.Time) time.Time {
	return localMidnight(value).AddDate(0, 0, 1)
}

func weekdayIndex(value time.Time) int {
	day := int(value.Weekday())
	if day == 0 { // Sunday follows Saturday in time.Weekday, but weekly is Monday-first.
		return 6
	}
	return day - 1
}
