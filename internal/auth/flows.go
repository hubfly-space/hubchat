package auth

import (
	"context"
	"errors"
	"time"

	"github.com/hubchat/hubchat/internal/ids"
)

// The lifetimes below are deliberately different from each other.
//
// A verification link is a convenience and can be reissued freely, so it lives
// long enough to survive a spam folder. A reset or sign-in link *is* a
// credential for the window it exists, so it is short. A 2FA challenge is
// shorter still: it only has to outlive the user reaching for their phone.
const (
	verificationTokenLifetime = 24 * time.Hour
	resetTokenLifetime        = 1 * time.Hour
	magicLinkLifetime         = 15 * time.Minute
	totpChallengeLifetime     = 5 * time.Minute

	// recoveryCodeCount is what the user gets on enrolment. Ten is enough to
	// print once and use over years without becoming a list nobody stores
	// carefully.
	recoveryCodeCount = 10

	// maxTOTPAttempts bounds guessing against one challenge. Six digits is a
	// million possibilities, so the limit — not the length — is what makes
	// brute force impractical.
	maxTOTPAttempts = 5
)

var (
	ErrTokenInvalid     = errors.New("auth: this link is invalid or has already been used")
	ErrTokenExpired     = errors.New("auth: this link has expired")
	ErrAccountLocked    = errors.New("auth: too many failed attempts")
	ErrTOTPRequired     = errors.New("auth: a second factor is required")
	ErrTOTPInvalid      = errors.New("auth: that code is not valid")
	ErrTOTPNotEnabled   = errors.New("auth: two-factor authentication is not enabled")
	ErrTOTPAlreadyOn    = errors.New("auth: two-factor authentication is already enabled")
	ErrPasswordMismatch = errors.New("auth: current password is incorrect")
)

// Challenge is the half-authenticated state between a correct password and a
// verified second factor.
type Challenge struct {
	Token     string
	ExpiresAt time.Time
}

// SignInResult is what Authenticate produces once lockout and 2FA are in play.
//
// Exactly one of Session or Challenge is set. Modelling it as one struct with
// two nullable fields rather than two return paths keeps the caller honest:
// there is no way to read Session without noticing Challenge exists.
type SignInResult struct {
	User          *User
	Session       *Session
	Challenge     *Challenge
	TrustedDevice *TrustedDeviceCredential
}

// SignIn verifies a password, applies lockout, and either issues a session or
// returns a 2FA challenge.
//
// This replaces calling Authenticate and CreateSession separately, because
// doing those two things without the steps in between — the lockout check
// before, the attempt counter after, the 2FA branch in the middle — is exactly
// the mistake the split invited.
func (s *Service) SignIn(ctx context.Context, email, password, userAgent, ip string) (*SignInResult, error) {
	return s.SignInWithTrustedDevice(ctx, email, password, userAgent, ip, "")
}

