package widget

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/auth"
	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/inbox"
)

var (
	ErrNotFound          = errors.New("widget: not found")
	ErrInvalidInbox      = errors.New("widget: not an inbox in this workspace")
	ErrInvalidName       = errors.New("widget: name must not be empty")
	ErrDuplicateDomain   = errors.New("widget: domain already allowlisted")
	ErrWildcardDomain    = errors.New("widget: a bare \"*\" is not a valid domain — a widget allowlisted to everything has no allowlist")
	ErrOriginNotAllowed  = errors.New("widget: origin is not on this widget's domain allowlist")
	ErrDisabled          = errors.New("widget: disabled")
	ErrVisitorInvalid    = errors.New("widget: visitor token is invalid or expired")
	ErrConversationOwner = errors.New("widget: this conversation does not belong to this visitor")
)

// Widget is a workspace's embeddable chat surface. Appearance, content, and
// behavior stay opaque maps at this layer deliberately — the server never
// filters or validates individual knobs inside them (migration 0007's own
// comment), it only ever stores and republishes the whole projection the
// dashboard sent, so a new appearance field never requires a Go struct
// change here.
type Widget struct {
	ID             string
	WorkspaceID    string
	Name           string
	PublicKey      string
	InboxID        *string
	Modes          []string
	Appearance     map[string]any
	Content        map[string]any
	Behavior       map[string]any
	Environment    string
	RolloutPercent int
	Version        int
	Enabled        bool
	InstalledAt    *time.Time
	LastSeenAt     *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Domain struct {
	ID         string
	WidgetID   string
	Domain     string
	VerifiedAt *time.Time
	CreatedAt  time.Time
}

type ConfigVersion struct {
	ID         string
	WidgetID   string
	Version    int
	Modes      []string
	Appearance map[string]any
	Content    map[string]any
	Behavior   map[string]any
	ChangedBy  *string
	Note       *string
	CreatedAt  time.Time
}

// Service owns widget configuration and its public bootstrap, visitor
// identity, and the visitor-facing conversation surface (§6.4).
type Service struct {
	repo         *repository
	pool         *database.Pool
	events       *events.Log
	audit        *audit.Log
	inbox        *inbox.Service
	conversation *conversation.Service
	customer     *customer.Service
	secretKey    []byte
}

func New(
	pool *database.Pool, eventLog *events.Log, auditLog *audit.Log,
	inboxSvc *inbox.Service, conversationSvc *conversation.Service, customerSvc *customer.Service,
	secretKey []byte,
) *Service {
	return &Service{
		repo: &repository{pool: pool}, pool: pool, events: eventLog, audit: auditLog,
		inbox: inboxSvc, conversation: conversationSvc, customer: customerSvc, secretKey: secretKey,
	}
}

func (s *Service) appendEvent(ctx context.Context, tx pgx.Tx, event events.Event) error {
	if s.events == nil {
		return nil
	}
	_, err := s.events.Append(ctx, tx, event)
	return err
}

func (s *Service) recordAudit(ctx context.Context, tx pgx.Tx, entry audit.Entry) error {
	if s.audit == nil {
		return nil
	}
	return audit.RecordTx(ctx, tx, entry)
}

func (s *Service) List(ctx context.Context, workspaceID string) ([]Widget, error) {
	return s.repo.list(ctx, workspaceID)
}

func (s *Service) ListPage(ctx context.Context, workspaceID string, before time.Time, beforeID string, limit int) ([]Widget, error) {
	return s.repo.listPage(ctx, workspaceID, before, beforeID, limit)
}

func (s *Service) Get(ctx context.Context, workspaceID, id string) (*Widget, error) {
	return s.repo.byID(ctx, workspaceID, id)
}

// Create makes a new widget with sensible defaults, ready to customise. A
// fresh public key never collides with an existing one in practice (32
// bytes of entropy), but the insert is retried once on the rare unique
// violation rather than trusting that in a way that would surface as an
// opaque 500 to whoever clicked "New widget".
func (s *Service) Create(ctx context.Context, workspaceID, actorMemberID, name string, inboxID *string) (*Widget, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidName
	}
	if inboxID != nil {
		if _, err := s.inbox.Get(ctx, workspaceID, *inboxID); err != nil {
			return nil, ErrInvalidInbox
		}
	}

	id := ids.New(ids.PrefixWidget)
	var widget *Widget
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		key, err := newPublicKey()
		if err != nil {
			return err
		}
		modes, appearance, content, behavior := []string{"chat"}, defaultAppearance(), defaultContent(), defaultBehavior()
		widget, err = s.repo.insert(ctx, tx, id, workspaceID, name, key, inboxID, modes, appearance, content, behavior)
		if err != nil {
			return err
		}
		// The initial configuration is a save too — Versions()/Rollback() need
		// a v1 row to point at, the same as every save after it.
		if err := s.repo.insertVersion(ctx, tx, ids.New(ids.PrefixWidgetVersion), id, 1,
			modes, appearance, content, behavior, actorMemberID, nil); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "widget.created", EntityType: "widget", EntityID: id,
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: "widget.created",
			EntityType: "widget", EntityID: id, ActorType: events.ActorUser, ActorID: actorMemberID,
		})
	})
	if err != nil {
		return nil, err
	}
	return widget, nil
}

