// Package savedview owns the persisted inbox and ticket filter views.
//
// A view is a named, workspace-scoped filter definition. Visibility is
// resolved server-side from the owner's membership and team membership; the
// browser never receives another member's personal view or an unrelated team
// view merely because it guessed an id.
package savedview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/jackc/pgx/v5"
)

var (
	ErrNotFound       = errors.New("saved view: not found")
	ErrInvalidName    = errors.New("saved view: name is required")
	ErrInvalidEntity  = errors.New("saved view: entity type must be conversation or ticket")
	ErrInvalidScope   = errors.New("saved view: scope must be personal, team, or workspace")
	ErrInvalidTarget  = errors.New("saved view: scope target is required")
	ErrNotOwner       = errors.New("saved view: only the owner may change this personal view")
	ErrInvalidFilters = errors.New("saved view: filters must be a JSON object")
)

type Service struct {
	pool  *database.Pool
	event *events.Log
	audit *audit.Log
}

type View struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspace_id"`
	Name        string         `json:"name"`
	Icon        *string        `json:"icon,omitempty"`
	EntityType  string         `json:"entity_type"`
	Scope       string         `json:"scope"`
	OwnerID     *string        `json:"owner_id,omitempty"`
	TeamID      *string        `json:"team_id,omitempty"`
	Filters     map[string]any `json:"filters"`
	Sort        map[string]any `json:"sort"`
	Position    int            `json:"position"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type Input struct {
	Name       string         `json:"name"`
	Icon       string         `json:"icon"`
	EntityType string         `json:"entity_type"`
	Scope      string         `json:"scope"`
	OwnerID    string         `json:"owner_id"`
	TeamID     string         `json:"team_id"`
	Filters    map[string]any `json:"filters"`
	Sort       map[string]any `json:"sort"`
	Position   int            `json:"position"`
}

func New(pool *database.Pool, eventLog *events.Log, auditLog *audit.Log) *Service {
	return &Service{pool: pool, event: eventLog, audit: auditLog}
}

// ListPage returns views visible to memberID. Team membership is resolved in
// the query so the authorization boundary cannot be bypassed by a forged
// team_id in a browser request.
func (s *Service) ListPage(ctx context.Context, workspaceID, memberID, role, entityType string, beforePosition *int, beforeID string, limit int) ([]View, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	args := []any{workspaceID, entityType, memberID}
	where := `workspace_id=$1 AND ($2='' OR entity_type=$2) AND (
		scope='workspace' OR
		(scope='personal' AND owner_id=$3) OR
		(scope='team' AND team_id IN (SELECT team_id FROM team_members WHERE member_id=$3))`
	if role == "owner" || role == "admin" {
		where += ` OR scope='team'`
	}
	where += `)`
	if beforePosition != nil {
		args = append(args, *beforePosition, beforeID)
		where += fmt.Sprintf(" AND (position,id)>($%d,$%d)", len(args)-1, len(args))
	}
	args = append(args, limit)
	query := `SELECT id,workspace_id,name,icon,entity_type,scope,owner_id,team_id,filters,sort,position,created_at,updated_at
		FROM saved_views WHERE ` + where + ` ORDER BY position,id LIMIT $` + fmt.Sprint(len(args))
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("saved view: list: %w", err)
	}
	defer rows.Close()
	result := make([]View, 0)
	for rows.Next() {
		item, scanErr := scan(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, *item)
	}
	return result, rows.Err()
}

func (s *Service) Get(ctx context.Context, workspaceID, memberID, role, id string) (*View, error) {
	item, err := scan(s.pool.QueryRow(ctx, `SELECT id,workspace_id,name,icon,entity_type,scope,owner_id,team_id,filters,sort,position,created_at,updated_at FROM saved_views WHERE workspace_id=$1 AND id=$2`, workspaceID, id))
	if err != nil {
		return nil, err
	}
	visible := item.Scope == "workspace" || role == "owner" || role == "admin"
	if item.Scope == "personal" && item.OwnerID != nil && *item.OwnerID == memberID {
		visible = true
	}
	if item.Scope == "team" && !visible {
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM team_members WHERE team_id=$1 AND member_id=$2)`, deref(item.TeamID), memberID).Scan(&visible); err != nil {
			return nil, err
		}
	}
	if !visible {
		return nil, ErrNotFound
	}
	return item, nil
}

