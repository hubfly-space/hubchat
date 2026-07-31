package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/ids"
	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidScope    = errors.New("automation: invalid scope")
	ErrInvalidTarget   = errors.New("automation: scope target is required")
	ErrInvalidShortcut = errors.New("automation: shortcut must start with a semicolon")
)

type Macro struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Folder      string    `json:"folder,omitempty"`
	Scope       string    `json:"scope"`
	OwnerID     *string   `json:"owner_id,omitempty"`
	TeamID      *string   `json:"team_id,omitempty"`
	Body        string    `json:"body"`
	Actions     []Action  `json:"actions"`
	UsageCount  int       `json:"usage_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type MacroInput struct {
	Name    string   `json:"name"`
	Folder  string   `json:"folder"`
	Scope   string   `json:"scope"`
	OwnerID string   `json:"owner_id"`
	TeamID  string   `json:"team_id"`
	Body    string   `json:"body"`
	Actions []Action `json:"actions"`
}

type SavedReply struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Shortcut    string    `json:"shortcut,omitempty"`
	Folder      string    `json:"folder,omitempty"`
	Scope       string    `json:"scope"`
	OwnerID     *string   `json:"owner_id,omitempty"`
	TeamID      *string   `json:"team_id,omitempty"`
	Body        string    `json:"body"`
	UsageCount  int       `json:"usage_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type SavedReplyInput struct {
	Name     string `json:"name"`
	Shortcut string `json:"shortcut"`
	Folder   string `json:"folder"`
	Scope    string `json:"scope"`
	OwnerID  string `json:"owner_id"`
	TeamID   string `json:"team_id"`
	Body     string `json:"body"`
}

