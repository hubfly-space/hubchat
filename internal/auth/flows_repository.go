package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ----------------------------------------------------------------- lockout

// lockState reports whether the account is currently locked, clearing an
// expired lock as a side effect.
//
// Clearing on read rather than on a schedule means there is no sweep to run
// and no window where a lock outlives its window because a job was behind.
func (r *repository) lockState(ctx context.Context, userID string) (bool, error) {
	var locked bool
	err := r.pool.QueryRow(ctx, `
		UPDATE users
		SET locked_until    = CASE WHEN locked_until <= now() THEN NULL ELSE locked_until END,
		    failed_attempts = CASE WHEN locked_until <= now() THEN 0 ELSE failed_attempts END
		WHERE id = $1
		RETURNING locked_until IS NOT NULL AND locked_until > now()
	`, userID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrUserNotFound
	}
	if err != nil {
		return false, fmt.Errorf("auth: read lock state: %w", err)
	}
	return locked, nil
}

// recordFailedAttempt increments the counter and locks the account once it
// crosses the threshold.
//
// Both happen in one statement so concurrent attempts cannot interleave a read
// and a write and let the limit be exceeded — which is exactly what a parallel
// password-spray does.
func (r *repository) recordFailedAttempt(ctx context.Context, userID string, limit int, window time.Duration) error {
	if limit <= 0 {
		limit = 5
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE users
		SET failed_attempts = failed_attempts + 1,
		    locked_until = CASE
		        WHEN failed_attempts + 1 >= $2 THEN now() + $3::interval
		        ELSE locked_until
		    END
		WHERE id = $1
	`, userID, limit, window.String())
	if err != nil {
		return fmt.Errorf("auth: record failed attempt: %w", err)
	}
	return nil
}

func (r *repository) clearFailedAttempts(ctx context.Context, userID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE users SET failed_attempts = 0, locked_until = NULL WHERE id = $1
	`, userID)
	if err != nil {
		return fmt.Errorf("auth: clear failed attempts: %w", err)
	}
	return nil
}

func (r *repository) markSignedIn(ctx context.Context, userID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE users SET last_sign_in_at = now() WHERE id = $1`, userID)
	return err
}

func (r *repository) anyUserExists(ctx context.Context) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users)`).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("auth: check for users: %w", err)
	}
	return exists, nil
}

// ------------------------------------------------------ email verification

func (r *repository) insertVerificationToken(
	ctx context.Context, id, userID, email string, tokenHash []byte, expiresAt time.Time,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO email_verification_tokens (id, user_id, email, token_hash, expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`, id, userID, email, tokenHash, expiresAt)
	if err != nil {
		return fmt.Errorf("auth: issue verification token: %w", err)
	}
	return nil
}

// consumeVerificationToken redeems a token and marks the address verified.
//
// The UPDATE ... WHERE used_at IS NULL is what makes it single-use: two
// concurrent redemptions cannot both match, so the second gets no row rather
// than a second success.
func (r *repository) consumeVerificationToken(ctx context.Context, tokenHash []byte) (string, error) {
	var userID string
	var expiresAt time.Time

	err := r.pool.QueryRow(ctx, `
		UPDATE email_verification_tokens
		SET used_at = now()
		WHERE token_hash = $1 AND used_at IS NULL
		RETURNING user_id, expires_at
	`, tokenHash).Scan(&userID, &expiresAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrTokenInvalid
	}
	if err != nil {
		return "", fmt.Errorf("auth: consume verification token: %w", err)
	}
	if time.Now().After(expiresAt) {
		return "", ErrTokenExpired
	}

	if err := r.markEmailVerified(ctx, userID); err != nil {
		return "", err
	}
	return userID, nil
}

func (r *repository) markEmailVerified(ctx context.Context, userID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE users SET email_verified_at = coalesce(email_verified_at, now())
		WHERE id = $1
	`, userID)
	if err != nil {
		return fmt.Errorf("auth: mark email verified: %w", err)
	}
	return nil
}

// ---------------------------------------------------------- password reset

func (r *repository) insertResetToken(
	ctx context.Context, id, userID string, tokenHash []byte, expiresAt time.Time,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO password_reset_tokens (id, user_id, token_hash, expires_at)
		VALUES ($1, $2, $3, $4)
	`, id, userID, tokenHash, expiresAt)
	if err != nil {
		return fmt.Errorf("auth: issue reset token: %w", err)
	}
	return nil
}

func (r *repository) consumeResetToken(ctx context.Context, tokenHash []byte) (string, error) {
	var userID string
	var expiresAt time.Time

	err := r.pool.QueryRow(ctx, `
		UPDATE password_reset_tokens
		SET used_at = now()
		WHERE token_hash = $1 AND used_at IS NULL
		RETURNING user_id, expires_at
	`, tokenHash).Scan(&userID, &expiresAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrTokenInvalid
	}
	if err != nil {
		return "", fmt.Errorf("auth: consume reset token: %w", err)
	}
	if time.Now().After(expiresAt) {
		return "", ErrTokenExpired
	}
	return userID, nil
}

func (r *repository) updatePassword(ctx context.Context, userID, hash string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1
	`, userID, hash)
	if err != nil {
		return fmt.Errorf("auth: update password: %w", err)
	}
	return nil
}

