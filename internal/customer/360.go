package customer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Customer360 is the bounded cross-module snapshot shown on the customer
// detail page. It is intentionally a snapshot rather than a second copy of
// the source records: each section keeps the owning record's id so a future
// deep link can open the authoritative screen.
type Customer360 struct {
	Companies              []CompanyReference      `json:"companies"`
	CompaniesTruncated     bool                    `json:"companies_truncated"`
	Conversations          []ConversationReference `json:"conversations"`
	ConversationsTruncated bool                    `json:"conversations_truncated"`
	Tickets                []TicketReference       `json:"tickets"`
	TicketsTruncated       bool                    `json:"tickets_truncated"`
	Events                 []EventReference        `json:"events"`
	EventsTruncated        bool                    `json:"events_truncated"`
	PageJourney            []PageVisitReference    `json:"page_journey"`
	PageJourneyTruncated   bool                    `json:"page_journey_truncated"`
	CurrentPage            *PageVisitReference     `json:"current_page,omitempty"`
	Device                 *DeviceReference        `json:"device,omitempty"`
	Sessions               []SessionReference      `json:"sessions"`
	SessionsTruncated      bool                    `json:"sessions_truncated"`
	Feedback               []FeedbackReference     `json:"feedback"`
	FeedbackTruncated      bool                    `json:"feedback_truncated"`
	Surveys                []SurveyReference       `json:"surveys"`
	SurveysTruncated       bool                    `json:"surveys_truncated"`
	Articles               []ArticleReference      `json:"articles"`
	ArticlesTruncated      bool                    `json:"articles_truncated"`
	Identities             []IdentityReference     `json:"identities"`
	IdentitiesTruncated    bool                    `json:"identities_truncated"`
	Merges                 []MergeReference        `json:"merges"`
	MergesTruncated        bool                    `json:"merges_truncated"`
	ContextMetadata        map[string]any          `json:"context_metadata,omitempty"`
}

type CompanyReference struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Domain *string `json:"domain,omitempty"`
	Tier   *string `json:"tier,omitempty"`
}

type ConversationReference struct {
	ID                 string     `json:"id"`
	Subject            *string    `json:"subject,omitempty"`
	State              string     `json:"state"`
	Channel            string     `json:"channel"`
	InboxID            string     `json:"inbox_id"`
	LastMessagePreview string     `json:"last_message_preview"`
	LastMessageAt      *time.Time `json:"last_message_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
}

type TicketReference struct {
	ID             string    `json:"id"`
	Number         int       `json:"number"`
	Prefix         string    `json:"prefix"`
	Title          string    `json:"title"`
	Status         string    `json:"status"`
	Priority       string    `json:"priority"`
	ConversationID *string   `json:"conversation_id,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
	CreatedAt      time.Time `json:"created_at"`
}

