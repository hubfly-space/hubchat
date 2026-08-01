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

func TestOAuthBuiltInProfilesSupplySafeDefaults(t *testing.T) {
	google := OAuth{Enabled: true, Provider: "google", ClientID: "id", ClientSecret: "secret"}
	if err := applyOAuthProfileDefaults(&google); err != nil {
		t.Fatal(err)
	}
	if google.Profile != OAuthProfileGoogle || google.AuthorizationURL != "https://accounts.google.com/o/oauth2/v2/auth" || google.UserinfoURL != "https://openidconnect.googleapis.com/v1/userinfo" {
		t.Fatalf("google defaults = %+v", google)
	}

	microsoft := OAuth{Enabled: true, Provider: "microsoft", ClientID: "id", ClientSecret: "secret"}
	if err := applyOAuthProfileDefaults(&microsoft); err != nil {
		t.Fatal(err)
	}
	if microsoft.Profile != OAuthProfileMicrosoft || microsoft.TokenURL != "https://login.microsoftonline.com/common/oauth2/v2.0/token" {
		t.Fatalf("microsoft defaults = %+v", microsoft)
	}
	if len(microsoft.Scopes) != 4 || microsoft.Scopes[3] != "User.Read" {
		t.Fatalf("microsoft scopes = %v", microsoft.Scopes)
	}
}

func TestOAuthGenericProfileRejectsUnknownProfile(t *testing.T) {
	if err := applyOAuthProfileDefaults(&OAuth{Provider: "acme", Profile: "unknown"}); err == nil {
		t.Fatal("unknown OAuth profile was accepted")
	}
}
