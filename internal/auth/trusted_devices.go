package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/ids"
)

const trustedDeviceLifetime = 30 * 24 * time.Hour

var ErrTrustedDeviceNotFound = errors.New("auth: trusted device not found")

// TrustedDeviceInfo is safe to show in account security settings. The raw
// credential is never returned after the one response that sets its cookie.
type TrustedDeviceInfo struct {
	ID         string
	Name       string
	UserAgent  string
	IP         string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	ExpiresAt  time.Time
	Current    bool
}

type TrustedDeviceCredential struct {
	Token     string
	ExpiresAt time.Time
}

func (s *Service) issueTrustedDevice(ctx context.Context, userID, userAgent, ip string) (*TrustedDeviceCredential, error) {
	token, err := NewToken()
	if err != nil {
		return nil, err
	}
	expiresAt := time.Now().Add(trustedDeviceLifetime)
	if err := s.repo.insertTrustedDevice(ctx, ids.New(ids.PrefixTrustedDevice), userID, HashToken(token), userAgent, ip, expiresAt); err != nil {
		return nil, err
	}
	return &TrustedDeviceCredential{Token: token, ExpiresAt: expiresAt}, nil
}

func (s *Service) trustedDeviceValid(ctx context.Context, userID, token string) (bool, error) {
	if token == "" {
		return false, nil
	}
	return s.repo.touchTrustedDevice(ctx, userID, HashToken(token))
}

// ListTrustedDevices returns only live devices owned by userID. The current
// raw cookie is compared by hash, just like sessions, so a database dump does
// not reveal which browser is currently trusted.
func (s *Service) ListTrustedDevices(ctx context.Context, userID, currentToken string) ([]TrustedDeviceInfo, error) {
	return s.repo.listTrustedDevices(ctx, userID, HashToken(currentToken))
}

// ListTrustedDevicesPage returns live devices in stable newest-first order.
// The compatibility ListTrustedDevices method remains available for bounded
// account-security callers that explicitly need the complete list.
func (s *Service) ListTrustedDevicesPage(ctx context.Context, userID, currentToken string, before time.Time, beforeID string, limit int) ([]TrustedDeviceInfo, error) {
	return s.repo.listTrustedDevicesPage(ctx, userID, HashToken(currentToken), before, beforeID, limit)
}

func (s *Service) RevokeTrustedDevice(ctx context.Context, userID, deviceID string) error {
	return s.repo.revokeTrustedDevice(ctx, userID, deviceID)
}

func (s *Service) RevokeAllTrustedDevices(ctx context.Context, userID string) error {
	return s.repo.revokeAllTrustedDevices(ctx, userID)
}

func (r *repository) insertTrustedDevice(ctx context.Context, id, userID string, tokenHash []byte, userAgent, ip string, expiresAt time.Time) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO trusted_devices (id, user_id, token_hash, name, user_agent, ip, expires_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, '')::inet, $7)
	`, id, userID, tokenHash, userAgent, userAgent, ip, expiresAt)
	if err != nil {
		return fmt.Errorf("auth: issue trusted device: %w", err)
	}
	return nil
}

func (r *repository) touchTrustedDevice(ctx context.Context, userID string, tokenHash []byte) (bool, error) {
	var id string
	err := r.pool.QueryRow(ctx, `
		UPDATE trusted_devices
		SET last_used_at = now()
		WHERE user_id = $1 AND token_hash = $2 AND revoked_at IS NULL AND expires_at > now()
		RETURNING id
	`, userID, tokenHash).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("auth: validate trusted device: %w", err)
	}
	return true, nil
}

func (r *repository) listTrustedDevices(ctx context.Context, userID string, currentHash []byte) ([]TrustedDeviceInfo, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, coalesce(name, ''), coalesce(user_agent, ''), coalesce(host(ip), ''),
		       created_at, last_used_at, expires_at, token_hash = $2
		FROM trusted_devices
		WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > now()
		ORDER BY created_at DESC, id DESC
	`, userID, currentHash)
	if err != nil {
		return nil, fmt.Errorf("auth: list trusted devices: %w", err)
	}
	defer rows.Close()

	devices := []TrustedDeviceInfo{}
	for rows.Next() {
		var device TrustedDeviceInfo
		if err := rows.Scan(&device.ID, &device.Name, &device.UserAgent, &device.IP,
			&device.CreatedAt, &device.LastUsedAt, &device.ExpiresAt, &device.Current); err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	return devices, rows.Err()
}

func (r *repository) listTrustedDevicesPage(ctx context.Context, userID string, currentHash []byte, before time.Time, beforeID string, limit int) ([]TrustedDeviceInfo, error) {
	if limit <= 0 || limit > 201 {
		limit = 101
	}
	args := []any{userID, currentHash}
	where := "user_id = $1 AND revoked_at IS NULL AND expires_at > now()"
	if !before.IsZero() {
		where += " AND (created_at, id) < ($3, $4)"
		args = append(args, before, beforeID)
	}
	args = append(args, limit)
	limitPlaceholder := fmt.Sprintf("$%d", len(args))
	rows, err := r.pool.Query(ctx, `
		SELECT id, coalesce(name, ''), coalesce(user_agent, ''), coalesce(host(ip), ''),
		       created_at, last_used_at, expires_at, token_hash = $2
		FROM trusted_devices
		WHERE `+where+`
		ORDER BY created_at DESC, id DESC
		LIMIT `+limitPlaceholder, args...)
	if err != nil {
		return nil, fmt.Errorf("auth: list trusted devices page: %w", err)
	}
	defer rows.Close()

	devices := make([]TrustedDeviceInfo, 0)
	for rows.Next() {
		var device TrustedDeviceInfo
		if err := rows.Scan(&device.ID, &device.Name, &device.UserAgent, &device.IP,
			&device.CreatedAt, &device.LastUsedAt, &device.ExpiresAt, &device.Current); err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	return devices, rows.Err()
}

func (r *repository) revokeTrustedDevice(ctx context.Context, userID, deviceID string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE trusted_devices SET revoked_at = now()
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL
	`, deviceID, userID)
	if err != nil {
		return fmt.Errorf("auth: revoke trusted device: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTrustedDeviceNotFound
	}
	return nil
}

func (r *repository) revokeAllTrustedDevices(ctx context.Context, userID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE trusted_devices SET revoked_at = now()
		WHERE user_id = $1 AND revoked_at IS NULL
	`, userID)
	if err != nil {
		return fmt.Errorf("auth: revoke trusted devices: %w", err)
	}
	return nil
}

func (r *repository) revokeTrustedDeviceByHash(ctx context.Context, userID string, tokenHash []byte) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE trusted_devices SET revoked_at = now()
		WHERE user_id = $1 AND token_hash = $2 AND revoked_at IS NULL
	`, userID, tokenHash)
	return err
}
