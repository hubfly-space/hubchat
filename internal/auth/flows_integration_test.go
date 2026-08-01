//go:build integration

package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/auth"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/database/dbtest"
)

func newService(t *testing.T, pool *database.Pool) *auth.Service {
	t.Helper()
	return auth.New(pool, auth.Options{
		SessionLifetime: time.Hour,
		LoginAttempts:   3,
		LockoutWindow:   time.Minute,
	})
}

const password = "correct-horse-battery-staple"

func seedUser(t *testing.T, ctx context.Context, svc *auth.Service, email string) *auth.User {
	t.Helper()
	user, err := svc.SignUp(ctx, "Test Person", email, password)
	if err != nil {
		t.Fatalf("sign up: %v", err)
	}
	return user
}

// ---------------------------------------------------------------- lockout

// §11.4 brute-force protection. Per-account, so a botnet spreading attempts
// across many addresses cannot slip past the per-IP limiter.
func TestSignInLocksAccountAfterRepeatedFailures(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newService(t, pool)
	seedUser(t, ctx, svc, "locked@example.com")

	for attempt := 1; attempt <= 3; attempt++ {
		_, err := svc.SignIn(ctx, "locked@example.com", "wrong", "agent", "")
		if !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("attempt %d: got %v, want ErrInvalidCredentials", attempt, err)
		}
	}

	// The correct password must now be refused too — otherwise the limit only
	// slows an attacker down until they happen to guess right.
	_, err := svc.SignIn(ctx, "locked@example.com", password, "agent", "")
	if !errors.Is(err, auth.ErrAccountLocked) {
		t.Fatalf("after lockout: got %v, want ErrAccountLocked", err)
	}
}

func TestSuccessfulSignInClearsFailureCount(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newService(t, pool)
	seedUser(t, ctx, svc, "recovers@example.com")

	// Two failures, then a success, then two more failures must not lock:
	// the counter is consecutive, not cumulative.
	for range 2 {
		_, _ = svc.SignIn(ctx, "recovers@example.com", "wrong", "agent", "")
	}
	if _, err := svc.SignIn(ctx, "recovers@example.com", password, "agent", ""); err != nil {
		t.Fatalf("valid sign-in after failures: %v", err)
	}
	for range 2 {
		_, _ = svc.SignIn(ctx, "recovers@example.com", "wrong", "agent", "")
	}

	if _, err := svc.SignIn(ctx, "recovers@example.com", password, "agent", ""); err != nil {
		t.Fatalf("account was locked despite an intervening success: %v", err)
	}
}

// An unknown address must be indistinguishable from a wrong password (§11.4).
func TestSignInDoesNotRevealWhetherAnAccountExists(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newService(t, pool)
	seedUser(t, ctx, svc, "real@example.com")

	_, missingErr := svc.SignIn(ctx, "nobody@example.com", password, "agent", "")
	_, wrongErr := svc.SignIn(ctx, "real@example.com", "wrong", "agent", "")

	if !errors.Is(missingErr, auth.ErrInvalidCredentials) {
		t.Fatalf("unknown account: got %v, want ErrInvalidCredentials", missingErr)
	}
	if !errors.Is(wrongErr, auth.ErrInvalidCredentials) {
		t.Fatalf("wrong password: got %v, want ErrInvalidCredentials", wrongErr)
	}
	if missingErr.Error() != wrongErr.Error() {
		t.Fatalf("the two failures are distinguishable: %q vs %q", missingErr, wrongErr)
	}
}

// ------------------------------------------------------- password reset

