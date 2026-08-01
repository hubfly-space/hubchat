package workspace

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
)

var (
	ErrRoleNotFound     = errors.New("workspace: role not found")
	ErrRoleInUse        = errors.New("workspace: role is assigned to one or more members")
	ErrRoleBuiltin      = errors.New("workspace: built-in roles cannot be changed")
	ErrRoleKeyInvalid   = errors.New("workspace: role key must be lowercase letters, numbers, or underscores")
	ErrRoleKeyReserved  = errors.New("workspace: role key is reserved")
	ErrRoleNameRequired = errors.New("workspace: role name is required")
)

var customRoleKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,31}$`)

// RoleInput describes a custom role's editable fields. Capability names are
// validated against authorization's closed catalog before any row is written.
type RoleInput struct {
	Key          string
	Name         string
	Description  *string
	Capabilities []authorization.Capability
}

// RoleUpdateInput keeps the role key immutable so existing membership rows
// cannot be stranded by a rename. Nil capabilities means "leave unchanged";
// an explicit empty slice intentionally removes every grant.
type RoleUpdateInput struct {
	Name         string
	Description  *string
	Capabilities []authorization.Capability
}

func normalizeRoleInput(input RoleInput) (RoleInput, error) {
	input.Key = strings.ToLower(strings.TrimSpace(input.Key))
	input.Name = strings.TrimSpace(input.Name)
	if !customRoleKeyPattern.MatchString(input.Key) {
		return RoleInput{}, ErrRoleKeyInvalid
	}
	if _, builtin := builtinRoles[input.Key]; builtin {
		return RoleInput{}, ErrRoleKeyReserved
	}
	if input.Name == "" {
		return RoleInput{}, ErrRoleNameRequired
	}
	input.Capabilities, _ = normalizeCapabilities(input.Capabilities)
	if input.Capabilities == nil {
		return RoleInput{}, ErrInvalidCapability
	}
	return input, nil
}

func normalizeCapabilities(values []authorization.Capability) ([]authorization.Capability, error) {
	allowed := make(map[authorization.Capability]struct{}, len(authorization.AllCapabilityNames()))
	for _, name := range authorization.AllCapabilityNames() {
		allowed[authorization.Capability(name)] = struct{}{}
	}
	seen := make(map[authorization.Capability]struct{}, len(values))
	result := make([]authorization.Capability, 0, len(values))
	for _, value := range values {
		value = authorization.Capability(strings.TrimSpace(string(value)))
		if _, ok := allowed[value]; !ok {
			return nil, ErrInvalidCapability
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}

var ErrInvalidCapability = errors.New("workspace: capability is not recognised")

func (s *Service) roleExists(ctx context.Context, workspaceID, roleKey string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM roles
			WHERE key=$2 AND (workspace_id=$1 OR (workspace_id IS NULL AND is_builtin))
		)
	`, workspaceID, strings.TrimSpace(roleKey)).Scan(&exists)
	return exists, err
}

func (s *Service) validateAssignableRole(ctx context.Context, workspaceID, role string) error {
	role = strings.TrimSpace(role)
	if role == "owner" {
		return ErrCannotDemoteOwner
	}
	exists, err := s.roleExists(ctx, workspaceID, role)
	if err != nil {
		return fmt.Errorf("workspace: validate role: %w", err)
	}
	if !exists {
		return ErrInvalidRole
	}
	return nil
}

