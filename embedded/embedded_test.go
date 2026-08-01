package embedded

import (
	"encoding/json"
	"io/fs"
	"testing"
)

func TestProductionAssetsAreEmbedded(t *testing.T) {
	tests := []struct {
		name string
		load func() (fs.FS, error)
		file string
	}{
		{name: "dashboard", load: Dashboard, file: "index.html"},
		{name: "portal", load: Portal, file: "index.html"},
		{name: "widget", load: Widget, file: "app.js"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bundle, err := test.load()
			if err != nil {
				t.Fatalf("load embedded bundle: %v", err)
			}
			payload, err := fs.ReadFile(bundle, test.file)
			if err != nil {
				t.Fatalf("read embedded %s: %v", test.file, err)
			}
			if len(payload) == 0 {
				t.Fatalf("embedded %s is empty", test.file)
			}
		})
	}

	if !json.Valid(OpenAPI()) {
		t.Fatal("embedded OpenAPI document is not valid JSON")
	}
}
