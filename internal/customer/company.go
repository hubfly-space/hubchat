package customer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
)

var (
	ErrCompanyNotFound    = errors.New("customer: company not found")
	ErrCompanyExternalID  = errors.New("customer: a company with this external id already exists")
	ErrInvalidCompanyName = errors.New("customer: company name must not be empty")
	ErrInvalidOwner       = errors.New("customer: owner is not a member of this workspace")
)

type Company struct {
	ID              string
	WorkspaceID     string
	Name            string
	Domain          *string
	ExternalID      *string
	Tier            *string
	OwnerID         *string
	SLAPolicyID     *string
	Attributes      map[string]any
	CustomerCount   int
	OpenTicketCount int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// companyColumns computes the two roster counts inline — a company card
// always shows them, and a correlated subquery per row is cheaper than a
// second round trip for what is otherwise a single-row read.
const companyColumns = `
	c.id, c.workspace_id, c.name, c.domain::text, c.external_id, c.tier, c.owner_id, c.sla_policy_id, c.attributes,
	(SELECT count(*) FROM company_customers cc WHERE cc.company_id = c.id) AS customer_count,
	(SELECT count(*) FROM tickets t WHERE t.company_id = c.id AND t.status NOT IN ('resolved', 'closed')) AS open_ticket_count,
	c.created_at, c.updated_at
`

func scanCompany(row interface{ Scan(dest ...any) error }) (*Company, error) {
	var co Company
	err := row.Scan(
		&co.ID, &co.WorkspaceID, &co.Name, &co.Domain, &co.ExternalID, &co.Tier, &co.OwnerID, &co.SLAPolicyID, &co.Attributes,
		&co.CustomerCount, &co.OpenTicketCount, &co.CreatedAt, &co.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCompanyNotFound
	}
	if err != nil {
		return nil, err
	}
	if co.Attributes == nil {
		co.Attributes = map[string]any{}
	}
	return &co, nil
}

// insertCompany writes the row and re-reads it through companyByID rather
// than a RETURNING clause: companyColumns' roster-count subqueries expect
// the "c" alias companyByID's query provides, which an INSERT...RETURNING
// cannot supply for a brand-new row with no counts to aggregate yet anyway.
func (r *repository) insertCompany(ctx context.Context, id, workspaceID, name string, domain, externalID, tier *string) (*Company, error) {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO companies (id, workspace_id, name, domain, external_id, tier)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, id, workspaceID, name, domain, externalID, tier)
	if err != nil {
		return nil, err
	}
	return r.companyByID(ctx, workspaceID, id)
}

func (r *repository) companyByID(ctx context.Context, workspaceID, id string) (*Company, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+companyColumns+`
		FROM companies c WHERE c.workspace_id = $1 AND c.id = $2
	`, workspaceID, id)
	return scanCompany(row)
}

func (r *repository) companyByExternalID(ctx context.Context, workspaceID, externalID string) (*Company, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+companyColumns+`
		FROM companies c WHERE c.workspace_id = $1 AND c.external_id = $2
	`, workspaceID, externalID)
	return scanCompany(row)
}

func (r *repository) companiesByIDs(ctx context.Context, workspaceID string, ids []string) ([]Company, error) {
	if len(ids) == 0 {
		return []Company{}, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT `+companyColumns+`
		FROM companies c WHERE c.workspace_id = $1 AND c.id = ANY($2)
	`, workspaceID, ids)
	if err != nil {
		return nil, fmt.Errorf("customer: companies by ids: %w", err)
	}
	defer rows.Close()

	out := []Company{}
	for rows.Next() {
		co, err := scanCompany(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *co)
	}
	return out, rows.Err()
}

func (r *repository) listCompanies(ctx context.Context, workspaceID, query string, limit int) ([]Company, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+companyColumns+`
		FROM companies c
		WHERE c.workspace_id = $1
		  AND ($2 = '' OR c.name ILIKE '%' || $2 || '%' OR c.domain::text ILIKE '%' || $2 || '%')
		ORDER BY c.name
		LIMIT $3
	`, workspaceID, query, limit)
	if err != nil {
		return nil, fmt.Errorf("customer: list companies: %w", err)
	}
	defer rows.Close()

	out := []Company{}
	for rows.Next() {
		co, err := scanCompany(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *co)
	}
	return out, rows.Err()
}

func (r *repository) listCompaniesPage(ctx context.Context, workspaceID, query, beforeName, beforeID string, limit int) ([]Company, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+companyColumns+`
		FROM companies c
		WHERE c.workspace_id = $1
		  AND ($2 = '' OR c.name ILIKE '%' || $2 || '%' OR c.domain::text ILIKE '%' || $2 || '%')
		  AND ($4 = '' OR (c.name, c.id) > ($3, $4))
		ORDER BY c.name, c.id
		LIMIT $5
	`, workspaceID, query, beforeName, beforeID, limit)
	if err != nil {
		return nil, fmt.Errorf("customer: list companies page: %w", err)
	}
	defer rows.Close()

	out := []Company{}
	for rows.Next() {
		co, err := scanCompany(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *co)
	}
	return out, rows.Err()
}

func (r *repository) updateCompany(ctx context.Context, workspaceID, id, name string, domain, externalID, tier, ownerID *string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE companies
		SET name = $3, domain = $4, external_id = $5, tier = $6, owner_id = $7, updated_at = now()
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id, name, domain, externalID, tier, ownerID)
	if err != nil {
		return fmt.Errorf("customer: update company: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrCompanyNotFound
	}
	return nil
}

func (r *repository) companyExists(ctx context.Context, workspaceID, id string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM companies WHERE workspace_id = $1 AND id = $2)
	`, workspaceID, id).Scan(&exists)
	return exists, err
}

