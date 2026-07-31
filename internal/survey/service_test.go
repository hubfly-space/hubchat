package survey

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestValidateQuestionsNormalizesStarsAndPositions(t *testing.T) {
	questions, err := validateQuestions([]QuestionInput{{Prompt: "How was it?", Type: "stars"}, {Prompt: "Why?", Type: "text"}})
	if err != nil {
		t.Fatalf("validate questions: %v", err)
	}
	if questions[0].Type != "star" || questions[0].Position != 0 || questions[1].Position != 1 {
		t.Fatalf("questions were not normalized: %+v", questions)
	}
}

func TestValidateQuestionsRejectsUnknownType(t *testing.T) {
	_, err := validateQuestions([]QuestionInput{{Prompt: "Bad", Type: "classifier"}})
	if !errors.Is(err, ErrInvalidQuestion) {
		t.Fatalf("expected invalid question, got %v", err)
	}
}

func TestValidateAnswersRequiresRequiredValues(t *testing.T) {
	err := validateAnswers([]Question{{ID: "q1", Required: true, Type: "text"}}, map[string]any{})
	if !errors.Is(err, ErrInvalidQuestion) {
		t.Fatalf("expected required answer error, got %v", err)
	}
}

func TestValidateAnswersChecksChoiceOptions(t *testing.T) {
	err := validateAnswers([]Question{{ID: "q1", Type: "choice", Options: []string{"yes", "no"}}}, map[string]any{"q1": "maybe"})
	if !errors.Is(err, ErrInvalidQuestion) {
		t.Fatalf("expected choice validation error, got %v", err)
	}
}

func TestTriggerMatchesResolutionDefaultsToResolved(t *testing.T) {
	if !triggerMatchesResolution(map[string]any{}, "resolved") {
		t.Fatal("empty trigger should default to ticket.resolved")
	}
	if triggerMatchesResolution(map[string]any{}, "closed") {
		t.Fatal("empty trigger should not match closed")
	}
}

func TestTriggerMatchesResolutionSupportsClosedAndStatusChanged(t *testing.T) {
	var trigger map[string]any
	if err := json.Unmarshal([]byte(`{"event":"ticket.closed"}`), &trigger); err != nil {
		t.Fatal(err)
	}
	if !triggerMatchesResolution(trigger, "closed") || triggerMatchesResolution(trigger, "resolved") {
		t.Fatal("closed trigger matched the wrong lifecycle state")
	}
	trigger = map[string]any{"on": "ticket.status_changed"}
	if !triggerMatchesResolution(trigger, "resolved") || !triggerMatchesResolution(trigger, "closed") {
		t.Fatal("status_changed trigger did not match terminal states")
	}
}
