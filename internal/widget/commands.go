package widget

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/jackc/pgx/v5"
)

var ErrCommandBindingInvalid = errors.New("widget: invalid command binding")
var ErrCommandBindingDisabled = errors.New("widget: command binding is disabled")
var ErrCommandPayloadTooLarge = errors.New("widget: command payload is too large")

type CommandBinding struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CommandInvocation struct {
	ID             string    `json:"id"`
	BindingID      string    `json:"binding_id"`
	ConversationID string    `json:"conversation_id"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	ExpiresAt      time.Time `json:"expires_at"`
}

// PendingCommand is the narrow, host-facing projection returned when a
// visitor reconnects. It contains no workspace or member data; the visitor
// token and conversation ownership check are the authorization boundary.
type PendingCommand struct {
	ID             string         `json:"command_id"`
	BindingID      string         `json:"binding_id"`
	Name           string         `json:"name"`
	ConversationID string         `json:"conversation_id"`
	Payload        map[string]any `json:"payload"`
	ExpiresAt      time.Time      `json:"expires_at"`
	// CreatedAt is an internal queue cursor. It is deliberately not exposed
	// to the host page: the command id and payload are the public contract.
	CreatedAt time.Time `json:"-"`
}

type PendingCommandPage struct {
	Items   []PendingCommand
	HasMore bool
}

const maxPendingCommandPage = 32

func (s *Service) ListCommandBindings(ctx context.Context, workspaceID string) ([]CommandBinding, error) {
	return s.ListCommandBindingsPage(ctx, workspaceID, time.Time{}, "", 201)
}

// ListCommandBindingsPage returns one bounded, newest-first page. The
// unpaged method remains for internal bounded lookups, while HTTP callers use
// the page method so a workspace cannot make the dashboard read an unbounded
// command-binding collection.
func (s *Service) ListCommandBindingsPage(ctx context.Context, workspaceID string, before time.Time, beforeID string, limit int) ([]CommandBinding, error) {
	if limit <= 0 {
		limit = 50
	}
	args := []any{workspaceID}
	where := "workspace_id=$1"
	if !before.IsZero() {
		args = append(args, before, beforeID)
		where += " AND (created_at < $2 OR (created_at = $2 AND id < $3))"
	}
	args = append(args, limit)
	limitArg := len(args)
	rows, err := s.pool.Query(ctx, `SELECT id,workspace_id,name,description,enabled,created_at,updated_at FROM customer_command_bindings WHERE `+where+` ORDER BY created_at DESC,id DESC LIMIT $`+strconv.Itoa(limitArg), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CommandBinding
	for rows.Next() {
		var item CommandBinding
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Description, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) CreateCommandBinding(ctx context.Context, workspaceID, memberID, name, description string) (*CommandBinding, error) {
	if !validCommandBindingName(name) {
		return nil, ErrCommandBindingInvalid
	}
	name = strings.TrimSpace(name)
	id := ids.New("ccb")
	var item CommandBinding
	err := s.pool.QueryRow(ctx, `INSERT INTO customer_command_bindings(id,workspace_id,name,description,created_by) VALUES($1,$2,$3,$4,$5) RETURNING id,workspace_id,name,description,enabled,created_at,updated_at`, id, workspaceID, name, strings.TrimSpace(description), memberID).Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Description, &item.Enabled, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func validCommandBindingName(name string) bool {
	name = strings.TrimSpace(name)
	if len(name) < 2 || len(name) > 80 {
		return false
	}
	for _, r := range name {
		if !(r == '_' || r == '-' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// UpdateCommandBinding changes the host-facing contract without changing its
// stable id. Disabling is the safe operational equivalent of deletion: old
// invocation history remains auditable, while agents can no longer deliver a
// command that the host has withdrawn.
func (s *Service) UpdateCommandBinding(ctx context.Context, workspaceID, memberID, bindingID, name, description string, enabled bool) (*CommandBinding, error) {
	if !validCommandBindingName(name) {
		return nil, ErrCommandBindingInvalid
	}
	name = strings.TrimSpace(name)
	var item CommandBinding
	err := s.pool.QueryRow(ctx, `
		UPDATE customer_command_bindings
		SET name=$3, description=$4, enabled=$5, updated_at=now()
		WHERE workspace_id=$1 AND id=$2
		RETURNING id,workspace_id,name,description,enabled,created_at,updated_at
	`, workspaceID, bindingID, name, strings.TrimSpace(description), enabled).Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Description, &item.Enabled, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Service) InvokeCommand(ctx context.Context, workspaceID, memberID, conversationID, bindingID string, payload map[string]any) (*CommandInvocation, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	raw, err := json.Marshal(payload)
	if err != nil || len(raw) > 4096 {
		return nil, ErrCommandPayloadTooLarge
	}
	conv, err := s.conversation.Get(ctx, workspaceID, conversationID)
	if err != nil {
		return nil, err
	}
	if conv.VisitorID == nil {
		return nil, ErrConversationOwner
	}
	id := ids.New("cci")
	expires := time.Now().UTC().Add(2 * time.Minute)
	item := &CommandInvocation{ID: id, BindingID: bindingID, ConversationID: conversationID, Status: "queued", CreatedAt: time.Now().UTC(), ExpiresAt: expires}
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var name string
		var enabled bool
		if err := tx.QueryRow(ctx, `SELECT name,enabled FROM customer_command_bindings WHERE workspace_id=$1 AND id=$2`, workspaceID, bindingID).Scan(&name, &enabled); err != nil {
			return err
		}
		if !enabled {
			return ErrCommandBindingDisabled
		}
		if _, err := tx.Exec(ctx, `INSERT INTO customer_command_invocations(id,workspace_id,binding_id,conversation_id,visitor_id,member_id,payload,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, workspaceID, bindingID, conversationID, *conv.VisitorID, memberID, payload, expires); err != nil {
			return err
		}
		if s.events == nil {
			return nil
		}
		_, err := s.events.Append(ctx, tx, events.Event{WorkspaceID: workspaceID, Type: events.CustomerCommand, EntityType: "conversation", EntityID: conversationID, ActorType: events.ActorUser, ActorID: memberID, Data: map[string]any{"command_id": id, "binding_id": bindingID, "name": name, "payload": payload, "expires_at": expires}})
		return err
	})
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) AcknowledgeCommand(ctx context.Context, workspaceID, conversationID string, visitor *Visitor, commandID, status string) error {
	if status != "acknowledged" && status != "ignored" && status != "failed" {
		return ErrCommandBindingInvalid
	}
	if err := s.checkOwnership(ctx, workspaceID, conversationID, visitor); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `UPDATE customer_command_invocations SET status=$5,acknowledged_at=now() WHERE workspace_id=$1 AND id=$2 AND conversation_id=$3 AND visitor_id=$4 AND expires_at>now() AND status IN ('queued','delivered')`, workspaceID, commandID, conversationID, visitor.ID, status)
	return err
}

