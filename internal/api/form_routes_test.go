package api

import (
	"testing"

	"github.com/hubchat/hubchat/internal/form"
)

func TestFormJSONPublicResponseOmitsWorkspaceAndRoutingDetails(t *testing.T) {
	item := form.Form{
		ID: "frm_public", WorkspaceID: "wrk_secret", Name: "Contact", Slug: "contact",
		Routing:        map[string]any{"inbox_id": "inb_internal"},
		SpamProtection: map[string]any{"rate_limit_per_hour": 1}, MaxSubmissions: intPointer(5), Enabled: true,
	}
	public := formJSON(item, false)
	for _, key := range []string{"workspace_id", "routing", "spam_protection", "submission_count", "enabled", "max_submissions", "created_at", "updated_at"} {
		if _, ok := public[key]; ok {
			t.Fatalf("public form response exposed internal key %q: %#v", key, public[key])
		}
	}
	if public["name"] != "Contact" || public["slug"] != "contact" {
		t.Fatalf("public form response lost public fields: %#v", public)
	}
}

func intPointer(value int) *int { return &value }
