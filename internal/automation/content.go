package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidScope        = errors.New("automation: invalid scope")
	ErrInvalidTarget       = errors.New("automation: scope target is required")
	ErrInvalidShortcut     = errors.New("automation: shortcut must start with a semicolon")
	ErrMacroForbidden      = errors.New("automation: macro is not available to this member")
	ErrMacroCapability     = errors.New("automation: macro action requires a capability")
	ErrMacroSubject        = errors.New("automation: macro subject must be a conversation or ticket")
	ErrSavedReplyForbidden = errors.New("automation: saved reply is not available to this member")
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

// MacroExecutionRequest makes the target of a human-triggered macro explicit.
// A macro must never infer a subject from browser state or a stale list row.
type MacroExecutionRequest struct {
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`
}

type MacroExecution struct {
	MacroID        string   `json:"macro_id"`
	SubjectType    string   `json:"subject_type"`
	SubjectID      string   `json:"subject_id"`
	ActionsApplied []Action `json:"actions_applied"`
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
	return s.ListMacrosPage(ctx, workspaceID, query, "", "", 0)
}

func (s *Service) ListMacrosPage(ctx context.Context, workspaceID, query, beforeName, beforeID string, limit int) ([]Macro, error) {
	return s.listMacrosPage(ctx, workspaceID, "", query, beforeName, beforeID, limit)
}

// ListMacrosForMemberPage hides personal macros from other members and team
// macros from members who are not on that team. The unfiltered compatibility
// method above remains useful to internal maintenance callers; browser routes
// use this member-scoped method.
func (s *Service) ListMacrosForMemberPage(ctx context.Context, workspaceID, memberID, query, beforeName, beforeID string, limit int) ([]Macro, error) {
	return s.listMacrosPage(ctx, workspaceID, memberID, query, beforeName, beforeID, limit)
}

func (s *Service) listMacrosPage(ctx context.Context, workspaceID, memberID, query, beforeName, beforeID string, limit int) ([]Macro, error) {
	if limit <= 0 || limit > 201 {
		limit = 100
	}
	sql := `SELECT id,workspace_id,name,coalesce(folder,''),scope,owner_id,team_id,body,actions,usage_count,created_at,updated_at FROM macros WHERE workspace_id=$1`
	args := []any{workspaceID}
	if memberID != "" {
		sql += ` AND (scope='workspace' OR (scope='personal' AND owner_id=$2) OR (scope='team' AND EXISTS (SELECT 1 FROM team_members tm WHERE tm.team_id=macros.team_id AND tm.member_id=$2)))`
		args = append(args, memberID)
	}
	queryArg := len(args) + 1
	sql += fmt.Sprintf(` AND ($%d='' OR name ILIKE '%%'||$%d||'%%' OR body ILIKE '%%'||$%d||'%%')`, queryArg, queryArg, queryArg)
	args = append(args, strings.TrimSpace(query))
	if beforeName != "" {
		beforeNameArg := len(args) + 1
		beforeIDArg := beforeNameArg + 1
		sql += fmt.Sprintf(` AND (name,id) > ($%d,$%d)`, beforeNameArg, beforeIDArg)
		args = append(args, beforeName, beforeID)
	}
	sql += ` ORDER BY name,id LIMIT $` + fmt.Sprint(len(args)+1)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, sql, args...)
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

func (s *Service) GetMacroForMember(ctx context.Context, workspaceID, memberID, id string) (*Macro, error) {
	item, err := s.GetMacro(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	visible, err := s.macroVisibleToMember(ctx, workspaceID, memberID, *item)
	if err != nil {
		return nil, err
	}
	if !visible {
		return nil, ErrMacroForbidden
	}
	return item, nil
}

func (s *Service) UpdateMacro(ctx context.Context, workspaceID, memberID, id string, input MacroInput) (*Macro, error) {
	current, err := s.GetMacro(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	visible, err := s.macroVisibleToMember(ctx, workspaceID, memberID, *current)
	if err != nil {
		return nil, err
	}
	if !visible {
		return nil, ErrMacroForbidden
	}
	if input.Scope == "" {
		input.Scope = current.Scope
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
	_, err = s.pool.Exec(ctx, `UPDATE macros SET name=$3,folder=NULLIF($4,''),scope=$5,owner_id=$6,team_id=$7,body=$8,actions=$9::jsonb,updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, id, strings.TrimSpace(input.Name), strings.TrimSpace(input.Folder), input.Scope, owner, team, strings.TrimSpace(input.Body), actionsJSON)
	if err != nil {
		return nil, fmt.Errorf("automation: update macro: %w", err)
	}
	return s.GetMacro(ctx, workspaceID, id)
}

