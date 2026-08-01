package main

import (
	"testing"

	"github.com/hubchat/hubchat/internal/config"
)

func TestApplyServeArgs(t *testing.T) {
	cfg := config.Config{Server: config.Server{Roles: config.AllRoles}}
	if err := applyServeArgs(&cfg, []string{"--roles=worker,scheduler"}); err != nil {
		t.Fatalf("applyServeArgs returned error: %v", err)
	}
	if len(cfg.Server.Roles) != 2 || cfg.Server.Roles[0] != config.RoleWorker || cfg.Server.Roles[1] != config.RoleScheduler {
		t.Fatalf("applyServeArgs returned %#v, want worker and scheduler", cfg.Server.Roles)
	}

	if err := applyServeArgs(&cfg, []string{"--roles", "http,realtime"}); err != nil {
		t.Fatalf("applyServeArgs with separate value returned error: %v", err)
	}
	if len(cfg.Server.Roles) != 2 || cfg.Server.Roles[0] != config.RoleHTTP || cfg.Server.Roles[1] != config.RoleRealtime {
		t.Fatalf("applyServeArgs returned %#v, want http and realtime", cfg.Server.Roles)
	}
}

func TestApplyServeArgsRejectsInvalidInput(t *testing.T) {
	cfg := config.Config{Server: config.Server{Roles: config.AllRoles}}
	for _, args := range [][]string{
		{"--roles"},
		{"--unknown"},
		{"--roles=invalid"},
	} {
		if err := applyServeArgs(&cfg, args); err == nil {
			t.Fatalf("applyServeArgs accepted %v", args)
		}
	}
}