func (r *repository) updateProfile(ctx context.Context, userID, name, avatarURL string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE users
		SET name = $2, avatar_url = NULLIF($3, ''), updated_at = now()
		WHERE id = $1
	`, userID, name, avatarURL)
	if err != nil {
		return fmt.Errorf("auth: update profile: %w", err)
	}
	return nil
}

// ------------------------------------------------------------ magic links

func (r *repository) insertMagicLink(
	ctx context.Context, id, userID, email string, tokenHash []byte,
	redirectTo, ip string, expiresAt time.Time,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO magic_link_tokens
			(id, user_id, email, token_hash, redirect_to, ip, expires_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, '')::inet, $7)
	`, id, userID, email, tokenHash, redirectTo, ip, expiresAt)
	if err != nil {
		return fmt.Errorf("auth: issue magic link: %w", err)
	}
	return nil
}

// consumeMagicLink redeems a link, checking that the address it was sent to is
// still the account's own.
func (r *repository) consumeMagicLink(ctx context.Context, tokenHash []byte) (userID, redirectTo string, err error) {
	var expiresAt time.Time
	var redirect *string
	var stillCurrent bool

	err = r.pool.QueryRow(ctx, `
		UPDATE magic_link_tokens t
		SET used_at = now()
		FROM users u
		WHERE t.token_hash = $1 AND t.used_at IS NULL AND u.id = t.user_id
		RETURNING t.user_id, t.expires_at, t.redirect_to, (u.email = t.email)
	`, tokenHash).Scan(&userID, &expiresAt, &redirect, &stillCurrent)

	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrTokenInvalid
	}
	if err != nil {
		return "", "", fmt.Errorf("auth: consume magic link: %w", err)
	}
	if time.Now().After(expiresAt) {
		return "", "", ErrTokenExpired
	}
	if !stillCurrent {
		// The account's address changed after the link was sent. Honouring it
		// would mean changing an email fails to cut off the previous one.
		return "", "", ErrTokenInvalid
	}

	if redirect != nil {
		redirectTo = *redirect
	}
	return userID, redirectTo, nil
}

// ------------------------------------------------------------------- TOTP

func (r *repository) totpEnabled(ctx context.Context, userID string) (bool, error) {
	var enabled bool
	err := r.pool.QueryRow(ctx, `
		SELECT totp_enabled_at IS NOT NULL AND totp_secret IS NOT NULL
		FROM users WHERE id = $1
	`, userID).Scan(&enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrUserNotFound
	}
	if err != nil {
		return false, fmt.Errorf("auth: read totp state: %w", err)
	}
	return enabled, nil
}

func (r *repository) totpSecret(ctx context.Context, userID string) (string, error) {
	var secret *string
	err := r.pool.QueryRow(ctx,
		`SELECT totp_secret FROM users WHERE id = $1`, userID).Scan(&secret)
	if err != nil {
		return "", fmt.Errorf("auth: read totp secret: %w", err)
	}
	if secret == nil {
		return "", ErrTOTPNotEnabled
	}
	return *secret, nil
}