func (s *Service) DeleteMacro(ctx context.Context, workspaceID, memberID, id string) error {
	current, err := s.GetMacro(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	visible, err := s.macroVisibleToMember(ctx, workspaceID, memberID, *current)
	if err != nil {
		return err
	}
	if !visible {
		return ErrMacroForbidden
	}
	result, err := s.pool.Exec(ctx, `DELETE FROM macros WHERE workspace_id=$1 AND id=$2`, workspaceID, id)
	if err != nil {
		return fmt.Errorf("automation: delete macro: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ExecuteMacro applies a human-triggered macro through the same deterministic
// action engine used by rules. Capability validation happens for the complete
// action list before the first action runs, so a macro cannot partially mutate
// a subject merely because the actor lacked permission for a later action.
func (s *Service) ExecuteMacro(ctx context.Context, workspaceID, memberID, id string, actor *authorization.Actor, request MacroExecutionRequest) (*MacroExecution, error) {
	if actor == nil || actor.WorkspaceID != workspaceID || actor.MemberID != memberID || !actor.Can(authorization.AutomationManage) {
		return nil, ErrMacroForbidden
	}
	if request.SubjectType != "conversation" && request.SubjectType != "ticket" || strings.TrimSpace(request.SubjectID) == "" {
		return nil, ErrMacroSubject
	}

	macro, err := s.GetMacro(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	visible, err := s.macroVisibleToMember(ctx, workspaceID, memberID, *macro)
	if err != nil {
		return nil, err
	}
	if !visible {
		return nil, ErrMacroForbidden
	}

	values := append([]Action(nil), macro.Actions...)
	if strings.TrimSpace(macro.Body) != "" {
		if request.SubjectType != "conversation" {
			return nil, fmt.Errorf("%w: macro reply text requires a conversation", ErrMacroSubject)
		}
		values = append([]Action{{ID: "macro_body", Type: "send_message", Params: map[string]any{"body": macro.Body}}}, values...)
	}
	if err := validateActions(values); err != nil {
		return nil, err
	}
	if err := validateMacroCapabilities(actor, request.SubjectType, values); err != nil {
		return nil, err
	}

	applied, err := s.applyActions(ctx, workspaceID, request.SubjectType, request.SubjectID, values, memberID)
	if err != nil {
		return nil, err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE macros SET usage_count=usage_count+1,updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, id); err != nil {
		return nil, err
	}
	return &MacroExecution{MacroID: macro.ID, SubjectType: request.SubjectType, SubjectID: request.SubjectID, ActionsApplied: applied}, nil
}

func (s *Service) macroVisibleToMember(ctx context.Context, workspaceID, memberID string, macro Macro) (bool, error) {
	switch macro.Scope {
	case "workspace":
		return true, nil
	case "personal":
		return macro.OwnerID != nil && *macro.OwnerID == memberID, nil
	case "team":
		if macro.TeamID == nil || *macro.TeamID == "" {
			return false, nil
		}
		var exists bool
		err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM team_members tm JOIN teams t ON t.id=tm.team_id WHERE t.workspace_id=$1 AND tm.team_id=$2 AND tm.member_id=$3)`, workspaceID, *macro.TeamID, memberID).Scan(&exists)
		return exists, err
	default:
		return false, nil
	}
}

func validateMacroCapabilities(actor *authorization.Actor, subjectType string, values []Action) error {
	seen := make(map[authorization.Capability]bool)
	for _, action := range values {
		capability, ok := macroActionCapability(action.Type, subjectType)
		if !ok {
			return fmt.Errorf("%w: action %s", ErrMacroCapability, action.Type)
		}
		if seen[capability] {
			continue
		}
		seen[capability] = true
		if !actor.Can(capability) {
			return fmt.Errorf("%w: %s", ErrMacroCapability, capability)
		}
	}
	return nil
}

func macroActionCapability(actionType, subjectType string) (authorization.Capability, bool) {
	switch actionType {
	case "assign_member", "assign_team", "move_inbox", "set_priority", "set_state", "add_tag", "remove_tag":
		if subjectType == "conversation" {
			return authorization.ConversationAssign, true
		}
		return authorization.TicketManage, true
	case "send_message":
		if subjectType != "conversation" {
			return "", false
		}
		return authorization.ConversationReply, true
	case "set_field":
		return authorization.TicketManage, subjectType == "ticket"
	case "send_email", "invoke_webhook":
		return authorization.IntegrationManage, true
	case "start_sla", "pause_sla":
		return authorization.SLAManage, true
	case "close_after_inactivity", "create_task":
		return authorization.AutomationManage, true
	default:
		return "", false
	}
}

func (s *Service) ListSavedReplies(ctx context.Context, workspaceID, query string) ([]SavedReply, error) {
	return s.ListSavedRepliesPage(ctx, workspaceID, query, "", "", 0)
}

func (s *Service) ListSavedRepliesPage(ctx context.Context, workspaceID, query, beforeName, beforeID string, limit int) ([]SavedReply, error) {
	return s.listSavedRepliesPage(ctx, workspaceID, "", query, beforeName, beforeID, limit)
}

func (s *Service) ListSavedRepliesForMemberPage(ctx context.Context, workspaceID, memberID, query, beforeName, beforeID string, limit int) ([]SavedReply, error) {
	return s.listSavedRepliesPage(ctx, workspaceID, memberID, query, beforeName, beforeID, limit)
}

func (s *Service) listSavedRepliesPage(ctx context.Context, workspaceID, memberID, query, beforeName, beforeID string, limit int) ([]SavedReply, error) {
	if limit <= 0 || limit > 201 {
		limit = 100
	}
	sql := `SELECT id,workspace_id,name,coalesce(shortcut::text,''),coalesce(folder,''),scope,owner_id,team_id,body,usage_count,created_at,updated_at FROM saved_replies WHERE workspace_id=$1 AND ($2='' OR name ILIKE '%'||$2||'%' OR body ILIKE '%'||$2||'%' OR shortcut::text ILIKE '%'||$2||'%')`
	args := []any{workspaceID, strings.TrimSpace(query)}
	if memberID != "" {
		sql += ` AND (scope='workspace' OR (scope='personal' AND owner_id=$3) OR (scope='team' AND EXISTS (SELECT 1 FROM team_members tm WHERE tm.team_id=saved_replies.team_id AND tm.member_id=$3)))`
		args = append(args, memberID)
	}
	if beforeName != "" {
		beforeNameArg := len(args) + 1
		beforeIDArg := beforeNameArg + 1
		sql += fmt.Sprintf(` AND (name,id) > ($%d,$%d)`, beforeNameArg, beforeIDArg)
		args = append(args, beforeName, beforeID)
	}
	sql += ` ORDER BY name,id LIMIT $` + fmt.Sprint(len(args)+1)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, sql, args...)
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

func (s *Service) UseSavedReplyForMember(ctx context.Context, workspaceID, memberID, id string) error {
	item, err := s.GetSavedReply(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	visible, err := s.savedReplyVisibleToMember(ctx, workspaceID, memberID, *item)
	if err != nil {
		return err
	}
	if !visible {
		return ErrSavedReplyForbidden
	}
	return s.UseSavedReply(ctx, workspaceID, id)
}

func (s *Service) savedReplyVisibleToMember(ctx context.Context, workspaceID, memberID string, reply SavedReply) (bool, error) {
	switch reply.Scope {
	case "workspace":
		return true, nil
	case "personal":
		return reply.OwnerID != nil && *reply.OwnerID == memberID, nil
	case "team":
		if reply.TeamID == nil || *reply.TeamID == "" {
			return false, nil
		}
		var exists bool
		err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM team_members tm JOIN teams t ON t.id=tm.team_id WHERE t.workspace_id=$1 AND tm.team_id=$2 AND tm.member_id=$3)`, workspaceID, *reply.TeamID, memberID).Scan(&exists)
		return exists, err
	default:
		return false, nil
	}
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
