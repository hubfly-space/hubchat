package ticket

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/workspace"
)

var (
	ErrEmptyTitle      = errors.New("ticket: title must not be empty")
	ErrInvalidStatus   = errors.New("ticket: not a recognised status")
	ErrInvalidPriority = errors.New("ticket: not a recognised priority")
	ErrInvalidAssignee = errors.New("ticket: assignee is not a member of this workspace")
	ErrInvalidTeam     = errors.New("ticket: not a team in this workspace")
	ErrInvalidInbox    = errors.New("ticket: not an inbox in this workspace")
	ErrInvalidCustomer = errors.New("ticket: not a customer in this workspace")
	ErrInvalidCompany  = errors.New("ticket: not a company in this workspace")
	ErrTagNotFound     = errors.New("ticket: not a tag in this workspace")
	ErrInvalidRelation = errors.New("ticket: not a recognised link relation")
	ErrLinkToSelf      = errors.New("ticket: a ticket cannot link to itself")
	ErrInvalidParent   = errors.New("ticket: not a ticket in this workspace")
	ErrParentCycle     = errors.New("ticket: that would make a ticket its own ancestor")
	ErrParentIsSelf    = errors.New("ticket: a ticket cannot be its own parent")
)

const entityTicket = "ticket"

// Service is deliberately built around workspace.Service rather than a raw
// pool for one thing only: allocating a ticket's display number, which lives
// on the workspaces row workspace owns (§6.3). Everything else here reads
// tenancy-scoped tables directly, the same as conversation/customer do.
type Service struct {
	repo      *repository
	fields    *fieldRepository
	pool      *database.Pool
	workspace *workspace.Service
	events    *events.Log
	audit     *audit.Log
}

func New(pool *database.Pool, workspaceSvc *workspace.Service, eventLog *events.Log, auditLog *audit.Log) *Service {
	return &Service{
		repo: &repository{pool: pool}, fields: &fieldRepository{pool: pool},
		pool: pool, workspace: workspaceSvc, events: eventLog, audit: auditLog,
	}
}

func (s *Service) appendEvent(ctx context.Context, tx pgx.Tx, event events.Event) error {
	if s.events == nil {
		return nil
	}
	_, err := s.events.Append(ctx, tx, event)
	return err
}

func (s *Service) recordAudit(ctx context.Context, tx pgx.Tx, entry audit.Entry) error {
	if s.audit == nil {
		return nil
	}
	if entry.ActorName == "" && entry.ActorType == audit.ActorUser && entry.ActorID != "" {
		if name, err := s.repo.memberDisplayName(ctx, entry.ActorID); err == nil {
			entry.ActorName = name
		}
	}
	return audit.RecordTx(ctx, tx, entry)
}

// CreateRequest is every caller-supplied field a new ticket can start with.
// InboxID is required (the shared Ticket contract's inbox_id is non-null);
// everything else is optional.
type CreateRequest struct {
	Title          string
	Description    string
	Type           *string
	Priority       string
	CustomerID     *string
	CompanyID      *string
	InboxID        string
	Channel        string
	AssigneeID     *string
	TeamID         *string
	ConversationID *string
	ParentID       *string
	DueAt          *time.Time
	FieldValues    map[string]any
}

// Create opens a new ticket, allocating its display number from the
// workspace's own counter inside the same transaction as the insert (§6.3) —
// so a create that fails after the number is drawn never leaves a ticket
// with that number missing; it simply leaves a gap, which reporting can
// tolerate but a *duplicate* number could not.
func (s *Service) Create(ctx context.Context, workspaceID, actorMemberID string, req CreateRequest) (*Ticket, error) {
	return s.create(ctx, workspaceID, audit.ActorUser, events.ActorUser, "member", actorMemberID, "", req)
}

// CreateAsCustomer opens a ticket from a customer-facing surface. Keeping the
// actor type explicit prevents a portal request from appearing in the audit
// log as if an agent had created it.
func (s *Service) CreateAsCustomer(ctx context.Context, workspaceID, customerID, customerName string, req CreateRequest) (*Ticket, error) {
	if req.CustomerID == nil || *req.CustomerID != customerID {
		return nil, ErrInvalidCustomer
	}
	return s.create(ctx, workspaceID, audit.ActorCustomer, events.ActorCustomer, "customer", customerID, customerName, req)
}

