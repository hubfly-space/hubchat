//go:build integration

package auth_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/auth"
	"github.com/hubchat/hubchat/internal/database/dbtest"
)

func TestOAuthStateIsSingleUseAndLinksVerifiedIdentity(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "access-token"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sub": "provider-user-1", "email": "sso@example.com", "name": "SSO Person", "email_verified": true,
		})
	}))
	defer provider.Close()

	svc := auth.New(pool, auth.Options{
		SessionLifetime: time.Hour,
		OAuth: &auth.OAuthOptions{
			Provider:         "test",
			ClientID:         "client",
			ClientSecret:     "secret",
			AuthorizationURL: provider.URL + "/authorize",
			TokenURL:         provider.URL + "/token",
			UserinfoURL:      provider.URL + "/userinfo",
			RedirectURL:      "https://hubchat.test/api/v1/auth/oauth/test/callback",
		},
	})

	authorizationURL, _, err := svc.BeginOAuth(ctx, "/overview")
	if err != nil {
		t.Fatalf("begin oauth: %v", err)
	}
	parsed, err := url.Parse(authorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	state := parsed.Query().Get("state")
	if state == "" || parsed.Query().Get("redirect_uri") == "" {
		t.Fatalf("authorization URL did not contain state and redirect URI: %s", authorizationURL)
	}

	result, err := svc.CompleteOAuth(ctx, state, "authorization-code", "test-agent", "")
	if err != nil {
		t.Fatalf("complete oauth: %v", err)
	}
	if result.User.Email != "sso@example.com" || result.Session == nil || result.Redirect != "/overview" {
		t.Fatalf("unexpected oauth result: %+v", result)
	}

	var accountCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM oauth_accounts WHERE provider = 'test' AND provider_uid = 'provider-user-1'`).Scan(&accountCount); err != nil {
		t.Fatal(err)
	}
	if accountCount != 1 {
		t.Fatalf("oauth account count = %d, want 1", accountCount)
	}

	if _, err := svc.CompleteOAuth(ctx, state, "authorization-code", "test-agent", ""); !errors.Is(err, auth.ErrOAuthStateInvalid) {
		t.Fatalf("replayed state error = %v, want ErrOAuthStateInvalid", err)
	}
}
