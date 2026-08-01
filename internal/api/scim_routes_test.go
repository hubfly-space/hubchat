package api

import "testing"

func TestParseSCIMFilter(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		wantUser string
		wantID   string
		wantErr  bool
	}{
		{name: "empty", value: "", wantUser: "", wantID: ""},
		{name: "username", value: `userName eq "Agent@Example.com"`, wantUser: "agent@example.com"},
		{name: "external id", value: `externalId eq "directory-123"`, wantID: "directory-123"},
		{name: "unsupported attribute", value: `displayName eq "Agent"`, wantErr: true},
		{name: "unquoted", value: "userName eq agent@example.com", wantErr: true},
		{name: "unsupported operator", value: `userName ne "agent@example.com"`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userName, externalID, err := parseSCIMFilter(tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, want error=%v", err, tt.wantErr)
			}
			if userName != tt.wantUser || externalID != tt.wantID {
				t.Fatalf("filter = (%q, %q), want (%q, %q)", userName, externalID, tt.wantUser, tt.wantID)
			}
		})
	}
}
