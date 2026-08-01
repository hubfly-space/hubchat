package workspace

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
)

var (
	ErrSCIMInvalidUserName    = errors.New("workspace: SCIM userName must be an email address")
	ErrSCIMExternalIDConflict = errors.New("workspace: SCIM externalId is already assigned")
	ErrSCIMUserNotFound       = errors.New("workspace: SCIM user not found")
	ErrSCIMOwnerDeactivation  = errors.New("workspace: the workspace owner cannot be deactivated")
)

// SCIMUser is the provider-neutral member projection exposed by the SCIM
// adapter. The user id is never used as a credential; it is only the stable
// SCIM resource id for this workspace membership.
type SCIMUser struct {
	ID          string
	UserID      string
	ExternalID  string
	UserName    string
	DisplayName string
	Active      bool
	Role        string
	CreatedAt   time.Time
}

// SCIMProvisionInput is the deliberately small subset of the SCIM User
// resource Hubchat can apply without guessing at an identity provider's
// private schema.
type SCIMProvisionInput struct {
	ExternalID  string
	UserName    string
	DisplayName string
	Active      *bool
	Role        string
}

// ListSCIMUsers returns bounded SCIM resources using the conventional
// startIndex/count window. The public Hubchat API remains cursor-based; SCIM
// uses its standard pagination shape so existing directory providers work.
func (s *Service) ListSCIMUsers(ctx context.Context, workspaceID, userName, externalID string, startIndex, count int) ([]SCIMUser, int, error) {
	if startIndex < 1 {
		startIndex = 1
	}
	if count <= 0 || count > 200 {
		count = 100
	}
	where := `m.workspace_id = $1 AND ($2 = '' OR u.email::text = $2) AND ($3 = '' OR m.scim_external_id = $3)`
	args := []any{workspaceID, strings.ToLower(strings.TrimSpace(userName)), strings.TrimSpace(externalID)}
	var total int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM workspace_members m JOIN users u ON u.id=m.user_id WHERE `+where,
		args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("workspace: SCIM count users: %w", err)
	}
	args = append(args, count, startIndex-1)
	rows, err := s.pool.Query(ctx, `
		SELECT m.id, m.user_id, coalesce(m.scim_external_id,''), u.email::text, u.name,
		       (m.deactivated_at IS NULL), m.role, m.created_at
		FROM workspace_members m JOIN users u ON u.id=m.user_id
		WHERE `+where+`
		ORDER BY m.created_at ASC, m.id ASC
		LIMIT $4 OFFSET $5`, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("workspace: SCIM list users: %w", err)
	}
	defer rows.Close()
	users := make([]SCIMUser, 0)
	for rows.Next() {
		var user SCIMUser
		if err := rows.Scan(&user.ID, &user.UserID, &user.ExternalID, &user.UserName, &user.DisplayName, &user.Active, &user.Role, &user.CreatedAt); err != nil {
			return nil, 0, err
		}
		users = append(users, user)
	}
	return users, total, rows.Err()
}

func (s *Service) GetSCIMUser(ctx context.Context, workspaceID, memberID string) (*SCIMUser, error) {
	var user SCIMUser
	err := s.pool.QueryRow(ctx, `
		SELECT m.id, m.user_id, coalesce(m.scim_external_id,''), u.email::text, u.name,
		       (m.deactivated_at IS NULL), m.role, m.created_at
		FROM workspace_members m JOIN users u ON u.id=m.user_id
		WHERE m.workspace_id=$1 AND m.id=$2`, workspaceID, memberID).
		Scan(&user.ID, &user.UserID, &user.ExternalID, &user.UserName, &user.DisplayName, &user.Active, &user.Role, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSCIMUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("workspace: SCIM get user: %w", err)
	}
	return &user, nil
}

// ProvisionSCIMUser creates or reconciles a membership by externalId or
// email. It is idempotent under provider retries and never creates a second
// global user for an existing email.
func (s *Service) ProvisionSCIMUser(ctx context.Context, workspaceID, actorID string, input SCIMProvisionInput) (*SCIMUser, bool, error) {
	input.UserName = normalizeSCIMEmail(input.UserName)
	if input.UserName == "" {
		return nil, false, ErrSCIMInvalidUserName
	}
	input.ExternalID = strings.TrimSpace(input.ExternalID)
	if input.ExternalID == "" {
		input.ExternalID = input.UserName
	}
	if input.DisplayName == "" {
		input.DisplayName = input.UserName
	}
	desiredRole := strings.TrimSpace(input.Role)
	if desiredRole == "" {
		desiredRole = "agent"
	} else if err := s.validateAssignableRole(ctx, workspaceID, desiredRole); err != nil {
		return nil, false, err
	}
	active := true
	if input.Active != nil {
		active = *input.Active
	}
	var created bool
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var memberID, userID, existingEmail, role string
		var existingExternal *string
		var deactivatedAt *time.Time
		err := tx.QueryRow(ctx, `
			SELECT m.id,m.user_id,u.email::text,m.scim_external_id,m.deactivated_at,m.role
			FROM workspace_members m JOIN users u ON u.id=m.user_id
			WHERE m.workspace_id=$1 AND m.scim_external_id=$2
			FOR UPDATE`, workspaceID, input.ExternalID).
			Scan(&memberID, &userID, &existingEmail, &existingExternal, &deactivatedAt, &role)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `SELECT id,email::text FROM users WHERE email=$1 FOR UPDATE`, input.UserName).Scan(&userID, &existingEmail)
			if errors.Is(err, pgx.ErrNoRows) {
				userID = ids.New(ids.PrefixUser)
				if _, err := tx.Exec(ctx, `INSERT INTO users(id,name,email,password_hash,email_verified_at) VALUES($1,$2,$3,NULL,now())`, userID, input.DisplayName, input.UserName); err != nil {
					return fmt.Errorf("workspace: SCIM create user: %w", err)
				}
				existingEmail = input.UserName
			} else if err != nil {
				return err
			}
			err = tx.QueryRow(ctx, `
				SELECT id,scim_external_id,deactivated_at,role FROM workspace_members
				WHERE workspace_id=$1 AND user_id=$2 FOR UPDATE`, workspaceID, userID).
				Scan(&memberID, &existingExternal, &deactivatedAt, &role)
			if errors.Is(err, pgx.ErrNoRows) {
				memberID = ids.New(ids.PrefixMember)
				if _, err := tx.Exec(ctx, `INSERT INTO workspace_members(id,workspace_id,user_id,role,scim_external_id,deactivated_at) VALUES($1,$2,$3,$4,$5,$6)`, memberID, workspaceID, userID, desiredRole, input.ExternalID, inactiveAt(active)); err != nil {
					return fmt.Errorf("workspace: SCIM create membership: %w", err)
				}
				created = true
				role = desiredRole
			} else if err != nil {
				return err
			} else if existingExternal != nil && *existingExternal != input.ExternalID {
				return ErrSCIMExternalIDConflict
			}
		} else if err != nil {
			return err
		}
		if !strings.EqualFold(existingEmail, input.UserName) {
			return ErrSCIMExternalIDConflict
		}
		if !created {
			if role == "owner" && !active {
				return ErrSCIMOwnerDeactivation
			}
			if _, err := tx.Exec(ctx, `UPDATE users SET name=$2,updated_at=now() WHERE id=$1`, userID, input.DisplayName); err != nil {
				return err
			}
			if input.Role != "" {
				role = desiredRole
			}
			if _, err := tx.Exec(ctx, `UPDATE workspace_members SET role=$3,scim_external_id=$4,deactivated_at=$5 WHERE workspace_id=$1 AND id=$2`, workspaceID, memberID, role, input.ExternalID, inactiveAt(active)); err != nil {
				return err
			}
		}
		action := audit.MemberProvisioned
		eventType := events.MemberProvisioned
		if !active {
			action = audit.MemberDeactivated
			eventType = events.MemberDeactivated
		} else if deactivatedAt != nil {
			action = audit.MemberReactivated
			eventType = events.MemberReactivated
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{WorkspaceID: workspaceID, ActorType: audit.ActorAPIKey, ActorID: actorID, ActorName: "SCIM provisioning", Action: action, EntityType: "member", EntityID: memberID, Metadata: map[string]any{"external_id": input.ExternalID, "active": active, "role": role}}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{WorkspaceID: workspaceID, Type: eventType, EntityType: "member", EntityID: memberID, ActorType: events.ActorAPIKey, ActorID: actorID, Data: map[string]any{"member_id": memberID, "external_id": input.ExternalID, "active": active}})
	})
	if err != nil {
		return nil, false, err
	}
	user, err := s.findSCIMUserByExternalID(ctx, workspaceID, input.ExternalID)
	return user, created, err
}

func (s *Service) findSCIMUserByExternalID(ctx context.Context, workspaceID, externalID string) (*SCIMUser, error) {
	var memberID string
	if err := s.pool.QueryRow(ctx, `SELECT id FROM workspace_members WHERE workspace_id=$1 AND scim_external_id=$2`, workspaceID, externalID).Scan(&memberID); err != nil {
		return nil, ErrSCIMUserNotFound
	}
	return s.GetSCIMUser(ctx, workspaceID, memberID)
}

func normalizeSCIMEmail(value string) string {
	address, err := mail.ParseAddress(strings.TrimSpace(value))
	if err != nil || address.Address != strings.TrimSpace(value) || !strings.Contains(address.Address, "@") {
		return ""
	}
	return strings.ToLower(address.Address)
}

func inactiveAt(active bool) *time.Time {
	if active {
		return nil
	}
	now := time.Now().UTC()
	return &now
}
