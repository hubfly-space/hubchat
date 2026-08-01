package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/ids"
)

const oauthStateLifetime = 10 * time.Minute

var (
	ErrOAuthDisabled        = errors.New("auth: oauth is not configured")
	ErrOAuthStateInvalid    = errors.New("auth: oauth state is invalid or expired")
	ErrOAuthProvider        = errors.New("auth: oauth provider rejected the identity")
	ErrOAuthEmailUnverified = errors.New("auth: oauth email is not verified")
	ErrOAuthDomainDenied    = errors.New("auth: oauth email domain is not allowed")
)

// OAuthOptions describes one operator-configured provider. URLs are validated
// as HTTPS URLs by config before they reach this module. RedirectURL is also
// fixed at boot, so no callback value can turn this client into an SSRF proxy.
type OAuthOptions struct {
	Provider         string
	Profile          string
	ClientID         string
	ClientSecret     string
	AuthorizationURL string
	TokenURL         string
	UserinfoURL      string
	RedirectURL      string
	Scopes           []string
	AllowedDomains   []string
}

type oauthProvider struct {
	pool PoolLike
	// Keep the concrete pool in the service's repository; PoolLike is only a
	// small compile-time seam for unit tests that do not need HTTP integration.
	config *OAuthOptions
	client *http.Client
}

// PoolLike is the subset needed by OAuth state and identity queries. The
// production database pool satisfies it; keeping this boundary small makes
// the provider's state machine testable without a live identity provider.
type PoolLike interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func newOAuthProvider(pool *database.Pool, opts *OAuthOptions) *oauthProvider {
	if opts == nil {
		return &oauthProvider{}
	}
	copyOpts := *opts
	copyOpts.Scopes = append([]string(nil), opts.Scopes...)
	copyOpts.AllowedDomains = append([]string(nil), opts.AllowedDomains...)
	return &oauthProvider{
		pool:   pool,
		config: &copyOpts,
		client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

type OAuthProviderInfo struct {
	Provider string `json:"provider"`
	Label    string `json:"label"`
}

func (s *Service) OAuthProvider() *OAuthProviderInfo {
	if s.oauth == nil || s.oauth.config == nil || s.oauth.config.Provider == "" {
		return nil
	}
	label := s.oauth.config.Provider
	switch s.oauth.config.Profile {
	case "google":
		label = "Google"
	case "microsoft":
		label = "Microsoft Entra ID"
	}
	return &OAuthProviderInfo{Provider: s.oauth.config.Provider, Label: label}
}

// BeginOAuth creates a single-use state and returns the provider URL. The
// redirect target is already constrained to a same-origin path by the API
// layer; it is stored server-side rather than trusted from the callback.
func (s *Service) BeginOAuth(ctx context.Context, next string) (string, string, error) {
	if s.oauth == nil || s.oauth.config == nil {
		return "", "", ErrOAuthDisabled
	}
	state, err := NewToken()
	if err != nil {
		return "", "", err
	}
	expiresAt := time.Now().Add(oauthStateLifetime)
	// State is intentionally short-lived. Opportunistic cleanup keeps a busy
	// sign-in surface from turning abandoned browser attempts into unbounded
	// rows; the active state remains protected by its expiry predicate below.
	if _, err := s.oauth.pool.Exec(ctx, `
		DELETE FROM oauth_signin_states
		WHERE expires_at < now() OR (consumed_at IS NOT NULL AND consumed_at < now() - interval '1 day')
	`); err != nil {
		return "", "", fmt.Errorf("auth: clean oauth states: %w", err)
	}
	_, err = s.oauth.pool.Exec(ctx, `
		INSERT INTO oauth_signin_states (id, state_hash, provider, redirect_to, expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`, ids.New("ost"), HashToken(state), s.oauth.config.Provider, next, expiresAt)
	if err != nil {
		return "", "", fmt.Errorf("auth: create oauth state: %w", err)
	}

	values := url.Values{}
	values.Set("client_id", s.oauth.config.ClientID)
	values.Set("redirect_uri", s.oauth.config.RedirectURL)
	values.Set("response_type", "code")
	values.Set("state", state)
	scopes := s.oauth.config.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "email", "profile"}
	}
	values.Set("scope", strings.Join(scopes, " "))
	return s.oauth.config.AuthorizationURL + "?" + values.Encode(), state, nil
}