func (s *Service) Create(ctx context.Context, workspaceID, memberID string, input Input) (*View, error) {
	input, err := normalizeInput(input, memberID)
	if err != nil {
		return nil, err
	}
	id := ids.New(ids.PrefixSavedView)
	filters, _ := json.Marshal(input.Filters)
	sort, _ := json.Marshal(input.Sort)
	var created *View
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if input.Scope == "team" {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM teams WHERE workspace_id=$1 AND id=$2)`, workspaceID, input.TeamID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return ErrInvalidTarget
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO saved_views(id,workspace_id,name,icon,entity_type,scope,owner_id,team_id,filters,sort,position)
			VALUES($1,$2,$3,NULLIF($4,''),$5,$6,NULLIF($7,''),NULLIF($8,''),$9::jsonb,$10::jsonb,$11)`,
			id, workspaceID, input.Name, input.Icon, input.EntityType, input.Scope, input.OwnerID, input.TeamID, filters, sort, input.Position); err != nil {
			return fmt.Errorf("saved view: insert: %w", err)
		}
		if s.audit != nil {
			if err := audit.RecordTx(ctx, tx, audit.Entry{WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: memberID, Action: "saved_view.created", EntityType: "saved_view", EntityID: id}); err != nil {
				return err
			}
		}
		if s.event != nil {
			if _, err := s.event.Append(ctx, tx, events.Event{WorkspaceID: workspaceID, Type: events.SavedViewCreated, EntityType: "saved_view", EntityID: id, ActorType: events.ActorUser, ActorID: memberID}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	created, err = s.Get(ctx, workspaceID, memberID, "owner", id)
	return created, err
}

func (s *Service) Update(ctx context.Context, workspaceID, memberID, role, id string, input Input) (*View, error) {
	current, err := s.Get(ctx, workspaceID, memberID, role, id)
	if err != nil {
		return nil, err
	}
	if current.Scope == "personal" && (current.OwnerID == nil || *current.OwnerID != memberID) {
		return nil, ErrNotOwner
	}
	input, err = normalizeInput(input, memberID)
	if err != nil {
		return nil, err
	}
	filters, _ := json.Marshal(input.Filters)
	sort, _ := json.Marshal(input.Sort)
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if input.Scope == "team" {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM teams WHERE workspace_id=$1 AND id=$2)`, workspaceID, input.TeamID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return ErrInvalidTarget
			}
		}
		result, execErr := tx.Exec(ctx, `UPDATE saved_views SET name=$3,icon=NULLIF($4,''),entity_type=$5,scope=$6,owner_id=NULLIF($7,''),team_id=NULLIF($8,''),filters=$9::jsonb,sort=$10::jsonb,position=$11,updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, id, input.Name, input.Icon, input.EntityType, input.Scope, input.OwnerID, input.TeamID, filters, sort, input.Position)
		if execErr != nil {
			return execErr
		}
		if result.RowsAffected() == 0 {
			return ErrNotFound
		}
		if s.audit != nil {
			if err := audit.RecordTx(ctx, tx, audit.Entry{WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: memberID, Action: "saved_view.updated", EntityType: "saved_view", EntityID: id}); err != nil {
				return err
			}
		}
		if s.event != nil {
			_, err := s.event.Append(ctx, tx, events.Event{WorkspaceID: workspaceID, Type: events.SavedViewUpdated, EntityType: "saved_view", EntityID: id, ActorType: events.ActorUser, ActorID: memberID})
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, workspaceID, memberID, role, id)
}

func (s *Service) Delete(ctx context.Context, workspaceID, memberID, role, id string) error {
	current, err := s.Get(ctx, workspaceID, memberID, role, id)
	if err != nil {
		return err
	}
	if current.Scope == "personal" && (current.OwnerID == nil || *current.OwnerID != memberID) {
		return ErrNotOwner
	}
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		result, err := tx.Exec(ctx, `DELETE FROM saved_views WHERE workspace_id=$1 AND id=$2`, workspaceID, id)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return ErrNotFound
		}
		if s.audit != nil {
			if err := audit.RecordTx(ctx, tx, audit.Entry{WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: memberID, Action: "saved_view.deleted", EntityType: "saved_view", EntityID: id}); err != nil {
				return err
			}
		}
		if s.event != nil {
			_, err := s.event.Append(ctx, tx, events.Event{WorkspaceID: workspaceID, Type: events.SavedViewDeleted, EntityType: "saved_view", EntityID: id, ActorType: events.ActorUser, ActorID: memberID})
			return err
		}
		return nil
	})
}

func normalizeInput(input Input, memberID string) (Input, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return Input{}, ErrInvalidName
	}
	if input.EntityType == "" {
		input.EntityType = "conversation"
	}
	if input.EntityType != "conversation" && input.EntityType != "ticket" {
		return Input{}, ErrInvalidEntity
	}
	if input.Scope == "" {
		input.Scope = "personal"
	}
	if input.Scope != "personal" && input.Scope != "team" && input.Scope != "workspace" {
		return Input{}, ErrInvalidScope
	}
	if input.Scope == "personal" {
		input.OwnerID = memberID
		input.TeamID = ""
	} else if input.Scope == "workspace" {
		input.OwnerID = ""
		input.TeamID = ""
	} else if strings.TrimSpace(input.TeamID) == "" {
		return Input{}, ErrInvalidTarget
	}
	if input.Filters == nil {
		input.Filters = map[string]any{}
	}
	if input.Sort == nil {
		input.Sort = map[string]any{"field": "last_message_at", "direction": "desc"}
	}
	return input, nil
}

func scan(row interface{ Scan(...any) error }) (*View, error) {
	var item View
	var filters, sort []byte
	if err := row.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Icon, &item.EntityType, &item.Scope, &item.OwnerID, &item.TeamID, &filters, &sort, &item.Position, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := json.Unmarshal(filters, &item.Filters); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(sort, &item.Sort); err != nil {
		return nil, err
	}
	if item.Filters == nil {
		item.Filters = map[string]any{}
	}
	if item.Sort == nil {
		item.Sort = map[string]any{}
	}
	return &item, nil
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
