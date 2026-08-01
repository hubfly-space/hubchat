package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/database"
)

// User is the subset of the users row this module needs. It is not the API
// representation — handlers shape their own response bodies.
type User struct {
	ID           string
	Name         string
	Email        string
	PasswordHash string
	AuthMethod   string
	CreatedAt    time.Time
}

var ErrUserNotFound = errors.New("auth: user not found")
var ErrEmailTaken = errors.New("auth: email already registered")

type repository struct {
	pool *database.Pool
}

func (r *repository) insertUser(ctx context.Context, id, name, email, passwordHash string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO users (id, name, email, password_hash, email_verified_at)
		VALUES ($1, $2, $3, $4, now())
	`, id, name, email, passwordHash)
	// email_verified_at is set immediately in this build: outbound email is
	// often unconfigured on a fresh self-host, and requiring verification
	// before anyone can sign in would lock out the very first owner account.
	// A real verification flow is a follow-up, not a foundation-blocking gap.

	if err != nil && isUniqueViolation(err) {
		return ErrEmailTaken
	}
	return err
}

func (r *repository) userByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	err := r.pool.QueryRow(ctx, `
		SELECT id, name, email, password_hash, created_at
		FROM users
		WHERE email = $1
	`, email).Scan(&u.ID, &u.Name, &u.Email, &u.PasswordHash, &u.CreatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *repository) userByID(ctx context.Context, id string) (*User, error) {
	var u User
	err := r.pool.QueryRow(ctx, `
		SELECT id, name, email, password_hash, created_at
		FROM users
		WHERE id = $1
	`, id).Scan(&u.ID, &u.Name, &u.Email, &u.PasswordHash, &u.CreatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *repository) insertSession(
	ctx context.Context,
	id, userID string,
	tokenHash []byte,
	userAgent, ip, authMethod string,
	expiresAt time.Time,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO user_sessions (id, user_id, token_hash, user_agent, ip, auth_method, expires_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, '')::inet, $6, $7)
	`, id, userID, tokenHash, userAgent, ip, authMethod, expiresAt)
	return err
}

// sessionUser resolves a session token hash to its user, only when the
// session is neither revoked nor expired, and bumps last_seen_at in the same
// query so a busy dashboard is not issuing a second write per request.
func (r *repository) sessionUser(ctx context.Context, tokenHash []byte) (*User, error) {
	var u User
	err := r.pool.QueryRow(ctx, `
		UPDATE user_sessions s
		SET last_seen_at = now()
		FROM users u
		WHERE s.token_hash = $1
		  AND s.revoked_at IS NULL
		  AND s.expires_at > now()
		  AND u.id = s.user_id
		RETURNING u.id, u.name, u.email, u.password_hash, u.created_at, s.auth_method
	`, tokenHash).Scan(&u.ID, &u.Name, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.AuthMethod)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *repository) revokeSession(ctx context.Context, tokenHash []byte) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE user_sessions SET revoked_at = now()
		WHERE token_hash = $1 AND revoked_at IS NULL
	`, tokenHash)
	return err
}

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}