func (r *repository) companyTagInWorkspace(ctx context.Context, workspaceID, tagID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM tags WHERE id = $1 AND workspace_id = $2)
	`, tagID, workspaceID).Scan(&exists)
	return exists, err
}

func (r *repository) addCompanyTag(ctx context.Context, companyID, tagID string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO company_tags (company_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING
	`, companyID, tagID)
	return err
}

func (r *repository) removeCompanyTag(ctx context.Context, companyID, tagID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM company_tags WHERE company_id = $1 AND tag_id = $2`, companyID, tagID)
	return err
}

func (r *repository) companyTagIDs(ctx context.Context, workspaceID, companyID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT ct.tag_id FROM company_tags ct
		JOIN companies c ON c.id = ct.company_id
		WHERE c.workspace_id = $1 AND ct.company_id = $2
	`, workspaceID, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// linkCustomer and unlinkCustomer manage the company_customers roster —
// which customers belong to this company (§6.9 account/company view).
func (r *repository) linkCustomer(ctx context.Context, companyID, customerID string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO company_customers (company_id, customer_id) VALUES ($1, $2) ON CONFLICT DO NOTHING
	`, companyID, customerID)
	return err
}

func (r *repository) unlinkCustomer(ctx context.Context, companyID, customerID string) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM company_customers WHERE company_id = $1 AND customer_id = $2
	`, companyID, customerID)
	return err
}

// companyCustomers returns the customers linked to companyID, for the
// company detail page's roster list.
func (r *repository) companyCustomers(ctx context.Context, workspaceID, companyID string, limit int) ([]Customer, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+customerColumns+`
		FROM customers
		WHERE workspace_id = $1 AND id IN (
			SELECT customer_id FROM company_customers WHERE company_id = $2
		)
		ORDER BY last_seen_at DESC NULLS LAST, first_seen_at DESC
		LIMIT $3
	`, workspaceID, companyID, limit)
	if err != nil {
		return nil, fmt.Errorf("customer: company customers: %w", err)
	}
	defer rows.Close()

	out := []Customer{}
	for rows.Next() {
		c, err := scanCustomer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- service

const entityCompany = "company"

// CreateCompany opens a new company record (§6.9 companies/accounts).
func (s *Service) CreateCompany(ctx context.Context, workspaceID, actorMemberID, name string, domain, externalID, tier *string) (*Company, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidCompanyName
	}

	id := ids.New(ids.PrefixCompany)
	var company *Company
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var insertErr error
		company, insertErr = s.repo.insertCompany(ctx, id, workspaceID, name, domain, externalID, tier)
		if insertErr != nil {
			if uniqueViolation(insertErr) {
				return ErrCompanyExternalID
			}
			return insertErr
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "company.created", EntityType: entityCompany, EntityID: id,
			Metadata: map[string]any{"name": name},
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: "company.created",
			EntityType: entityCompany, EntityID: id, ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"id": id, "name": name},
		})
	})
	if err != nil {
		return nil, err
	}
	return company, nil
}

func (s *Service) Company(ctx context.Context, workspaceID, id string) (*Company, error) {
	return s.repo.companyByID(ctx, workspaceID, id)
}

