package workspace

import (
	"errors"
	"testing"
)

func TestValidateLegalHold(t *testing.T) {
	valid := []LegalHoldInput{
		{Category: "all", Reason: "Incident review"},
		{Category: "events", Reason: "Preserve event history"},
		{Category: "sessions", Reason: "Regulatory request"},
	}
	for _, input := range valid {
		if err := validateLegalHold(input); err != nil {
			t.Errorf("validateLegalHold(%+v) = %v", input, err)
		}
	}
	for _, input := range []LegalHoldInput{
		{Category: "unknown", Reason: "reason"},
		{Category: "all", Reason: "   "},
		{Category: "all", Reason: string(make([]byte, 501))},
	} {
		if err := validateLegalHold(input); !errors.Is(err, ErrInvalidLegalHold) {
			t.Errorf("validateLegalHold(%+v) = %v, want ErrInvalidLegalHold", input, err)
		}
	}
}