type OAuthSignInResult struct {
	User          *User
	Session       *Session
	Challenge     *Challenge
	TrustedDevice *TrustedDeviceCredential
	Redirect      string
}

// CompleteOAuth validates and consumes state, exchanges the authorization
// code against the configured provider, then links or creates the local user.
// Linking is allowed only for an explicitly verified provider email.
func (s *Service) CompleteOAuth(ctx context.Context, state, code, userAgent, ip string) (*OAuthSignInResult, error) {
	return s.CompleteOAuthWithTrustedDevice(ctx, state, code, userAgent, ip, "")
}

func (s *Service) CompleteOAuthWithTrustedDevice(ctx context.Context, state, code, userAgent, ip, trustedToken string) (*OAuthSignInResult, error) {
	if s.oauth == nil || s.oauth.config == nil {
		return nil, ErrOAuthDisabled
	}
	redirect, err := s.oauth.consumeState(ctx, state, s.oauth.config.Provider)
	if err != nil {
		return nil, err
	}
	identity, err := s.oauth.identity(ctx, code)
	if err != nil {
		return nil, err
	}
	if !identity.Verified {
		return nil, ErrOAuthEmailUnverified
	}
	if !allowedOAuthDomain(identity.Email, s.oauth.config.AllowedDomains) {
		return nil, ErrOAuthDomainDenied
	}

	user, err := s.linkOAuthIdentity(ctx, s.oauth.config.Provider, identity)
	if err != nil {
		return nil, err
	}

	result, err := s.finishUserSignIn(ctx, user, userAgent, ip, trustedToken, AuthMethodOAuth)
	if err != nil {
		return nil, err
	}
	return &OAuthSignInResult{User: user, Session: result.Session, Challenge: result.Challenge, TrustedDevice: result.TrustedDevice, Redirect: redirect}, nil
}

type oauthIdentity struct {
	Subject  string
	Email    string
	Name     string
	Verified bool
}

func (p *oauthProvider) consumeState(ctx context.Context, state, provider string) (string, error) {
	if state == "" {
		return "", ErrOAuthStateInvalid
	}
	var redirect string
	err := p.pool.QueryRow(ctx, `
		UPDATE oauth_signin_states
		SET consumed_at = now()
		WHERE state_hash = $1 AND provider = $2 AND consumed_at IS NULL AND expires_at > now()
		RETURNING redirect_to
	`, HashToken(state), provider).Scan(&redirect)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrOAuthStateInvalid
	}
	if err != nil {
		return "", fmt.Errorf("auth: consume oauth state: %w", err)
	}
	return redirect, nil
}

func (p *oauthProvider) identity(ctx context.Context, code string) (*oauthIdentity, error) {
	if code == "" {
		return nil, ErrOAuthProvider
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", p.config.ClientID)
	form.Set("client_secret", p.config.ClientSecret)
	form.Set("redirect_uri", p.config.RedirectURL)
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, p.config.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, ErrOAuthProvider
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, ErrOAuthProvider
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, ErrOAuthProvider
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&token); err != nil || token.AccessToken == "" {
		return nil, ErrOAuthProvider
	}

	profileReq, err := http.NewRequestWithContext(reqCtx, http.MethodGet, p.config.UserinfoURL, nil)
	if err != nil {
		return nil, ErrOAuthProvider
	}
	profileReq.Header.Set("Authorization", "Bearer "+token.AccessToken)
	profileResp, err := p.client.Do(profileReq)
	if err != nil {
		return nil, ErrOAuthProvider
	}
	defer profileResp.Body.Close()
	if profileResp.StatusCode < 200 || profileResp.StatusCode >= 300 {
		return nil, ErrOAuthProvider
	}
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(io.LimitReader(profileResp.Body, 1<<20)).Decode(&raw); err != nil {
		return nil, ErrOAuthProvider
	}
	identity := parseOAuthIdentity(raw, p.config.Profile)
	if identity.Subject == "" || !looksLikeEmail(identity.Email) {
		return nil, ErrOAuthProvider
	}
	if identity.Name == "" {
		identity.Name = identity.Email
	}
	return identity, nil
}