func TestPasswordResetRoundTripAndSingleUse(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newService(t, pool)
	seedUser(t, ctx, svc, "reset@example.com")

	token, _, err := svc.IssuePasswordReset(ctx, "reset@example.com")
	if err != nil {
		t.Fatalf("issue reset: %v", err)
	}

	const newPassword = "an-entirely-different-secret"
	if _, err := svc.ResetPassword(ctx, token, newPassword); err != nil {
		t.Fatalf("reset password: %v", err)
	}

	if _, err := svc.SignIn(ctx, "reset@example.com", newPassword, "agent", ""); err != nil {
		t.Fatalf("sign in with the new password: %v", err)
	}
	if _, err := svc.SignIn(ctx, "reset@example.com", password, "agent", ""); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatal("the old password still works after a reset")
	}

	// Replaying the link must fail — a reset token is a credential, and a
	// reusable one left in an inbox is a permanent backdoor.
	if _, err := svc.ResetPassword(ctx, token, "yet-another-secret"); !errors.Is(err, auth.ErrTokenInvalid) {
		t.Fatalf("token replay: got %v, want ErrTokenInvalid", err)
	}
}

// A reset is what someone does when they think they are compromised, so it
// must end the attacker's session too.
func TestPasswordResetRevokesExistingSessions(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newService(t, pool)
	user := seedUser(t, ctx, svc, "revoke@example.com")

	session, err := svc.CreateSession(ctx, user.ID, "attacker", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := svc.UserForSession(ctx, session.Token); err != nil {
		t.Fatalf("session should be live before the reset: %v", err)
	}

	token, _, err := svc.IssuePasswordReset(ctx, "revoke@example.com")
	if err != nil {
		t.Fatalf("issue reset: %v", err)
	}
	if _, err := svc.ResetPassword(ctx, token, "a-brand-new-password-x"); err != nil {
		t.Fatalf("reset: %v", err)
	}

	if _, err := svc.UserForSession(ctx, session.Token); err == nil {
		t.Fatal("a session that predated the password reset is still valid")
	}
}

func TestAdminPasswordResetRoundTripAndSessionRevocation(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newService(t, pool)
	user := seedUser(t, ctx, svc, "admin-reset@example.com")
	session, err := svc.CreateSession(ctx, user.ID, "admin-reset-browser", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	const newPassword = "admin-reset-password-x"
	if _, err := svc.ResetPasswordForAdmin(ctx, user.Email, newPassword); err != nil {
		t.Fatalf("admin reset password: %v", err)
	}
	if _, err := svc.UserForSession(ctx, session.Token); err == nil {
		t.Fatal("an existing session survived an administrator password reset")
	}
	if _, err := svc.SignIn(ctx, user.Email, newPassword, "new-browser", ""); err != nil {
		t.Fatalf("sign in with administrator-set password: %v", err)
	}
}

func TestResetPasswordRejectsWeakPasswords(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newService(t, pool)
	seedUser(t, ctx, svc, "weak@example.com")

	token, _, err := svc.IssuePasswordReset(ctx, "weak@example.com")
	if err != nil {
		t.Fatalf("issue reset: %v", err)
	}
	if _, err := svc.ResetPassword(ctx, token, "short"); !errors.Is(err, auth.ErrWeakPassword) {
		t.Fatalf("got %v, want ErrWeakPassword", err)
	}
}

// -------------------------------------------------------- email verification

func TestEmailVerificationRoundTrip(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newService(t, pool)
	user := seedUser(t, ctx, svc, "verify@example.com")

	token, _, err := svc.IssueEmailVerification(ctx, user.ID, user.Email)
	if err != nil {
		t.Fatalf("issue verification: %v", err)
	}

	verified, err := svc.VerifyEmail(ctx, token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if verified.ID != user.ID {
		t.Fatalf("verified the wrong user: %s", verified.ID)
	}

	if _, err := svc.VerifyEmail(ctx, token); !errors.Is(err, auth.ErrTokenInvalid) {
		t.Fatalf("token replay: got %v, want ErrTokenInvalid", err)
	}
}

// ------------------------------------------------------------ magic links

func TestMagicLinkSignsInAndIsSingleUse(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newService(t, pool)
	seedUser(t, ctx, svc, "magic@example.com")

	token, _, err := svc.IssueMagicLink(ctx, "magic@example.com", "/inbox", "")
	if err != nil {
		t.Fatalf("issue magic link: %v", err)
	}

	result, err := svc.RedeemMagicLink(ctx, token, "agent", "")
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if result.Session == nil {
		t.Fatal("redeeming a magic link produced no session")
	}
	if _, err := svc.UserForSession(ctx, result.Session.Token); err != nil {
		t.Fatalf("the issued session is not usable: %v", err)
	}

	if _, err := svc.RedeemMagicLink(ctx, token, "agent", ""); !errors.Is(err, auth.ErrTokenInvalid) {
		t.Fatalf("link replay: got %v, want ErrTokenInvalid", err)
	}
}

// ------------------------------------------------------------------- TOTP

func TestTOTPEnrolmentAndChallenge(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newService(t, pool)
	user := seedUser(t, ctx, svc, "totp@example.com")

	secret, uri, err := svc.BeginTOTPEnrolment(ctx, user.ID, "Hubchat")
	if err != nil {
		t.Fatalf("begin enrolment: %v", err)
	}
	if uri == "" {
		t.Fatal("no provisioning uri returned")
	}

	// A wrong code must not complete enrolment — that is the check that stops
	// someone locking themselves out with a QR code they never scanned.
	if _, err := svc.CompleteTOTPEnrolment(ctx, user.ID, secret, "000000"); !errors.Is(err, auth.ErrTOTPInvalid) {
		t.Fatalf("bad enrolment code: got %v, want ErrTOTPInvalid", err)
	}
	if enabled, _ := svc.TOTPEnabled(ctx, user.ID); enabled {
		t.Fatal("a failed enrolment enabled two-factor anyway")
	}

	codes, err := svc.CompleteTOTPEnrolment(ctx, user.ID, secret, currentCode(t, secret))
	if err != nil {
		t.Fatalf("complete enrolment: %v", err)
	}
	if len(codes) != 10 {
		t.Fatalf("got %d recovery codes, want 10", len(codes))
	}

	// Sign-in now yields a challenge rather than a session.
	result, err := svc.SignIn(ctx, "totp@example.com", password, "agent", "")
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}
	if result.Session != nil {
		t.Fatal("a session was issued without the second factor")
	}
	if result.Challenge == nil {
		t.Fatal("no second-factor challenge was issued")
	}

	verified, err := svc.VerifyTOTPChallenge(ctx, result.Challenge.Token, currentCode(t, secret), "agent", "")
	if err != nil {
		t.Fatalf("verify challenge: %v", err)
	}
	if verified.Session == nil {
		t.Fatal("verifying the challenge produced no session")
	}
}

// Losing the authenticator is exactly when the second factor is most in the
// way, so a recovery code must work — once.
func TestRecoveryCodeSatisfiesChallengeOnce(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newService(t, pool)
	user := seedUser(t, ctx, svc, "recovery@example.com")

	secret, _, err := svc.BeginTOTPEnrolment(ctx, user.ID, "Hubchat")
	if err != nil {
		t.Fatalf("begin enrolment: %v", err)
	}
	codes, err := svc.CompleteTOTPEnrolment(ctx, user.ID, secret, currentCode(t, secret))
	if err != nil {
		t.Fatalf("complete enrolment: %v", err)
	}

	first, err := svc.SignIn(ctx, "recovery@example.com", password, "agent", "")
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}
	if _, err := svc.VerifyTOTPChallenge(ctx, first.Challenge.Token, codes[0], "agent", ""); err != nil {
		t.Fatalf("recovery code rejected: %v", err)
	}

	remaining, err := svc.RemainingRecoveryCodes(ctx, user.ID)
	if err != nil {
		t.Fatalf("count codes: %v", err)
	}
	if remaining != 9 {
		t.Fatalf("got %d unused codes, want 9", remaining)
	}

	// The same code must not work twice.
	second, err := svc.SignIn(ctx, "recovery@example.com", password, "agent", "")
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}
	if _, err := svc.VerifyTOTPChallenge(ctx, second.Challenge.Token, codes[0], "agent", ""); !errors.Is(err, auth.ErrTOTPInvalid) {
		t.Fatalf("a spent recovery code was accepted again: %v", err)
	}
}

