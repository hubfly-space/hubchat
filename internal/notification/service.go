package notification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	filemodule "github.com/hubchat/hubchat/internal/file"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/jobs"
	"github.com/jackc/pgx/v5"
)

var ErrNotFound = errors.New("notification: not found")
var ErrInvalidPreference = errors.New("notification: invalid preference")

type Service struct {
	pool      *database.Pool
	jobs      *jobs.Client
	files     *filemodule.Service
	surveys   SurveyDispatcher
	publicURL *url.URL
	seenMu    sync.Mutex
	seen      map[string]int64
}

// SurveyDispatcher is deliberately a small boundary so notification delivery
// can react to ticket lifecycle events without owning survey persistence.
type SurveyDispatcher interface {
	NotifyTicketResolution(context.Context, string, string, string, string) error
}

type Notification struct {
	ID            string     `json:"id"`
	WorkspaceID   string     `json:"workspace_id"`
	MemberID      string     `json:"member_id"`
	Type          string     `json:"type"`
	Title         string     `json:"title"`
	Body          string     `json:"body"`
	EntityType    *string    `json:"entity_type"`
	EntityID      *string    `json:"entity_id"`
	URL           *string    `json:"url"`
	SourceEventID *string    `json:"-"`
	ReadAt        *time.Time `json:"read_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

type Preference struct {
	Type    string `json:"type"`
	InApp   bool   `json:"in_app"`
	Email   bool   `json:"email"`
	Browser bool   `json:"browser"`
	Sound   bool   `json:"sound"`
}

type PreferenceInput struct {
	Type    string `json:"type"`
	InApp   bool   `json:"in_app"`
	Email   bool   `json:"email"`
	Browser bool   `json:"browser"`
	Sound   bool   `json:"sound"`
}

var preferenceTypes = map[string]bool{
	"assignment": true, "mention": true, "reply": true, "sla_warning": true,
	"sla_breach": true, "team_unassigned": true, "feedback": true,
}

type ListFilter struct {
	Before   time.Time
	BeforeID string
	Limit    int
	Unread   bool
}

func New(pool *database.Pool, queue ...*jobs.Client) *Service {
	var jobClient *jobs.Client
	if len(queue) > 0 {
		jobClient = queue[0]
	}
	return &Service{pool: pool, jobs: jobClient, seen: make(map[string]int64)}
}

func (s *Service) SetSurveyDispatcher(dispatcher SurveyDispatcher) {
	s.surveys = dispatcher
}

func (s *Service) SetPublicURL(publicURL *url.URL) {
	s.publicURL = publicURL
}

// SetFileService lets customer-facing reply delivery include files attached
// to the message. It is optional for installations without storage.
func (s *Service) SetFileService(files *filemodule.Service) {
	s.files = files
}

func (s *Service) Preferences(ctx context.Context, workspaceID, memberID string) ([]Preference, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT type,in_app,email,browser,sound
		FROM notification_preferences
		WHERE workspace_id=$1 AND member_id=$2
		ORDER BY type
	`, workspaceID, memberID)
	if err != nil {
		return nil, fmt.Errorf("notification: list preferences: %w", err)
	}
	defer rows.Close()
	result := make([]Preference, 0)
	for rows.Next() {
		var item Preference
		if err := rows.Scan(&item.Type, &item.InApp, &item.Email, &item.Browser, &item.Sound); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) SavePreferences(ctx context.Context, workspaceID, memberID string, inputs []PreferenceInput) ([]Preference, error) {
	inputs, err := normalizePreferences(inputs)
	if err != nil {
		return nil, err
	}
	for _, input := range inputs {
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO notification_preferences(workspace_id,member_id,type,in_app,email,browser,sound)
			VALUES($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (member_id,type) DO UPDATE SET
				workspace_id=EXCLUDED.workspace_id,in_app=EXCLUDED.in_app,email=EXCLUDED.email,
				browser=EXCLUDED.browser,sound=EXCLUDED.sound
		`, workspaceID, memberID, input.Type, input.InApp, input.Email, input.Browser, input.Sound); err != nil {
			return nil, fmt.Errorf("notification: save preference: %w", err)
		}
	}
	return s.Preferences(ctx, workspaceID, memberID)
}

func normalizePreferences(inputs []PreferenceInput) ([]PreferenceInput, error) {
	if len(inputs) > len(preferenceTypes) {
		return nil, ErrInvalidPreference
	}
	result := make([]PreferenceInput, len(inputs))
	seen := make(map[string]bool, len(inputs))
	for index, input := range inputs {
		input.Type = strings.TrimSpace(strings.ToLower(input.Type))
		if !preferenceTypes[input.Type] || seen[input.Type] {
			return nil, ErrInvalidPreference
		}
		seen[input.Type] = true
		result[index] = input
	}
	return result, nil
}

func (s *Service) List(ctx context.Context, workspaceID, memberID string, filter ListFilter) ([]Notification, error) {
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	if filter.Before.IsZero() {
		filter.Before = time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
	}
	where := `workspace_id = $1 AND member_id = $2 AND (created_at, id) < ($3, $4)`
	if filter.Unread {
		where += ` AND read_at IS NULL`
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, workspace_id, member_id, type, title, body, entity_type, entity_id, url, source_event_id, read_at, created_at
		FROM notifications
		WHERE `+where+`
		ORDER BY created_at DESC, id DESC
		LIMIT $5
	`, workspaceID, memberID, filter.Before, filter.BeforeID, filter.Limit)
	if err != nil {
		return nil, fmt.Errorf("notification: list: %w", err)
	}
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		var item Notification
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.MemberID, &item.Type, &item.Title, &item.Body,
			&item.EntityType, &item.EntityID, &item.URL, &item.SourceEventID, &item.ReadAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// NotifyAssignment creates one in-app assignment alert for the newly assigned
// member. sourceEventID makes event-consumer retries safe while preserving
// separate notifications for later assignments of the same entity.
func (s *Service) NotifyAssignment(ctx context.Context, workspaceID, memberID, actorID, entityType, entityID, sourceEventID string) error {
	if memberID == "" || (entityType != "conversation" && entityType != "ticket") {
		return nil
	}
	label := "conversation"
	url := "/inbox?conversation=" + entityID
	if entityType == "ticket" {
		label = "ticket"
		url = "/tickets/" + entityID
	}
	return s.insertForMember(ctx, workspaceID, memberID, actorID, "assignment", "Assigned "+label, "A "+label+" was assigned to you.", url, entityType, entityID, sourceEventID)
}

// NotifySLA fans an approaching or breached timer out to the subject's
// assignee, team members, and followers. Recipient resolution is repeated at
// event-consumption time so an assignment change cannot expose an old queue
// membership and all predicates remain workspace-scoped.
func (s *Service) NotifySLA(ctx context.Context, workspaceID, entityType, entityID, kind, sourceEventID string) error {
	preferenceType, notificationType, title, body := slaNotification(kind)
	if preferenceType == "" || (entityType != "conversation" && entityType != "ticket") {
		return nil
	}

	var recipients string
	if entityType == "conversation" {
		recipients = `
			SELECT c.assignee_id AS member_id FROM conversations c
			WHERE c.workspace_id=$1 AND c.id=$2 AND c.assignee_id IS NOT NULL
			UNION ALL
			SELECT tm.member_id FROM conversations c
			JOIN team_members tm ON tm.team_id=c.team_id
			JOIN workspace_members m ON m.id=tm.member_id AND m.workspace_id=c.workspace_id
			WHERE c.workspace_id=$1 AND c.id=$2 AND c.team_id IS NOT NULL
			UNION ALL
			SELECT f.member_id FROM conversation_followers f
			JOIN conversations c ON c.id=f.conversation_id AND c.workspace_id=$1
			WHERE f.conversation_id=$2`
	} else {
		recipients = `
			SELECT t.assignee_id AS member_id FROM tickets t
			WHERE t.workspace_id=$1 AND t.id=$2 AND t.assignee_id IS NOT NULL
			UNION ALL
			SELECT tm.member_id FROM tickets t
			JOIN team_members tm ON tm.team_id=t.team_id
			JOIN workspace_members m ON m.id=tm.member_id AND m.workspace_id=t.workspace_id
			WHERE t.workspace_id=$1 AND t.id=$2 AND t.team_id IS NOT NULL
			UNION ALL
			SELECT f.member_id FROM ticket_followers f
			JOIN tickets t ON t.id=f.ticket_id AND t.workspace_id=$1
			WHERE f.ticket_id=$2`
	}

	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT recipients.member_id
		FROM (`+recipients+`) recipients
		JOIN workspace_members members ON members.id=recipients.member_id AND members.workspace_id=$1
		LEFT JOIN notification_preferences preferences
			ON preferences.workspace_id=$1 AND preferences.member_id=recipients.member_id AND preferences.type=$3
		WHERE preferences.member_id IS NULL OR coalesce(preferences.in_app,false)
		   OR coalesce(preferences.email,false) OR coalesce(preferences.browser,false)
		   OR coalesce(preferences.sound,false)
	`, workspaceID, entityID, preferenceType)
	if err != nil {
		return fmt.Errorf("notification: resolve SLA recipients: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var memberID string
		if err := rows.Scan(&memberID); err != nil {
			return err
		}
		url := "/inbox?conversation=" + entityID
		if entityType == "ticket" {
			url = "/tickets/" + entityID
		}
		if err := s.insertForMember(ctx, workspaceID, memberID, "", notificationType, title, body, url, entityType, entityID, sourceEventID); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Service) insertForMember(ctx context.Context, workspaceID, memberID, actorID, typ, title, body, url, entityType, entityID, sourceEventID string) error {
	preferenceType := preferenceTypeFor(typ)
	var notificationID string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO notifications
			(id,workspace_id,member_id,type,title,body,entity_type,entity_id,url,source_event_id)
		SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,'')
		FROM workspace_members members
		LEFT JOIN notification_preferences preferences
			ON preferences.workspace_id=$2 AND preferences.member_id=$3 AND preferences.type=$12
		WHERE members.workspace_id=$2 AND members.id=$3
		  AND ($11='' OR members.id<>$11)
		  AND (preferences.member_id IS NULL OR coalesce(preferences.in_app,false)
		       OR coalesce(preferences.email,false) OR coalesce(preferences.browser,false)
		       OR coalesce(preferences.sound,false))
		  AND NOT EXISTS (
			SELECT 1 FROM notifications existing
			WHERE existing.workspace_id=$2 AND existing.member_id=$3
			  AND NULLIF($10,'') IS NOT NULL AND existing.source_event_id=$10
		  )
		ON CONFLICT DO NOTHING
		RETURNING id
	`, ids.New(ids.PrefixNotification), workspaceID, memberID, typ, title, body, entityType, entityID, url, sourceEventID, actorID, preferenceType).Scan(&notificationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("notification: insert event alert: %w", err)
	}
	return s.queueEmail(ctx, workspaceID, memberID, notificationID, preferenceType, title, body, url)
}

func preferenceTypeFor(notificationType string) string {
	if notificationType == "customer_reply" {
		return "reply"
	}
	return notificationType
}

type emailPayload struct {
	To            string   `json:"to"`
	Subject       string   `json:"subject"`
	Body          string   `json:"body"`
	WorkspaceID   string   `json:"workspace_id,omitempty"`
	AttachmentIDs []string `json:"attachment_ids,omitempty"`
}

func (s *Service) queueEmail(ctx context.Context, workspaceID, memberID, notificationID, preferenceType, title, body, url string) error {
	if s.jobs == nil {
		return nil
	}
	var address, name string
	var enabled bool
	err := s.pool.QueryRow(ctx, `
		SELECT u.email::text,u.name,coalesce(preferences.email,false)
		FROM workspace_members members
		JOIN users u ON u.id=members.user_id
		LEFT JOIN notification_preferences preferences
			ON preferences.workspace_id=members.workspace_id
			AND preferences.member_id=members.id AND preferences.type=$3
		WHERE members.workspace_id=$1 AND members.id=$2
	`, workspaceID, memberID, preferenceType).Scan(&address, &name, &enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("notification: resolve email recipient: %w", err)
	}
	if !enabled || strings.TrimSpace(address) == "" {
		return nil
	}
	message := "Hi " + strings.TrimSpace(name) + ",\n\n" + title + "\n\n" + body
	if strings.TrimSpace(url) != "" {
		message += "\n\nOpen in Hubchat: " + url
	}
	_, err = s.jobs.Enqueue(ctx, jobs.Spec{
		WorkspaceID: workspaceID,
		Queue:       "email",
		Type:        "email.send",
		Payload:     emailPayload{To: address, Subject: title, Body: message},
		DedupeKey:   "notification-email:" + notificationID,
	})
	if errors.Is(err, jobs.ErrDuplicate) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("notification: queue email: %w", err)
	}
	return nil
}

