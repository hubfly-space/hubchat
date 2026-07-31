package config

import "testing"

func TestParseRoles(t *testing.T) {
	roles, err := ParseRoles("worker, scheduler")
	if err != nil {
		t.Fatalf("ParseRoles returned error: %v", err)
	}
	if len(roles) != 2 || roles[0] != RoleWorker || roles[1] != RoleScheduler {
		t.Fatalf("ParseRoles returned %#v, want worker and scheduler", roles)
	}

	roles, err = ParseRoles("all")
	if err != nil {
		t.Fatalf("ParseRoles(all) returned error: %v", err)
	}
	if len(roles) != len(AllRoles) {
		t.Fatalf("ParseRoles(all) returned %d roles, want %d", len(roles), len(AllRoles))
	}

	if _, err := ParseRoles("worker,unknown"); err == nil {
		t.Fatal("ParseRoles accepted an unknown role")
	}
}

func TestServerHas(t *testing.T) {
	server := Server{Roles: []Role{RoleWorker}}
	if !server.Has(RoleWorker) {
		t.Fatal("Server.Has did not find worker role")
	}
	if server.Has(RoleHTTP) {
		t.Fatal("Server.Has reported an unconfigured HTTP role")
	}
}