// SignInWithTrustedDevice accepts a previously issued device credential only
// to satisfy the account's existing TOTP requirement. It never becomes a
// session and is scoped to the same user account.
func (s *Service) SignInWithTrustedDevice(ctx context.Context, email, password, userAgent, ip, trustedToken string) (*SignInResult, error) {
	user, err := s.repo.userByEmail(ctx, normalizeEmail(email))
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			// Hash a dummy password anyway so a missing account and a wrong
			// password take comparable time. bcrypt is slow by design, and
			// skipping it here would turn sign-in into an account-existence
			// oracle measurable over the network (§11.4).
			burnPasswordTime()
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	locked, err := s.repo.lockState(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	if locked {
		return nil, ErrAccountLocked
	}

	if !VerifyPassword(user.PasswordHash, password) {
		if err := s.repo.recordFailedAttempt(ctx, user.ID, s.security.LoginAttempts, s.security.LockoutWindow); err != nil {
			return nil, err
		}
		return nil, ErrInvalidCredentials
	}

	if err := s.repo.clearFailedAttempts(ctx, user.ID); err != nil {
		return nil, err
	}

	enabled, err := s.repo.totpEnabled(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	if enabled {
		if trusted, err := s.trustedDeviceValid(ctx, user.ID, trustedToken); err != nil {
			return nil, err
		} else if trusted {
			return s.createSessionResult(ctx, user, userAgent, ip, AuthMethodPassword)
		}
		challenge, err := s.issueTOTPChallengeWithMethod(ctx, user.ID, userAgent, ip, AuthMethodPassword)
		if err != nil {
			return nil, err
		}
		return &SignInResult{User: user, Challenge: challenge}, nil
	}

	return s.createSessionResult(ctx, user, userAgent, ip, AuthMethodPassword)
}

func (s *Service) createSessionResult(ctx context.Context, user *User, userAgent, ip, authMethod string) (*SignInResult, error) {
	session, err := s.CreateSessionWithMethod(ctx, user.ID, userAgent, ip, authMethod)
	if err != nil {
		return nil, err
	}
	if err := s.repo.markSignedIn(ctx, user.ID); err != nil {
		return nil, err
	}
	return &SignInResult{User: user, Session: session}, nil
}

func (s *Service) finishUserSignIn(ctx context.Context, user *User, userAgent, ip, trustedToken, authMethod string) (*SignInResult, error) {
	enabled, err := s.repo.totpEnabled(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	if enabled {
		if trusted, err := s.trustedDeviceValid(ctx, user.ID, trustedToken); err != nil {
			return nil, err
		} else if trusted {
			return s.createSessionResult(ctx, user, userAgent, ip, authMethod)
		}
		challenge, err := s.issueTOTPChallengeWithMethod(ctx, user.ID, userAgent, ip, authMethod)
		if err != nil {
			return nil, err
		}
		return &SignInResult{User: user, Challenge: challenge}, nil
	}
	return s.createSessionResult(ctx, user, userAgent, ip, authMethod)
}

// burnPasswordTime spends roughly one bcrypt's worth of CPU so that a
// nonexistent account is not visibly faster to reject than a real one.
func burnPasswordTime() {
	// The hash is a fixed, valid bcrypt digest of a value nothing can match.
	const decoy = "$2a$12$C6UzMDM.H6dfI/f/IKcEe.6oj6UjPqNBOaFmqPQdCMDgZgL1L8mYy"
	_ = VerifyPassword(decoy, "not-the-password")
}

// ------------------------------------------------------- email verification

// IssueEmailVerification creates a verification token for a user's address.
func (s *Service) IssueEmailVerification(ctx context.Context, userID, email string) (string, time.Time, error) {
	token, err := NewToken()
	if err != nil {
		return "", time.Time{}, err
	}

	expiresAt := time.Now().Add(verificationTokenLifetime)
	err = s.repo.insertVerificationToken(
		ctx, ids.New(ids.PrefixEmailToken), userID, normalizeEmail(email), HashToken(token), expiresAt)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

// VerifyEmail redeems a verification token, marking the address confirmed.
func (s *Service) VerifyEmail(ctx context.Context, token string) (*User, error) {
	userID, err := s.repo.consumeVerificationToken(ctx, HashToken(token))
	if err != nil {
		return nil, err
	}
	return s.repo.userByID(ctx, userID)
}

// --------------------------------------------------------- password reset

// IssuePasswordReset creates a reset token, or reports that no account exists
// *to the caller only*.
//
// The handler must not pass that distinction on to the client: "no account
// with that address" on a public form is an enumeration oracle. The service
// returns it so the handler can skip sending mail, then answer identically
// either way.
func (s *Service) IssuePasswordReset(ctx context.Context, email string) (token string, user *User, err error) {
	user, err = s.repo.userByEmail(ctx, normalizeEmail(email))
	if err != nil {
		return "", nil, err
	}

	token, err = NewToken()
	if err != nil {
		return "", nil, err
	}

	err = s.repo.insertResetToken(
		ctx, ids.New(ids.PrefixResetToken), user.ID, HashToken(token),
		time.Now().Add(resetTokenLifetime))
	if err != nil {
		return "", nil, err
	}
	return token, user, nil
}

// ResetPassword redeems a reset token and sets a new password.
//
// Every other session is revoked. A password reset is what someone does when
// they believe their account is compromised, and leaving the attacker's
// session alive would make the reset theatre.
func (s *Service) ResetPassword(ctx context.Context, token, newPassword string) (*User, error) {
	if len(newPassword) < minPasswordLength {
		return nil, ErrWeakPassword
	}

	userID, err := s.repo.consumeResetToken(ctx, HashToken(token))
	if err != nil {
		return nil, err
	}

	hash, err := HashPassword(newPassword)
	if err != nil {
		return nil, err
	}
	if err := s.repo.updatePassword(ctx, userID, hash); err != nil {
		return nil, err
	}
	if err := s.repo.revokeAllSessions(ctx, userID, nil); err != nil {
		return nil, err
	}
	if err := s.repo.revokeAllTrustedDevices(ctx, userID); err != nil {
		return nil, err
	}
	if err := s.repo.clearFailedAttempts(ctx, userID); err != nil {
		return nil, err
	}

	return s.repo.userByID(ctx, userID)
}

// ChangePassword updates a password for a signed-in user.
//
// Other sessions are revoked but the current one is kept: forcing someone to
// sign in again immediately after they deliberately changed their password is
// friction with no security benefit, while leaving *other* devices signed in
// is the actual risk.
func (s *Service) ChangePassword(ctx context.Context, userID, currentPassword, newPassword, keepToken string) error {
	user, err := s.repo.userByID(ctx, userID)
	if err != nil {
		return err
	}
	if !VerifyPassword(user.PasswordHash, currentPassword) {
		return ErrPasswordMismatch
	}
	if len(newPassword) < minPasswordLength {
		return ErrWeakPassword
	}

	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.repo.updatePassword(ctx, userID, hash); err != nil {
		return err
	}
	if err := s.repo.revokeAllSessions(ctx, userID, optionalHash(keepToken)); err != nil {
		return err
	}
	return s.repo.revokeAllTrustedDevices(ctx, userID)
}

// ------------------------------------------------------------ magic links

// IssueMagicLink creates a single-use sign-in token.
func (s *Service) IssueMagicLink(ctx context.Context, email, redirectTo, ip string) (token string, user *User, err error) {
	user, err = s.repo.userByEmail(ctx, normalizeEmail(email))
	if err != nil {
		return "", nil, err
	}

	token, err = NewToken()
	if err != nil {
		return "", nil, err
	}

	err = s.repo.insertMagicLink(
		ctx, ids.New("mlk"), user.ID, user.Email, HashToken(token), redirectTo, ip,
		time.Now().Add(magicLinkLifetime))
	if err != nil {
		return "", nil, err
	}
	return token, user, nil
}

// RedeemMagicLink exchanges a link token for a session.
//
// The address is re-checked against the account's current one. A link mailed
// to an address that has since been changed must not still sign its holder in
// — otherwise "change your email" fails to revoke access from the old one.
func (s *Service) RedeemMagicLink(ctx context.Context, token, userAgent, ip string) (*SignInResult, error) {
	return s.RedeemMagicLinkWithTrustedDevice(ctx, token, userAgent, ip, "")
}

func (s *Service) RedeemMagicLinkWithTrustedDevice(ctx context.Context, token, userAgent, ip, trustedToken string) (*SignInResult, error) {
	userID, redirectTo, err := s.repo.consumeMagicLink(ctx, HashToken(token))
	if err != nil {
		return nil, err
	}

	user, err := s.repo.userByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// A magic link proves control of the mailbox, so it also verifies the
	// address if that had not happened yet.
	if err := s.repo.markEmailVerified(ctx, userID); err != nil {
		return nil, err
	}

	// A second factor still applies. A previously trusted device is the only
	// exception, and it is checked against this exact user above the session
	// boundary rather than treated as a general bearer login token.
	result, err := s.finishUserSignIn(ctx, user, userAgent, ip, trustedToken, AuthMethodMagicLink)
	if err != nil {
		return nil, err
	}
	_ = redirectTo // returned to the handler through the token row when needed
	return result, nil
}

// ------------------------------------------------------------------- TOTP

func (s *Service) issueTOTPChallenge(ctx context.Context, userID, userAgent, ip string) (*Challenge, error) {
	return s.issueTOTPChallengeWithMethod(ctx, userID, userAgent, ip, AuthMethodPassword)
}

func (s *Service) issueTOTPChallengeWithMethod(ctx context.Context, userID, userAgent, ip, authMethod string) (*Challenge, error) {
	token, err := NewToken()
	if err != nil {
		return nil, err
	}

	expiresAt := time.Now().Add(totpChallengeLifetime)
	err = s.repo.insertTOTPChallenge(
		ctx, ids.New("tch"), userID, HashToken(token), userAgent, ip, authMethod, expiresAt)
	if err != nil {
		return nil, err
	}
	return &Challenge{Token: token, ExpiresAt: expiresAt}, nil
}

// BeginTOTPEnrolment generates a secret and the URI to show as a QR code.
//
// The secret is not stored until CompleteTOTPEnrolment confirms the user can
// actually produce a code from it. Storing it at this step would let someone
// lock themselves out by scanning a QR code that never made it into their app.
func (s *Service) BeginTOTPEnrolment(ctx context.Context, userID, issuer string) (secret, uri string, err error) {
	enabled, err := s.repo.totpEnabled(ctx, userID)
	if err != nil {
		return "", "", err
	}
	if enabled {
		return "", "", ErrTOTPAlreadyOn
	}

	user, err := s.repo.userByID(ctx, userID)
	if err != nil {
		return "", "", err
	}

	secret, err = NewTOTPSecret()
	if err != nil {
		return "", "", err
	}
	return secret, TOTPProvisioningURI(secret, user.Email, issuer), nil
}

// CompleteTOTPEnrolment verifies a code against a candidate secret and, on
// success, stores it and returns fresh recovery codes.
//
// The plaintext codes are returned exactly once. Only their hashes are stored,
// so a database read cannot reconstruct them — the same rule as any other
// credential (§11.5).
func (s *Service) CompleteTOTPEnrolment(ctx context.Context, userID, secret, code string) ([]string, error) {
	if !VerifyTOTP(secret, code, time.Now()) {
		return nil, ErrTOTPInvalid
	}

	codes, err := NewRecoveryCodes(recoveryCodeCount)
	if err != nil {
		return nil, err
	}

	hashes := make([][]byte, len(codes))
	for i, code := range codes {
		hashes[i] = HashToken(NormalizeRecoveryCode(code))
	}

	if err := s.repo.enableTOTP(ctx, userID, secret, hashes); err != nil {
		return nil, err
	}
	return codes, nil
}

// DisableTOTP turns off two-factor authentication.
//
// Requires the current password: an unlocked laptop must not be enough to
// remove the factor that protects against an unlocked laptop.
func (s *Service) DisableTOTP(ctx context.Context, userID, password string) error {
	user, err := s.repo.userByID(ctx, userID)
	if err != nil {
		return err
	}
	if !VerifyPassword(user.PasswordHash, password) {
		return ErrPasswordMismatch
	}
	if err := s.repo.disableTOTP(ctx, userID); err != nil {
		return err
	}
	return s.repo.revokeAllTrustedDevices(ctx, userID)
}

// VerifyTOTPChallenge exchanges a challenge plus a code for a session.
//
// Accepts either an authenticator code or an unused recovery code, because
// "my phone is gone" is precisely when the second factor is most in the way.
func (s *Service) VerifyTOTPChallenge(ctx context.Context, challengeToken, code, userAgent, ip string) (*SignInResult, error) {
	return s.VerifyTOTPChallengeWithTrust(ctx, challengeToken, code, userAgent, ip, false)
}

func (s *Service) VerifyTOTPChallengeWithTrust(ctx context.Context, challengeToken, code, userAgent, ip string, trustDevice bool) (*SignInResult, error) {
	userID, attempts, authMethod, err := s.repo.loadTOTPChallenge(ctx, HashToken(challengeToken))
	if err != nil {
		return nil, err
	}
	if attempts >= maxTOTPAttempts {
		return nil, ErrAccountLocked
	}

	secret, err := s.repo.totpSecret(ctx, userID)
	if err != nil {
		return nil, err
	}

	ok := VerifyTOTP(secret, code, time.Now())
	if !ok {
		used, recoveryErr := s.repo.consumeRecoveryCode(ctx, userID, HashToken(NormalizeRecoveryCode(code)))
		if recoveryErr != nil {
			return nil, recoveryErr
		}
		ok = used
	}

	if !ok {
		if err := s.repo.recordTOTPAttempt(ctx, HashToken(challengeToken)); err != nil {
			return nil, err
		}
		return nil, ErrTOTPInvalid
	}

	if err := s.repo.consumeTOTPChallenge(ctx, HashToken(challengeToken)); err != nil {
		return nil, err
	}

	user, err := s.repo.userByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	var trusted *TrustedDeviceCredential
	if trustDevice {
		trusted, err = s.issueTrustedDevice(ctx, userID, userAgent, ip)
		if err != nil {
			return nil, err
		}
	}
	session, err := s.CreateSessionWithMethod(ctx, userID, userAgent, ip, authMethod)
	if err != nil {
		if trusted != nil {
			_ = s.repo.revokeTrustedDeviceByHash(ctx, userID, HashToken(trusted.Token))
		}
		return nil, err
	}
	if err := s.repo.markSignedIn(ctx, userID); err != nil {
		return nil, err
	}

	return &SignInResult{User: user, Session: session, TrustedDevice: trusted}, nil
}

// TOTPEnabled reports whether a user has a second factor configured.
func (s *Service) TOTPEnabled(ctx context.Context, userID string) (bool, error) {
	return s.repo.totpEnabled(ctx, userID)
}

// RemainingRecoveryCodes counts unused codes, so the interface can warn before
// the last one is spent.
func (s *Service) RemainingRecoveryCodes(ctx context.Context, userID string) (int, error) {
	return s.repo.countUnusedRecoveryCodes(ctx, userID)
}

// --------------------------------------------------------------- sessions

// SessionInfo describes one active session for the "where am I signed in"
// screen (§11.1 session management page).
type SessionInfo struct {
	ID         string
	UserAgent  string
	IP         string
	LastSeenAt time.Time
	CreatedAt  time.Time
	ExpiresAt  time.Time
	// Current marks the session making the request, so the interface can
	// label it rather than inviting someone to revoke themselves by accident.
	Current bool
}

// ListSessions returns a user's live sessions, most recently active first.
func (s *Service) ListSessions(ctx context.Context, userID, currentToken string) ([]SessionInfo, error) {
	currentHash := HashToken(currentToken)
	var out []SessionInfo
	var before time.Time
	var beforeID string
	for {
		page, err := s.repo.listSessionsPage(ctx, userID, currentHash, before, beforeID, 201)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if len(page) < 201 {
			return out, nil
		}
		last := page[len(page)-1]
		before, beforeID = last.LastSeenAt, last.ID
	}
}

// ListSessionsPage returns active sessions in stable, cursor-friendly order.
// The compatibility ListSessions method above remains available to internal
// callers that explicitly need the complete bounded set.
func (s *Service) ListSessionsPage(ctx context.Context, userID, currentToken string, before time.Time, beforeID string, limit int) ([]SessionInfo, error) {
	return s.repo.listSessionsPage(ctx, userID, HashToken(currentToken), before, beforeID, limit)
}

// RevokeSession ends one session.
//
// Scoped to the owning user: a session id is not a capability, and being able
// to revoke by id alone would let anyone sign everyone else out.
func (s *Service) RevokeSession(ctx context.Context, userID, sessionID string) error {
	return s.repo.revokeSessionByID(ctx, userID, sessionID)
}

// RevokeOtherSessions ends every session but the caller's own.
func (s *Service) RevokeOtherSessions(ctx context.Context, userID, keepToken string) error {
	return s.repo.revokeAllSessions(ctx, userID, optionalHash(keepToken))
}

// UpdateProfile changes the display name and avatar.
func (s *Service) UpdateProfile(ctx context.Context, userID, name, avatarURL string) (*User, error) {
	if err := s.repo.updateProfile(ctx, userID, name, avatarURL); err != nil {
		return nil, err
	}
	return s.repo.userByID(ctx, userID)
}

// UserByID exposes a user lookup for handlers that already hold an id.
func (s *Service) UserByID(ctx context.Context, userID string) (*User, error) {
	return s.repo.userByID(ctx, userID)
}

// AnyUserExists reports whether the installation has been set up at all.
// Used by the first-run flow to decide whether owner creation is still open.
func (s *Service) AnyUserExists(ctx context.Context) (bool, error) {
	return s.repo.anyUserExists(ctx)
}

const minPasswordLength = 12

// optionalHash converts a token to its stored hash, or nil when there is no
// token to spare. The nil case matters: revokeAllSessions treats it as "revoke
// everything", and hashing the empty string would instead spare a session that
// can never exist.
func optionalHash(token string) []byte {
	if token == "" {
		return nil
	}
	return HashToken(token)
}

// newRecoveryCodeID mints an id for one stored recovery code.
func newRecoveryCodeID() string {
	return ids.New("rcv")
}