func slaNotification(kind string) (preferenceType, notificationType, title, body string) {
	switch kind {
	case "approaching":
		return "sla_warning", "sla_warning", "SLA approaching breach", "A support timer is approaching its target."
	case "breached":
		return "sla_breach", "sla_breach", "SLA breached", "A support timer has breached its target."
	default:
		return "", "", "", ""
	}
}

// RunEventConsumer turns committed assignment and SLA events into durable
// alerts and optional queued email delivery. It follows the same gap-draining
// protocol as realtime and automation consumers, while source_event_id makes
// processing idempotent.
func (s *Service) RunEventConsumer(ctx context.Context, signals <-chan events.Signal, source interface {
	Since(context.Context, string, int64, int) ([]events.Record, error)
}) {
	for {
		select {
		case <-ctx.Done():
			return
		case signal, ok := <-signals:
			if !ok {
				return
			}
			s.seenMu.Lock()
			after, exists := s.seen[signal.WorkspaceID]
			if !exists {
				after = signal.Sequence - 1
			}
			s.seenMu.Unlock()
			for {
				records, err := source.Since(ctx, signal.WorkspaceID, after, 200)
				if err != nil || len(records) == 0 {
					break
				}
				failed := false
				for _, record := range records {
					if err := s.processEvent(ctx, record); err != nil {
						failed = true
						break
					}
					after = record.Sequence
				}
				if failed {
					break
				}
				s.seenMu.Lock()
				s.seen[signal.WorkspaceID] = after
				s.seenMu.Unlock()
				if len(records) < 200 {
					break
				}
			}
		}
	}
}