// CreateRole adds a workspace-owned custom role and its permissions
// atomically. Built-in role keys are reserved so a workspace cannot shadow
// the global role catalog and make authorization ambiguous.
func (s *Service) CreateRole(ctx context.Context, workspaceID, actorMemberID string, input RoleInput) (*RoleDefinition, error) {
	input, err := normalizeRoleInput(input)
	if err != nil {
		return nil, err
	}
	id := ids.New("rol")
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO roles (id, workspace_id, key, name, description, is_builtin)
			VALUES ($1,$2,$3,$4,$5,false)
		`, id, workspaceID, input.Key, input.Name, input.Description); err != nil {
			if isUniqueViolation(err) {
				return ErrRoleKeyTaken
			}
			return fmt.Errorf("workspace: create role: %w", err)
		}
		if err := replaceRolePermissions(ctx, tx, id, input.Capabilities); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: audit.RoleCreated, EntityType: "role", EntityID: id,
			Metadata: map[string]any{"key": input.Key, "capabilities": capabilityStrings(input.Capabilities)},
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.RoleCreated, EntityType: "role", EntityID: id,
			ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"role_id": id, "key": input.Key},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.roleByID(ctx, workspaceID, id)
}

var ErrRoleKeyTaken = errors.New("workspace: role key is already in use")

// UpdateRole changes a custom role's presentation and capability bundle.
func (s *Service) UpdateRole(ctx context.Context, workspaceID, actorMemberID, roleID string, input RoleUpdateInput) (*RoleDefinition, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, ErrRoleNameRequired
	}
	if input.Capabilities != nil {
		var err error
		input.Capabilities, err = normalizeCapabilities(input.Capabilities)
		if err != nil {
			return nil, err
		}
	}
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var key string
		var builtin bool
		if err := tx.QueryRow(ctx, `SELECT key,is_builtin FROM roles WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspaceID, roleID).Scan(&key, &builtin); errors.Is(err, pgx.ErrNoRows) {
			return ErrRoleNotFound
		} else if err != nil {
			return err
		}
		if builtin {
			return ErrRoleBuiltin
		}
		if _, err := tx.Exec(ctx, `UPDATE roles SET name=$3, description=coalesce($4,description) WHERE workspace_id=$1 AND id=$2`, workspaceID, roleID, strings.TrimSpace(input.Name), input.Description); err != nil {
			return err
		}
		if input.Capabilities != nil {
			if err := replaceRolePermissions(ctx, tx, roleID, input.Capabilities); err != nil {
				return err
			}
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: audit.RoleUpdated, EntityType: "role", EntityID: roleID,
			Metadata: map[string]any{"key": key, "capabilities_changed": input.Capabilities != nil},
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.RoleUpdated, EntityType: "role", EntityID: roleID,
			ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"role_id": roleID, "key": key},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.roleByID(ctx, workspaceID, roleID)
}

// DeleteRole removes an unused custom role. A role cannot disappear while a
// member still refers to its key; callers must make an explicit reassignment.
func (s *Service) DeleteRole(ctx context.Context, workspaceID, actorMemberID, roleID string) error {
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var key string
		var builtin bool
		if err := tx.QueryRow(ctx, `SELECT key,is_builtin FROM roles WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspaceID, roleID).Scan(&key, &builtin); errors.Is(err, pgx.ErrNoRows) {
			return ErrRoleNotFound
		} else if err != nil {
			return err
		}
		if builtin {
			return ErrRoleBuiltin
		}
		var assigned int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM workspace_members WHERE workspace_id=$1 AND role=$2`, workspaceID, key).Scan(&assigned); err != nil {
			return err
		}
		if assigned > 0 {
			return ErrRoleInUse
		}
		if _, err := tx.Exec(ctx, `DELETE FROM roles WHERE workspace_id=$1 AND id=$2`, workspaceID, roleID); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: audit.RoleDeleted, EntityType: "role", EntityID: roleID,
			Metadata: map[string]any{"key": key},
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.RoleDeleted, EntityType: "role", EntityID: roleID,
			ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"role_id": roleID, "key": key},
		})
	})
}

func replaceRolePermissions(ctx context.Context, tx pgx.Tx, roleID string, capabilities []authorization.Capability) error {
	if _, err := tx.Exec(ctx, `DELETE FROM role_permissions WHERE role_id=$1`, roleID); err != nil {
		return err
	}
	for _, capability := range capabilities {
		if _, err := tx.Exec(ctx, `INSERT INTO role_permissions (role_id, capability) VALUES ($1,$2)`, roleID, string(capability)); err != nil {
			return err
		}
	}
	return nil
}

func capabilityStrings(capabilities []authorization.Capability) []string {
	result := make([]string, len(capabilities))
	for i, capability := range capabilities {
		result[i] = string(capability)
	}
	return result
}

func (s *Service) roleByID(ctx context.Context, workspaceID, roleID string) (*RoleDefinition, error) {
	var role RoleDefinition
	err := s.pool.QueryRow(ctx, `
		SELECT r.id, coalesce(r.workspace_id,''), r.key, r.name, r.description, r.is_builtin,
		       coalesce(array_agg(rp.capability) FILTER (WHERE rp.capability IS NOT NULL), '{}')
		FROM roles r LEFT JOIN role_permissions rp ON rp.role_id=r.id
		WHERE r.id=$1 AND (r.workspace_id=$2 OR (r.workspace_id IS NULL AND r.is_builtin))
		GROUP BY r.id, r.workspace_id, r.key, r.name, r.description, r.is_builtin
	`, roleID, workspaceID).Scan(&role.ID, &role.WorkspaceID, &role.Key, &role.Name, &role.Description, &role.IsBuiltin, &role.Capabilities)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRoleNotFound
	}
	if err != nil {
		return nil, err
	}
	if role.Key == "owner" {
		role.Capabilities = authorization.AllCapabilityNames()
	}
	return &role, nil
}
