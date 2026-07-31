package sla

import (
	"testing"
	"time"
)

func TestSubjectSLARemainingUsesLiveDeadlineForRunningTimers(t *testing.T) {
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)
	deadline := now.Add(90 * time.Second)
	got := subjectSLARemaining(subjectSLARow{
		State:          "active",
		DeadlineAt:     &deadline,
		TargetMinutes:  30,
		ElapsedMinutes: 1,
	}, now)
	if got != 90 {
		t.Fatalf("remaining = %d seconds, want 90", got)
	}
}

func TestSubjectSLARemainingUsesStoredElapsedForPausedTimers(t *testing.T) {
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)
	deadline := now.Add(90 * time.Second)
	got := subjectSLARemaining(subjectSLARow{
		State:          "paused",
		DeadlineAt:     &deadline,
		TargetMinutes:  30,
		ElapsedMinutes: 7,
	}, now)
	if got != 23*60 {
		t.Fatalf("remaining = %d seconds, want %d", got, 23*60)
	}
}

func TestMergeSubjectSLACombinesTimersAndKeepsMostUrgentState(t *testing.T) {
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)
	firstDeadline := now.Add(10 * time.Minute)
	resolutionDeadline := now.Add(2 * time.Hour)
	result := map[string]*SubjectSLA{}

	mergeSubjectSLA(result, subjectSLARow{
		SubjectID: "conv_1", PolicyID: "sla_1", Kind: "first_response", State: "active", DeadlineAt: &firstDeadline,
	}, now)
	mergeSubjectSLA(result, subjectSLARow{
		SubjectID: "conv_1", PolicyID: "sla_1", Kind: "resolution", State: "breached", DeadlineAt: &resolutionDeadline,
	}, now)

	item := result["conv_1"]
	if item == nil || item.State != "breached" || item.PolicyID != "sla_1" {
		t.Fatalf("summary = %#v, want breached summary for sla_1", item)
	}
	if item.FirstResponseRemaining == nil || *item.FirstResponseRemaining != 10*60 {
		t.Fatalf("first response remaining = %#v, want %d", item.FirstResponseRemaining, 10*60)
	}
	if item.ResolutionRemaining == nil || *item.ResolutionRemaining != 2*60*60 {
		t.Fatalf("resolution remaining = %#v, want %d", item.ResolutionRemaining, 2*60*60)
	}
}

func TestSubjectSLAStateExposesWarnedActiveTimerAsApproaching(t *testing.T) {
	warnedAt := time.Date(2026, time.July, 31, 9, 55, 0, 0, time.UTC)
	got := subjectSLAState(subjectSLARow{State: "active", WarnedAt: &warnedAt})
	if got != "approaching" {
		t.Fatalf("state = %q, want approaching", got)
	}
}
