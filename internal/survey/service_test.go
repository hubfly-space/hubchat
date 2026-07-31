package survey

import (
	"errors"
	"testing"
)

func TestValidateQuestionsNormalizesStarsAndPositions(t *testing.T) {
	questions, err := validateQuestions([]QuestionInput{{Prompt: "How was it?", Type: "stars"}, {Prompt: "Why?", Type: "text"}})
	if err != nil { t.Fatalf("validate questions: %v", err) }
	if questions[0].Type != "star" || questions[0].Position != 0 || questions[1].Position != 1 { t.Fatalf("questions were not normalized: %+v", questions) }
}

func TestValidateQuestionsRejectsUnknownType(t *testing.T) {
	_, err := validateQuestions([]QuestionInput{{Prompt: "Bad", Type: "classifier"}})
	if !errors.Is(err, ErrInvalidQuestion) { t.Fatalf("expected invalid question, got %v", err) }
}

func TestValidateAnswersRequiresRequiredValues(t *testing.T) {
	err := validateAnswers([]Question{{ID: "q1", Required: true, Type: "text"}}, map[string]any{})
	if !errors.Is(err, ErrInvalidQuestion) { t.Fatalf("expected required answer error, got %v", err) }
}

func TestValidateAnswersChecksChoiceOptions(t *testing.T) {
	err := validateAnswers([]Question{{ID: "q1", Type: "choice", Options: []string{"yes", "no"}}}, map[string]any{"q1": "maybe"})
	if !errors.Is(err, ErrInvalidQuestion) { t.Fatalf("expected choice validation error, got %v", err) }
}