func (s *Service) ListMacros(ctx context.Context, workspaceID, query string) ([]Macro, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,workspace_id,name,coalesce(folder,''),scope,owner_id,team_id,body,actions,usage_count,created_at,updated_at FROM macros WHERE workspace_id=$1 AND ($2='' OR name ILIKE '%'||$2||'%' OR body ILIKE '%'||$2||'%') ORDER BY name,id`, workspaceID, strings.TrimSpace(query))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Macro, 0)
	for rows.Next() {
		item, err := scanMacro(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *item)
	}
	return result, rows.Err()
}

func (s *Service) CreateMacro(ctx context.Context, workspaceID, memberID string, input MacroInput) (*Macro, error) {
	if input.Scope == "" {
		input.Scope = "workspace"
	}
	if input.Scope == "personal" {
		input.OwnerID = memberID
	}
	if err := validateContent(input.Name, input.Scope, input.OwnerID, input.TeamID, input.Actions); err != nil {
		return nil, err
	}
	if input.Scope == "team" && !s.teamExists(ctx, workspaceID, input.TeamID) {
		return nil, ErrInvalidTarget
	}
	owner, team := nullableTarget(input.Scope, input.OwnerID, input.TeamID, memberID)
	actionsJSON, _ := json.Marshal(input.Actions)
	id := ids.New(ids.PrefixMacro)
	_, err := s.pool.Exec(ctx, `INSERT INTO macros(id,workspace_id,name,folder,scope,owner_id,team_id,body,actions) VALUES($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8,$9::jsonb)`, id, workspaceID, strings.TrimSpace(input.Name), strings.TrimSpace(input.Folder), input.Scope, owner, team, input.Body, actionsJSON)
	if err != nil {
		return nil, fmt.Errorf("automation: create macro: %w", err)
	}
	return s.GetMacro(ctx, workspaceID, id)
}

func (s *Service) GetMacro(ctx context.Context, workspaceID, id string) (*Macro, error) {
	item, err := scanMacro(s.pool.QueryRow(ctx, `SELECT id,workspace_id,name,coalesce(folder,''),scope,owner_id,team_id,body,actions,usage_count,created_at,updated_at FROM macros WHERE workspace_id=$1 AND id=$2`, workspaceID, id))
	if errors.Is(err, ErrNotFound) {
		return nil, ErrNotFound
	}
	return item, err
}
func (s *Service) UseMacro(ctx context.Context, workspaceID, id string) error {
	result, err := s.pool.Exec(ctx, `UPDATE macros SET usage_count=usage_count+1,updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) ListSavedReplies(ctx context.Context, workspaceID, query string) ([]SavedReply, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,workspace_id,name,coalesce(shortcut::text,''),coalesce(folder,''),scope,owner_id,team_id,body,usage_count,created_at,updated_at FROM saved_replies WHERE workspace_id=$1 AND ($2='' OR name ILIKE '%'||$2||'%' OR body ILIKE '%'||$2||'%' OR shortcut::text ILIKE '%'||$2||'%') ORDER BY name,id`, workspaceID, strings.TrimSpace(query))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]SavedReply, 0)
	for rows.Next() {
		item, err := scanSavedReply(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *item)
	}
	return result, rows.Err()
}

func (s *Service) CreateSavedReply(ctx context.Context, workspaceID, memberID string, input SavedReplyInput) (*SavedReply, error) {
	if input.Scope == "" {
		input.Scope = "workspace"
	}
	if input.Scope == "personal" {
		input.OwnerID = memberID
	}
	if strings.TrimSpace(input.Shortcut) != "" && !strings.HasPrefix(strings.TrimSpace(input.Shortcut), ";") {
		return nil, ErrInvalidShortcut
	}
	if err := validateContent(input.Name, input.Scope, input.OwnerID, input.TeamID, nil); err != nil {
		return nil, err
	}
	if input.Scope == "team" && !s.teamExists(ctx, workspaceID, input.TeamID) {
		return nil, ErrInvalidTarget
	}
	owner, team := nullableTarget(input.Scope, input.OwnerID, input.TeamID, memberID)
	id := ids.New(ids.PrefixSavedReply)
	_, err := s.pool.Exec(ctx, `INSERT INTO saved_replies(id,workspace_id,name,shortcut,folder,scope,owner_id,team_id,body) VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,$7,$8,$9)`, id, workspaceID, strings.TrimSpace(input.Name), strings.TrimSpace(input.Shortcut), strings.TrimSpace(input.Folder), input.Scope, owner, team, strings.TrimSpace(input.Body))
	if err != nil {
		return nil, fmt.Errorf("automation: create saved reply: %w", err)
	}
	return s.GetSavedReply(ctx, workspaceID, id)
}
func (s *Service) GetSavedReply(ctx context.Context, workspaceID, id string) (*SavedReply, error) {
	item, err := scanSavedReply(s.pool.QueryRow(ctx, `SELECT id,workspace_id,name,coalesce(shortcut::text,''),coalesce(folder,''),scope,owner_id,team_id,body,usage_count,created_at,updated_at FROM saved_replies WHERE workspace_id=$1 AND id=$2`, workspaceID, id))
	return item, err
}
func (s *Service) UseSavedReply(ctx context.Context, workspaceID, id string) error {
	result, err := s.pool.Exec(ctx, `UPDATE saved_replies SET usage_count=usage_count+1,updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func validateContent(name, scope, ownerID, teamID string, contentActions []Action) error {
	if strings.TrimSpace(name) == "" {
		return ErrInvalidName
	}
	if scope == "" {
		scope = "workspace"
	}
	if scope != "personal" && scope != "team" && scope != "workspace" {
		return ErrInvalidScope
	}
	if scope == "personal" && ownerID == "" {
		return ErrInvalidTarget
	}
	if scope == "team" && teamID == "" {
		return ErrInvalidTarget
	}
	if contentActions != nil {
		return validateActions(contentActions)
	}
	return nil
}
func nullableTarget(scope, ownerID, teamID, memberID string) (any, any) {
	if scope == "personal" {
		if ownerID == "" {
			ownerID = memberID
		}
		return ownerID, nil
	}
	if scope == "team" {
		return nil, teamID
	}
	return nil, nil
}
func (s *Service) teamExists(ctx context.Context, workspaceID, teamID string) bool {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM teams WHERE workspace_id=$1 AND id=$2)`, workspaceID, teamID).Scan(&exists); err != nil {
		return false
	}
	return exists
}
func scanMacro(row interface{ Scan(...any) error }) (*Macro, error) {
	var item Macro
	var actions []byte
	if err := row.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Folder, &item.Scope, &item.OwnerID, &item.TeamID, &item.Body, &actions, &item.UsageCount, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal(actions, &item.Actions)
	return &item, nil
}
func scanSavedReply(row interface{ Scan(...any) error }) (*SavedReply, error) {
	var item SavedReply
	if err := row.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Shortcut, &item.Folder, &item.Scope, &item.OwnerID, &item.TeamID, &item.Body, &item.UsageCount, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &item, nil
}
