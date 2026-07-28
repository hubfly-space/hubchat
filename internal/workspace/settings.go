package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
)

// Settings is the extensible part of a workspace's configuration.
//
// The core identity fields (name, slug, timezone, ticket prefix) are their
// own columns because every query in the product joins or filters on them.
// Everything here is different: each field is read by exactly one settings
// screen and never queried against, which is precisely what
// docs/architecture.md's "JSONB for bounded flexible payloads" convention is
// for. The Go struct is still a fixed, validated shape — this is not an
// arbitrary key-value store, just a column that happens to be jsonb instead
// of six more columns on `workspaces`.
type Settings struct {
	Branding BrandingSettings `json:"branding"`
	Security SecuritySettings `json:"security"`
	Privacy  PrivacySettings  `json:"privacy"`
}

type BrandingSettings struct {
	AccentColor  string `json:"accent_color"`
	EmailFooter  string `json:"email_footer"`
	HideBranding bool   `json:"hide_branding"`
}

type SecuritySettings struct {
	RequireTwoFactor    bool     `json:"require_two_factor"`
	RestrictSignup      bool     `json:"restrict_signup"`
	AllowedEmailDomains []string `json:"allowed_email_domains"`
	IPAllowlist         []string `json:"ip_allowlist"`
}

type PrivacySettings struct {
	// "full" | "country_only" | "none" — §12 IP storage policy.
	IPStorage      string `json:"ip_storage"`
	TrackPageViews bool   `json:"track_page_views"`
	// Retention in days per category, keyed by the category names §12 and
	// the export/delete workflows use: "conversations", "events", "sessions",
	// "audit_logs", "webhook_deliveries", "survey_responses". 0 means forever.
	RetentionDays           map[string]int `json:"retention_days"`
	AllowedLocalStorageKeys []string       `json:"allowed_local_storage_keys"`
	AllowedCookieKeys       []string       `json:"allowed_cookie_keys"`
	RequireConsent          bool           `json:"require_consent"`
	ConsentNotice           string         `json:"consent_notice"`
	PrivacyPolicyURL        string         `json:"privacy_policy_url"`
}

// defaultSettings is what a workspace has before anyone has touched a
// settings screen — the DB column starts as `{}`, and unmarshalling that into
// this struct already zero-values everything, so this function mostly exists
// to name the *product* defaults (e.g. "full" IP storage until Privacy is
// visited) rather than accept Go's zero value as an accidental policy choice.
func defaultSettings() Settings {
	return Settings{
		Security: SecuritySettings{
			AllowedEmailDomains: []string{},
			IPAllowlist:         []string{},
		},
		Privacy: PrivacySettings{
			IPStorage:               "full",
			RetentionDays:           map[string]int{},
			AllowedLocalStorageKeys: []string{},
			AllowedCookieKeys:       []string{},
		},
	}
}

// normalize fills nil slices/maps with empty ones after unmarshalling.
//
// A settings row saved before a given field existed — or one written back by
// an older build that round-tripped a Go zero value — stores that field as
// JSON `null`. Go happily unmarshals `null` into a nil slice, and this is the
// one place that gets cleaned up before the API or any caller sees it, so
// "every array field is always an array" is a property of the type, not
// something each screen has to defend against on its own.
func (s *Settings) normalize() {
	if s.Security.AllowedEmailDomains == nil {
		s.Security.AllowedEmailDomains = []string{}
	}
	if s.Security.IPAllowlist == nil {
		s.Security.IPAllowlist = []string{}
	}
	if s.Privacy.RetentionDays == nil {
		s.Privacy.RetentionDays = map[string]int{}
	}
	if s.Privacy.AllowedLocalStorageKeys == nil {
		s.Privacy.AllowedLocalStorageKeys = []string{}
	}
	if s.Privacy.AllowedCookieKeys == nil {
		s.Privacy.AllowedCookieKeys = []string{}
	}
}

var validIPStorage = map[string]bool{"full": true, "country_only": true, "none": true}

// ErrInvalidSettings is returned when a settings payload fails validation.
var ErrInvalidSettings = errors.New("workspace: invalid settings value")

// GetSettings returns the workspace's settings, filled in with product
// defaults for anything never explicitly saved.
func (s *Service) GetSettings(ctx context.Context, workspaceID string) (*Settings, error) {
	raw, err := s.repo.settingsJSON(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	settings := defaultSettings()
	if len(raw) > 0 && string(raw) != "{}" {
		if err := json.Unmarshal(raw, &settings); err != nil {
			return nil, fmt.Errorf("workspace: decode settings: %w", err)
		}
	}
	settings.normalize()
	return &settings, nil
}

// UpdateGeneral changes the core identity fields (§6.1 workspace settings).
func (s *Service) UpdateGeneral(
	ctx context.Context, workspaceID, actorMemberID, name, ticketPrefix, timezone, defaultLanguage string,
) (*Workspace, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidName
	}
	ticketPrefix = strings.ToUpper(strings.TrimSpace(ticketPrefix))
	if !ticketPrefixPattern.MatchString(ticketPrefix) {
		return nil, ErrInvalidSettings
	}

	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.updateGeneral(ctx, tx, workspaceID, name, ticketPrefix, timezone, defaultLanguage); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: audit.WorkspaceUpdated, EntityType: "workspace", EntityID: workspaceID,
			Metadata: map[string]any{"section": "general"},
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: "workspace.updated",
			EntityType: "workspace", EntityID: workspaceID,
			ActorType: events.ActorUser, ActorID: actorMemberID,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repo.byID(ctx, workspaceID)
}