func (s *Service) processEvent(ctx context.Context, record events.Record) error {
	switch record.Type {
	case events.ConversationAssigned, events.TicketUpdated:
		var data struct {
			AssigneeID *string `json:"assignee_id"`
		}
		if err := json.Unmarshal(record.Data, &data); err != nil {
			return fmt.Errorf("notification: decode assignment event: %w", err)
		}
		if data.AssigneeID == nil {
			return nil
		}
		return s.NotifyAssignment(ctx, record.WorkspaceID, *data.AssigneeID, record.ActorID, record.EntityType, record.EntityID, record.ID)
	case events.SLAApproaching, events.SLABreached:
		kind := "breached"
		if record.Type == events.SLAApproaching {
			kind = "approaching"
		}
		return s.NotifySLA(ctx, record.WorkspaceID, record.EntityType, record.EntityID, kind, record.ID)
	case events.FeedbackStatusChanged:
		return s.NotifyFeedbackSubscribers(ctx, record.WorkspaceID, record.EntityID, record.ID)
	case events.ChangelogPublished:
		return s.NotifyChangelogSubscribers(ctx, record.WorkspaceID, record.EntityID, record.ID)
	case events.MessageCreated:
		var message conversation.MessageEvent
		if err := json.Unmarshal(record.Data, &message); err != nil {
			return fmt.Errorf("notification: decode message event: %w", err)
		}
		if message.AuthorType != "agent" || message.Kind != "reply" || strings.TrimSpace(message.Body) == "" {
			return nil
		}
		return s.NotifyTicketCustomerReply(ctx, record.WorkspaceID, message, record.ID)
	case events.TicketCreated:
		return s.NotifyTicketCustomer(ctx, record.WorkspaceID, record.EntityID, record.ID, record.Type)
	case events.TicketStateSet:
		if err := s.NotifyTicketCustomer(ctx, record.WorkspaceID, record.EntityID, record.ID, record.Type); err != nil {
			return err
		}
		if s.surveys == nil {
			return nil
		}
		var state struct {
			To string `json:"to"`
		}
		if err := json.Unmarshal(record.Data, &state); err != nil {
			return fmt.Errorf("notification: decode ticket state event: %w", err)
		}
		if state.To != "resolved" && state.To != "closed" {
			return nil
		}
		return s.surveys.NotifyTicketResolution(ctx, record.WorkspaceID, record.EntityID, record.ID, state.To)
	default:
		return nil
	}
}

