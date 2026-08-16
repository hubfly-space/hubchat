package customer

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// VisitorContext is the safe, workspace-scoped context available before an
// anonymous visitor identifies themselves. It deliberately contains no
// message bodies or raw event payloads; those remain behind their owning
// conversation/timeline permissions.
type VisitorContext struct {
	VisitorID            string               `json:"visitor_id"`
	CustomerID           *string              `json:"customer_id,omitempty"`
	Presence             string               `json:"presence"`
	FirstSeenAt          time.Time            `json:"first_seen_at"`
	LastSeenAt           time.Time            `json:"last_seen_at"`
	CurrentPage          *PageVisitReference  `json:"current_page,omitempty"`
	PageJourney          []PageVisitReference `json:"page_journey"`
	PageJourneyTruncated bool                 `json:"page_journey_truncated"`
	Device               *DeviceReference     `json:"device,omitempty"`
	Session              *SessionReference    `json:"session,omitempty"`
	ContextMetadata      map[string]any       `json:"context_metadata,omitempty"`
}

// VisitorContext returns only the public-widget context belonging to this
// visitor. The workspace predicate is required even though visitor ids are
// opaque, because ids are not an authorization mechanism.
func (s *Service) VisitorContext(ctx context.Context, workspaceID, visitorID string) (*VisitorContext, error) {
	result := VisitorContext{PageJourney: []PageVisitReference{}}
	if err := s.pool.QueryRow(ctx, `
		SELECT id, customer_id, first_seen_at, last_seen_at
		FROM visitors
		WHERE workspace_id=$1 AND id=$2
	`, workspaceID, visitorID).Scan(&result.VisitorID, &result.CustomerID, &result.FirstSeenAt, &result.LastSeenAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	result.Presence = "offline"
	if session, err := s.latestVisitorSession(ctx, workspaceID, visitorID); err != nil {
		return nil, err
	} else if session != nil {
		result.Session = session
		if session.EndedAt == nil && time.Since(session.LastSeenAt) <= 2*time.Minute {
			result.Presence = "online"
		}
		result.Device = deviceFromSession(session)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, payload, occurred_at
		FROM customer_events
		WHERE workspace_id=$1 AND visitor_id=$2 AND type='page.viewed'
		ORDER BY occurred_at DESC, id DESC
		LIMIT 11
	`, workspaceID, visitorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var payload map[string]any
		var occurredAt time.Time
		if err := rows.Scan(&id, &payload, &occurredAt); err != nil {
			return nil, err
		}
		page := pageVisitFromPayload(id, payload, occurredAt)
		result.PageJourney = append(result.PageJourney, page)
		if result.Device == nil {
			result.Device = &DeviceReference{}
		}
		mergeDeviceFromPayload(result.Device, payload, page)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(result.PageJourney) > 10 {
		result.PageJourney = result.PageJourney[:10]
		result.PageJourneyTruncated = true
	}
	if len(result.PageJourney) > 0 {
		result.CurrentPage = &result.PageJourney[0]
	} else if result.Session != nil && result.Session.CurrentURL != nil {
		result.CurrentPage = &PageVisitReference{
			ID:             result.Session.ID,
			URL:            result.Session.CurrentURL,
			Title:          stringPtrValue(result.Session.CurrentTitle),
			Device:         stringPtrValue(result.Session.Device),
			Browser:        stringPtrValue(result.Session.Browser),
			OS:             stringPtrValue(result.Session.OS),
			ReferrerOrigin: stringPtrValue(result.Session.Referrer),
			Platform:       stringPtrValue(result.Session.Platform),
			UserAgent:      stringPtrValue(result.Session.UserAgent),
			Viewport:       result.Session.Viewport,
			OccurredAt:     result.Session.LastSeenAt,
		}
	}
	result.ContextMetadata, err = s.latestContextMetadata(ctx, workspaceID, visitorID)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Service) latestContextMetadata(ctx context.Context, workspaceID, visitorID string) (map[string]any, error) {
	var payload map[string]any
	err := s.pool.QueryRow(ctx, `
		SELECT payload
		FROM customer_events
		WHERE workspace_id=$1 AND visitor_id=$2 AND type='context.updated'
		ORDER BY occurred_at DESC, id DESC
		LIMIT 1
	`, workspaceID, visitorID).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return sanitizeContextMetadata(payload), nil
}

func (s *Service) latestVisitorSession(ctx context.Context, workspaceID, visitorID string) (*SessionReference, error) {
	var item SessionReference
	err := s.pool.QueryRow(ctx, `
		SELECT id, ip_prefix, ip_country, device, browser, os, referrer, landing_url, current_url, current_title, language, timezone, platform, user_agent, viewport, page_views, started_at, last_seen_at, ended_at
		FROM contact_sessions
		WHERE workspace_id=$1 AND visitor_id=$2
		ORDER BY last_seen_at DESC, id DESC
		LIMIT 1
	`, workspaceID, visitorID).Scan(&item.ID, &item.IPPrefix, &item.IPCountry, &item.Device, &item.Browser, &item.OS, &item.Referrer, &item.LandingURL, &item.CurrentURL, &item.CurrentTitle, &item.Language, &item.Timezone, &item.Platform, &item.UserAgent, &item.Viewport, &item.PageViews, &item.StartedAt, &item.LastSeenAt, &item.EndedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func deviceFromSession(session *SessionReference) *DeviceReference {
	if session == nil {
		return nil
	}
	device := &DeviceReference{
		Device:         stringPtrValue(session.Device),
		Browser:        stringPtrValue(session.Browser),
		OS:             stringPtrValue(session.OS),
		Language:       stringPtrValue(session.Language),
		Timezone:       stringPtrValue(session.Timezone),
		Platform:       stringPtrValue(session.Platform),
		UserAgent:      stringPtrValue(session.UserAgent),
		ReferrerOrigin: stringPtrValue(session.Referrer),
		Viewport:       session.Viewport,
	}
	if device.Device == "" && device.Browser == "" && device.OS == "" && device.Language == "" && device.Timezone == "" && device.Platform == "" && device.UserAgent == "" && device.Viewport == nil {
		return nil
	}
	return device
}

func mergeDeviceFromSession(device *DeviceReference, session *SessionReference) {
	if device == nil || session == nil {
		return
	}
	if device.Device == "" {
		device.Device = stringPtrValue(session.Device)
	}
	if device.Browser == "" {
		device.Browser = stringPtrValue(session.Browser)
	}
	if device.OS == "" {
		device.OS = stringPtrValue(session.OS)
	}
	if device.Language == "" {
		device.Language = stringPtrValue(session.Language)
	}
	if device.Timezone == "" {
		device.Timezone = stringPtrValue(session.Timezone)
	}
	if device.Platform == "" {
		device.Platform = stringPtrValue(session.Platform)
	}
	if device.UserAgent == "" {
		device.UserAgent = stringPtrValue(session.UserAgent)
	}
	if device.ReferrerOrigin == "" {
		device.ReferrerOrigin = stringPtrValue(session.Referrer)
	}
	if device.Viewport == nil {
		device.Viewport = session.Viewport
	}
}

func pageVisitFromPayload(id string, payload map[string]any, occurredAt time.Time) PageVisitReference {
	page := PageVisitReference{ID: id, OccurredAt: occurredAt}
	page.URL = pageURLFromPayload(payload)
	page.Title, _ = payloadString(payload, "title")
	page.Device, _ = payloadString(payload, "device")
	page.Browser, _ = payloadString(payload, "browser")
	page.OS, _ = payloadString(payload, "os")
	page.Platform, _ = payloadString(payload, "platform")
	page.ReferrerOrigin, _ = payloadString(payload, "referrer_origin")
	page.UserAgent, _ = payloadString(payload, "user_agent")
	page.Viewport = viewportFromPayload(payload)
	return page
}

func mergeDeviceFromPayload(device *DeviceReference, payload map[string]any, page PageVisitReference) {
	if device.Language == "" {
		device.Language, _ = payloadString(payload, "language")
	}
	if device.Timezone == "" {
		device.Timezone, _ = payloadString(payload, "timezone")
	}
	if device.Platform == "" {
		device.Platform, _ = payloadString(payload, "platform")
	}
	if device.Device == "" {
		device.Device = page.Device
	}
	if device.Browser == "" {
		device.Browser = page.Browser
	}
	if device.OS == "" {
		device.OS = page.OS
	}
	if device.ReferrerOrigin == "" {
		device.ReferrerOrigin = page.ReferrerOrigin
	}
	if device.UserAgent == "" {
		device.UserAgent = page.UserAgent
	}
	if device.Viewport == nil {
		device.Viewport = page.Viewport
	}
}

func viewportFromPayload(payload map[string]any) *ViewportReference {
	value, ok := payload["viewport"].(map[string]any)
	if !ok {
		return nil
	}
	viewport := &ViewportReference{
		Width:            intFromPayload(value, "width"),
		Height:           intFromPayload(value, "height"),
		DevicePixelRatio: floatFromPayload(value, "device_pixel_ratio"),
	}
	if viewport.Width == 0 && viewport.Height == 0 && viewport.DevicePixelRatio == 0 {
		return nil
	}
	return viewport
}

func intFromPayload(payload map[string]any, key string) int {
	value, ok := payload[key].(float64)
	if !ok || value < 0 || value > 10000 {
		return 0
	}
	return int(value)
}

func floatFromPayload(payload map[string]any, key string) float64 {
	value, ok := payload[key].(float64)
	if !ok || value < 0 || value > 100 {
		return 0
	}
	return value
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

const (
	maxContextMetadataKeys   = 32
	maxContextMetadataDepth  = 3
	maxContextMetadataString = 256
)

// sanitizeContextMetadata keeps the compact context panel useful without
// turning it into an unbounded raw-event viewer. SDK context is application
// supplied, so common credential-like keys are always omitted and nested
// values are bounded before they cross the dashboard API.
func sanitizeContextMetadata(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	keys := make([]string, 0, len(input))
	for key := range input {
		if key != "" && len(key) <= 64 && !sensitiveContextKey(key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) > maxContextMetadataKeys {
		keys = keys[:maxContextMetadataKeys]
	}
	out := make(map[string]any, len(keys))
	for _, key := range keys {
		if value, ok := boundedContextValue(input[key], maxContextMetadataDepth); ok {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sensitiveContextKey(key string) bool {
	normalized := strings.ToLower(strings.NewReplacer("-", "_", " ", "_").Replace(key))
	for _, part := range []string{"password", "secret", "token", "authorization", "cookie", "credential", "private_key", "api_key"} {
		if strings.Contains(normalized, part) {
			return true
		}
	}
	return false
}

func boundedContextValue(value any, depth int) (any, bool) {
	switch typed := value.(type) {
	case nil:
		return nil, true
	case string:
		if len(typed) > maxContextMetadataString {
			return typed[:maxContextMetadataString] + "…", true
		}
		return typed, true
	case bool, float64, int, int64:
		return typed, true
	case map[string]any:
		if depth <= 0 {
			return "[nested object]", true
		}
		return sanitizeContextMetadataDepth(typed, depth-1), true
	case []any:
		if depth <= 0 {
			return "[nested list]", true
		}
		out := make([]any, 0, minInt(len(typed), 16))
		for _, item := range typed {
			if len(out) == 16 {
				break
			}
			if bounded, ok := boundedContextValue(item, depth-1); ok {
				out = append(out, bounded)
			}
		}
		return out, true
	default:
		return fmt.Sprint(typed), true
	}
}

func sanitizeContextMetadataDepth(input map[string]any, depth int) map[string]any {
	keys := make([]string, 0, minInt(len(input), 16))
	for key := range input {
		if key != "" && len(key) <= 64 && !sensitiveContextKey(key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) > 16 {
		keys = keys[:16]
	}
	out := make(map[string]any, len(keys))
	for _, key := range keys {
		if value, ok := boundedContextValue(input[key], depth); ok {
			out[key] = value
		}
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
