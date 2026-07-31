package api

import (
	"strings"
	"testing"

	"github.com/hubchat/hubchat/internal/config"
)

func TestStorageReadiness(t *testing.T) {
	tests := []struct {
		name   string
		config config.Config
		ready  bool
		want   string
	}{
		{
			name: "local directory may be created on first upload",
			config: config.Config{Storage: config.Storage{
				Backend:   "local",
				LocalPath: t.TempDir() + "/files",
			}},
			ready: true,
			want:  "created on the first upload",
		},
		{
			name: "local path must be a directory",
			config: config.Config{Storage: config.Storage{
				Backend:   "local",
				LocalPath: t.TempDir(),
			}},
			ready: true,
			want:  "available",
		},
		{
			name: "unsupported backend fails",
			config: config.Config{Storage: config.Storage{
				Backend: "filesystem",
			}},
			ready: false,
			want:  "not supported",
		},
		{
			name: "S3 requires credentials",
			config: config.Config{Storage: config.Storage{
				Backend: "s3",
			}},
			ready: false,
			want:  "missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ready, detail := storageReadiness(Deps{Config: tt.config})
			if ready != tt.ready {
				t.Fatalf("storageReadiness() ready = %v, want %v", ready, tt.ready)
			}
			if !strings.Contains(detail, tt.want) {
				t.Fatalf("storageReadiness() detail = %q, want it to contain %q", detail, tt.want)
			}
		})
	}
}
