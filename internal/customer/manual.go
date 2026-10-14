package customer

import (
	"context"
	"errors"
	"net/mail"
	"strings"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/jackc/pgx/v5"
)

var ErrDuplicateCustomer = errors.New("customer: a matching email or external ID already exists; review the existing profile")
var ErrInvalidCustomer = errors.New("customer: supply a name and a valid email or external ID")

type ManualInput struct {
	Name       string `json:"name"`
	Email      string `json:"email"`
	Phone      string `json:"phone"`
	ExternalID string `json:"external_id"`
}

// CreateManual records an agent-entered identity as unverified. It never merges
// a claimed email with an existing identity or upgrades its verification.
func (s *Service) CreateManual(ctx context.Context, workspaceID, memberID string, input ManualInput) (*Customer, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.ExternalID = strings.TrimSpace(input.ExternalID)
	if input.Name == "" || len(input.Name) > 500 || (input.Email == "" && input.ExternalID == "") || len(input.ExternalID) > 500 || len(input.Phone) > 100 {
		return nil, ErrInvalidCustomer
	}
	if input.Email != "" {
		parsed, err := mail.ParseAddress(input.Email)
		if err != nil || parsed.Address != input.Email {
			return nil, ErrInvalidCustomer
		}
	}
	id := ids.New(ids.PrefixCustomer)
	var created *Customer
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		// Serialize manual creates within this workspace so duplicate checks and
		// insertion cannot race, including case-insensitive email matches.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "customer:manual:"+workspaceID); err != nil {
			return err
		}
		var duplicate bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM customers WHERE workspace_id=$1 AND (($2<>'' AND email=$2::citext) OR ($3<>'' AND external_id=$3)))`, workspaceID, input.Email, input.ExternalID).Scan(&duplicate); err != nil {
			return err
		}
		if duplicate {
			return ErrDuplicateCustomer
		}
		var err error
		created, err = scanCustomer(tx.QueryRow(ctx, `INSERT INTO customers(id,workspace_id,name,email,phone,external_id,verification) VALUES($1,$2,$3,NULLIF($4,'')::citext,NULLIF($5,''),NULLIF($6,''),'unverified') RETURNING `+customerColumns, id, workspaceID, input.Name, input.Email, strings.TrimSpace(input.Phone), input.ExternalID))
		if err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{WorkspaceID: workspaceID, Type: events.CustomerCreated, EntityType: "customer", EntityID: id, ActorType: events.ActorUser, ActorID: memberID})
	})
	return created, err
}

// HasEmail supports a conflict preview; it never establishes identity.
func (s *Service) HasEmail(ctx context.Context, workspaceID, email string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM customers WHERE workspace_id=$1 AND email=$2::citext)`, workspaceID, strings.TrimSpace(email)).Scan(&exists)
	return exists, err
}