// PendingCommands claims a small batch of commands that were queued while a
// visitor was offline. Claiming and selecting happen in one transaction so a
// second tab or a second Hubchat process cannot deliver the same queued row at
// the same time. Expired rows are closed while we are already touching this
// visitor; no unbounded cleanup work is performed on the request path.
func (s *Service) PendingCommands(ctx context.Context, workspaceID, conversationID string, visitor *Visitor) ([]PendingCommand, error) {
	page, err := s.PendingCommandsPage(ctx, workspaceID, conversationID, visitor, time.Time{}, "", maxPendingCommandPage)
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

// PendingCommandsPage claims one bounded, oldest-first page of commands that
// were queued while the visitor was offline. The cursor is forward-moving:
// claimed rows become delivered, while any extra row used to detect a next
// page remains queued until the caller asks for that page. This prevents a
// reconnect response from silently marking commands as delivered without
// returning them to the host.
func (s *Service) PendingCommandsPage(ctx context.Context, workspaceID, conversationID string, visitor *Visitor, after time.Time, afterID string, limit int) (PendingCommandPage, error) {
	if err := s.checkOwnership(ctx, workspaceID, conversationID, visitor); err != nil {
		return PendingCommandPage{}, err
	}
	if limit <= 0 || limit > maxPendingCommandPage {
		limit = maxPendingCommandPage
	}

	commands := make([]PendingCommand, 0, 32)
	page := PendingCommandPage{}
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE customer_command_invocations
			SET status='expired'
			WHERE workspace_id=$1 AND conversation_id=$2 AND visitor_id=$3
			  AND status IN ('queued','delivered') AND expires_at<=now()
		`, workspaceID, conversationID, visitor.ID); err != nil {
			return err
		}

		args := []any{workspaceID, conversationID, visitor.ID}
		where := `i.workspace_id=$1 AND i.conversation_id=$2 AND i.visitor_id=$3
			  AND i.status='queued' AND i.expires_at>now() AND b.enabled`
		if !after.IsZero() {
			args = append(args, after, afterID)
			where += ` AND (i.created_at>$4 OR (i.created_at=$4 AND i.id>$5))`
		}
		args = append(args, limit+1)
		limitPlaceholder := strconv.Itoa(len(args))
		rows, err := tx.Query(ctx, `
			SELECT i.id, i.binding_id, b.name, i.conversation_id, i.payload, i.expires_at, i.created_at
			FROM customer_command_invocations i
			JOIN customer_command_bindings b ON b.id=i.binding_id AND b.workspace_id=i.workspace_id
			WHERE `+where+`
			ORDER BY i.created_at ASC, i.id ASC
			LIMIT $`+limitPlaceholder+`
			FOR UPDATE OF i SKIP LOCKED
		`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var item PendingCommand
			if err := rows.Scan(&item.ID, &item.BindingID, &item.Name, &item.ConversationID, &item.Payload, &item.ExpiresAt, &item.CreatedAt); err != nil {
				rows.Close()
				return err
			}
			commands = append(commands, item)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		page.HasMore = len(commands) > limit
		if page.HasMore {
			commands = commands[:limit]
		}
		page.Items = commands

		for _, item := range page.Items {
			if _, err := tx.Exec(ctx, `
				UPDATE customer_command_invocations
				SET status='delivered', delivered_at=COALESCE(delivered_at, now())
				WHERE workspace_id=$1 AND id=$2 AND status='queued'
			`, workspaceID, item.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return PendingCommandPage{}, err
	}
	if page.Items == nil {
		page.Items = []PendingCommand{}
	}
	return page, nil
}