// NotifyChangelogSubscribers queues one email per opted-in customer for a
// newly published entry. Preference resolution happens at event-consumption
// time, and the source event/customer pair is the durable dedupe key.
func (s *Service) NotifyChangelogSubscribers(ctx context.Context, workspaceID, entryID, sourceEventID string) error {
	if s.jobs == nil {
		return nil
	}
	var title, body string
	if err := s.pool.QueryRow(ctx, `
		SELECT title,body FROM changelog_entries
		WHERE workspace_id=$1 AND id=$2 AND published_at IS NOT NULL
	`, workspaceID, entryID).Scan(&title, &body); errors.Is(err, pgx.ErrNoRows) {
		return nil
	} else if err != nil {
		return fmt.Errorf("notification: resolve changelog entry: %w", err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT c.id,NULLIF(c.email::text,''),coalesce(c.name,'')
		FROM customers c
		LEFT JOIN customer_notification_preferences preferences
		  ON preferences.customer_id=c.id AND preferences.workspace_id=c.workspace_id
		WHERE c.workspace_id=$1 AND NULLIF(c.email::text,'') IS NOT NULL
		  AND coalesce(preferences.changelog,false)
		  AND NOT EXISTS (
			SELECT 1 FROM email_suppressions suppression
			WHERE suppression.workspace_id=c.workspace_id
			  AND suppression.address::text=c.email::text
		  )
	`, workspaceID)
	if err != nil {
		return fmt.Errorf("notification: resolve changelog subscribers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var customerID, address, name string
		if err := rows.Scan(&customerID, &address, &name); err != nil {
			return err
		}
		link := s.changelogLink(entryID)
		message := "Hi " + strings.TrimSpace(name) + ",\n\n" + body
		if link != "" {
			message += "\n\nRead the update: " + link
		}
		_, err := s.jobs.Enqueue(ctx, jobs.Spec{
			WorkspaceID: workspaceID,
			Queue:       "email",
			Type:        "email.send",
			Payload:     emailPayload{To: address, Subject: title, Body: message},
			DedupeKey:   "changelog-email:" + sourceEventID + ":" + customerID,
		})
		if errors.Is(err, jobs.ErrDuplicate) {
			continue
		}
		if err != nil {
			return fmt.Errorf("notification: queue changelog email: %w", err)
		}
	}
	return rows.Err()
}

func (s *Service) changelogLink(entryID string) string {
	path := "/portal/changelog#" + url.PathEscape(entryID)
	if s.publicURL == nil {
		return path
	}
	base := *s.publicURL
	base.Path = strings.TrimRight(base.Path, "/") + "/portal/changelog"
	base.Fragment = entryID
	base.RawQuery = ""
	return base.String()
}

func (s *Service) UnreadCount(ctx context.Context, workspaceID, memberID string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM notifications
		WHERE workspace_id = $1 AND member_id = $2 AND read_at IS NULL
	`, workspaceID, memberID).Scan(&count)
	return count, err
}

func (s *Service) MarkRead(ctx context.Context, workspaceID, memberID, id string) error {
	result, err := s.pool.Exec(ctx, `
		UPDATE notifications SET read_at = coalesce(read_at, now())
		WHERE workspace_id = $1 AND member_id = $2 AND id = $3
	`, workspaceID, memberID, id)
	if err != nil {
		return fmt.Errorf("notification: mark read: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) MarkAllRead(ctx context.Context, workspaceID, memberID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE notifications SET read_at = now()
		WHERE workspace_id = $1 AND member_id = $2 AND read_at IS NULL
	`, workspaceID, memberID)
	return err
}

// NotifyConversationMessage fans a customer reply out to the members who can
// act on that conversation. Recipient resolution happens here, not in the
// browser or handler, so assignment/team/follower changes are respected and a
// member from another workspace can never be selected.
func (s *Service) NotifyConversationMessage(ctx context.Context, workspaceID, conversationID, messageID, authorType, authorMemberID, body string) error {
	if authorType != "customer" {
		return nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT recipients.member_id FROM (
			SELECT c.assignee_id AS member_id
			FROM conversations c
			WHERE c.workspace_id = $1 AND c.id = $2 AND c.assignee_id IS NOT NULL
			UNION ALL
			SELECT tm.member_id
			FROM conversations c
			JOIN team_members tm ON tm.team_id = c.team_id
			JOIN workspace_members m ON m.id = tm.member_id AND m.workspace_id = c.workspace_id
			WHERE c.workspace_id = $1 AND c.id = $2 AND c.team_id IS NOT NULL
			UNION ALL
			SELECT tf.member_id
			FROM conversation_followers tf
			JOIN conversations c ON c.id = tf.conversation_id AND c.workspace_id = $1
			WHERE tf.conversation_id = $2
		) recipients
		LEFT JOIN notification_preferences preferences
			ON preferences.workspace_id=$1 AND preferences.member_id=recipients.member_id AND preferences.type='reply'
		WHERE recipients.member_id IS NOT NULL AND ($3 = '' OR recipients.member_id <> $3)
		  AND (preferences.member_id IS NULL OR coalesce(preferences.in_app,false)
		       OR coalesce(preferences.email,false) OR coalesce(preferences.browser,false)
		       OR coalesce(preferences.sound,false))
	`, workspaceID, conversationID, authorMemberID)
	if err != nil {
		return fmt.Errorf("notification: resolve recipients: %w", err)
	}
	defer rows.Close()

	url := "/inbox?conversation=" + conversationID
	preview := strings.Join(strings.Fields(body), " ")
	if len(preview) > 240 {
		preview = preview[:240] + "…"
	}
	for rows.Next() {
		var memberID string
		if err := rows.Scan(&memberID); err != nil {
			return err
		}
		if err := s.insertForMember(ctx, workspaceID, memberID, authorMemberID, "customer_reply", "New customer reply", preview, url, "message", messageID, "message:"+messageID); err != nil {
			return err
		}
	}
	return rows.Err()
}

// NotifyFeedbackSubscribers queues one status-change email per subscribed
// customer. The event ID is part of each dedupe key, so event-consumer retries
// cannot send duplicates while later status changes still notify normally.
func (s *Service) NotifyFeedbackSubscribers(ctx context.Context, workspaceID, itemID, sourceEventID string) error {
	if s.jobs == nil {
		return nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT c.id, coalesce(c.email::text,''), coalesce(c.name,''), i.title, i.status
		FROM feedback_subscriptions subscriptions
		JOIN feedback_items i ON i.id=subscriptions.item_id AND i.workspace_id=$1
		JOIN customers c ON c.id=subscriptions.customer_id AND c.workspace_id=$1
		LEFT JOIN customer_notification_preferences preferences
		  ON preferences.customer_id=c.id AND preferences.workspace_id=c.workspace_id
		WHERE subscriptions.item_id=$2 AND NULLIF(c.email::text,'') IS NOT NULL
		  AND coalesce(preferences.feedback_updates,true)
		  AND NOT EXISTS (
			SELECT 1 FROM email_suppressions suppression
			WHERE suppression.workspace_id=c.workspace_id
			  AND suppression.address::text=c.email::text
		  )
	`, workspaceID, itemID)
	if err != nil {
		return fmt.Errorf("notification: resolve feedback subscribers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var customerID, address, name, title, status string
		if err := rows.Scan(&customerID, &address, &name, &title, &status); err != nil {
			return err
		}
		subject := "Feedback update: " + title
		body := "Hi " + strings.TrimSpace(name) + ",\n\nThe feedback item “" + title + "” is now " + strings.ReplaceAll(status, "_", " ") + ".\n\nYou are receiving this because you followed this feedback item."
		_, err := s.jobs.Enqueue(ctx, jobs.Spec{
			WorkspaceID: workspaceID,
			Queue:       "email",
			Type:        "email.send",
			Payload:     emailPayload{To: address, Subject: subject, Body: body},
			DedupeKey:   "feedback-status-email:" + sourceEventID + ":" + customerID,
		})
		if errors.Is(err, jobs.ErrDuplicate) {
			continue
		}
		if err != nil {
			return fmt.Errorf("notification: queue feedback email: %w", err)
		}
	}
	return rows.Err()
}

// NotifyTicketCustomer queues one email for the customer attached to a ticket
// after creation or a status transition. The preference is resolved at
// consumption time, and the event/customer pair is the dedupe key so replaying
// an event cannot create a second delivery while later changes still notify.
func (s *Service) NotifyTicketCustomer(ctx context.Context, workspaceID, ticketID, sourceEventID string, eventType events.Type) error {
	if s.jobs == nil {
		return nil
	}
	var customerID, address, name, number, title, status string
	err := s.pool.QueryRow(ctx, `
		SELECT c.id, NULLIF(c.email::text,''), coalesce(c.name,''),
		       t.prefix || '-' || t.number::text, t.title, t.status
		FROM tickets t
		JOIN customers c ON c.id=t.customer_id AND c.workspace_id=t.workspace_id
		LEFT JOIN customer_notification_preferences preferences
		  ON preferences.customer_id=c.id AND preferences.workspace_id=c.workspace_id
		WHERE t.workspace_id=$1 AND t.id=$2
		  AND NULLIF(c.email::text,'') IS NOT NULL
		  AND coalesce(preferences.ticket_status,true)
		  AND NOT EXISTS (
			SELECT 1 FROM email_suppressions suppression
			WHERE suppression.workspace_id=c.workspace_id
			  AND suppression.address::text=c.email::text
		  )
	`, workspaceID, ticketID).Scan(&customerID, &address, &name, &number, &title, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("notification: resolve ticket customer: %w", err)
	}

	subject, body := ticketCustomerMessage(eventType, name, number, title, status)
	_, err = s.jobs.Enqueue(ctx, jobs.Spec{
		WorkspaceID: workspaceID,
		Queue:       "email",
		Type:        "email.send",
		Payload:     emailPayload{To: address, Subject: subject, Body: body},
		DedupeKey:   "ticket-status-email:" + sourceEventID + ":" + customerID,
	})
	if errors.Is(err, jobs.ErrDuplicate) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("notification: queue ticket customer email: %w", err)
	}
	return nil
}

// NotifyTicketCustomerReply queues a transactional customer email for an
// agent reply on a non-email ticket conversation. Email-channel replies are
// deliberately excluded because emailchannel owns their RFC message IDs,
// In-Reply-To headers, and attachment processing.
func (s *Service) NotifyTicketCustomerReply(ctx context.Context, workspaceID string, message conversation.MessageEvent, sourceEventID string) error {
	if s.jobs == nil || strings.TrimSpace(sourceEventID) == "" {
		return nil
	}
	var customerID, address, name, number, title, ticketID string
	err := s.pool.QueryRow(ctx, `
		SELECT customer.id, NULLIF(customer.email::text,''), coalesce(customer.name,''),
		       ticket.prefix || '-' || ticket.number::text, ticket.title, ticket.id
		FROM conversations conversation
		JOIN tickets ticket
		  ON ticket.workspace_id=conversation.workspace_id AND ticket.conversation_id=conversation.id
		JOIN customers customer
		  ON customer.workspace_id=ticket.workspace_id AND customer.id=ticket.customer_id
		WHERE conversation.workspace_id=$1 AND conversation.id=$2
		  AND conversation.channel <> 'email'
		  AND NULLIF(customer.email::text,'') IS NOT NULL
		  AND NOT EXISTS (
			SELECT 1 FROM email_suppressions suppression
			WHERE suppression.workspace_id=customer.workspace_id
			  AND suppression.address::text=customer.email::text
		  )
	`, workspaceID, message.ConversationID).Scan(&customerID, &address, &name, &number, &title, &ticketID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("notification: resolve ticket reply customer: %w", err)
	}

	subject, body := ticketReplyMessage(name, number, title, message.AuthorName, message.Body, s.ticketLink(ticketID))
	var attachmentIDs []string
	if s.files != nil {
		attachments, attachmentErr := s.files.MessageAttachments(ctx, workspaceID, message.MessageID)
		if attachmentErr != nil {
			return fmt.Errorf("notification: load ticket reply attachments: %w", attachmentErr)
		}
		for _, attachment := range attachments {
			attachmentIDs = append(attachmentIDs, attachment.ID)
		}
	}
	_, err = s.jobs.Enqueue(ctx, jobs.Spec{
		WorkspaceID: workspaceID,
		Queue:       "email",
		Type:        "email.send",
		Payload: emailPayload{
			To: address, Subject: subject, Body: body,
			WorkspaceID: workspaceID, AttachmentIDs: attachmentIDs,
		},
		DedupeKey: "ticket-reply-email:" + sourceEventID + ":" + customerID,
	})
	if errors.Is(err, jobs.ErrDuplicate) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("notification: queue ticket reply email: %w", err)
	}
	return nil
}

func ticketReplyMessage(name, number, title, authorName, body, link string) (string, string) {
	subject := "New reply on ticket " + number
	message := "Hi " + strings.TrimSpace(name) + ",\n\n"
	if strings.TrimSpace(authorName) == "" {
		message += "Our support team replied to your ticket"
	} else {
		message += strings.TrimSpace(authorName) + " replied to your ticket"
	}
	message += " “" + title + "”:\n\n" + body
	if link != "" {
		message += "\n\nView your request: " + link
	}
	return subject, message
}

func (s *Service) ticketLink(ticketID string) string {
	if strings.TrimSpace(ticketID) == "" {
		return ""
	}
	path := "/portal/tickets/" + url.PathEscape(ticketID)
	if s.publicURL == nil {
		return path
	}
	base := *s.publicURL
	base.Path = strings.TrimRight(base.Path, "/") + path
	base.RawQuery = ""
	base.Fragment = ""
	return base.String()
}

func ticketCustomerMessage(eventType events.Type, name, number, title, status string) (string, string) {
	statusLabel := strings.ReplaceAll(status, "_", " ")
	if eventType == events.TicketCreated {
		return "Ticket " + number + " received", "Hi " + strings.TrimSpace(name) + ",\n\nWe received your request “" + title + "”. Its current status is " + statusLabel + "."
	}
	return "Ticket " + number + " update", "Hi " + strings.TrimSpace(name) + ",\n\nYour ticket “" + title + "” is now " + statusLabel + "."
}