// EventReference intentionally omits the event payload. Payloads can contain
// customer-sensitive values and the dedicated timeline endpoint applies the
// viewer's audited redaction policy before returning them.
type EventReference struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	Source     string    `json:"source"`
	URL        *string   `json:"url,omitempty"`
	RequestID  *string   `json:"request_id,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

type PageVisitReference struct {
	ID             string             `json:"id"`
	URL            *string            `json:"url,omitempty"`
	Title          string             `json:"title,omitempty"`
	Device         string             `json:"device,omitempty"`
	Browser        string             `json:"browser,omitempty"`
	OS             string             `json:"os,omitempty"`
	Platform       string             `json:"platform,omitempty"`
	ReferrerOrigin string             `json:"referrer_origin,omitempty"`
	UserAgent      string             `json:"user_agent,omitempty"`
	Viewport       *ViewportReference `json:"viewport,omitempty"`
	OccurredAt     time.Time          `json:"occurred_at"`
}

type DeviceReference struct {
	Device         string             `json:"device,omitempty"`
	Browser        string             `json:"browser,omitempty"`
	OS             string             `json:"os,omitempty"`
	Language       string             `json:"language,omitempty"`
	Timezone       string             `json:"timezone,omitempty"`
	Platform       string             `json:"platform,omitempty"`
	ReferrerOrigin string             `json:"referrer_origin,omitempty"`
	UserAgent      string             `json:"user_agent,omitempty"`
	Viewport       *ViewportReference `json:"viewport,omitempty"`
}

type ViewportReference struct {
	Width            int     `json:"width,omitempty"`
	Height           int     `json:"height,omitempty"`
	DevicePixelRatio float64 `json:"device_pixel_ratio,omitempty"`
}

type SessionReference struct {
	ID           string             `json:"id"`
	IPPrefix     *string            `json:"ip_prefix,omitempty"`
	IPCountry    *string            `json:"ip_country,omitempty"`
	Device       *string            `json:"device,omitempty"`
	Browser      *string            `json:"browser,omitempty"`
	OS           *string            `json:"os,omitempty"`
	Referrer     *string            `json:"referrer,omitempty"`
	LandingURL   *string            `json:"landing_url,omitempty"`
	CurrentURL   *string            `json:"current_url,omitempty"`
	CurrentTitle *string            `json:"current_title,omitempty"`
	Language     *string            `json:"language,omitempty"`
	Timezone     *string            `json:"timezone,omitempty"`
	Platform     *string            `json:"platform,omitempty"`
	UserAgent    *string            `json:"user_agent,omitempty"`
	Viewport     *ViewportReference `json:"viewport,omitempty"`
	PageViews    int                `json:"page_views"`
	StartedAt    time.Time          `json:"started_at"`
	LastSeenAt   time.Time          `json:"last_seen_at"`
	EndedAt      *time.Time         `json:"ended_at,omitempty"`
}

type FeedbackReference struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	BoardID      string    `json:"board_id"`
	BoardName    string    `json:"board_name"`
	Status       string    `json:"status"`
	VoteCount    int       `json:"vote_count"`
	CommentCount int       `json:"comment_count"`
	CreatedAt    time.Time `json:"created_at"`
}

type SurveyReference struct {
	ID          string     `json:"id"`
	SurveyID    string     `json:"survey_id"`
	SurveyName  string     `json:"survey_name"`
	SurveyType  string     `json:"survey_type"`
	Score       *float64   `json:"score,omitempty"`
	Comment     string     `json:"comment,omitempty"`
	SubmittedAt *time.Time `json:"submitted_at,omitempty"`
}

type ArticleReference struct {
	ID        string    `json:"id"`
	ArticleID string    `json:"article_id"`
	Title     string    `json:"title"`
	Slug      string    `json:"slug"`
	Helpful   bool      `json:"helpful"`
	Comment   string    `json:"comment,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type IdentityReference struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Value      string     `json:"value"`
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
	IsPrimary  bool       `json:"is_primary"`
	CreatedAt  time.Time  `json:"created_at"`
}