func TestTOTPChallengeLimitsGuesses(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newService(t, pool)
	user := seedUser(t, ctx, svc, "guess@example.com")

	secret, _, _ := svc.BeginTOTPEnrolment(ctx, user.ID, "Hubchat")
	if _, err := svc.CompleteTOTPEnrolment(ctx, user.ID, secret, currentCode(t, secret)); err != nil {
		t.Fatalf("complete enrolment: %v", err)
	}

	result, err := svc.SignIn(ctx, "guess@example.com", password, "agent", "")
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}

	for range 5 {
		_, _ = svc.VerifyTOTPChallenge(ctx, result.Challenge.Token, "000000", "agent", "")
	}

	// Even the right code must now be refused: the challenge is spent.
	_, err = svc.VerifyTOTPChallenge(ctx, result.Challenge.Token, currentCode(t, secret), "agent", "")
	if !errors.Is(err, auth.ErrAccountLocked) {
		t.Fatalf("after exhausting guesses: got %v, want ErrAccountLocked", err)
	}
}

// Removing the second factor must require the password: an unlocked laptop
// should not be enough to undo the protection against an unlocked laptop.
func TestDisableTOTPRequiresPassword(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newService(t, pool)
	user := seedUser(t, ctx, svc, "disable@example.com")

	secret, _, _ := svc.BeginTOTPEnrolment(ctx, user.ID, "Hubchat")
	if _, err := svc.CompleteTOTPEnrolment(ctx, user.ID, secret, currentCode(t, secret)); err != nil {
		t.Fatalf("complete enrolment: %v", err)
	}

	if err := svc.DisableTOTP(ctx, user.ID, "not-the-password"); !errors.Is(err, auth.ErrPasswordMismatch) {
		t.Fatalf("got %v, want ErrPasswordMismatch", err)
	}
	if enabled, _ := svc.TOTPEnabled(ctx, user.ID); !enabled {
		t.Fatal("two-factor was disabled despite a wrong password")
	}

	if err := svc.DisableTOTP(ctx, user.ID, password); err != nil {
		t.Fatalf("disable with correct password: %v", err)
	}
	if enabled, _ := svc.TOTPEnabled(ctx, user.ID); enabled {
		t.Fatal("two-factor is still enabled after a successful disable")
	}
}

