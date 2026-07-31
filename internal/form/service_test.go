package form

import "testing"

func TestValidateDefinitionRejectsUnsafeSlugsAndDuplicateFields(t *testing.T) {
	base := CreateInput{Name: "Bug report", Slug: "bug-report", Purpose: "ticket", Access: "public"}
	if err := validateDefinition(base); err != nil {
		t.Fatalf("valid definition rejected: %v", err)
	}

	for _, slug := range []string{"Bug Report", "../admin", "ends-", "two--words"} {
		input := base
		input.Slug = slug
		if err := validateDefinition(input); err != ErrInvalidSlug {
			t.Errorf("slug %q: got %v, want ErrInvalidSlug", slug, err)
		}
	}

	duplicate := base
	duplicate.Fields = []FieldInput{
		{Key: "email", Label: "Email", Type: "email"},
		{Key: "email", Label: "Alternate email", Type: "email"},
	}
	if err := validateDefinition(duplicate); err != ErrInvalidField {
		t.Fatalf("duplicate field key: got %v, want ErrInvalidField", err)
	}
}

func TestValidateSubmissionEnforcesRequiredTypesAndOptions(t *testing.T) {
	fields := []Field{
		{Key: "email", Label: "Email", Type: "email", Required: true},
		{Key: "priority", Label: "Priority", Type: "enum", Options: []string{"low", "high"}, Required: true},
		{Key: "impact", Label: "Impact", Type: "rating", Required: false},
	}

	if err := validateSubmission(fields, map[string]any{"email": "person@example.com", "priority": "high", "impact": float64(5)}); err != nil {
		t.Fatalf("valid submission rejected: %v", err)
	}
	cases := []map[string]any{
		{"priority": "high"},
		{"email": "not-an-email", "priority": "high"},
		{"email": "person@example.com", "priority": "urgent"},
		{"email": "person@example.com", "priority": "low", "impact": float64(7)},
	}
	for i, values := range cases {
		if err := validateSubmission(fields, values); err == nil {
			t.Errorf("case %d: invalid submission was accepted", i)
		}
	}
}

func TestValidateSubmissionSkipsInactiveConditionalFields(t *testing.T) {
	fields := []Field{
		{Key: "kind", Label: "Kind", Type: "enum", Options: []string{"bug", "question"}, Required: true},
		{Key: "steps", Label: "Steps", Type: "text", Required: true, Condition: map[string]any{"field": "kind", "operator": "equals", "value": "bug"}},
	}
	if err := validateSubmission(fields, map[string]any{"kind": "question"}); err != nil {
		t.Fatalf("inactive conditional field should not be required: %v", err)
	}
	if err := validateSubmission(fields, map[string]any{"kind": "bug"}); err == nil {
		t.Fatal("active conditional field was not required")
	}
}