var ticketPrefixPattern = regexp.MustCompile(`^[A-Z]{2,8}$`)

// UpdateBranding changes logo/icon URLs plus the branding settings group.
func (s *Service) UpdateBranding(
	ctx context.Context, workspaceID, actorMemberID string, logoURL, iconURL *string, branding BrandingSettings,
) (*Workspace, error) {
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.updateBrandingURLs(ctx, tx, workspaceID, logoURL, iconURL); err != nil {
			return err
		}
		if err := s.repo.mergeSettings(ctx, tx, workspaceID, func(current *Settings) {
			current.Branding = branding
		}); err != nil {
			return err
		}
		return s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: audit.WorkspaceUpdated, EntityType: "workspace", EntityID: workspaceID,
			Metadata: map[string]any{"section": "branding"},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repo.byID(ctx, workspaceID)
}

// UpdateSecuritySettings changes the security policy group.
//
// Gated on workspace.manage_security rather than workspace.manage at the
// handler layer — this is the one settings group §5.1/§5.2 reserves for the
// owner and security-privileged admins specifically, not general
// administration.
func (s *Service) UpdateSecuritySettings(
	ctx context.Context, workspaceID, actorMemberID string, security SecuritySettings,
) (*Settings, error) {
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.mergeSettings(ctx, tx, workspaceID, func(current *Settings) {
			current.Security = security
		}); err != nil {
			return err
		}
		return s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "workspace.security_settings_changed", EntityType: "workspace", EntityID: workspaceID,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.GetSettings(ctx, workspaceID)
}

// UpdatePrivacySettings changes the privacy and retention policy group.
func (s *Service) UpdatePrivacySettings(
	ctx context.Context, workspaceID, actorMemberID string, privacy PrivacySettings,
) (*Settings, error) {
	if privacy.IPStorage == "" {
		privacy.IPStorage = "full"
	}
	if !validIPStorage[privacy.IPStorage] {
		return nil, ErrInvalidSettings
	}
	for category, days := range privacy.RetentionDays {
		if days < 0 {
			return nil, fmt.Errorf("%w: retention for %q cannot be negative", ErrInvalidSettings, category)
		}
	}

	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.mergeSettings(ctx, tx, workspaceID, func(current *Settings) {
			current.Privacy = privacy
		}); err != nil {
			return err
		}
		return s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "workspace.privacy_settings_changed", EntityType: "workspace", EntityID: workspaceID,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.GetSettings(ctx, workspaceID)
}

// ---------------------------------------------------------------- repository

func (r *repository) settingsJSON(ctx context.Context, workspaceID string) (json.RawMessage, error) {
	var raw json.RawMessage
	err := r.pool.QueryRow(ctx,
		`SELECT settings FROM workspaces WHERE id = $1`, workspaceID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("workspace: read settings: %w", err)
	}
	return raw, nil
}

func (r *repository) updateGeneral(
	ctx context.Context, tx pgx.Tx, workspaceID, name, ticketPrefix, timezone, defaultLanguage string,
) error {
	tag, err := tx.Exec(ctx, `
		UPDATE workspaces
		SET name = $2, ticket_prefix = $3, timezone = $4, default_language = $5, updated_at = now()
		WHERE id = $1
	`, workspaceID, name, ticketPrefix, timezone, defaultLanguage)
	if err != nil {
		return fmt.Errorf("workspace: update general settings: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *repository) updateBrandingURLs(ctx context.Context, tx pgx.Tx, workspaceID string, logoURL, iconURL *string) error {
	tag, err := tx.Exec(ctx, `
		UPDATE workspaces SET logo_url = $2, icon_url = $3, updated_at = now()
		WHERE id = $1
	`, workspaceID, logoURL, iconURL)
	if err != nil {
		return fmt.Errorf("workspace: update branding urls: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// mergeSettings reads the current settings under a row lock, applies mutate,
// and writes the result back — a read-modify-write rather than a blind
// overwrite, so updating the Security group cannot race with, and silently
// discard, a concurrent update to Branding.
func (r *repository) mergeSettings(ctx context.Context, tx pgx.Tx, workspaceID string, mutate func(*Settings)) error {
	var raw json.RawMessage
	err := tx.QueryRow(ctx,
		`SELECT settings FROM workspaces WHERE id = $1 FOR UPDATE`, workspaceID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("workspace: lock settings: %w", err)
	}

	settings := defaultSettings()
	if len(raw) > 0 && string(raw) != "{}" {
		if err := json.Unmarshal(raw, &settings); err != nil {
			return fmt.Errorf("workspace: decode settings: %w", err)
		}
	}
	settings.normalize()

	mutate(&settings)

	encoded, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("workspace: encode settings: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE workspaces SET settings = $2, updated_at = now() WHERE id = $1`,
		workspaceID, encoded,
	); err != nil {
		return fmt.Errorf("workspace: write settings: %w", err)
	}
	return nil
}
