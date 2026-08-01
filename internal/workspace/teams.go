package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
)

var (
	ErrTeamNotFound           = errors.New("workspace: team not found")
	ErrInvalidTeamName        = errors.New("workspace: team name is required")
	ErrInvalidRoutingStrategy = errors.New("workspace: not a recognised routing strategy")
)

// validRoutingStrategies mirrors the CHECK constraint on teams.routing_strategy
// (migration 0001).
var validRoutingStrategies = map[string]bool{
	"manual": true, "round_robin": true, "least_active": true,
	"team_queue": true, "customer_owner": true, "weighted": true,
}

// CreateTeam creates a team with an initial set of members.
func (s *Service) CreateTeam(
	ctx context.Context, workspaceID, actorMemberID, name string,
	description *string, leadID *string, routingStrategy string, memberIDs []string,
) (*Team, error) {
	return s.CreateTeamWithRouting(ctx, workspaceID, actorMemberID, name, description, leadID, routingStrategy, memberIDs, nil)
}

func (s *Service) CreateTeamWithRouting(
	ctx context.Context, workspaceID, actorMemberID, name string,
	description *string, leadID *string, routingStrategy string, memberIDs []string, routingConfig map[string]any,
) (*Team, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidTeamName
	}
	if routingStrategy == "" {
		routingStrategy = "manual"
	}
	if !validRoutingStrategies[routingStrategy] {
		return nil, ErrInvalidRoutingStrategy
	}

	id := ids.New(ids.PrefixTeam)

	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.insertTeam(ctx, tx, id, workspaceID, name, description, leadID, routingStrategy, routingConfig); err != nil {
			return err
		}
		for _, memberID := range memberIDs {
			if err := s.repo.insertTeamMember(ctx, tx, id, memberID); err != nil {
				return err
			}
		}

		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "team.created", EntityType: "team", EntityID: id,
			Metadata: map[string]any{"name": name},
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.TeamCreated,
			EntityType: "team", EntityID: id, ActorType: events.ActorUser, ActorID: actorMemberID,
		})
	})
	if err != nil {
		if errors.Is(err, errUniqueTeamName) {
			return nil, ErrTeamNameTaken
		}
		return nil, err
	}

	return s.repo.teamByID(ctx, workspaceID, id)
}

// ErrTeamNameTaken is returned when a team name collides within a workspace.
var ErrTeamNameTaken = errors.New("workspace: a team with this name already exists")

// UpdateTeam changes a team's descriptive fields and routing strategy.
// Membership is managed separately through AddTeamMember/RemoveTeamMember,
// since adding one person to a team of fifty should not require resubmitting
// the other forty-nine.
func (s *Service) UpdateTeam(
	ctx context.Context, workspaceID, actorMemberID, teamID, name string,
	description *string, leadID *string, routingStrategy string,
) (*Team, error) {
	return s.UpdateTeamWithRouting(ctx, workspaceID, actorMemberID, teamID, name, description, leadID, routingStrategy, nil)
}

func (s *Service) UpdateTeamWithRouting(
	ctx context.Context, workspaceID, actorMemberID, teamID, name string,
	description *string, leadID *string, routingStrategy string, routingConfig map[string]any,
) (*Team, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidTeamName
	}
	if !validRoutingStrategies[routingStrategy] {
		return nil, ErrInvalidRoutingStrategy
	}

	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.updateTeam(ctx, tx, workspaceID, teamID, name, description, leadID, routingStrategy, routingConfig); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "team.updated", EntityType: "team", EntityID: teamID,
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.TeamUpdated,
			EntityType: "team", EntityID: teamID, ActorType: events.ActorUser, ActorID: actorMemberID,
		})
	})
	if err != nil {
		if errors.Is(err, errUniqueTeamName) {
			return nil, ErrTeamNameTaken
		}
		return nil, err
	}

	return s.repo.teamByID(ctx, workspaceID, teamID)
}

// DeleteTeam removes a team. Conversations and inboxes referencing it are
// left with a null team, per the SET NULL foreign keys migration 0002
// declares — a deleted team un-assigns its work rather than orphaning rows
// that would otherwise reference a nonexistent id.
func (s *Service) DeleteTeam(ctx context.Context, workspaceID, actorMemberID, teamID string) error {
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.deleteTeam(ctx, tx, workspaceID, teamID); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "team.deleted", EntityType: "team", EntityID: teamID,
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.TeamDeleted,
			EntityType: "team", EntityID: teamID, ActorType: events.ActorUser, ActorID: actorMemberID,
		})
	})
}

// AddTeamMember and RemoveTeamMember manage one membership at a time.
// Idempotent: adding an existing member or removing an absent one succeeds
// silently, because the caller's desired end state ("this person is on the
// team" / "this person is not") already holds either way.
func (s *Service) AddTeamMember(ctx context.Context, workspaceID, teamID, memberID string) error {
	return s.repo.insertTeamMemberScoped(ctx, workspaceID, teamID, memberID)
}

func (s *Service) RemoveTeamMember(ctx context.Context, workspaceID, teamID, memberID string) error {
	return s.repo.deleteTeamMemberScoped(ctx, workspaceID, teamID, memberID)
}

// ListTeams is already available via the bootstrap query at
// repository.listTeams; exposed here as a standalone call for the teams
// settings screen so it does not have to load the whole bootstrap payload to
// refresh one list.
func (s *Service) ListTeams(ctx context.Context, workspaceID string) ([]Team, error) {
	return s.repo.listTeams(ctx, workspaceID)
}

