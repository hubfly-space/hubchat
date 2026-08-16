package customer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
)

var (
	ErrEventTooLarge  = errors.New("customer: event payload exceeds the configured size limit")
	ErrInvalidSource  = errors.New("customer: not a recognised event source")
	ErrEmptyEventType = errors.New("customer: event type must not be empty")
)

var validEventSources = map[string]bool{
	"widget_init": true, "js_sdk": true, "rest_api": true, "identity_token": true,
	"portal_profile": true, "form": true, "url_params": true, "local_storage": true,
	"cookie": true, "event": true,
}

// ---------------------------------------------------------------- contact sessions

type ContactSession struct {
	ID           string
	WorkspaceID  string
	CustomerID   *string
	VisitorID    *string
	WidgetID     *string
	IPPrefix     *string
	IPCountry    *string
	Browser      *string
	OS           *string
	Device       *string
	Referrer     *string
	LandingURL   *string
	CurrentURL   *string
	CurrentTitle *string
	Language     *string
	Timezone     *string
	Platform     *string
	UserAgent    *string
	Viewport     map[string]any
	PageViews    int
	Consent      map[string]any
	StartedAt    time.Time
	LastSeenAt   time.Time
	EndedAt      *time.Time
}

const contactSessionColumns = `
	id, workspace_id, customer_id, visitor_id, widget_id, ip_prefix, ip_country, browser, os, device,
	referrer, landing_url, current_url, current_title, language, timezone, platform, user_agent,
	viewport, page_views, consent, started_at, last_seen_at, ended_at
`

func scanContactSession(row interface{ Scan(dest ...any) error }) (*ContactSession, error) {
	var s ContactSession
	err := row.Scan(
		&s.ID, &s.WorkspaceID, &s.CustomerID, &s.VisitorID, &s.WidgetID, &s.IPPrefix, &s.IPCountry, &s.Browser, &s.OS, &s.Device,
		&s.Referrer, &s.LandingURL, &s.CurrentURL, &s.CurrentTitle, &s.Language, &s.Timezone, &s.Platform, &s.UserAgent,
		&s.Viewport, &s.PageViews, &s.Consent, &s.StartedAt, &s.LastSeenAt, &s.EndedAt,
	)
	if err != nil {
		return nil, err
	}
	if s.Consent == nil {
		s.Consent = map[string]any{}
	}
	return &s, nil
}

func (r *repository) sessionsByCustomer(ctx context.Context, workspaceID, customerID string, limit int) ([]ContactSession, error) {
	return r.sessionsByCustomerPage(ctx, workspaceID, customerID, time.Time{}, "", limit)
}