// --------------------------------------------------------------- sessions

func TestSessionListingAndRevocation(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newService(t, pool)
	user := seedUser(t, ctx, svc, "sessions@example.com")

	current, _ := svc.CreateSession(ctx, user.ID, "this-browser", "")
	other, _ := svc.CreateSession(ctx, user.ID, "other-browser", "")

	sessions, err := svc.ListSessions(ctx, user.ID, current.Token)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}

	var currentCount int
	var otherID string
	for _, session := range sessions {
		if session.Current {
			currentCount++
		} else {
			otherID = session.ID
		}
	}
	if currentCount != 1 {
		t.Fatalf("%d sessions were marked current, want exactly 1", currentCount)
	}

	if err := svc.RevokeSession(ctx, user.ID, otherID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := svc.UserForSession(ctx, other.Token); err == nil {
		t.Fatal("the revoked session still resolves")
	}
	if _, err := svc.UserForSession(ctx, current.Token); err != nil {
		t.Fatalf("revoking one session invalidated another: %v", err)
	}
}

func TestSessionListingSupportsCursorPagination(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newService(t, pool)
	user := seedUser(t, ctx, svc, "session-pages@example.com")
	first, _ := svc.CreateSession(ctx, user.ID, "first-browser", "")
	_, _ = svc.CreateSession(ctx, user.ID, "second-browser", "")
	_, _ = svc.CreateSession(ctx, user.ID, "third-browser", "")

	// Make the ordering independent of wall-clock resolution and assert that
	// the ID tie-breaker is part of the cursor contract.
	if _, err := pool.Exec(ctx, `
		UPDATE user_sessions
		SET last_seen_at = CASE id
			WHEN (SELECT id FROM user_sessions WHERE user_id=$1 AND user_agent='first-browser') THEN '2026-01-03T00:00:00Z'::timestamptz
			WHEN (SELECT id FROM user_sessions WHERE user_id=$1 AND user_agent='second-browser') THEN '2026-01-02T00:00:00Z'::timestamptz
			WHEN (SELECT id FROM user_sessions WHERE user_id=$1 AND user_agent='third-browser') THEN '2026-01-01T00:00:00Z'::timestamptz
		END
		WHERE user_id = $1
	`, user.ID); err != nil {
		t.Fatalf("set session order: %v", err)
	}

	page, err := svc.ListSessionsPage(ctx, user.ID, first.Token, time.Time{}, "", 2)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(page) != 2 || page[0].UserAgent != "first-browser" || page[1].UserAgent != "second-browser" {
		t.Fatalf("first page = %#v, want first and second sessions", page)
	}

	next, err := svc.ListSessionsPage(ctx, user.ID, first.Token, page[1].LastSeenAt, page[1].ID, 2)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(next) != 1 || next[0].UserAgent != "third-browser" {
		t.Fatalf("second page = %#v, want third session", next)
	}
}

