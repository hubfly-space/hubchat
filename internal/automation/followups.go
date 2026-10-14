package automation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/ids"
)

var ErrInvalidFollowUp = errors.New("automation: a follow-up needs a note and a future time within one year")

type FollowUp struct {
	ID           string    `json:"id"`
	Body         string    `json:"body"`
	ScheduledFor time.Time `json:"scheduled_for"`
	State        string    `json:"state"`
}

func (s *Service) ScheduleFollowUp(ctx context.Context, workspaceID, memberID, conversationID, body string, at time.Time) (*FollowUp, error) {
	body = strings.TrimSpace(body)
	if body == "" || len(body) > 5000 || !at.After(time.Now()) || at.After(time.Now().Add(366*24*time.Hour)) {
		return nil, ErrInvalidFollowUp
	}
	if _, err := s.conversation.Get(ctx, workspaceID, conversationID); err != nil {
		return nil, err
	}
	id := ids.New(ids.PrefixScheduledAction)
	payload, _ := json.Marshal(map[string]string{"body": body})
	if _, err := s.pool.Exec(ctx, `INSERT INTO scheduled_actions(id,workspace_id,kind,subject_type,subject_id,payload,scheduled_for,created_by) VALUES($1,$2,'follow_up','conversation',$3,$4,$5,$6)`, id, workspaceID, conversationID, payload, at, memberID); err != nil {
		return nil, err
	}
	return &FollowUp{ID: id, Body: body, ScheduledFor: at, State: "pending"}, nil
}
func (s *Service) FollowUps(ctx context.Context, workspaceID, memberID, conversationID string) ([]FollowUp, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,payload->>'body',scheduled_for,state FROM scheduled_actions WHERE workspace_id=$1 AND created_by=$2 AND subject_type='conversation' AND subject_id=$3 AND kind='follow_up' AND state IN ('pending','running') ORDER BY scheduled_for,id LIMIT 100`, workspaceID, memberID, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FollowUp{}
	for rows.Next() {
		var item FollowUp
		if err := rows.Scan(&item.ID, &item.Body, &item.ScheduledFor, &item.State); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func (s *Service) CancelFollowUp(ctx context.Context, workspaceID, memberID, conversationID, id string) error {
	result, err := s.pool.Exec(ctx, `UPDATE scheduled_actions SET state='cancelled',cancelled_at=now() WHERE workspace_id=$1 AND created_by=$2 AND subject_id=$3 AND id=$4 AND kind='follow_up' AND state='pending'`, workspaceID, memberID, conversationID, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