// enableTOTP stores the secret and replaces the recovery codes atomically.
//
// One transaction because a user left with a secret but no recovery codes has
// no way back in if they lose the device — and that is precisely the state a
// partial write would produce.
func (r *repository) enableTOTP(ctx context.Context, userID, secret string, codeHashes [][]byte) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("auth: begin totp enrolment: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE users SET totp_secret = $2, totp_enabled_at = now(), updated_at = now()
		WHERE id = $1
	`, userID, secret); err != nil {
		return fmt.Errorf("auth: store totp secret: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM recovery_codes WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("auth: clear recovery codes: %w", err)
	}

	for _, hash := range codeHashes {
		if _, err := tx.Exec(ctx, `
			INSERT INTO recovery_codes (id, user_id, code_hash) VALUES ($1, $2, $3)
		`, newRecoveryCodeID(), userID, hash); err != nil {
			return fmt.Errorf("auth: store recovery code: %w", err)
		}
	}

	return tx.Commit(ctx)
}

func (r *repository) disableTOTP(ctx context.Context, userID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("auth: begin totp removal: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE users SET totp_secret = NULL, totp_enabled_at = NULL, updated_at = now()
		WHERE id = $1
	`, userID); err != nil {
		return fmt.Errorf("auth: clear totp secret: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM recovery_codes WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("auth: clear recovery codes: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *repository) insertTOTPChallenge(
	ctx context.Context, id, userID string, tokenHash []byte,
	userAgent, ip string, expiresAt time.Time,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO totp_challenges (id, user_id, token_hash, user_agent, ip, expires_at)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, '')::inet, $6)
	`, id, userID, tokenHash, userAgent, ip, expiresAt)
	if err != nil {
		return fmt.Errorf("auth: issue totp challenge: %w", err)
	}
	return nil
}

func (r *repository) loadTOTPChallenge(ctx context.Context, tokenHash []byte) (userID string, attempts int, err error) {
	var expiresAt time.Time
	err = r.pool.QueryRow(ctx, `
		SELECT user_id, attempts, expires_at
		FROM totp_challenges
		WHERE token_hash = $1 AND used_at IS NULL
	`, tokenHash).Scan(&userID, &attempts, &expiresAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, ErrTokenInvalid
	}
	if err != nil {
		return "", 0, fmt.Errorf("auth: load totp challenge: %w", err)
	}
	if time.Now().After(expiresAt) {
		return "", 0, ErrTokenExpired
	}
	return userID, attempts, nil
}

func (r *repository) recordTOTPAttempt(ctx context.Context, tokenHash []byte) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE totp_challenges SET attempts = attempts + 1 WHERE token_hash = $1`, tokenHash)
	return err
}

func (r *repository) consumeTOTPChallenge(ctx context.Context, tokenHash []byte) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE totp_challenges SET used_at = now() WHERE token_hash = $1`, tokenHash)
	return err
}

// consumeRecoveryCode marks a code used and reports whether it was valid.
//
// Single-use is enforced by the WHERE clause, not by a read-then-write: two
// simultaneous redemptions of the same code must not both succeed.
func (r *repository) consumeRecoveryCode(ctx context.Context, userID string, codeHash []byte) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE recovery_codes
		SET used_at = now()
		WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL
	`, userID, codeHash)
	if err != nil {
		return false, fmt.Errorf("auth: consume recovery code: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *repository) countUnusedRecoveryCodes(ctx context.Context, userID string) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM recovery_codes WHERE user_id = $1 AND used_at IS NULL
	`, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("auth: count recovery codes: %w", err)
	}
	return count, nil
}

// --------------------------------------------------------------- sessions

func (r *repository) listSessionsPage(ctx context.Context, userID string, currentHash []byte, before time.Time, beforeID string, limit int) ([]SessionInfo, error) {
	if limit <= 0 || limit > 201 {
		limit = 101
	}
	where := "user_id = $1 AND revoked_at IS NULL AND expires_at > now()"
	args := []any{userID, currentHash}
	if !before.IsZero() {
		where += " AND (last_seen_at, id) < ($3, $4)"
		args = append(args, before, beforeID)
	}
	args = append(args, limit)
	limitPlaceholder := fmt.Sprintf("$%d", len(args))
	rows, err := r.pool.Query(ctx, `
		SELECT id, coalesce(user_agent, ''), coalesce(host(ip), ''),
		       last_seen_at, created_at, expires_at, token_hash = $2
		FROM user_sessions
		WHERE `+where+`
		ORDER BY last_seen_at DESC, id DESC
		LIMIT `+limitPlaceholder, args...)
	if err != nil {
		return nil, fmt.Errorf("auth: list sessions: %w", err)
	}
	defer rows.Close()

	out := []SessionInfo{}
	for rows.Next() {
		var s SessionInfo
		if err := rows.Scan(&s.ID, &s.UserAgent, &s.IP,
			&s.LastSeenAt, &s.CreatedAt, &s.ExpiresAt, &s.Current); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *repository) revokeSessionByID(ctx context.Context, userID, sessionID string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE user_sessions SET revoked_at = now()
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL
	`, sessionID, userID)
	if err != nil {
		return fmt.Errorf("auth: revoke session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// revokeAllSessions ends every session for a user, optionally sparing one.
func (r *repository) revokeAllSessions(ctx context.Context, userID string, keepHash []byte) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE user_sessions SET revoked_at = now()
		WHERE user_id = $1
		  AND revoked_at IS NULL
		  AND ($2::bytea IS NULL OR token_hash <> $2)
	`, userID, keepHash)
	if err != nil {
		return fmt.Errorf("auth: revoke sessions: %w", err)
	}
	return nil
}

// ErrSessionNotFound is returned when a session id does not belong to the user.
var ErrSessionNotFound = errors.New("auth: session not found")