// ListTeamsPage keeps the team directory bounded while preserving the same
// alphabetical order as ListTeams. The id tiebreaker makes duplicate names
// deterministic even though names are normally unique within a workspace.
func (s *Service) ListTeamsPage(ctx context.Context, workspaceID, beforeName, beforeID string, limit int) ([]Team, error) {
	return s.repo.listTeamsPage(ctx, workspaceID, beforeName, beforeID, limit)
}

// ---------------------------------------------------------------- repository

var errUniqueTeamName = errors.New("workspace: duplicate team name")

func (r *repository) insertTeam(
	ctx context.Context, tx pgx.Tx, id, workspaceID, name string, description, leadID *string, routingStrategy string, routingConfig map[string]any,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO teams (id, workspace_id, name, description, lead_id, routing_strategy, routing_config)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, id, workspaceID, name, description, leadID, routingStrategy, routingJSON(routingConfig))
	if err != nil && isUniqueViolation(err) {
		return errUniqueTeamName
	}
	if err != nil {
		return fmt.Errorf("workspace: insert team: %w", err)
	}
	return nil
}

func (r *repository) updateTeam(
	ctx context.Context, tx pgx.Tx, workspaceID, teamID, name string, description, leadID *string, routingStrategy string, routingConfig map[string]any,
) error {
	tag, err := tx.Exec(ctx, `
		UPDATE teams SET name = $3, description = $4, lead_id = $5, routing_strategy = $6,
			routing_config = CASE WHEN $7::jsonb IS NULL THEN routing_config ELSE $7::jsonb END
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, teamID, name, description, leadID, routingStrategy, nullableRoutingJSON(routingConfig))
	if err != nil {
		if isUniqueViolation(err) {
			return errUniqueTeamName
		}
		return fmt.Errorf("workspace: update team: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTeamNotFound
	}
	return nil
}

func (r *repository) deleteTeam(ctx context.Context, tx pgx.Tx, workspaceID, teamID string) error {
	tag, err := tx.Exec(ctx,
		`DELETE FROM teams WHERE workspace_id = $1 AND id = $2`, workspaceID, teamID)
	if err != nil {
		return fmt.Errorf("workspace: delete team: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTeamNotFound
	}
	return nil
}

func (r *repository) teamByID(ctx context.Context, workspaceID, teamID string) (*Team, error) {
	var t Team
	err := r.pool.QueryRow(ctx, `
		SELECT t.id, t.name, t.description, t.lead_id, t.routing_strategy, t.routing_config, t.created_at,
		       coalesce(
		           array_agg(tm.member_id) FILTER (WHERE tm.member_id IS NOT NULL),
		           '{}'
		       )
		FROM teams t
		LEFT JOIN team_members tm ON tm.team_id = t.id
		WHERE t.workspace_id = $1 AND t.id = $2
		GROUP BY t.id, t.name, t.description, t.lead_id, t.routing_strategy, t.created_at
	`, workspaceID, teamID).Scan(&t.ID, &t.Name, &t.Description, &t.LeadID, &t.RoutingStrategy, &t.RoutingConfig, &t.CreatedAt, &t.MemberIDs)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTeamNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("workspace: load team: %w", err)
	}
	if t.RoutingConfig == nil {
		t.RoutingConfig = map[string]any{}
	}
	return &t, nil
}

func routingJSON(value map[string]any) []byte {
	if value == nil {
		value = map[string]any{}
	}
	data, _ := json.Marshal(value)
	return data
}

func nullableRoutingJSON(value map[string]any) []byte {
	if value == nil {
		return nil
	}
	return routingJSON(value)
}

func (r *repository) insertTeamMember(ctx context.Context, tx pgx.Tx, teamID, memberID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO team_members (team_id, member_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, teamID, memberID)
	if err != nil {
		return fmt.Errorf("workspace: add team member: %w", err)
	}
	return nil
}

// insertTeamMemberScoped verifies the team belongs to workspaceID before
// inserting, so a caller cannot add a member to a team in another tenant by
// guessing its id (§11.3).
func (r *repository) insertTeamMemberScoped(ctx context.Context, workspaceID, teamID, memberID string) error {
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO team_members (team_id, member_id)
		SELECT $2, $3
		FROM teams WHERE id = $2 AND workspace_id = $1
		ON CONFLICT DO NOTHING
	`, workspaceID, teamID, memberID)
	if err != nil {
		return fmt.Errorf("workspace: add team member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Either the team does not exist in this workspace, or the member was
		// already on it. Distinguishing the two costs a second query for a
		// case the caller treats identically either way — see ErrTeamNotFound
		// below for when that distinction does matter.
		if exists, err := r.teamExists(ctx, workspaceID, teamID); err != nil {
			return err
		} else if !exists {
			return ErrTeamNotFound
		}
	}
	return nil
}

func (r *repository) deleteTeamMemberScoped(ctx context.Context, workspaceID, teamID, memberID string) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM team_members
		WHERE team_id = $2 AND member_id = $3
		  AND team_id IN (SELECT id FROM teams WHERE workspace_id = $1)
	`, workspaceID, teamID, memberID)
	if err != nil {
		return fmt.Errorf("workspace: remove team member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if exists, err := r.teamExists(ctx, workspaceID, teamID); err != nil {
			return err
		} else if !exists {
			return ErrTeamNotFound
		}
	}
	return nil
}

func (r *repository) teamExists(ctx context.Context, workspaceID, teamID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM teams WHERE workspace_id = $1 AND id = $2)`,
		workspaceID, teamID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("workspace: check team exists: %w", err)
	}
	return exists, nil
}
