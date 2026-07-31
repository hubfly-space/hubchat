package notification

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/ids"
)

var ErrNotFound = errors.New("notification: not found")

type Service struct{ pool *database.Pool }

type Notification struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspace_id"`
	MemberID    string     `json:"member_id"`
	Type        string     `json:"type"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	EntityType  *string    `json:"entity_type"`
	EntityID    *string    `json:"entity_id"`
	URL         *string    `json:"url"`
	ReadAt      *time.Time `json:"read_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

type ListFilter struct {
	Before   time.Time
	BeforeID string
	Limit    int
	Unread   bool
}

func New(pool *database.Pool) *Service { return &Service{pool: pool} }

func (s *Service) List(ctx context.Context, workspaceID, memberID string, filter ListFilter) ([]Notification, error) {
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	if filter.Before.IsZero() {
		filter.Before = time.Now().Add(time.Hour)
	}
	where := `workspace_id = $1 AND member_id = $2 AND (created_at, id) < ($3, $4)`
	if filter.Unread {
		where += ` AND read_at IS NULL`
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, workspace_id, member_id, type, title, body, entity_type, entity_id, url, read_at, created_at
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
			&item.EntityType, &item.EntityID, &item.URL, &item.ReadAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
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
		SELECT DISTINCT member_id FROM (
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
		WHERE member_id IS NOT NULL AND ($3 = '' OR member_id <> $3)
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
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO notifications
				(id, workspace_id, member_id, type, title, body, entity_type, entity_id, url)
			SELECT $1, $2, $3, 'customer_reply', 'New customer reply', $4, 'message', $5, $6
			WHERE NOT EXISTS (
				SELECT 1 FROM notifications
				WHERE workspace_id = $2 AND member_id = $3
				  AND type = 'customer_reply' AND entity_type = 'message' AND entity_id = $7
			)
		`, ids.New(ids.PrefixNotification), workspaceID, memberID, preview, messageID, url, messageID); err != nil {
			return fmt.Errorf("notification: insert: %w", err)
		}
	}
	return rows.Err()
}