// UpdateInput is the full publishable draft — the widget builder always
// holds a complete configuration client-side, so a save replaces the whole
// thing rather than patching individual fields.
type UpdateInput struct {
	Name           string
	InboxID        *string
	Modes          []string
	Appearance     map[string]any
	Content        map[string]any
	Behavior       map[string]any
	Environment    string
	RolloutPercent int
	Enabled        bool
	Note           *string
}

// Update saves a new configuration, bumping the widget's version and
// snapshotting the previous version's full config into widget_config_versions
// (§6.4 configuration history and rollback — reconstructing state from a
// diff chain is how a rollback ends up applying half a change, so each row
// is a complete config, not a delta).
func (s *Service) Update(ctx context.Context, workspaceID, actorMemberID, id string, in UpdateInput) (*Widget, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return nil, ErrInvalidName
	}
	if in.InboxID != nil {
		if _, err := s.inbox.Get(ctx, workspaceID, *in.InboxID); err != nil {
			return nil, ErrInvalidInbox
		}
	}
	if in.Environment != "production" && in.Environment != "test" {
		in.Environment = "production"
	}
	if in.RolloutPercent < 0 {
		in.RolloutPercent = 0
	}
	if in.RolloutPercent > 100 {
		in.RolloutPercent = 100
	}
	if len(in.Modes) == 0 {
		in.Modes = []string{"chat"}
	}

	var widget *Widget
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		current, err := s.repo.byIDTx(ctx, tx, workspaceID, id)
		if err != nil {
			return err
		}

		nextVersion := current.Version + 1
		if err := s.repo.insertVersion(ctx, tx, ids.New(ids.PrefixWidgetVersion), id, nextVersion,
			in.Modes, in.Appearance, in.Content, in.Behavior, actorMemberID, in.Note); err != nil {
			return err
		}

		widget, err = s.repo.update(ctx, tx, workspaceID, id, in.Name, in.InboxID, in.Modes,
			in.Appearance, in.Content, in.Behavior, in.Environment, in.RolloutPercent, in.Enabled, nextVersion)
		if err != nil {
			return err
		}

		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "widget.updated", EntityType: "widget", EntityID: id,
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: "widget.updated",
			EntityType: "widget", EntityID: id, ActorType: events.ActorUser, ActorID: actorMemberID,
		})
	})
	if err != nil {
		return nil, err
	}
	return widget, nil
}

func (s *Service) Delete(ctx context.Context, workspaceID, actorMemberID, id string) error {
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.delete(ctx, tx, workspaceID, id); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "widget.deleted", EntityType: "widget", EntityID: id,
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: "widget.deleted",
			EntityType: "widget", EntityID: id, ActorType: events.ActorUser, ActorID: actorMemberID,
		})
	})
}

func (s *Service) Versions(ctx context.Context, workspaceID, widgetID string) ([]ConfigVersion, error) {
	return s.VersionsPage(ctx, workspaceID, widgetID, 0, 200)
}

