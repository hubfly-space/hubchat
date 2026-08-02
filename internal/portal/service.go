package portal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/auth"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/ids"
)

var (
	ErrNotFound         = errors.New("portal: not found")
	ErrNotConfigured    = errors.New("portal: no enabled portal matches this request")
	ErrPortalRequired   = errors.New("portal: portal identifier is required")
	ErrTokenInvalid     = errors.New("portal: invalid or already used link")
	ErrTokenExpired     = errors.New("portal: link expired")
	ErrSessionInvalid   = errors.New("portal: session expired")
	ErrCustomerNotFound = errors.New("portal: customer not found")
	ErrInvalidProfile   = errors.New("portal: invalid profile")
	ErrForbidden        = errors.New("portal: action is not enabled")
)

const magicLinkLifetime = 15 * time.Minute

type Options struct{ SessionLifetime time.Duration }

type Service struct {
	pool            *database.Pool
	sessionLifetime time.Duration
}

type NavigationItem struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Href     string `json:"href"`
	External bool   `json:"external"`
	Position int16  `json:"position"`
}

type Portal struct {
	ID              string
	WorkspaceID     string
	Name            string
	Subdomain       string
	Theme           map[string]any
	Features        map[string]any
	AuthMethods     []string
	Permissions     map[string]any
	DefaultInboxID  *string
	DefaultLanguage string
	Enabled         bool
	Navigation      []NavigationItem
	Domains         []Domain
}

type Domain struct {
	ID                string     `json:"id"`
	PortalID          string     `json:"portal_id"`
	Domain            string     `json:"domain"`
	Status            string     `json:"status"`
	VerificationToken string     `json:"verification_token,omitempty"`
	VerifiedAt        *time.Time `json:"verified_at,omitempty"`
	LastCheckedAt     *time.Time `json:"last_checked_at,omitempty"`
}

type Customer struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Company  string `json:"company,omitempty"`
	Language string `json:"language,omitempty"`
	Timezone string `json:"timezone,omitempty"`
}

// NotificationPreferences are the customer-controlled, non-transactional
// portal notifications. Replies remain mandatory because they are part of a
// support conversation rather than a marketing preference.
type NotificationPreferences struct {
	TicketStatus    bool `json:"ticket_status"`
	FeedbackUpdates bool `json:"feedback_updates"`
	Changelog       bool `json:"changelog"`
	Surveys         bool `json:"surveys"`
}

type NotificationPreferencesInput struct {
	TicketStatus    *bool `json:"ticket_status"`
	FeedbackUpdates *bool `json:"feedback_updates"`
	Changelog       *bool `json:"changelog"`
	Surveys         *bool `json:"surveys"`
}

type ProfileInput struct {
	Name        *string                       `json:"name"`
	Language    *string                       `json:"language"`
	Timezone    *string                       `json:"timezone"`
	Preferences *NotificationPreferencesInput `json:"preferences"`
}

type Session struct {
	Token       string
	PortalID    string
	WorkspaceID string
	CustomerID  string
	AuthMethod  string
	ExpiresAt   time.Time
	Portal      *Portal
	Customer    Customer
}

type MagicLink struct {
	Token     string
	ExpiresAt time.Time
	Customer  Customer
}