// FindCompanyByExternalID is the workspace-scoped lookup used by resumable
// company imports. External IDs are the stable conflict key; names and
// domains are mutable display data.
func (s *Service) FindCompanyByExternalID(ctx context.Context, workspaceID, externalID string) (*Company, error) {
	return s.repo.companyByExternalID(ctx, workspaceID, strings.TrimSpace(externalID))
}

func (s *Service) Companies(ctx context.Context, workspaceID string, ids []string) ([]Company, error) {
	return s.repo.companiesByIDs(ctx, workspaceID, ids)
}

func (s *Service) ListCompanies(ctx context.Context, workspaceID, query string, limit int) ([]Company, error) {
	if limit <= 0 || limit > 10000 {
		limit = 50
	}
	return s.repo.listCompanies(ctx, workspaceID, strings.TrimSpace(query), limit)
}

// ListCompaniesPage returns one cursor-ordered page for the dashboard
// directory. Company names sort ascending, with the id as a deterministic
// tiebreaker so concurrent inserts cannot repeat a page.
func (s *Service) ListCompaniesPage(ctx context.Context, workspaceID, query, beforeName, beforeID string, limit int) ([]Company, error) {
	if limit <= 0 || limit > 201 {
		limit = 50
	}
	return s.repo.listCompaniesPage(ctx, workspaceID, strings.TrimSpace(query), beforeName, beforeID, limit)
}

func (s *Service) UpdateCompany(
	ctx context.Context, workspaceID, actorMemberID, id, name string, domain, externalID, tier, ownerID *string,
) (*Company, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidCompanyName
	}
	if ownerID != nil {
		ok, err := s.repo.memberInWorkspace(ctx, workspaceID, *ownerID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrInvalidOwner
		}
	}

	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.updateCompany(ctx, workspaceID, id, name, domain, externalID, tier, ownerID); err != nil {
			if uniqueViolation(err) {
				return ErrCompanyExternalID
			}
			return err
		}
		return s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "company.updated", EntityType: entityCompany, EntityID: id,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repo.companyByID(ctx, workspaceID, id)
}

func (s *Service) AddCompanyTag(ctx context.Context, workspaceID, companyID, tagID string) error {
	ok, err := s.repo.companyTagInWorkspace(ctx, workspaceID, tagID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrTagNotFound
	}
	if _, err := s.repo.companyByID(ctx, workspaceID, companyID); err != nil {
		return err
	}
	return s.repo.addCompanyTag(ctx, companyID, tagID)
}

func (s *Service) RemoveCompanyTag(ctx context.Context, workspaceID, companyID, tagID string) error {
	if _, err := s.repo.companyByID(ctx, workspaceID, companyID); err != nil {
		return err
	}
	return s.repo.removeCompanyTag(ctx, companyID, tagID)
}

func (s *Service) CompanyTags(ctx context.Context, workspaceID, companyID string) ([]string, error) {
	return s.repo.companyTagIDs(ctx, workspaceID, companyID)
}

// LinkCustomer and UnlinkCustomer manage a company's customer roster —
// which customer records belong to this account.
func (s *Service) LinkCustomer(ctx context.Context, workspaceID, companyID, customerID string) error {
	if _, err := s.repo.companyByID(ctx, workspaceID, companyID); err != nil {
		return err
	}
	if _, err := s.repo.byID(ctx, workspaceID, customerID); err != nil {
		return err
	}
	return s.repo.linkCustomer(ctx, companyID, customerID)
}

func (s *Service) UnlinkCustomer(ctx context.Context, workspaceID, companyID, customerID string) error {
	return s.repo.unlinkCustomer(ctx, companyID, customerID)
}

func (s *Service) CompanyCustomers(ctx context.Context, workspaceID, companyID string, limit int) ([]Customer, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.List(ctx, workspaceID, ListFilter{CompanyID: companyID, Limit: limit})
}

// CompanyCustomersPage exposes the same stable customer-directory ordering for
// a company's roster, so large accounts do not stop at the first 50 contacts.
func (s *Service) CompanyCustomersPage(ctx context.Context, workspaceID, companyID string, before time.Time, beforeID string, limit int) ([]Customer, error) {
	return s.List(ctx, workspaceID, ListFilter{CompanyID: companyID, Before: before, BeforeID: beforeID, Limit: limit})
}

func (s *Service) companyInWorkspace(ctx context.Context, workspaceID, companyID string) (bool, error) {
	return s.repo.companyExists(ctx, workspaceID, companyID)
}