func parseOAuthIdentity(raw map[string]json.RawMessage, profile string) *oauthIdentity {
	emailKeys := []string{"email"}
	nameKeys := []string{"name", "preferred_username"}
	verified := firstBool(raw, "email_verified", "verified")
	if profile == "microsoft" {
		// Microsoft Graph /me uses mail for a populated mailbox and
		// userPrincipalName for directory accounts without a separate mail
		// attribute. The response came from the configured Graph endpoint with
		// the access token, so the directory identity is verified even though
		// Graph does not emit email_verified.
		emailKeys = []string{"mail", "userPrincipalName", "email"}
		nameKeys = []string{"displayName", "name", "userPrincipalName"}
		verified = true
	}
	return &oauthIdentity{
		Subject:  firstString(raw, "sub", "id"),
		Email:    normalizeEmail(firstString(raw, emailKeys...)),
		Name:     strings.TrimSpace(firstString(raw, nameKeys...)),
		Verified: verified,
	}
}

func firstString(raw map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		var value string
		if err := json.Unmarshal(raw[key], &value); err == nil && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstBool(raw map[string]json.RawMessage, keys ...string) bool {
	for _, key := range keys {
		var value bool
		if err := json.Unmarshal(raw[key], &value); err == nil {
			return value
		}
		var text string
		if err := json.Unmarshal(raw[key], &text); err == nil && strings.EqualFold(text, "true") {
			return true
		}
	}
	return false
}

func allowedOAuthDomain(email string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	_, domain, ok := strings.Cut(normalizeEmail(email), "@")
	if !ok || domain == "" {
		return false
	}
	for _, candidate := range allowed {
		if strings.EqualFold(strings.TrimPrefix(strings.ToLower(candidate), "@"), domain) {
			return true
		}
	}
	return false
}

func (s *Service) linkOAuthIdentity(ctx context.Context, provider string, identity *oauthIdentity) (*User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var user User
	err = tx.QueryRow(ctx, `
		SELECT u.id, u.name, u.email, COALESCE(u.password_hash, ''), u.created_at
		FROM oauth_accounts oa JOIN users u ON u.id = oa.user_id
		WHERE oa.provider = $1 AND oa.provider_uid = $2
		FOR UPDATE OF u
	`, provider, identity.Subject).Scan(&user.ID, &user.Name, &user.Email, &user.PasswordHash, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			SELECT id, name, email, COALESCE(password_hash, ''), created_at
			FROM users WHERE email = $1 FOR UPDATE
		`, identity.Email).Scan(&user.ID, &user.Name, &user.Email, &user.PasswordHash, &user.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			user.ID = ids.New(ids.PrefixUser)
			user.Name = identity.Name
			user.Email = identity.Email
			err = tx.QueryRow(ctx, `
				INSERT INTO users (id, name, email, password_hash, email_verified_at)
				VALUES ($1, $2, $3, NULL, now())
				RETURNING created_at
			`, user.ID, user.Name, user.Email).Scan(&user.CreatedAt)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("auth: link oauth identity: %w", err)
	}
	inserted, err := tx.Exec(ctx, `
		INSERT INTO oauth_accounts (id, user_id, provider, provider_uid)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (provider, provider_uid) DO NOTHING
	`, ids.New(ids.PrefixOAuthAccount), user.ID, provider, identity.Subject)
	if err != nil {
		return nil, fmt.Errorf("auth: save oauth identity: %w", err)
	}
	if inserted.RowsAffected() == 0 {
		// A concurrent callback may have won the provider-identity race after
		// the initial lookup. Never issue a session for the losing local user.
		if err := tx.QueryRow(ctx, `
			SELECT u.id, u.name, u.email, COALESCE(u.password_hash, ''), u.created_at
			FROM oauth_accounts oa JOIN users u ON u.id = oa.user_id
			WHERE oa.provider = $1 AND oa.provider_uid = $2
			FOR UPDATE OF u
		`, provider, identity.Subject).Scan(&user.ID, &user.Name, &user.Email, &user.PasswordHash, &user.CreatedAt); err != nil {
			return nil, fmt.Errorf("auth: resolve concurrent oauth identity: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &user, nil
}