type Ticket struct {
	ID             string    `json:"id"`
	Number         int       `json:"number"`
	Prefix         string    `json:"prefix"`
	Title          string    `json:"title"`
	Description    string    `json:"description"`
	Status         string    `json:"status"`
	Priority       string    `json:"priority"`
	ConversationID *string   `json:"conversation_id"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type TicketFilter struct {
	Before   time.Time
	BeforeID string
	Limit    int
}

type CreateRequest struct {
	Name           string
	Subdomain      string
	DefaultInboxID *string
}

type UpdateRequest struct {
	Name            *string
	Subdomain       *string
	Theme           map[string]any
	Features        map[string]any
	AuthMethods     []string
	Permissions     map[string]any
	Navigation      *[]NavigationItem
	DefaultInboxID  *string
	DefaultLanguage *string
	Enabled         *bool
}

func New(pool *database.Pool, opts Options) *Service {
	if opts.SessionLifetime <= 0 {
		opts.SessionLifetime = 30 * 24 * time.Hour
	}
	return &Service{pool: pool, sessionLifetime: opts.SessionLifetime}
}

func (s *Service) List(ctx context.Context, workspaceID string) ([]Portal, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, workspace_id, name, subdomain::text, theme, features,
		       auth_methods, permissions, default_inbox_id, default_language, enabled
		FROM portals WHERE workspace_id = $1 ORDER BY created_at ASC
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("portal: list: %w", err)
	}
	defer rows.Close()
	var out []Portal
	for rows.Next() {
		p, err := scanPortal(rows)
		if err != nil {
			return nil, err
		}
		if err := s.loadNavigation(ctx, p); err != nil {
			return nil, err
		}
		if err := s.loadDomains(ctx, p); err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// ListPage bounds the portal administration directory. Name and id are used
// as the cursor because the Portal wire model intentionally does not expose
// the internal creation timestamp.
func (s *Service) ListPage(ctx context.Context, workspaceID, beforeName, beforeID string, limit int) ([]Portal, error) {
	if limit <= 0 || limit > 201 {
		limit = 101
	}
	where := "workspace_id = $1"
	args := []any{workspaceID}
	if beforeName != "" {
		where += " AND (name,id) > ($2,$3)"
		args = append(args, beforeName, beforeID)
	}
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, `
		SELECT id, workspace_id, name, subdomain::text, theme, features,
		       auth_methods, permissions, default_inbox_id, default_language, enabled
		FROM portals WHERE `+where+` ORDER BY name ASC, id ASC LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("portal: list page: %w", err)
	}
	defer rows.Close()
	out := []Portal{}
	for rows.Next() {
		p, err := scanPortal(rows)
		if err != nil {
			return nil, err
		}
		if err := s.loadNavigation(ctx, p); err != nil {
			return nil, err
		}
		if err := s.loadDomains(ctx, p); err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Service) Get(ctx context.Context, workspaceID, id string) (*Portal, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, workspace_id, name, subdomain::text, theme, features,
		       auth_methods, permissions, default_inbox_id, default_language, enabled
		FROM portals WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id)
	p, err := scanPortal(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("portal: get: %w", err)
	}
	if err := s.loadNavigation(ctx, p); err != nil {
		return nil, err
	}
	if err := s.loadDomains(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) Create(ctx context.Context, workspaceID string, req CreateRequest) (*Portal, error) {
	name := strings.TrimSpace(req.Name)
	subdomain := strings.Trim(strings.ToLower(strings.TrimSpace(req.Subdomain)), ".")
	if name == "" || subdomain == "" {
		return nil, errors.New("portal: name and subdomain are required")
	}
	if req.DefaultInboxID != nil {
		var ok bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM inboxes WHERE id = $1 AND workspace_id = $2)`, *req.DefaultInboxID, workspaceID).Scan(&ok); err != nil {
			return nil, err
		}
		if !ok {
			return nil, errors.New("portal: default inbox is not in this workspace")
		}
	}
	id := ids.New(ids.PrefixPortal)
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO portals (id, workspace_id, name, subdomain, theme, features, auth_methods, permissions, default_inbox_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`, id, workspaceID, name, subdomain,
			jsonBytes(map[string]any{"accent": "#3B6EF6"}),
			jsonBytes(map[string]any{"tickets": true, "knowledge_base": true, "feedback": true, "forms": true, "changelog": true}),
			[]string{"magic_link"}, jsonBytes(map[string]any{"view_company_tickets": false}), req.DefaultInboxID); err != nil {
			return fmt.Errorf("portal: create: %w", err)
		}
		for position, item := range []struct{ label, href string }{{"Guides", "/kb"}, {"Requests", "/tickets"}, {"Forms", "/forms"}, {"Roadmap", "/feedback"}, {"Changelog", "/changelog"}} {
			if _, err := tx.Exec(ctx, `
			INSERT INTO portal_navigation_items (id, portal_id, label, href, position)
			VALUES ($1, $2, $3, $4, $5)
		`, ids.New(ids.PrefixPortalNavItem), id, item.label, item.href, position); err != nil {
				return fmt.Errorf("portal: create navigation: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, workspaceID, id)
}

func (s *Service) Update(ctx context.Context, workspaceID, id string, req UpdateRequest) (*Portal, error) {
	if req.Subdomain != nil {
		value := strings.Trim(strings.ToLower(strings.TrimSpace(*req.Subdomain)), ".")
		req.Subdomain = &value
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("portal: update begin: %w", err)
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE portals SET
			name = COALESCE($3, name), subdomain = COALESCE($4, subdomain),
			theme = COALESCE($5, theme), features = COALESCE($6, features),
			auth_methods = COALESCE($7, auth_methods), permissions = COALESCE($8, permissions),
			default_inbox_id = COALESCE($9, default_inbox_id),
			default_language = COALESCE($10, default_language), enabled = COALESCE($11, enabled),
			updated_at = now()
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id, req.Name, req.Subdomain, optionalJSON(req.Theme), optionalJSON(req.Features),
		nilIfEmpty(req.AuthMethods), optionalJSON(req.Permissions), req.DefaultInboxID, req.DefaultLanguage, req.Enabled)
	if err != nil {
		return nil, fmt.Errorf("portal: update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	if req.Navigation != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM portal_navigation_items WHERE portal_id = $1`, id); err != nil {
			return nil, fmt.Errorf("portal: replace navigation: %w", err)
		}
		for position, item := range *req.Navigation {
			label := strings.TrimSpace(item.Label)
			href := strings.TrimSpace(item.Href)
			if label == "" || href == "" {
				return nil, errors.New("portal: navigation label and target are required")
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO portal_navigation_items (id, portal_id, label, href, external, position)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, ids.New(ids.PrefixPortalNavItem), id, label, href, item.External, position); err != nil {
				return nil, fmt.Errorf("portal: insert navigation: %w", err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("portal: update commit: %w", err)
	}
	return s.Get(ctx, workspaceID, id)
}

func jsonBytes(value map[string]any) []byte {
	data, _ := json.Marshal(value)
	return data
}

func optionalJSON(value map[string]any) []byte {
	if value == nil {
		return nil
	}
	return jsonBytes(value)
}

func nilIfEmpty(value []string) []string {
	if len(value) == 0 {
		return nil
	}
	return value
}

// Resolve selects by opaque id or public subdomain. An empty identifier is
// accepted only for a deployment with exactly one enabled portal.
func (s *Service) Resolve(ctx context.Context, identifier string) (*Portal, error) {
	identifier = strings.TrimSpace(strings.ToLower(identifier))
	if identifier == "" {
		return s.resolveDefault(ctx)
	}
	row := s.pool.QueryRow(ctx, `
		SELECT id, workspace_id, name, subdomain::text, theme, features,
		       auth_methods, permissions, default_inbox_id, default_language, enabled
		FROM portals
		WHERE enabled = true AND (
			id = $1 OR subdomain::text = $1 OR EXISTS (
				SELECT 1 FROM portal_domains d
				WHERE d.portal_id = portals.id AND d.domain::text = $1 AND d.status = 'verified'
			)
		)
	`, identifier)
	portal, err := scanPortal(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("portal: resolve: %w", err)
	}
	if err := s.loadNavigation(ctx, portal); err != nil {
		return nil, err
	}
	if err := s.loadDomains(ctx, portal); err != nil {
		return nil, err
	}
	return portal, nil
}

func (s *Service) resolveDefault(ctx context.Context) (*Portal, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, workspace_id, name, subdomain::text, theme, features,
		       auth_methods, permissions, default_inbox_id, default_language, enabled
		FROM portals WHERE enabled = true ORDER BY created_at ASC LIMIT 2
	`)
	if err != nil {
		return nil, fmt.Errorf("portal: resolve default: %w", err)
	}
	defer rows.Close()
	var portals []*Portal
	for rows.Next() {
		portal, err := scanPortal(rows)
		if err != nil {
			return nil, err
		}
		portals = append(portals, portal)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(portals) == 0 {
		return nil, ErrNotConfigured
	}
	if len(portals) > 1 {
		return nil, ErrPortalRequired
	}
	if err := s.loadNavigation(ctx, portals[0]); err != nil {
		return nil, err
	}
	if err := s.loadDomains(ctx, portals[0]); err != nil {
		return nil, err
	}
	return portals[0], nil
}

func (s *Service) loadDomains(ctx context.Context, p *Portal) error {
	rows, err := s.pool.Query(ctx, `
		SELECT id, portal_id, domain::text, status, verification_token, verified_at, last_checked_at
		FROM portal_domains WHERE portal_id = $1 ORDER BY created_at ASC
	`, p.ID)
	if err != nil {
		return fmt.Errorf("portal: load domains: %w", err)
	}
	defer rows.Close()
	p.Domains = make([]Domain, 0)
	for rows.Next() {
		var item Domain
		if err := rows.Scan(&item.ID, &item.PortalID, &item.Domain, &item.Status, &item.VerificationToken, &item.VerifiedAt, &item.LastCheckedAt); err != nil {
			return err
		}
		p.Domains = append(p.Domains, item)
	}
	return rows.Err()
}

func normalizeDomain(value string) (string, error) {
	domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
	if domain == "" || len(domain) > 253 || strings.Contains(domain, "/") || strings.Contains(domain, ":") || net.ParseIP(domain) != nil {
		return "", errors.New("portal: enter a valid hostname")
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return "", errors.New("portal: custom domain must include a registrable suffix")
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", errors.New("portal: enter a valid hostname")
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return "", errors.New("portal: enter a valid hostname")
			}
		}
	}
	return domain, nil
}

func (s *Service) ListDomains(ctx context.Context, workspaceID, portalID string) ([]Domain, error) {
	p, err := s.Get(ctx, workspaceID, portalID)
	if err != nil {
		return nil, err
	}
	return p.Domains, nil
}

// ListDomainsPage bounds the portal domain administration directory. Domains
// are normalized before insertion, so domain and id provide a stable,
// workspace-scoped cursor without exposing the internal creation timestamp.
func (s *Service) ListDomainsPage(ctx context.Context, workspaceID, portalID, beforeDomain, beforeID string, limit int) ([]Domain, error) {
	if limit <= 0 || limit > 201 {
		limit = 101
	}
	where := "p.workspace_id = $1 AND d.portal_id = $2"
	args := []any{workspaceID, portalID}
	if beforeDomain != "" {
		where += " AND (d.domain::text,d.id) > ($3,$4)"
		args = append(args, beforeDomain, beforeID)
	}
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, `
		SELECT d.id, d.portal_id, d.domain::text, d.status, d.verification_token, d.verified_at, d.last_checked_at
		FROM portal_domains d JOIN portals p ON p.id = d.portal_id
		WHERE `+where+` ORDER BY d.domain ASC, d.id ASC LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("portal: list domain page: %w", err)
	}
	defer rows.Close()
	out := []Domain{}
	for rows.Next() {
		var item Domain
		if err := rows.Scan(&item.ID, &item.PortalID, &item.Domain, &item.Status, &item.VerificationToken, &item.VerifiedAt, &item.LastCheckedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) AddDomain(ctx context.Context, workspaceID, portalID, value string) (*Domain, error) {
	domain, err := normalizeDomain(value)
	if err != nil {
		return nil, err
	}
	if _, err := s.Get(ctx, workspaceID, portalID); err != nil {
		return nil, err
	}
	token, err := auth.NewToken()
	if err != nil {
		return nil, err
	}
	var item Domain
	err = s.pool.QueryRow(ctx, `
		INSERT INTO portal_domains (id, portal_id, domain, verification_token)
		VALUES ($1, $2, $3, $4)
		RETURNING id, portal_id, domain::text, status, verification_token, verified_at, last_checked_at
	`, ids.New(ids.PrefixPortalDomain), portalID, domain, token).Scan(&item.ID, &item.PortalID, &item.Domain, &item.Status, &item.VerificationToken, &item.VerifiedAt, &item.LastCheckedAt)
	if err != nil {
		return nil, fmt.Errorf("portal: add domain: %w", err)
	}
	return &item, nil
}

// VerifyDomain checks the TXT record at _hubchat-verification.<domain>.
// Failed checks are persisted for operator visibility and can be retried after
// DNS propagation without creating another domain row.
func (s *Service) VerifyDomain(ctx context.Context, workspaceID, portalID, domainID string) (*Domain, error) {
	var item Domain
	err := s.pool.QueryRow(ctx, `
		SELECT d.id, d.portal_id, d.domain::text, d.status, d.verification_token, d.verified_at, d.last_checked_at
		FROM portal_domains d JOIN portals p ON p.id = d.portal_id
		WHERE p.workspace_id = $1 AND d.portal_id = $2 AND d.id = $3
	`, workspaceID, portalID, domainID).Scan(&item.ID, &item.PortalID, &item.Domain, &item.Status, &item.VerificationToken, &item.VerifiedAt, &item.LastCheckedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	records, lookupErr := net.LookupTXT("_hubchat-verification." + item.Domain)
	verified := false
	for _, record := range records {
		if strings.TrimSpace(record) == item.VerificationToken {
			verified = true
			break
		}
	}
	if lookupErr != nil {
		item.Status = "failed"
	} else if verified {
		item.Status = "verified"
	} else {
		item.Status = "failed"
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE portal_domains SET status=$4, verified_at=CASE WHEN $4='verified' THEN now() ELSE verified_at END, last_checked_at=now()
		WHERE id=$1 AND portal_id=$2 AND EXISTS (SELECT 1 FROM portals WHERE id=$2 AND workspace_id=$3)
	`, item.ID, portalID, workspaceID, item.Status); err != nil {
		return nil, err
	}
	return s.domain(ctx, workspaceID, portalID, domainID)
}

func (s *Service) domain(ctx context.Context, workspaceID, portalID, domainID string) (*Domain, error) {
	var item Domain
	err := s.pool.QueryRow(ctx, `
		SELECT d.id, d.portal_id, d.domain::text, d.status, d.verification_token, d.verified_at, d.last_checked_at
		FROM portal_domains d JOIN portals p ON p.id=d.portal_id
		WHERE p.workspace_id=$1 AND d.portal_id=$2 AND d.id=$3
	`, workspaceID, portalID, domainID).Scan(&item.ID, &item.PortalID, &item.Domain, &item.Status, &item.VerificationToken, &item.VerifiedAt, &item.LastCheckedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Service) DeleteDomain(ctx context.Context, workspaceID, portalID, domainID string) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM portal_domains d USING portals p
		WHERE d.id=$1 AND d.portal_id=$2 AND p.id=d.portal_id AND p.workspace_id=$3
	`, domainID, portalID, workspaceID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanPortal(row interface{ Scan(...any) error }) (*Portal, error) {
	var p Portal
	var theme, features, permissions []byte
	err := row.Scan(&p.ID, &p.WorkspaceID, &p.Name, &p.Subdomain, &theme, &features,
		&p.AuthMethods, &permissions, &p.DefaultInboxID, &p.DefaultLanguage, &p.Enabled)
	if err != nil {
		return nil, err
	}
	p.Theme = decodeObject(theme)
	p.Features = decodeObject(features)
	p.Permissions = decodeObject(permissions)
	if p.AuthMethods == nil {
		p.AuthMethods = []string{}
	}
	p.Navigation = []NavigationItem{}
	return &p, nil
}

func decodeObject(value []byte) map[string]any {
	var out map[string]any
	if len(value) > 0 && json.Unmarshal(value, &out) == nil && out != nil {
		return out
	}
	return map[string]any{}
}

func (s *Service) loadNavigation(ctx context.Context, portal *Portal) error {
	rows, err := s.pool.Query(ctx, `
		SELECT id, label, href, external, position
		FROM portal_navigation_items WHERE portal_id = $1
		ORDER BY position ASC, id ASC
	`, portal.ID)
	if err != nil {
		return fmt.Errorf("portal: navigation: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item NavigationItem
		if err := rows.Scan(&item.ID, &item.Label, &item.Href, &item.External, &item.Position); err != nil {
			return err
		}
		portal.Navigation = append(portal.Navigation, item)
	}
	return rows.Err()
}

func (s *Service) IssueMagicLink(ctx context.Context, portalID, email string) (*MagicLink, error) {
	portal, err := s.Resolve(ctx, portalID)
	if err != nil {
		return nil, err
	}
	if !contains(portal.AuthMethods, "magic_link") {
		return nil, ErrForbidden
	}
	customer, err := s.customerByEmail(ctx, portal.WorkspaceID, email)
	if err != nil {
		return nil, err
	}
	token, err := auth.NewToken()
	if err != nil {
		return nil, err
	}
	expiresAt := time.Now().Add(magicLinkLifetime)
	_, err = s.pool.Exec(ctx, `
		INSERT INTO portal_access_tokens
			(id, workspace_id, portal_id, customer_id, token_hash, purpose, expires_at)
		VALUES ($1, $2, $3, $4, $5, 'magic_link', $6)
	`, ids.New(ids.PrefixPortalToken), portal.WorkspaceID, portal.ID, customer.ID,
		auth.HashToken(token), expiresAt)
	if err != nil {
		return nil, fmt.Errorf("portal: issue magic link: %w", err)
	}
	return &MagicLink{Token: token, ExpiresAt: expiresAt, Customer: *customer}, nil
}

func (s *Service) RedeemMagicLink(ctx context.Context, token, userAgent, ip string) (*Session, error) {
	var portalID, workspaceID, customerID string
	var expiresAt time.Time
	err := s.pool.QueryRow(ctx, `
		UPDATE portal_access_tokens SET used_at = now()
		WHERE token_hash = $1 AND purpose = 'magic_link' AND used_at IS NULL
		RETURNING portal_id, workspace_id, customer_id, expires_at
	`, auth.HashToken(token)).Scan(&portalID, &workspaceID, &customerID, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTokenInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("portal: redeem magic link: %w", err)
	}
	if time.Now().After(expiresAt) {
		return nil, ErrTokenExpired
	}
	raw, err := auth.NewToken()
	if err != nil {
		return nil, err
	}
	sessionExpires := time.Now().Add(s.sessionLifetime)
	_, err = s.pool.Exec(ctx, `
		INSERT INTO portal_sessions
			(id, workspace_id, portal_id, customer_id, token_hash, auth_method,
			 user_agent, ip, expires_at)
		VALUES ($1, $2, $3, $4, $5, 'magic_link', $6, NULLIF($7, '')::inet, $8)
	`, ids.New(ids.PrefixPortalSession), workspaceID, portalID, customerID,
		auth.HashToken(raw), userAgent, ip, sessionExpires)
	if err != nil {
		return nil, fmt.Errorf("portal: create session: %w", err)
	}
	return s.session(ctx, raw, portalID)
}

func (s *Service) session(ctx context.Context, raw, portalID string) (*Session, error) {
	var session Session
	var portal Portal
	var theme, features, permissions []byte
	var customer Customer
	err := s.pool.QueryRow(ctx, `
		SELECT s.portal_id, s.workspace_id, s.customer_id, s.auth_method, s.expires_at,
		       p.id, p.name, p.subdomain::text, p.theme, p.features, p.auth_methods,
		       p.permissions, p.default_inbox_id, p.default_language, p.enabled,
		       c.id, coalesce(c.name, ''), coalesce(c.email::text, ''),
		       coalesce(c.language, ''), coalesce(c.timezone, '')
		FROM portal_sessions s
		JOIN portals p ON p.id = s.portal_id AND p.enabled = true
		JOIN customers c ON c.id = s.customer_id AND c.workspace_id = s.workspace_id
		WHERE s.token_hash = $1 AND ($2 = '' OR s.portal_id = $2)
		  AND s.revoked_at IS NULL AND s.expires_at > now()
	`, auth.HashToken(raw), portalID).Scan(
		&session.PortalID, &session.WorkspaceID, &session.CustomerID, &session.AuthMethod, &session.ExpiresAt,
		&portal.ID, &portal.Name, &portal.Subdomain, &theme, &features, &portal.AuthMethods,
		&permissions, &portal.DefaultInboxID, &portal.DefaultLanguage, &portal.Enabled,
		&customer.ID, &customer.Name, &customer.Email, &customer.Language, &customer.Timezone,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSessionInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("portal: resolve session: %w", err)
	}
	portal.WorkspaceID = session.WorkspaceID
	portal.Theme = decodeObject(theme)
	portal.Features = decodeObject(features)
	portal.Permissions = decodeObject(permissions)
	portal.Navigation = []NavigationItem{}
	if err := s.loadNavigation(ctx, &portal); err != nil {
		return nil, err
	}
	session.Token, session.Portal, session.Customer = raw, &portal, customer
	_, _ = s.pool.Exec(ctx, `UPDATE portal_sessions SET last_seen_at = now() WHERE token_hash = $1`, auth.HashToken(raw))
	return &session, nil
}

func (s *Service) Session(ctx context.Context, raw, portalID string) (*Session, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, ErrSessionInvalid
	}
	return s.session(ctx, raw, portalID)
}

func (s *Service) Logout(ctx context.Context, raw string) error {
	_, err := s.pool.Exec(ctx, `UPDATE portal_sessions SET revoked_at = now() WHERE token_hash = $1`, auth.HashToken(raw))
	return err
}

// RevokeCustomerSessions invalidates every portal session for a customer.
// Account deletion calls this before anonymisation so another browser or
// device cannot continue using the now-anonymised customer record.
func (s *Service) RevokeCustomerSessions(ctx context.Context, workspaceID, customerID string) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM portal_sessions
		WHERE workspace_id=$1 AND customer_id=$2
	`, workspaceID, customerID)
	if err != nil {
		return fmt.Errorf("portal: revoke customer sessions: %w", err)
	}
	return nil
}

// Preferences returns defaults even when a customer has never changed a
// setting. The left join keeps onboarding friction at zero while making every
// read workspace- and customer-scoped.
func (s *Service) Preferences(ctx context.Context, workspaceID, customerID string) (*NotificationPreferences, error) {
	var result NotificationPreferences
	err := s.pool.QueryRow(ctx, `
		SELECT coalesce(p.ticket_status,true), coalesce(p.feedback_updates,true),
		       coalesce(p.changelog,false), coalesce(p.surveys,true)
		FROM customers c
		LEFT JOIN customer_notification_preferences p
		  ON p.customer_id=c.id AND p.workspace_id=c.workspace_id
		WHERE c.workspace_id=$1 AND c.id=$2
	`, workspaceID, customerID).Scan(&result.TicketStatus, &result.FeedbackUpdates, &result.Changelog, &result.Surveys)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCustomerNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("portal: load preferences: %w", err)
	}
	return &result, nil
}

// UpdateProfile persists the customer-editable profile fields and any
// supplied preference fields atomically. Email is intentionally excluded: it
// is the verified sign-in identity and requires a separate re-verification
// flow before it may change.
func (s *Service) UpdateProfile(ctx context.Context, session *Session, input ProfileInput) (*Customer, error) {
	if session == nil || session.Portal == nil {
		return nil, ErrSessionInvalid
	}
	name, language, timezone := input.Name, input.Language, input.Timezone
	if name != nil {
		value := strings.TrimSpace(*name)
		if value == "" || len(value) > 200 {
			return nil, ErrInvalidProfile
		}
		name = &value
	}
	if language != nil {
		value := strings.TrimSpace(*language)
		if len(value) > 32 {
			return nil, ErrInvalidProfile
		}
		language = &value
	}
	if timezone != nil {
		value := strings.TrimSpace(*timezone)
		if len(value) > 64 {
			return nil, ErrInvalidProfile
		}
		timezone = &value
	}
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE customers SET
				name=CASE WHEN $3 THEN $4 ELSE name END,
				language=CASE WHEN $5 THEN NULLIF($6,'') ELSE language END,
				timezone=CASE WHEN $7 THEN NULLIF($8,'') ELSE timezone END
			WHERE workspace_id=$1 AND id=$2
		`, session.WorkspaceID, session.CustomerID, name != nil, valueOrEmpty(name), language != nil, valueOrEmpty(language), timezone != nil, valueOrEmpty(timezone)); err != nil {
			return fmt.Errorf("portal: update profile: %w", err)
		}
		if input.Preferences == nil {
			return nil
		}
		preferences := input.Preferences
		_, err := tx.Exec(ctx, `
			INSERT INTO customer_notification_preferences
				(customer_id,workspace_id,ticket_status,feedback_updates,changelog,surveys)
			VALUES ($1,$2,coalesce($3::boolean,true),coalesce($4::boolean,true),coalesce($5::boolean,false),coalesce($6::boolean,true))
			ON CONFLICT (customer_id) DO UPDATE SET
				workspace_id=EXCLUDED.workspace_id,
				ticket_status=CASE WHEN $3::boolean IS NULL THEN customer_notification_preferences.ticket_status ELSE EXCLUDED.ticket_status END,
				feedback_updates=CASE WHEN $4::boolean IS NULL THEN customer_notification_preferences.feedback_updates ELSE EXCLUDED.feedback_updates END,
				changelog=CASE WHEN $5::boolean IS NULL THEN customer_notification_preferences.changelog ELSE EXCLUDED.changelog END,
				surveys=CASE WHEN $6::boolean IS NULL THEN customer_notification_preferences.surveys ELSE EXCLUDED.surveys END,
				updated_at=now()
		`, session.CustomerID, session.WorkspaceID, preferences.TicketStatus, preferences.FeedbackUpdates, preferences.Changelog, preferences.Surveys)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.customerByID(ctx, session.WorkspaceID, session.CustomerID)
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (s *Service) customerByID(ctx context.Context, workspaceID, customerID string) (*Customer, error) {
	var customer Customer
	err := s.pool.QueryRow(ctx, `
		SELECT c.id, coalesce(c.name,''), coalesce(c.email::text,''),
		       coalesce(c.language,''), coalesce(c.timezone,''), coalesce(co.name,'')
		FROM customers c
		LEFT JOIN company_customers cc ON cc.customer_id=c.id
		LEFT JOIN companies co ON co.id=cc.company_id AND co.workspace_id=c.workspace_id
		WHERE c.workspace_id=$1 AND c.id=$2
		ORDER BY co.name NULLS LAST LIMIT 1
	`, workspaceID, customerID).Scan(&customer.ID, &customer.Name, &customer.Email, &customer.Language, &customer.Timezone, &customer.Company)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCustomerNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("portal: load customer: %w", err)
	}
	return &customer, nil
}

func (s *Service) customerByEmail(ctx context.Context, workspaceID, email string) (*Customer, error) {
	var customer Customer
	err := s.pool.QueryRow(ctx, `
		SELECT c.id, coalesce(c.name, ''), coalesce(c.email::text, ''),
		       coalesce(c.language, ''), coalesce(c.timezone, ''), coalesce(co.name, '')
		FROM customers c
		LEFT JOIN company_customers cc ON cc.customer_id = c.id
		LEFT JOIN companies co ON co.id = cc.company_id AND co.workspace_id = c.workspace_id
		WHERE c.workspace_id = $1 AND lower(c.email::text) = lower($2)
		ORDER BY c.verification = 'verified' DESC, c.created_at ASC LIMIT 1
	`, workspaceID, strings.TrimSpace(email)).Scan(
		&customer.ID, &customer.Name, &customer.Email, &customer.Language, &customer.Timezone, &customer.Company)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCustomerNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("portal: find customer: %w", err)
	}
	return &customer, nil
}

func (s *Service) Tickets(ctx context.Context, session *Session, filter TicketFilter) ([]Ticket, error) {
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 25
	}
	if filter.Before.IsZero() {
		filter.Before = time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
	}
	companyWide := permission(session.Portal, "view_company_tickets")
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.number, t.prefix, t.title, t.description, t.status, t.priority,
		       t.conversation_id, t.created_at, t.updated_at
		FROM tickets t
		WHERE t.workspace_id = $1
		  AND (t.customer_id = $2 OR ($3 AND EXISTS (
			SELECT 1 FROM company_customers mine
			JOIN company_customers other ON other.company_id = mine.company_id
			WHERE mine.customer_id = $2 AND other.customer_id = t.customer_id
		  )))
		  AND (t.updated_at, t.id) < ($4, $5)
		ORDER BY t.updated_at DESC, t.id DESC LIMIT $6
	`, session.WorkspaceID, session.CustomerID, companyWide, filter.Before, filter.BeforeID, filter.Limit)
	if err != nil {
		return nil, fmt.Errorf("portal: list tickets: %w", err)
	}
	defer rows.Close()
	var out []Ticket
	for rows.Next() {
		var ticket Ticket
		if err := rows.Scan(&ticket.ID, &ticket.Number, &ticket.Prefix, &ticket.Title, &ticket.Description,
			&ticket.Status, &ticket.Priority, &ticket.ConversationID, &ticket.CreatedAt, &ticket.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, ticket)
	}
	return out, rows.Err()
}

func (s *Service) CanAccessTicket(ctx context.Context, session *Session, ticketID string) (bool, error) {
	companyWide := permission(session.Portal, "view_company_tickets")
	var allowed bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM tickets t WHERE t.id = $1 AND t.workspace_id = $2 AND (
			t.customer_id = $3 OR ($4 AND EXISTS (
				SELECT 1 FROM company_customers mine
				JOIN company_customers other ON other.company_id = mine.company_id
				WHERE mine.customer_id = $3 AND other.customer_id = t.customer_id
			))
		))
	`, ticketID, session.WorkspaceID, session.CustomerID, companyWide).Scan(&allowed)
	return allowed, err
}

func permission(portal *Portal, key string) bool {
	if portal == nil {
		return false
	}
	allowed, _ := portal.Permissions[key].(bool)
	return allowed
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