type MergeReference struct {
	ID              string         `json:"id"`
	WinnerID        string         `json:"winner_id"`
	LoserID         string         `json:"loser_id"`
	MovedCounts     map[string]any `json:"moved_counts"`
	ReversibleUntil *time.Time     `json:"reversible_until,omitempty"`
	ReversedAt      *time.Time     `json:"reversed_at,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
}

const (
	customer360PageSize  = 25
	customer360QuerySize = customer360PageSize + 1
)

// Customer360 loads the recent cross-module records associated with a
// customer. Every query carries both workspace_id and customer_id. The
// extra row in each result is retained only to tell the caller that the
// bounded snapshot was truncated; the UI can then link to the owning list.
func (s *Service) Customer360(ctx context.Context, workspaceID, customerID string) (*Customer360, error) {
	if _, err := s.repo.byID(ctx, workspaceID, customerID); err != nil {
		return nil, err
	}

	result := &Customer360{}

	companyRows, err := s.pool.Query(ctx, `
		SELECT c.id, c.name, c.domain::text, c.tier
		FROM companies c
		JOIN company_customers cc ON cc.company_id=c.id
		WHERE c.workspace_id=$1 AND cc.customer_id=$2
		ORDER BY c.name ASC, c.id ASC
		LIMIT $3
	`, workspaceID, customerID, customer360QuerySize)
	if err != nil {
		return nil, fmt.Errorf("customer: load 360 companies: %w", err)
	}
	for companyRows.Next() {
		var item CompanyReference
		if err := companyRows.Scan(&item.ID, &item.Name, &item.Domain, &item.Tier); err != nil {
			companyRows.Close()
			return nil, fmt.Errorf("customer: scan 360 company: %w", err)
		}
		result.Companies = append(result.Companies, item)
	}
	if err := companyRows.Err(); err != nil {
		companyRows.Close()
		return nil, fmt.Errorf("customer: read 360 companies: %w", err)
	}
	companyRows.Close()
	if len(result.Companies) > customer360PageSize {
		result.Companies = result.Companies[:customer360PageSize]
		result.CompaniesTruncated = true
	}

	conversationRows, err := s.pool.Query(ctx, `
		SELECT id, subject, state, channel, inbox_id, last_message_preview, last_message_at, created_at
		FROM conversations
		WHERE workspace_id=$1 AND customer_id=$2
		ORDER BY last_message_at DESC, id DESC
		LIMIT $3
	`, workspaceID, customerID, customer360QuerySize)
	if err != nil {
		return nil, fmt.Errorf("customer: load 360 conversations: %w", err)
	}
	for conversationRows.Next() {
		var item ConversationReference
		if err := conversationRows.Scan(&item.ID, &item.Subject, &item.State, &item.Channel, &item.InboxID, &item.LastMessagePreview, &item.LastMessageAt, &item.CreatedAt); err != nil {
			conversationRows.Close()
			return nil, fmt.Errorf("customer: scan 360 conversation: %w", err)
		}
		result.Conversations = append(result.Conversations, item)
	}
	if err := conversationRows.Err(); err != nil {
		conversationRows.Close()
		return nil, fmt.Errorf("customer: read 360 conversations: %w", err)
	}
	conversationRows.Close()
	if len(result.Conversations) > customer360PageSize {
		result.Conversations = result.Conversations[:customer360PageSize]
		result.ConversationsTruncated = true
	}

	ticketRows, err := s.pool.Query(ctx, `
		SELECT id, number, prefix, title, status, priority, conversation_id, updated_at, created_at
		FROM tickets
		WHERE workspace_id=$1 AND customer_id=$2
		ORDER BY updated_at DESC, id DESC
		LIMIT $3
	`, workspaceID, customerID, customer360QuerySize)
	if err != nil {
		return nil, fmt.Errorf("customer: load 360 tickets: %w", err)
	}
	for ticketRows.Next() {
		var item TicketReference
		if err := ticketRows.Scan(&item.ID, &item.Number, &item.Prefix, &item.Title, &item.Status, &item.Priority, &item.ConversationID, &item.UpdatedAt, &item.CreatedAt); err != nil {
			ticketRows.Close()
			return nil, fmt.Errorf("customer: scan 360 ticket: %w", err)
		}
		result.Tickets = append(result.Tickets, item)
	}
	if err := ticketRows.Err(); err != nil {
		ticketRows.Close()
		return nil, fmt.Errorf("customer: read 360 tickets: %w", err)
	}
	ticketRows.Close()
	if len(result.Tickets) > customer360PageSize {
		result.Tickets = result.Tickets[:customer360PageSize]
		result.TicketsTruncated = true
	}

	eventRows, err := s.pool.Query(ctx, `
		SELECT id, type, source, url, request_id, occurred_at
		FROM customer_events
		WHERE workspace_id=$1 AND customer_id=$2
		ORDER BY occurred_at DESC, id DESC
		LIMIT $3
	`, workspaceID, customerID, customer360QuerySize)
	if err != nil {
		return nil, fmt.Errorf("customer: load 360 events: %w", err)
	}
	for eventRows.Next() {
		var item EventReference
		if err := eventRows.Scan(&item.ID, &item.Type, &item.Source, &item.URL, &item.RequestID, &item.OccurredAt); err != nil {
			eventRows.Close()
			return nil, fmt.Errorf("customer: scan 360 event: %w", err)
		}
		result.Events = append(result.Events, item)
	}
	if err := eventRows.Err(); err != nil {
		eventRows.Close()
		return nil, fmt.Errorf("customer: read 360 events: %w", err)
	}
	eventRows.Close()
	if len(result.Events) > customer360PageSize {
		result.Events = result.Events[:customer360PageSize]
		result.EventsTruncated = true
	}

	pageRows, err := s.pool.Query(ctx, `
		SELECT id, payload, occurred_at
		FROM customer_events
		WHERE workspace_id=$1 AND customer_id=$2 AND type='page.viewed'
		ORDER BY occurred_at DESC, id DESC
		LIMIT $3
	`, workspaceID, customerID, 11)
	if err != nil {
		return nil, fmt.Errorf("customer: load 360 page journey: %w", err)
	}
	for pageRows.Next() {
		var payload map[string]any
		var id string
		var occurredAt time.Time
		if err := pageRows.Scan(&id, &payload, &occurredAt); err != nil {
			pageRows.Close()
			return nil, fmt.Errorf("customer: scan 360 page journey: %w", err)
		}
		item := pageVisitFromPayload(id, payload, occurredAt)
		result.PageJourney = append(result.PageJourney, item)
		if result.Device == nil {
			result.Device = &DeviceReference{}
		}
		mergeDeviceFromPayload(result.Device, payload, item)
	}
	if err := pageRows.Err(); err != nil {
		pageRows.Close()
		return nil, fmt.Errorf("customer: read 360 page journey: %w", err)
	}
	pageRows.Close()
	if len(result.PageJourney) > 10 {
		result.PageJourney = result.PageJourney[:10]
		result.PageJourneyTruncated = true
	}
	if len(result.PageJourney) > 0 {
		result.CurrentPage = &result.PageJourney[0]
	}
	result.ContextMetadata, err = s.latestCustomerContextMetadata(ctx, workspaceID, customerID)
	if err != nil {
		return nil, fmt.Errorf("customer: load 360 context metadata: %w", err)
	}

	sessionRows, err := s.pool.Query(ctx, `
		SELECT id, ip_prefix, ip_country, device, browser, os, referrer, landing_url, current_url, current_title, language, timezone, platform, user_agent, viewport, page_views, started_at, last_seen_at, ended_at
		FROM contact_sessions
		WHERE workspace_id=$1 AND customer_id=$2
		ORDER BY last_seen_at DESC, id DESC
		LIMIT $3
	`, workspaceID, customerID, customer360QuerySize)
	if err != nil {
		return nil, fmt.Errorf("customer: load 360 sessions: %w", err)
	}
	for sessionRows.Next() {
		var item SessionReference
		if err := sessionRows.Scan(&item.ID, &item.IPPrefix, &item.IPCountry, &item.Device, &item.Browser, &item.OS, &item.Referrer, &item.LandingURL, &item.CurrentURL, &item.CurrentTitle, &item.Language, &item.Timezone, &item.Platform, &item.UserAgent, &item.Viewport, &item.PageViews, &item.StartedAt, &item.LastSeenAt, &item.EndedAt); err != nil {
			sessionRows.Close()
			return nil, fmt.Errorf("customer: scan 360 session: %w", err)
		}
		result.Sessions = append(result.Sessions, item)
	}
	if err := sessionRows.Err(); err != nil {
		sessionRows.Close()
		return nil, fmt.Errorf("customer: read 360 sessions: %w", err)
	}
	sessionRows.Close()
	if len(result.Sessions) > customer360PageSize {
		result.Sessions = result.Sessions[:customer360PageSize]
		result.SessionsTruncated = true
	}
	// A session is also a source of live context. A customer can reach the
	// inbox through an identify call, a form, or another SDK event before the
	// first page.viewed event is committed. Keep the right-hand context panel
	// useful in that case, and prefer the session URL when it is newer than the
	// bounded page-event snapshot.
	if len(result.Sessions) > 0 {
		latest := &result.Sessions[0]
		if result.Device == nil {
			result.Device = deviceFromSession(latest)
		} else {
			sessionDevice := deviceFromSession(latest)
			if sessionDevice != nil {
				mergeDeviceFromSession(result.Device, latest)
			}
		}
		if latest.CurrentURL != nil && (result.CurrentPage == nil || latest.LastSeenAt.After(result.CurrentPage.OccurredAt)) {
			result.CurrentPage = &PageVisitReference{
				ID:             latest.ID,
				URL:            latest.CurrentURL,
				Title:          stringPtrValue(latest.CurrentTitle),
				Device:         stringPtrValue(latest.Device),
				Browser:        stringPtrValue(latest.Browser),
				OS:             stringPtrValue(latest.OS),
				ReferrerOrigin: stringPtrValue(latest.Referrer),
				Platform:       stringPtrValue(latest.Platform),
				UserAgent:      stringPtrValue(latest.UserAgent),
				Viewport:       latest.Viewport,
				OccurredAt:     latest.LastSeenAt,
			}
		}
	}

	feedbackRows, err := s.pool.Query(ctx, `
		SELECT fi.id, fi.title, fi.board_id, fb.name, fi.status,
		       fi.vote_count, fi.comment_count, fi.created_at
		FROM feedback_items fi
		JOIN feedback_boards fb ON fb.id=fi.board_id AND fb.workspace_id=$1
		WHERE fi.workspace_id=$1 AND fi.merged_into_id IS NULL
		  AND (fi.submitter_id=$2
		       OR EXISTS (SELECT 1 FROM feedback_votes fv
		                 WHERE fv.workspace_id=$1 AND fv.item_id=fi.id AND fv.customer_id=$2)
		       OR EXISTS (SELECT 1 FROM feedback_subscriptions fs
		                 JOIN feedback_items fsi ON fsi.id=fs.item_id AND fsi.workspace_id=$1
		                 WHERE fs.item_id=fi.id AND fs.customer_id=$2))
		ORDER BY fi.created_at DESC, fi.id DESC
		LIMIT $3
	`, workspaceID, customerID, customer360QuerySize)
	if err != nil {
		return nil, fmt.Errorf("customer: load 360 feedback: %w", err)
	}
	for feedbackRows.Next() {
		var item FeedbackReference
		if err := feedbackRows.Scan(&item.ID, &item.Title, &item.BoardID, &item.BoardName, &item.Status, &item.VoteCount, &item.CommentCount, &item.CreatedAt); err != nil {
			feedbackRows.Close()
			return nil, fmt.Errorf("customer: scan 360 feedback: %w", err)
		}
		result.Feedback = append(result.Feedback, item)
	}
	if err := feedbackRows.Err(); err != nil {
		feedbackRows.Close()
		return nil, fmt.Errorf("customer: read 360 feedback: %w", err)
	}
	feedbackRows.Close()
	if len(result.Feedback) > customer360PageSize {
		result.Feedback = result.Feedback[:customer360PageSize]
		result.FeedbackTruncated = true
	}

	surveyRows, err := s.pool.Query(ctx, `
		SELECT sr.id, sr.survey_id, s.name, s.type, sr.score, sr.comment, sr.submitted_at
		FROM survey_responses sr
		JOIN surveys s ON s.id=sr.survey_id AND s.workspace_id=$1
		WHERE sr.workspace_id=$1 AND sr.customer_id=$2 AND sr.submitted_at IS NOT NULL
		ORDER BY sr.submitted_at DESC, sr.id DESC
		LIMIT $3
	`, workspaceID, customerID, customer360QuerySize)
	if err != nil {
		return nil, fmt.Errorf("customer: load 360 surveys: %w", err)
	}
	for surveyRows.Next() {
		var item SurveyReference
		if err := surveyRows.Scan(&item.ID, &item.SurveyID, &item.SurveyName, &item.SurveyType, &item.Score, &item.Comment, &item.SubmittedAt); err != nil {
			surveyRows.Close()
			return nil, fmt.Errorf("customer: scan 360 survey: %w", err)
		}
		result.Surveys = append(result.Surveys, item)
	}
	if err := surveyRows.Err(); err != nil {
		surveyRows.Close()
		return nil, fmt.Errorf("customer: read 360 surveys: %w", err)
	}
	surveyRows.Close()
	if len(result.Surveys) > customer360PageSize {
		result.Surveys = result.Surveys[:customer360PageSize]
		result.SurveysTruncated = true
	}

	articleRows, err := s.pool.Query(ctx, `
		SELECT af.id, af.article_id, a.title, a.slug::text, af.helpful, af.comment, af.created_at
		FROM article_feedback af
		JOIN articles a ON a.id=af.article_id AND a.workspace_id=$1
		WHERE af.workspace_id=$1 AND af.customer_id=$2
		ORDER BY af.created_at DESC, af.id DESC
		LIMIT $3
	`, workspaceID, customerID, customer360QuerySize)
	if err != nil {
		return nil, fmt.Errorf("customer: load 360 articles: %w", err)
	}
	for articleRows.Next() {
		var item ArticleReference
		if err := articleRows.Scan(&item.ID, &item.ArticleID, &item.Title, &item.Slug, &item.Helpful, &item.Comment, &item.CreatedAt); err != nil {
			articleRows.Close()
			return nil, fmt.Errorf("customer: scan 360 article: %w", err)
		}
		result.Articles = append(result.Articles, item)
	}
	if err := articleRows.Err(); err != nil {
		articleRows.Close()
		return nil, fmt.Errorf("customer: read 360 articles: %w", err)
	}
	articleRows.Close()
	if len(result.Articles) > customer360PageSize {
		result.Articles = result.Articles[:customer360PageSize]
		result.ArticlesTruncated = true
	}

	identityRows, err := s.pool.Query(ctx, `
		SELECT kind, id, value, verified_at, is_primary, created_at
		FROM (
			SELECT 'email'::text AS kind, id, email::text AS value, verified_at, is_primary, created_at
			FROM customer_emails WHERE workspace_id=$1 AND customer_id=$2
			UNION ALL
			SELECT 'phone'::text AS kind, id, phone AS value, verified_at, is_primary, created_at
			FROM customer_phones WHERE workspace_id=$1 AND customer_id=$2
		) identities
		ORDER BY created_at DESC, id DESC
		LIMIT $3
	`, workspaceID, customerID, customer360QuerySize)
	if err != nil {
		return nil, fmt.Errorf("customer: load 360 identities: %w", err)
	}
	for identityRows.Next() {
		var item IdentityReference
		if err := identityRows.Scan(&item.Kind, &item.ID, &item.Value, &item.VerifiedAt, &item.IsPrimary, &item.CreatedAt); err != nil {
			identityRows.Close()
			return nil, fmt.Errorf("customer: scan 360 identity: %w", err)
		}
		result.Identities = append(result.Identities, item)
	}
	if err := identityRows.Err(); err != nil {
		identityRows.Close()
		return nil, fmt.Errorf("customer: read 360 identities: %w", err)
	}
	identityRows.Close()
	if len(result.Identities) > customer360PageSize {
		result.Identities = result.Identities[:customer360PageSize]
		result.IdentitiesTruncated = true
	}

	mergeRows, err := s.pool.Query(ctx, `
		SELECT id, winner_id, loser_id, moved_counts, reversible_until, reversed_at, created_at
		FROM identity_merge_history
		WHERE workspace_id=$1 AND (winner_id=$2 OR loser_id=$2)
		ORDER BY created_at DESC, id DESC
		LIMIT $3
	`, workspaceID, customerID, customer360QuerySize)
	if err != nil {
		return nil, fmt.Errorf("customer: load 360 merges: %w", err)
	}
	for mergeRows.Next() {
		var item MergeReference
		var raw []byte
		if err := mergeRows.Scan(&item.ID, &item.WinnerID, &item.LoserID, &raw, &item.ReversibleUntil, &item.ReversedAt, &item.CreatedAt); err != nil {
			mergeRows.Close()
			return nil, fmt.Errorf("customer: scan 360 merge: %w", err)
		}
		item.MovedCounts = map[string]any{}
		if len(raw) > 0 && json.Unmarshal(raw, &item.MovedCounts) != nil {
			item.MovedCounts = map[string]any{}
		}
		result.Merges = append(result.Merges, item)
	}
	if err := mergeRows.Err(); err != nil {
		mergeRows.Close()
		return nil, fmt.Errorf("customer: read 360 merges: %w", err)
	}
	mergeRows.Close()
	if len(result.Merges) > customer360PageSize {
		result.Merges = result.Merges[:customer360PageSize]
		result.MergesTruncated = true
	}

	return result, nil
}

func (s *Service) latestCustomerContextMetadata(ctx context.Context, workspaceID, customerID string) (map[string]any, error) {
	var payload map[string]any
	err := s.pool.QueryRow(ctx, `
		SELECT payload
		FROM customer_events
		WHERE workspace_id=$1 AND customer_id=$2 AND type='context.updated'
		ORDER BY occurred_at DESC, id DESC
		LIMIT 1
	`, workspaceID, customerID).Scan(&payload)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return sanitizeContextMetadata(payload), nil
}

func payloadString(payload map[string]any, key string) (string, bool) {
	value, ok := payload[key].(string)
	return value, ok && value != ""
}

func pageURLFromPayload(payload map[string]any) *string {
	if page, ok := payload["page"].(map[string]any); ok {
		if origin, ok := payloadString(page, "origin"); ok {
			path, _ := payloadString(page, "path")
			value := origin + path
			return NormalizeObservedURL(&value)
		}
	}
	if value, ok := payloadString(payload, "page_url"); ok {
		return NormalizeObservedURL(&value)
	}
	return nil
}

// NormalizeObservedURL keeps customer journey URLs useful while preventing
// query strings and fragments from becoming a second telemetry channel for
// credentials, email addresses, or other page-local state. It is shared by
// widget ingestion and the 360 projection so both sources display identically.
func NormalizeObservedURL(raw *string) *string {
	if raw == nil {
		return nil
	}
	parsed, err := url.Parse(strings.TrimSpace(*raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.ForceQuery = false
	parsed.RawPath = ""
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	value := parsed.String()
	return &value
}