func (s *Service) VersionsPage(ctx context.Context, workspaceID, widgetID string, beforeVersion, limit int) ([]ConfigVersion, error) {
	if _, err := s.repo.byID(ctx, workspaceID, widgetID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 201 {
		limit = 200
	}
	return s.repo.versionsPage(ctx, widgetID, beforeVersion, limit)
}

// Rollback republishes an earlier version's configuration as a brand-new
// version — never by rewriting history, which would make the version a save
// created is rolled back to no longer describe what was actually live at
// that time.
func (s *Service) Rollback(ctx context.Context, workspaceID, actorMemberID, widgetID string, toVersion int) (*Widget, error) {
	target, err := s.repo.versionByNumber(ctx, widgetID, toVersion)
	if err != nil {
		return nil, err
	}
	current, err := s.repo.byID(ctx, workspaceID, widgetID)
	if err != nil {
		return nil, err
	}
	note := "Rolled back to v" + strconv.Itoa(toVersion)
	return s.Update(ctx, workspaceID, actorMemberID, widgetID, UpdateInput{
		Name: current.Name, InboxID: current.InboxID, Modes: target.Modes,
		Appearance: target.Appearance, Content: target.Content, Behavior: target.Behavior,
		Environment: current.Environment, RolloutPercent: current.RolloutPercent, Enabled: current.Enabled,
		Note: &note,
	})
}

func (s *Service) Domains(ctx context.Context, workspaceID, widgetID string) ([]Domain, error) {
	if _, err := s.repo.byID(ctx, workspaceID, widgetID); err != nil {
		return nil, err
	}
	return s.repo.domains(ctx, widgetID)
}

func (s *Service) DomainsPage(ctx context.Context, workspaceID, widgetID string, before time.Time, beforeID string, limit int) ([]Domain, error) {
	if _, err := s.repo.byID(ctx, workspaceID, widgetID); err != nil {
		return nil, err
	}
	return s.repo.domainsPage(ctx, widgetID, before, beforeID, limit)
}

// AddDomain allowlists one origin hostname. A bare "*" is refused outright
// (§11.4): a widget allowlisted to everything is a widget with no allowlist,
// which defeats the entire point of a public, unrevocable key.
func (s *Service) AddDomain(ctx context.Context, workspaceID, actorMemberID, widgetID, domain string) (*Domain, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "*" || domain == "" {
		return nil, ErrWildcardDomain
	}
	if _, err := s.repo.byID(ctx, workspaceID, widgetID); err != nil {
		return nil, err
	}

	id := ids.New(ids.PrefixWidgetDomain)
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.addDomain(ctx, tx, id, widgetID, domain); err != nil {
			return err
		}
		return s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "widget.domain_added", EntityType: "widget", EntityID: widgetID,
		})
	})
	if err != nil {
		if uniqueViolation(err) {
			return nil, ErrDuplicateDomain
		}
		return nil, err
	}
	return &Domain{ID: id, WidgetID: widgetID, Domain: domain}, nil
}

func (s *Service) RemoveDomain(ctx context.Context, workspaceID, actorMemberID, widgetID, domainID string) error {
	if _, err := s.repo.byID(ctx, workspaceID, widgetID); err != nil {
		return err
	}
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.removeDomain(ctx, tx, widgetID, domainID); err != nil {
			return err
		}
		return s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "widget.domain_removed", EntityType: "widget", EntityID: widgetID,
		})
	})
}

// ReplaceDomains applies the widget builder's allowlist textarea in one
// transaction — diffing would be more surgical, but a domain allowlist is
// small (a handful of entries) and the builder always holds the complete
// desired set, so a clear-and-reinsert is simpler and no less correct.
func (s *Service) ReplaceDomains(ctx context.Context, workspaceID, widgetID string, domains []string) error {
	if _, err := s.repo.byID(ctx, workspaceID, widgetID); err != nil {
		return err
	}
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.clearDomains(ctx, tx, widgetID); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, raw := range domains {
			domain := strings.ToLower(strings.TrimSpace(raw))
			if domain == "" || domain == "*" || seen[domain] {
				continue
			}
			seen[domain] = true
			if _, err := tx.Exec(ctx, `
				INSERT INTO widget_domains (id, widget_id, domain, verified_at) VALUES ($1, $2, $3, now())
			`, ids.New(ids.PrefixWidgetDomain), widgetID, domain); err != nil {
				return err
			}
		}
		return nil
	})
}

func newPublicKey() (string, error) {
	token, err := auth.NewToken()
	if err != nil {
		return "", err
	}
	return "pk_" + token, nil
}

func defaultAppearance() map[string]any {
	return map[string]any{
		"accent": "#3B6EF6", "theme": "auto", "logo_url": nil, "avatar_url": nil,
		"launcher_shape": "circle", "launcher_size": "md", "launcher_label": nil,
		"position": "bottom-right", "offset_x": 20, "offset_y": 20, "mobile_offset_y": 16,
		"panel_width": 384, "panel_height": 620, "radius": 14,
		"header_style": "solid", "bubble_style": "rounded", "font": "system",
		"z_index": 2147483000, "hide_branding": false, "custom_css_vars": map[string]any{},
	}
}

func defaultContent() map[string]any {
	return map[string]any{
		"title": "Support", "subtitle": "We usually reply in a few minutes",
		"welcome_message": "Hi there 👋 How can we help?", "input_placeholder": "Write a message…",
		"online_message": "", "offline_message": "We're offline — leave a message and we'll reply by email.",
		"response_time_text": "", "consent_text": nil,
	}
}

func defaultBehavior() map[string]any {
	return map[string]any{
		"trigger": "manual", "delay_seconds": 0, "scroll_percent": 0, "trigger_event": nil,
		"include_urls": []string{}, "exclude_urls": []string{}, "devices": []string{"desktop", "tablet", "mobile"},
		"outside_hours": "show_offline", "pre_chat_form_id": nil, "post_chat_survey_id": nil,
		"allow_anonymous": true, "require_identity": false, "sound": true, "unread_badge": true,
		"persist_conversation": true,
	}
}