func (s *Service) create(
	ctx context.Context, workspaceID string,
	auditActor audit.ActorType, eventActor events.ActorType, statusActor, actorID, actorName string,
	req CreateRequest,
) (*Ticket, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return nil, ErrEmptyTitle
	}
	if req.InboxID == "" {
		return nil, ErrInvalidInbox
	}
	if ok, err := s.repo.inboxInWorkspace(ctx, workspaceID, req.InboxID); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrInvalidInbox
	}
	if req.CustomerID != nil {
		if ok, err := s.repo.customerInWorkspace(ctx, workspaceID, *req.CustomerID); err != nil {
			return nil, err
		} else if !ok {
			return nil, ErrInvalidCustomer
		}
	}
	companyID := req.CompanyID
	if companyID != nil {
		if ok, err := s.repo.companyInWorkspace(ctx, workspaceID, *companyID); err != nil {
			return nil, err
		} else if !ok {
			return nil, ErrInvalidCompany
		}
	} else if req.CustomerID != nil {
		// A ticket displays its customer's company without the caller having
		// to look it up first — the same convenience the customer context
		// panel gets for free from the customer record itself.
		derived, err := s.repo.primaryCompanyID(ctx, *req.CustomerID)
		if err != nil {
			return nil, err
		}
		companyID = derived
	}
	if req.AssigneeID != nil {
		if ok, err := s.repo.memberInWorkspace(ctx, workspaceID, *req.AssigneeID); err != nil {
			return nil, err
		} else if !ok {
			return nil, ErrInvalidAssignee
		}
	}
	if req.TeamID != nil {
		if ok, err := s.repo.teamInWorkspace(ctx, workspaceID, *req.TeamID); err != nil {
			return nil, err
		} else if !ok {
			return nil, ErrInvalidTeam
		}
	}
	if req.ParentID != nil {
		if ok, err := s.repo.exists(ctx, workspaceID, *req.ParentID); err != nil {
			return nil, err
		} else if !ok {
			return nil, ErrInvalidParent
		}
	}

	channel := req.Channel
	if channel == "" {
		channel = "manual"
	}
	priority := req.Priority
	if priority == "" {
		priority = "normal"
	}
	if !validPriorities[priority] {
		return nil, ErrInvalidPriority
	}

	id := ids.New(ids.PrefixTicket)
	var ticket *Ticket

	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		prefix, number, err := s.workspace.AllocateTicketNumber(ctx, tx, workspaceID)
		if err != nil {
			return err
		}

		if err := s.repo.insert(ctx, tx,
			id, workspaceID, number, prefix, title, req.Description, priority,
			req.Type, req.CustomerID, companyID, &req.InboxID, channel,
			req.ConversationID, req.ParentID, req.DueAt,
		); err != nil {
			return err
		}

		if req.AssigneeID != nil {
			if err := s.repo.setAssignee(ctx, tx, id, req.AssigneeID); err != nil {
				return err
			}
		}
		if req.TeamID != nil {
			if err := s.repo.setTeam(ctx, tx, id, req.TeamID); err != nil {
				return err
			}
		}
		if err := s.repo.insertStatusHistory(ctx, tx, ids.New(ids.PrefixStatusHistory), id, "", "new", statusActor, actorID); err != nil {
			return err
		}

		for key, value := range req.FieldValues {
			if err := s.setFieldValueTx(ctx, tx, workspaceID, entityTicket, id, key, value); err != nil {
				return err
			}
		}

		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: auditActor, ActorID: actorID, ActorName: actorName,
			Action: "ticket.created", EntityType: entityTicket, EntityID: id,
			Metadata: map[string]any{"title": title, "number": number, "prefix": prefix},
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.TicketCreated,
			EntityType: entityTicket, EntityID: id,
			ActorType: eventActor, ActorID: actorID,
			Data: map[string]any{"id": id, "number": number, "prefix": prefix, "title": title},
		})
	})
	if err != nil {
		return nil, err
	}

	ticket, err = s.repo.byID(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	return ticket, nil
}

func (s *Service) Get(ctx context.Context, workspaceID, id string) (*Ticket, error) {
	return s.repo.byID(ctx, workspaceID, id)
}

// UpdateDetails changes title/description/type under optimistic concurrency
// (§13 conventions): expectedVersion must match what is currently stored.
func (s *Service) UpdateDetails(
	ctx context.Context, workspaceID, actorMemberID, id string, expectedVersion int,
	title, description string, ttype *string, dueAt *time.Time,
) (*Ticket, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, ErrEmptyTitle
	}

	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.updateDetails(ctx, workspaceID, id, expectedVersion, title, description, ttype, dueAt); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "ticket.updated", EntityType: entityTicket, EntityID: id,
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.TicketUpdated,
			EntityType: entityTicket, EntityID: id, ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"id": id, "title": title},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repo.byID(ctx, workspaceID, id)
}