func (r *repository) sessionsByCustomerPage(ctx context.Context, workspaceID, customerID string, before time.Time, beforeID string, limit int) ([]ContactSession, error) {
	query := `
		SELECT ` + contactSessionColumns + `
		FROM contact_sessions
		WHERE workspace_id = $1 AND customer_id = $2`
	args := []any{workspaceID, customerID}
	if !before.IsZero() {
		query += ` AND (started_at,id) < ($3,$4)`
		args = append(args, before, beforeID)
	}
	query += fmt.Sprintf(" ORDER BY started_at DESC,id DESC LIMIT $%d", len(args)+1)
	args = append(args, limit)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("customer: sessions by customer: %w", err)
	}
	defer rows.Close()

	out := []ContactSession{}
	for rows.Next() {
		s, err := scanContactSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- customer events

type CustomerEvent struct {
	ID          string
	WorkspaceID string
	CustomerID  *string
	VisitorID   *string
	SessionID   *string
	Type        string
	Source      string
	URL         *string
	RequestID   *string
	Payload     map[string]any
	OccurredAt  time.Time
}

const customerEventColumns = `
	id, workspace_id, customer_id, visitor_id, session_id, type, source, url, request_id, payload, occurred_at
`

func scanCustomerEvent(row interface{ Scan(dest ...any) error }) (*CustomerEvent, error) {
	var e CustomerEvent
	err := row.Scan(
		&e.ID, &e.WorkspaceID, &e.CustomerID, &e.VisitorID, &e.SessionID, &e.Type, &e.Source, &e.URL, &e.RequestID, &e.Payload, &e.OccurredAt,
	)
	if err != nil {
		return nil, err
	}
	if e.Payload == nil {
		e.Payload = map[string]any{}
	}
	return &e, nil
}

func (r *repository) insertCustomerEvent(
	ctx context.Context, id, workspaceID string, customerID, sessionID *string, eventType, source string, url *string, requestID string, payload map[string]any,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO customer_events (id, workspace_id, customer_id, session_id, type, source, url, request_id, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9)
	`, id, workspaceID, customerID, sessionID, eventType, source, url, requestID, payload)
	return err
}

// timelineByCustomer returns a customer's events, most recent first,
// cursor-paginated on (occurred_at, id) per §16.
func (r *repository) timelineByCustomer(ctx context.Context, workspaceID, customerID string, before time.Time, beforeID string, limit int) ([]CustomerEvent, error) {
	if before.IsZero() {
		before = time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+customerEventColumns+`
		FROM customer_events
		WHERE workspace_id = $1 AND customer_id = $2 AND (occurred_at, id) < ($3, $4)
		ORDER BY occurred_at DESC, id DESC
		LIMIT $5
	`, workspaceID, customerID, before, beforeID, limit)
	if err != nil {
		return nil, fmt.Errorf("customer: timeline: %w", err)
	}
	defer rows.Close()

	out := []CustomerEvent{}
	for rows.Next() {
		e, err := scanCustomerEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// listEvents is the developer event stream (§6.10): every event in the
// workspace, optionally narrowed by type, newest first.
func (r *repository) listEvents(ctx context.Context, workspaceID, eventType string, before time.Time, beforeID string, limit int) ([]CustomerEvent, error) {
	if before.IsZero() {
		before = time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+customerEventColumns+`
		FROM customer_events
		WHERE workspace_id = $1 AND ($2 = '' OR type = $2) AND (occurred_at, id) < ($3, $4)
		ORDER BY occurred_at DESC, id DESC
		LIMIT $5
	`, workspaceID, eventType, before, beforeID, limit)
	if err != nil {
		return nil, fmt.Errorf("customer: list events: %w", err)
	}
	defer rows.Close()

	out := []CustomerEvent{}
	for rows.Next() {
		e, err := scanCustomerEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// retentionSweep deletes customer_events and contact_sessions past each
// workspace's own configured retention window (workspace Settings.Privacy.
// RetentionDays["events"|"sessions"], §12) — a single cross-workspace
// statement rather than a per-workspace loop, since the cutoff is data the
// query can read directly off the owning workspaces row.
func (r *repository) retentionSweep(ctx context.Context) (eventsDeleted, sessionsDeleted int64, err error) {
	eventsTag, err := r.pool.Exec(ctx, `
		DELETE FROM customer_events ce
		USING workspaces w
		WHERE ce.workspace_id = w.id
		  AND coalesce((w.settings #>> '{privacy,retention_days,events}')::int, 0) > 0
		  AND NOT EXISTS (
				SELECT 1 FROM workspace_legal_holds lh
				WHERE lh.workspace_id = w.id AND lh.released_at IS NULL
				  AND lh.category IN ('all', 'events')
			)
		  AND ce.occurred_at < now() - make_interval(days => (w.settings #>> '{privacy,retention_days,events}')::int)
	`)
	if err != nil {
		return 0, 0, fmt.Errorf("customer: retention sweep events: %w", err)
	}

	sessionsTag, err := r.pool.Exec(ctx, `
		DELETE FROM contact_sessions cs
		USING workspaces w
		WHERE cs.workspace_id = w.id
		  AND coalesce((w.settings #>> '{privacy,retention_days,sessions}')::int, 0) > 0
		  AND NOT EXISTS (
				SELECT 1 FROM workspace_legal_holds lh
				WHERE lh.workspace_id = w.id AND lh.released_at IS NULL
				  AND lh.category IN ('all', 'sessions')
			)
		  AND cs.started_at < now() - make_interval(days => (w.settings #>> '{privacy,retention_days,sessions}')::int)
	`)
	if err != nil {
		return eventsTag.RowsAffected(), 0, fmt.Errorf("customer: retention sweep sessions: %w", err)
	}

	return eventsTag.RowsAffected(), sessionsTag.RowsAffected(), nil
}

// ---------------------------------------------------------------- service

// Sessions returns a customer's contact sessions, most recent first — the
// customer detail page's "Sessions" tab.
func (s *Service) Sessions(ctx context.Context, workspaceID, customerID string, limit int) ([]ContactSession, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return s.repo.sessionsByCustomer(ctx, workspaceID, customerID, limit)
}

// SessionsPage returns a customer's contact sessions newest first with a
// started_at/id cursor, preserving tenant scoping for every page.
func (s *Service) SessionsPage(ctx context.Context, workspaceID, customerID string, before time.Time, beforeID string, limit int) ([]ContactSession, error) {
	if limit <= 0 || limit > 201 {
		limit = 100
	}
	return s.repo.sessionsByCustomerPage(ctx, workspaceID, customerID, before, beforeID, limit)
}

// IngestEvent validates and records one application event (§6.10, §26.4).
// This is the authenticated ingestion path — a signed request from the
// dashboard's own API or a server-side integration counts as the 'rest_api'
// source; the unauthenticated widget/SDK path arrives in Stage 5 once
// visitor tokens exist, and will call the same service method.
func (s *Service) IngestEvent(
	ctx context.Context, workspaceID, customerID, eventType, source string, url *string, payload map[string]any,
) (*CustomerEvent, error) {
	return s.ingestEvent(ctx, workspaceID, customerID, eventType, source, url, "", payload)
}

// IngestVisitorEvent is the bounded ingestion path used by the public widget.
// It keeps the event and the visitor's rolling contact session in one
// transaction, so the dashboard can reliably show the current page, device,
// and recent journey instead of depending on a best-effort second write.
func (s *Service) IngestVisitorEvent(
	ctx context.Context, workspaceID, visitorID, customerID, eventType, source string, url *string, payload map[string]any,
) (*CustomerEvent, error) {
	return s.ingestVisitorEvent(ctx, workspaceID, visitorID, customerID, eventType, source, url, payload, nil)
}

type NetworkContext struct {
	IPPrefix    string
	CountryCode string
	CountryName string
}

func (s *Service) IngestVisitorEventWithNetwork(
	ctx context.Context, workspaceID, visitorID, customerID, eventType, source string, url *string, payload map[string]any, network *NetworkContext,
) (*CustomerEvent, error) {
	return s.ingestVisitorEvent(ctx, workspaceID, visitorID, customerID, eventType, source, url, payload, network)
}

func (s *Service) ingestVisitorEvent(
	ctx context.Context, workspaceID, visitorID, customerID, eventType, source string, url *string, payload map[string]any, network *NetworkContext,
) (*CustomerEvent, error) {
	if eventType == "" {
		return nil, ErrEmptyEventType
	}
	if !validEventSources[source] {
		return nil, ErrInvalidSource
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if encoded, err := jsonSize(payload); err != nil {
		return nil, err
	} else if s.maxEventBytes > 0 && encoded > s.maxEventBytes {
		return nil, ErrEventTooLarge
	}
	if customerID != "" {
		if _, err := s.repo.byID(ctx, workspaceID, customerID); err != nil {
			return nil, err
		}
	}

	eventID := ids.New(ids.PrefixCustomerEvent)
	sessionID := ids.New(ids.PrefixContactSession)
	var event CustomerEvent
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var existing string
		err := tx.QueryRow(ctx, `
			SELECT id FROM contact_sessions
			WHERE workspace_id=$1 AND visitor_id=$2 AND ended_at IS NULL
			ORDER BY last_seen_at DESC, id DESC LIMIT 1 FOR UPDATE
		`, workspaceID, visitorID).Scan(&existing)
		if errors.Is(err, pgx.ErrNoRows) {
			if _, err = tx.Exec(ctx, `
				INSERT INTO contact_sessions (
					id, workspace_id, customer_id, visitor_id, ip_prefix, ip_country, browser, os, device, referrer,
					landing_url, current_url, current_title, language, timezone, platform, user_agent,
					viewport, page_views
				) VALUES ($1,$2,NULLIF($3,''),$4,NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),NULLIF($10,''),$11,$12,NULLIF($13,''),NULLIF($14,''),NULLIF($15,''),NULLIF($16,''),NULLIF($17,''),$18,$19)
			`, sessionID, workspaceID, customerID, visitorID,
				networkPrefix(network), networkCountry(network), boundedSessionString(payload, "browser"), boundedSessionString(payload, "os"), boundedSessionString(payload, "device"), boundedSessionString(payload, "referrer_origin"),
				url, url, boundedSessionString(payload, "title"), boundedSessionString(payload, "language"), boundedSessionString(payload, "timezone"), boundedSessionString(payload, "platform"), boundedSessionString(payload, "user_agent"),
				viewportJSONValue(payload), pageViewsFor(eventType)); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			sessionID = existing
			if _, err = tx.Exec(ctx, `
				UPDATE contact_sessions SET
					customer_id=COALESCE(NULLIF($3,''),customer_id),
					ip_prefix=COALESCE(NULLIF($4,''),ip_prefix), ip_country=COALESCE(NULLIF($5,''),ip_country),
					browser=COALESCE(NULLIF($6,''),browser), os=COALESCE(NULLIF($7,''),os), device=COALESCE(NULLIF($8,''),device),
					current_url=COALESCE($9,current_url), current_title=COALESCE(NULLIF($10,''),current_title),
					language=COALESCE(NULLIF($11,''),language), timezone=COALESCE(NULLIF($12,''),timezone),
					platform=COALESCE(NULLIF($13,''),platform), user_agent=COALESCE(NULLIF($14,''),user_agent),
					viewport=COALESCE($15,viewport), page_views=page_views+$16, last_seen_at=now()
				WHERE workspace_id=$1 AND id=$2
			`, workspaceID, sessionID, customerID, networkPrefix(network), networkCountry(network), boundedSessionString(payload, "browser"), boundedSessionString(payload, "os"), boundedSessionString(payload, "device"), url,
				boundedSessionString(payload, "title"), boundedSessionString(payload, "language"), boundedSessionString(payload, "timezone"), boundedSessionString(payload, "platform"), boundedSessionString(payload, "user_agent"), viewportJSONValue(payload), pageViewsFor(eventType)); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO customer_events (id, workspace_id, customer_id, visitor_id, session_id, type, source, url, payload)
			VALUES ($1,$2,NULLIF($3,''),$4,$5,$6,$7,$8,$9)
		`, eventID, workspaceID, customerID, visitorID, sessionID, eventType, source, url, payload); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.EventReceived, EntityType: "customer", EntityID: customerID,
			ActorType: events.ActorSystem, Data: map[string]any{"type": eventType, "source": source, "customer_id": customerID, "visitor_id": visitorID},
		})
	})
	if err != nil {
		return nil, err
	}
	event = CustomerEvent{ID: eventID, WorkspaceID: workspaceID, VisitorID: stringPointer(visitorID), SessionID: stringPointer(sessionID), Type: eventType, Source: source, URL: url, CustomerID: emptyStringPointer(customerID), Payload: payload, OccurredAt: time.Now().UTC()}
	return &event, nil
}

// AttachVisitorSessions closes the identity gap when an anonymous visitor
// identifies after browsing. Existing sessions remain in the same workspace
// and are simply attributed to the now-known customer.
func (s *Service) AttachVisitorSessions(ctx context.Context, workspaceID, visitorID, customerID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE contact_sessions SET customer_id=$3
		WHERE workspace_id=$1 AND visitor_id=$2 AND (customer_id IS NULL OR customer_id=$3)
	`, workspaceID, visitorID, customerID)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE customer_events SET customer_id=$3
		WHERE workspace_id=$1 AND visitor_id=$2 AND customer_id IS NULL
	`, workspaceID, visitorID, customerID)
	return err
}

func stringValue(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func boundedSessionString(payload map[string]any, key string) string {
	value := strings.TrimSpace(stringValue(payload, key))
	if len(value) > 512 {
		return value[:512]
	}
	return value
}

func viewportJSONValue(payload map[string]any) []byte {
	viewport := viewportFromPayload(payload)
	if viewport == nil {
		return nil
	}
	encoded, err := json.Marshal(viewport)
	if err != nil {
		return nil
	}
	return encoded
}

func pageViewsFor(eventType string) int {
	if eventType == "page.viewed" {
		return 1
	}
	return 0
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func networkPrefix(network *NetworkContext) string {
	if network == nil {
		return ""
	}
	return network.IPPrefix
}

func networkCountry(network *NetworkContext) string {
	if network == nil {
		return ""
	}
	if network.CountryName != "" {
		return network.CountryName + " (" + network.CountryCode + ")"
	}
	return network.CountryCode
}
func emptyStringPointer(value string) *string { return stringPointer(value) }

// IngestEventWithRequestID is the authenticated ingestion variant. Keeping
// the original method preserves the SDK/widget service boundary, while HTTP
// handlers can attach their request correlation id for the developer event
// explorer and support diagnostics.
func (s *Service) IngestEventWithRequestID(
	ctx context.Context, workspaceID, customerID, eventType, source string, url *string, requestID string, payload map[string]any,
) (*CustomerEvent, error) {
	return s.ingestEvent(ctx, workspaceID, customerID, eventType, source, url, requestID, payload)
}

func (s *Service) ingestEvent(
	ctx context.Context, workspaceID, customerID, eventType, source string, url *string, requestID string, payload map[string]any,
) (*CustomerEvent, error) {
	if eventType == "" {
		return nil, ErrEmptyEventType
	}
	if !validEventSources[source] {
		return nil, ErrInvalidSource
	}
	if customerID != "" {
		if _, err := s.repo.byID(ctx, workspaceID, customerID); err != nil {
			return nil, err
		}
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if encoded, err := jsonSize(payload); err != nil {
		return nil, err
	} else if s.maxEventBytes > 0 && encoded > s.maxEventBytes {
		return nil, ErrEventTooLarge
	}

	id := ids.New(ids.PrefixCustomerEvent)
	var custID *string
	if customerID != "" {
		custID = &customerID
	}

	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.insertCustomerEvent(ctx, id, workspaceID, custID, nil, eventType, source, url, requestID, payload); err != nil {
			return err
		}
		// One fixed log type for every application event, the same way
		// MessageCreated covers both replies and notes — the app-specific
		// name (`page.viewed`, `checkout.started`, ...) lives in Data, which
		// is what automation's event.received trigger and the dev event
		// stream both key off, not workspace_events.type itself.
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.EventReceived,
			EntityType: "customer", EntityID: derefOrEmptyStr(custID),
			ActorType: events.ActorSystem,
			RequestID: requestID,
			Data:      map[string]any{"type": eventType, "source": source, "customer_id": custID},
		})
	})
	if err != nil {
		return nil, err
	}
	return &CustomerEvent{
		ID: id, WorkspaceID: workspaceID, CustomerID: custID, Type: eventType, Source: source,
		URL: url, RequestID: optionalRequestID(requestID), Payload: payload, OccurredAt: time.Now().UTC(),
	}, nil
}

func (s *Service) Timeline(ctx context.Context, workspaceID, customerID string, before time.Time, beforeID string, limit int) ([]CustomerEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.repo.timelineByCustomer(ctx, workspaceID, customerID, before, beforeID, limit)
}

func (s *Service) ListEvents(ctx context.Context, workspaceID, eventType string, before time.Time, beforeID string, limit int) ([]CustomerEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.repo.listEvents(ctx, workspaceID, eventType, before, beforeID, limit)
}

// RunRetentionSweep deletes events and sessions past each workspace's own
// retention window. Called by the recurring job JobRetentionSweep, the same
// self-perpetuating pattern conversation.JobWakeSnoozed established.
func (s *Service) RunRetentionSweep(ctx context.Context) (eventsDeleted, sessionsDeleted int64, err error) {
	return s.repo.retentionSweep(ctx)
}

const JobRetentionSweep = "customer.retention_sweep"

func derefOrEmptyStr(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func optionalRequestID(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func jsonSize(v map[string]any) (int64, error) {
	encoded, err := json.Marshal(v)
	if err != nil {
		return 0, err
	}
	return int64(len(encoded)), nil
}
