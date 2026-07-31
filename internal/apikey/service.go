// Package apikey owns workspace-scoped bearer credentials for the public API.
// Full tokens are returned only at creation time; authentication compares a
// SHA-256 digest and never needs to recover the original value.
package apikey

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidName  = errors.New("api key: name is required")
	ErrInvalidToken = errors.New("api key: invalid or expired token")
	ErrNotFound     = errors.New("api key: not found")
	ErrRandomness   = errors.New("api key: could not generate token")
)

type Service struct{ pool *database.Pool }

type Key struct {
	ID          string
	WorkspaceID string
	Name        string
	Prefix      string
	Scopes      []string
	LastUsedAt  *time.Time
	ExpiresAt   *time.Time
	CreatedBy   *string
	RevokedAt   *time.Time
	CreatedAt   time.Time
}

type Created struct {
	Key
	Token string
}

type Principal struct {
	KeyID       string
	WorkspaceID string
	CreatedBy   *string
	Scopes      []string
}

func New(pool *database.Pool) *Service { return &Service{pool: pool} }

func (s *Service) Create(ctx context.Context, workspaceID, memberID, name string, scopes []string, expiresAt *time.Time) (*Created, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidName
	}
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	hash := tokenHash(token)
	id := ids.New(ids.PrefixAPIKey)
	prefix := token
	if len(prefix) > 22 {
		prefix = prefix[:22]
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO api_keys (id, workspace_id, name, prefix, key_hash, scopes, expires_at, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`, id, workspaceID, name, prefix, hash[:], uniqueStrings(scopes), expiresAt, memberID)
	if err != nil {
		return nil, fmt.Errorf("api key: create: %w", err)
	}
	key, err := s.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	return &Created{Key: *key, Token: token}, nil
}

func (s *Service) List(ctx context.Context, workspaceID string) ([]Key, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, workspace_id, name, prefix, scopes, last_used_at, expires_at,
		       created_by, revoked_at, created_at
		FROM api_keys WHERE workspace_id=$1 ORDER BY created_at DESC, id DESC
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("api key: list: %w", err)
	}
	defer rows.Close()
	result := make([]Key, 0)
	for rows.Next() {
		key, scanErr := scanKey(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, *key)
	}
	return result, rows.Err()
}

func (s *Service) Get(ctx context.Context, workspaceID, id string) (*Key, error) {
	key, err := scanKey(s.pool.QueryRow(ctx, `
		SELECT id, workspace_id, name, prefix, scopes, last_used_at, expires_at,
		       created_by, revoked_at, created_at
		FROM api_keys WHERE workspace_id=$1 AND id=$2
	`, workspaceID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("api key: get: %w", err)
	}
	return key, nil
}

func (s *Service) Revoke(ctx context.Context, workspaceID, id string) error {
	result, err := s.pool.Exec(ctx, `
		UPDATE api_keys SET revoked_at=coalesce(revoked_at, now())
		WHERE workspace_id=$1 AND id=$2
	`, workspaceID, id)
	if err != nil {
		return fmt.Errorf("api key: revoke: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Authenticate resolves a raw Bearer token and records usage only after all
// revocation and expiry predicates have passed. The returned principal is
// intentionally narrower than an agent session; the caller maps scopes to
// capabilities and keeps tenant selection tied to the key's workspace.
func (s *Service) Authenticate(ctx context.Context, raw string) (*Principal, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "hc_live_") {
		return nil, ErrInvalidToken
	}
	hash := tokenHash(raw)
	var principal Principal
	var scopes []string
	err := s.pool.QueryRow(ctx, `
		SELECT id, workspace_id, created_by, scopes
		FROM api_keys
		WHERE key_hash=$1 AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > now())
	`, hash[:]).Scan(&principal.KeyID, &principal.WorkspaceID, &principal.CreatedBy, &scopes)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, fmt.Errorf("api key: authenticate: %w", err)
	}
	principal.Scopes = uniqueStrings(scopes)
	if _, err := s.pool.Exec(ctx, `UPDATE api_keys SET last_used_at=now() WHERE id=$1`, principal.KeyID); err != nil {
		return nil, fmt.Errorf("api key: record usage: %w", err)
	}
	return &principal, nil
}

func newToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("%w: %v", ErrRandomness, err)
	}
	return "hc_live_" + base64.RawURLEncoding.EncodeToString(bytes), nil
}

func tokenHash(token string) [sha256.Size]byte { return sha256.Sum256([]byte(token)) }

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func scanKey(row interface{ Scan(...any) error }) (*Key, error) {
	var key Key
	err := row.Scan(&key.ID, &key.WorkspaceID, &key.Name, &key.Prefix, &key.Scopes,
		&key.LastUsedAt, &key.ExpiresAt, &key.CreatedBy, &key.RevokedAt, &key.CreatedAt)
	return &key, err
}
