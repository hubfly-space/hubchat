// Package auth owns credentials, sessions, and the sign-in flows.
//
// # Responsibilities
//
// Password hashing, magic links, TOTP, recovery codes, and session
// issue/rotate/revoke. OAuth remains an optional adapter boundary; the base
// binary does not enable a provider.
//
// # Boundary
//
// Never decides *what* a signed-in user may do — that is authorization. This
// module answers only "who is this?".
package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/ids"
)

var (
	ErrInvalidCredentials = errors.New("auth: invalid email or password")
	ErrWeakPassword       = errors.New("auth: password must be at least 12 characters")
	ErrInvalidEmail       = errors.New("auth: a valid email address is required")
)

const (
	AuthMethodPassword  = "password"
	AuthMethodMagicLink = "magic_link"
	AuthMethodOAuth     = "oauth"
)

// Session is what a caller needs to set the session cookie and to know when
// it expires.
type Session struct {
	Token     string
	ExpiresAt time.Time
}

// Service is the auth module's entry point. It holds a raw pool rather than a
// transaction because sign-up spans exactly one statement here; the workspace
// module wraps the *combined* owner-plus-workspace creation in its own
// transaction (see workspace.Service.Bootstrap).
type Service struct {
	repo     *repository
	pool     *database.Pool
	session  sessionConfig
	security securityConfig
	oauth    *oauthProvider
}

// securityConfig holds the brute-force policy. Copied in at construction for
// the same reason sessionConfig is: this module needs two numbers from the
// global config, not a dependency on all of it.
type securityConfig struct {
	// LoginAttempts is how many consecutive failures lock an account.
	LoginAttempts int
	// LockoutWindow is how long the lock lasts.
	LockoutWindow time.Duration
}

// sessionConfig is intentionally unexported and tiny — the one setting this
// module needs from the global config, copied in at construction so auth does
// not import the whole config package's surface.
type sessionConfig struct {
	Lifetime     time.Duration
	CookieDomain string
	CookieSecure bool
}

// Options is the configuration auth needs. A struct rather than positional
// parameters because the list has grown past the point where a call site reads
// unambiguously.
type Options struct {
	SessionLifetime time.Duration
	CookieDomain    string
	CookieSecure    bool
	LoginAttempts   int
	LockoutWindow   time.Duration
	OAuth           *OAuthOptions
}

// New constructs the auth service.
func New(pool *database.Pool, opts Options) *Service {
	if opts.LoginAttempts <= 0 {
		opts.LoginAttempts = 5
	}
	if opts.LockoutWindow <= 0 {
		opts.LockoutWindow = 15 * time.Minute
	}

	return &Service{
		repo: &repository{pool: pool},
		pool: pool,
		session: sessionConfig{
			Lifetime:     opts.SessionLifetime,
			CookieDomain: opts.CookieDomain,
			CookieSecure: opts.CookieSecure,
		},
		security: securityConfig{
			LoginAttempts: opts.LoginAttempts,
			LockoutWindow: opts.LockoutWindow,
		},
		oauth: newOAuthProvider(pool, opts.OAuth),
	}
}

// SignUp creates a new user account. Email uniqueness is enforced by the
// database, not a check-then-insert race in this method.
func (s *Service) SignUp(ctx context.Context, name, email, password string) (*User, error) {
	email = normalizeEmail(email)

	if !looksLikeEmail(email) {
		return nil, ErrInvalidEmail
	}
	if len(password) < 12 {
		return nil, ErrWeakPassword
	}

	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}

	id := ids.New(ids.PrefixUser)
	if err := s.repo.insertUser(ctx, id, strings.TrimSpace(name), email, hash); err != nil {
		return nil, err
	}

	return &User{ID: id, Name: name, Email: email}, nil
}

// Authenticate verifies credentials and returns the user on success.
//
// The same error is returned whether the email is unknown or the password is
// wrong (§11.4) — an attacker probing which addresses have accounts learns
// nothing from the response.
func (s *Service) Authenticate(ctx context.Context, email, password string) (*User, error) {
	user, err := s.repo.userByEmail(ctx, normalizeEmail(email))
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	if !VerifyPassword(user.PasswordHash, password) {
		return nil, ErrInvalidCredentials
	}
	return user, nil
}

// CreateSession issues a new session for userID and returns the raw token —
// the only time the raw value exists outside the client's cookie jar. Only
// its hash is persisted (§11.5).
func (s *Service) CreateSession(ctx context.Context, userID, userAgent, ip string) (*Session, error) {
	return s.CreateSessionWithMethod(ctx, userID, userAgent, ip, AuthMethodPassword)
}

func (s *Service) CreateSessionWithMethod(ctx context.Context, userID, userAgent, ip, authMethod string) (*Session, error) {
	if authMethod != AuthMethodPassword && authMethod != AuthMethodMagicLink && authMethod != AuthMethodOAuth {
		authMethod = AuthMethodPassword
	}
	token, err := NewToken()
	if err != nil {
		return nil, err
	}

	expiresAt := time.Now().Add(s.session.Lifetime)
	id := ids.New(ids.PrefixSession)

	if err := s.repo.insertSession(ctx, id, userID, HashToken(token), userAgent, ip, authMethod, expiresAt); err != nil {
		return nil, err
	}

	return &Session{Token: token, ExpiresAt: expiresAt}, nil
}

// UserForSession resolves a raw session token to its user. Returns
// ErrUserNotFound if the token is unknown, expired, or revoked — callers
// should treat all three identically (an expired session is not
// distinguishable from a nonexistent one to the client).
func (s *Service) UserForSession(ctx context.Context, token string) (*User, error) {
	return s.repo.sessionUser(ctx, HashToken(token))
}

// Logout revokes the session behind token. Idempotent: revoking an
// already-revoked or unknown token is not an error.
func (s *Service) Logout(ctx context.Context, token string) error {
	return s.repo.revokeSession(ctx, HashToken(token))
}

// CookieSecure and CookieDomain expose the resolved cookie policy so the HTTP
// layer can set the cookie without importing config itself.
func (s *Service) CookieSecure() bool             { return s.session.CookieSecure }
func (s *Service) CookieDomain() string           { return s.session.CookieDomain }
func (s *Service) SessionLifetime() time.Duration { return s.session.Lifetime }

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func looksLikeEmail(email string) bool {
	at := strings.IndexByte(email, '@')
	return at > 0 && at < len(email)-1 && !strings.Contains(email[at+1:], "@")
}
