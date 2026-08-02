package task

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/jackc/pgx/v5"
)

var (
	ErrNotFound        = errors.New("task: not found")
	ErrInvalidTitle    = errors.New("task: title is required")
	ErrInvalidState    = errors.New("task: invalid state transition")
	ErrInvalidSubject  = errors.New("task: invalid subject")
	ErrInvalidAssignee = errors.New("task: assignee is not a workspace member")
)

const (
	StateOpen      = "open"
	StateCompleted = "completed"
	StateCancelled = "cancelled"
)

var subjectTables = map[string]string{
	"conversation": "conversations",
	"ticket":       "tickets",
	"customer":     "customers",
	"feedback":     "feedback_items",
}

// Service owns task persistence. Every query includes workspace_id, even when
// the caller already resolved an authenticated actor, so a leaked identifier
// can never cross a tenant boundary.
type Service struct{ pool *database.Pool }

type Task struct {
	ID            string     `json:"id"`
	WorkspaceID   string     `json:"workspace_id"`
	Title         string     `json:"title"`
	Description   string     `json:"description,omitempty"`
	State         string     `json:"state"`
	SubjectType   string     `json:"subject_type,omitempty"`
	SubjectID     string     `json:"subject_id,omitempty"`
	AssigneeID    string     `json:"assignee_id,omitempty"`
	AssigneeName  string     `json:"assignee_name,omitempty"`
	DueAt         *time.Time `json:"due_at,omitempty"`
	CreatedBy     string     `json:"created_by,omitempty"`
	CreatedByName string     `json:"created_by_name,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type Input struct {
	Title       string     `json:"title"`
	Description string     `json:"description"`
	SubjectType string     `json:"subject_type"`
	SubjectID   string     `json:"subject_id"`
	AssigneeID  string     `json:"assignee_id"`
	DueAt       *time.Time `json:"due_at"`
}

// AutomationInput keeps relative due dates deterministic at the service
// boundary while leaving the public API in terms of an absolute due_at.
type AutomationInput struct {
	Title           string
	Description     string
	SubjectType     string
	SubjectID       string
	AssigneeID      string
	DueAfterMinutes int
}

type UpdateInput struct {
	Title       *string    `json:"title"`
	Description *string    `json:"description"`
	State       *string    `json:"state"`
	AssigneeID  *string    `json:"assignee_id"`
	DueAt       *time.Time `json:"due_at"`
}

func New(pool *database.Pool) *Service { return &Service{pool: pool} }

func (s *Service) Create(ctx context.Context, workspaceID, actorID string, input Input) (*Task, error) {
	return s.create(ctx, workspaceID, actorID, input)
}

func (s *Service) CreateFromAutomation(ctx context.Context, workspaceID, actorID string, input AutomationInput) (*Task, error) {
	if input.DueAfterMinutes < 0 {
		return nil, errors.New("task: due_after_minutes cannot be negative")
	}
	dueAt := time.Time{}
	if input.DueAfterMinutes > 0 {
		dueAt = time.Now().UTC().Add(time.Duration(input.DueAfterMinutes) * time.Minute)
	}
	return s.create(ctx, workspaceID, actorID, Input{
		Title: input.Title, Description: input.Description, SubjectType: input.SubjectType,
		SubjectID: input.SubjectID, AssigneeID: input.AssigneeID,
		DueAt: timePointerOrNil(dueAt),
	})
}

func (s *Service) create(ctx context.Context, workspaceID, actorID string, input Input) (*Task, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, ErrInvalidTitle
	}
	if len(title) > 500 {
		return nil, errors.New("task: title is too long")
	}
	if len(input.Description) > 20_000 {
		return nil, errors.New("task: description is too long")
	}
	subjectType := strings.TrimSpace(input.SubjectType)
	subjectID := strings.TrimSpace(input.SubjectID)
	if (subjectType == "") != (subjectID == "") {
		return nil, ErrInvalidSubject
	}
	if subjectType != "" {
		if _, ok := subjectTables[subjectType]; !ok {
			return nil, ErrInvalidSubject
		}
		exists, err := s.subjectExists(ctx, workspaceID, subjectType, subjectID)
		if err != nil {
			return nil, fmt.Errorf("task: validate subject: %w", err)
		}
		if !exists {
			return nil, ErrInvalidSubject
		}
	}
	assignee := strings.TrimSpace(input.AssigneeID)
	if assignee != "" {
		valid, err := s.memberExists(ctx, workspaceID, assignee)
		if err != nil {
			return nil, fmt.Errorf("task: validate assignee: %w", err)
		}
		if !valid {
			return nil, ErrInvalidAssignee
		}
	}
	var task Task
	err := s.pool.QueryRow(ctx, `
		INSERT INTO tasks(id,workspace_id,title,description,subject_type,subject_id,assignee_id,due_at,created_by)
		VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),$8,NULLIF($9,''))
		RETURNING id,workspace_id,title,description,state,coalesce(subject_type,''),coalesce(subject_id,''),
		          coalesce(assignee_id,''),due_at,coalesce(created_by,''),completed_at,created_at,updated_at
	`, ids.New(ids.PrefixTask), workspaceID, title, input.Description, subjectType, subjectID, assignee, input.DueAt, actorID).Scan(
		&task.ID, &task.WorkspaceID, &task.Title, &task.Description, &task.State,
		&task.SubjectType, &task.SubjectID, &task.AssigneeID, &task.DueAt,
		&task.CreatedBy, &task.CompletedAt, &task.CreatedAt, &task.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("task: create: %w", err)
	}
	return s.hydrateNames(ctx, &task)
}

func (s *Service) Get(ctx context.Context, workspaceID, id string) (*Task, error) {
	task, err := s.getRaw(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	return s.hydrateNames(ctx, task)
}

// ListPage returns newest tasks first. The created_at/id tuple is the cursor
// boundary, and filters are applied before that boundary so every page is
// workspace-scoped and stable while new tasks arrive.
func (s *Service) ListPage(ctx context.Context, workspaceID, state, assigneeID, query string, overdue bool, before time.Time, beforeID string, limit int) ([]Task, error) {
	state = strings.TrimSpace(state)
	if state != "" && !validState(state) {
		return nil, ErrInvalidState
	}
	query = strings.TrimSpace(query)
	args := []any{workspaceID}
	where := []string{"t.workspace_id=$1"}
	if state != "" {
		args = append(args, state)
		where = append(where, fmt.Sprintf("t.state=$%d", len(args)))
	}
	if assigneeID != "" {
		args = append(args, assigneeID)
		where = append(where, fmt.Sprintf("t.assignee_id=$%d", len(args)))
	}
	if query != "" {
		args = append(args, "%"+query+"%")
		where = append(where, fmt.Sprintf("(t.title ILIKE $%d OR t.description ILIKE $%d)", len(args), len(args)))
	}
	if overdue {
		where = append(where, "t.state='open' AND t.due_at IS NOT NULL AND t.due_at < now()")
	}
	if !before.IsZero() {
		args = append(args, before, beforeID)
		where = append(where, fmt.Sprintf("(t.created_at,t.id) < ($%d,$%d)", len(args)-1, len(args)))
	}
	querySQL := `
		SELECT t.id,t.workspace_id,t.title,t.description,t.state,coalesce(t.subject_type,''),coalesce(t.subject_id,''),
		       coalesce(t.assignee_id,''),coalesce(au.name,''),t.due_at,coalesce(t.created_by,''),coalesce(cu.name,''),
		       t.completed_at,t.created_at,t.updated_at
		FROM tasks t
		LEFT JOIN workspace_members am ON am.id=t.assignee_id AND am.workspace_id=t.workspace_id
		LEFT JOIN users au ON au.id=am.user_id
		LEFT JOIN workspace_members cm ON cm.id=t.created_by AND cm.workspace_id=t.workspace_id
		LEFT JOIN users cu ON cu.id=cm.user_id
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY t.created_at DESC,t.id DESC`
	if limit > 0 {
		args = append(args, limit)
		querySQL += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	rows, err := s.pool.Query(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("task: list: %w", err)
	}
	defer rows.Close()
	items := make([]Task, 0)
	for rows.Next() {
		var item Task
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.Title, &item.Description, &item.State,
			&item.SubjectType, &item.SubjectID, &item.AssigneeID, &item.AssigneeName, &item.DueAt,
			&item.CreatedBy, &item.CreatedByName, &item.CompletedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("task: scan list: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("task: list rows: %w", err)
	}
	return items, nil
}

func (s *Service) Update(ctx context.Context, workspaceID, id string, input UpdateInput) (*Task, error) {
	if input.Title != nil {
		value := strings.TrimSpace(*input.Title)
		if value == "" {
			return nil, ErrInvalidTitle
		}
		if len(value) > 500 {
			return nil, errors.New("task: title is too long")
		}
		input.Title = &value
	}
	if input.Description != nil && len(*input.Description) > 20_000 {
		return nil, errors.New("task: description is too long")
	}
	if input.State != nil && !validState(*input.State) {
		return nil, ErrInvalidState
	}
	if input.AssigneeID != nil && strings.TrimSpace(*input.AssigneeID) != "" {
		valid, err := s.memberExists(ctx, workspaceID, strings.TrimSpace(*input.AssigneeID))
		if err != nil {
			return nil, fmt.Errorf("task: validate assignee: %w", err)
		}
		if !valid {
			return nil, ErrInvalidAssignee
		}
	}
	current, err := s.getRaw(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if input.State != nil && !validTransition(current.State, *input.State) {
		return nil, ErrInvalidState
	}
	title := current.Title
	if input.Title != nil {
		title = *input.Title
	}
	description := current.Description
	if input.Description != nil {
		description = *input.Description
	}
	state := current.State
	if input.State != nil {
		state = *input.State
	}
	assignee := current.AssigneeID
	if input.AssigneeID != nil {
		assignee = strings.TrimSpace(*input.AssigneeID)
	}
	dueAt := current.DueAt
	if input.DueAt != nil {
		dueAt = input.DueAt
	}
	completedAt := current.CompletedAt
	if state == StateCompleted && current.State != StateCompleted {
		now := time.Now().UTC()
		completedAt = &now
	} else if state != StateCompleted {
		completedAt = nil
	}
	var task Task
	err = s.pool.QueryRow(ctx, `
		UPDATE tasks SET title=$3,description=$4,state=$5,assignee_id=NULLIF($6,''),due_at=$7,completed_at=$8,updated_at=now()
		WHERE workspace_id=$1 AND id=$2
		RETURNING id,workspace_id,title,description,state,coalesce(subject_type,''),coalesce(subject_id,''),
		          coalesce(assignee_id,''),due_at,coalesce(created_by,''),completed_at,created_at,updated_at
	`, workspaceID, id, title, description, state, assignee, dueAt, completedAt).Scan(
		&task.ID, &task.WorkspaceID, &task.Title, &task.Description, &task.State,
		&task.SubjectType, &task.SubjectID, &task.AssigneeID, &task.DueAt,
		&task.CreatedBy, &task.CompletedAt, &task.CreatedAt, &task.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("task: update: %w", err)
	}
	return s.hydrateNames(ctx, &task)
}

func (s *Service) getRaw(ctx context.Context, workspaceID, id string) (*Task, error) {
	var item Task
	err := s.pool.QueryRow(ctx, `
		SELECT id,workspace_id,title,description,state,coalesce(subject_type,''),coalesce(subject_id,''),
		       coalesce(assignee_id,''),due_at,coalesce(created_by,''),completed_at,created_at,updated_at
		FROM tasks WHERE workspace_id=$1 AND id=$2
	`, workspaceID, id).Scan(&item.ID, &item.WorkspaceID, &item.Title, &item.Description, &item.State,
		&item.SubjectType, &item.SubjectID, &item.AssigneeID, &item.DueAt, &item.CreatedBy,
		&item.CompletedAt, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("task: get: %w", err)
	}
	return &item, nil
}

func (s *Service) hydrateNames(ctx context.Context, item *Task) (*Task, error) {
	if item.AssigneeID != "" {
		_ = s.pool.QueryRow(ctx, `SELECT coalesce(u.name,'') FROM workspace_members m JOIN users u ON u.id=m.user_id WHERE m.workspace_id=$1 AND m.id=$2`, item.WorkspaceID, item.AssigneeID).Scan(&item.AssigneeName)
	}
	if item.CreatedBy != "" {
		_ = s.pool.QueryRow(ctx, `SELECT coalesce(u.name,'') FROM workspace_members m JOIN users u ON u.id=m.user_id WHERE m.workspace_id=$1 AND m.id=$2`, item.WorkspaceID, item.CreatedBy).Scan(&item.CreatedByName)
	}
	return item, nil
}

func (s *Service) memberExists(ctx context.Context, workspaceID, memberID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspace_members WHERE workspace_id=$1 AND id=$2)`, workspaceID, memberID).Scan(&exists)
	return exists, err
}

func (s *Service) subjectExists(ctx context.Context, workspaceID, subjectType, subjectID string) (bool, error) {
	table := subjectTables[subjectType]
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE workspace_id=$1 AND id=$2)`, workspaceID, subjectID).Scan(&exists)
	return exists, err
}

func validState(value string) bool {
	return value == StateOpen || value == StateCompleted || value == StateCancelled
}

func validTransition(from, to string) bool {
	if from == to {
		return true
	}
	return (from == StateOpen && (to == StateCompleted || to == StateCancelled)) ||
		((from == StateCompleted || from == StateCancelled) && to == StateOpen)
}

func timePointerOrNil(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