// A session id is not a capability: revoking must be scoped to its owner, or
// anyone holding an id could sign anyone else out.
func TestRevokeSessionIsScopedToItsOwner(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newService(t, pool)
	victim := seedUser(t, ctx, svc, "victim@example.com")
	attacker := seedUser(t, ctx, svc, "attacker@example.com")

	victimSession, _ := svc.CreateSession(ctx, victim.ID, "victim-browser", "")
	sessions, _ := svc.ListSessions(ctx, victim.ID, "")
	if len(sessions) != 1 {
		t.Fatalf("setup: got %d sessions, want 1", len(sessions))
	}

	if err := svc.RevokeSession(ctx, attacker.ID, sessions[0].ID); err == nil {
		t.Fatal("one user revoked another user's session")
	}
	if _, err := svc.UserForSession(ctx, victimSession.Token); err != nil {
		t.Fatalf("the victim's session was revoked by a third party: %v", err)
	}
}

func TestChangePasswordKeepsCurrentSessionAndDropsOthers(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newService(t, pool)
	user := seedUser(t, ctx, svc, "change@example.com")

	current, _ := svc.CreateSession(ctx, user.ID, "this-browser", "")
	other, _ := svc.CreateSession(ctx, user.ID, "other-browser", "")

	if err := svc.ChangePassword(ctx, user.ID, "wrong", "a-new-password-here", current.Token); !errors.Is(err, auth.ErrPasswordMismatch) {
		t.Fatalf("got %v, want ErrPasswordMismatch", err)
	}

	if err := svc.ChangePassword(ctx, user.ID, password, "a-new-password-here", current.Token); err != nil {
		t.Fatalf("change password: %v", err)
	}

	if _, err := svc.UserForSession(ctx, current.Token); err != nil {
		t.Fatalf("the session that changed the password was revoked: %v", err)
	}
	if _, err := svc.UserForSession(ctx, other.Token); err == nil {
		t.Fatal("another device stayed signed in after a password change")
	}
}

// currentCode produces the code an authenticator app would show right now,
// standing in for the user's phone.
func currentCode(t *testing.T, secret string) string {
	t.Helper()

	code, err := auth.GenerateTOTP(secret, time.Now())
	if err != nil {
		t.Fatalf("generate totp code: %v", err)
	}
	return code
}
