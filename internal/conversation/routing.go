package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/events"
)

type routingMember struct {
	id       string
	weight   int
	assigned int
}

type routingTeam struct {
	id       string
	strategy string
	config   map[string]any
}

// routeNewConversation applies the inbox's first configured team strategy.
// The team row is locked for the duration of the transaction so simultaneous
// starts cannot make round-robin or weighted decisions from the same snapshot.
// A team is still assigned when no member is eligible; that preserves a
// visible queue instead of silently dropping work.
func (s *Service) routeNewConversation(ctx context.Context, tx pgx.Tx, workspaceID, conversationID, inboxID string, customerID *string) error {
	teams, err := loadRoutingTeams(ctx, tx, workspaceID, inboxID)
	if err != nil {
		return err
	}
	if len(teams) == 0 {
		return nil
	}
	team := teams[0]
	for _, candidate := range teams {
		matches, err := routingTeamMatches(ctx, tx, workspaceID, customerID, candidate.config)
		if err != nil {
			return err
		}
		if matches {
			team = candidate
			break
		}
	}
	teamID, strategy := team.id, team.strategy

	/*
		The team row is locked by the query below. This is deliberately separate
		from loading the candidates so a concurrent start cannot consume the same
		round-robin cursor.
	*/
	if err := tx.QueryRow(ctx, `
		SELECT team_id
		FROM inbox_teams it
		WHERE it.inbox_id=$1 AND it.team_id=$2
	`, inboxID, teamID).Scan(new(string)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT id FROM teams WHERE id=$1 FOR UPDATE`, teamID); err != nil {
		return err
	}

	var assignee *string
	if strategy == "customer_owner" && customerID != nil {
		var owner *string
		if err := tx.QueryRow(ctx, `SELECT owner_id FROM customers WHERE workspace_id=$1 AND id=$2`, workspaceID, *customerID).Scan(&owner); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if owner != nil {
			var eligible bool
			if err := tx.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM team_members tm
					JOIN workspace_members wm ON wm.id=tm.member_id
					WHERE tm.team_id=$1 AND tm.member_id=$2 AND wm.workspace_id=$3 AND wm.accepting_conversations
				)
			`, teamID, *owner, workspaceID).Scan(&eligible); err != nil {
				return err
			}
			if eligible {
				assignee = owner
			}
		}
	} else if strategy != "manual" && strategy != "team_queue" {
		members, err := loadRoutingMembers(ctx, tx, workspaceID, teamID)
		if err != nil {
			return err
		}
		if len(members) > 0 {
			if strategy == "round_robin" {
				selected, err := consumeRoundRobin(ctx, tx, teamID, len(members))
				if err != nil {
					return err
				}
				assignee = &members[selected].id
			} else {
				selected := chooseRoutingMember(strategy, conversationID, members)
				assignee = &selected
			}
		}
	}

	assignedTeamID := teamID
	if err := s.repo.setTeam(ctx, tx, conversationID, &assignedTeamID); err != nil {
		return err
	}
	if assignee != nil {
		if err := s.repo.setAssignee(ctx, tx, conversationID, assignee); err != nil {
			return err
		}
	}
	if err := s.recordAudit(ctx, tx, audit.Entry{
		WorkspaceID: workspaceID, ActorType: audit.ActorSystem,
		Action: "conversation.assigned", EntityType: entityConversation, EntityID: conversationID,
		Metadata: map[string]any{"team_id": teamID, "assignee_id": derefOr(assignee, ""), "strategy": strategy},
	}); err != nil {
		return err
	}
	return s.appendEvent(ctx, tx, events.Event{
		WorkspaceID: workspaceID, Type: events.ConversationAssigned,
		EntityType: entityConversation, EntityID: conversationID, ActorType: events.ActorSystem,
		Data: map[string]any{"conversation_id": conversationID, "team_id": teamID, "assignee_id": assignee, "strategy": strategy},
	})
}

func loadRoutingTeams(ctx context.Context, tx pgx.Tx, workspaceID, inboxID string) ([]routingTeam, error) {
	rows, err := tx.Query(ctx, `
		SELECT t.id, t.routing_strategy, t.routing_config
		FROM inbox_teams it JOIN teams t ON t.id=it.team_id AND t.workspace_id=$1
		WHERE it.inbox_id=$2 ORDER BY t.created_at, t.id
	`, workspaceID, inboxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var teams []routingTeam
	for rows.Next() {
		var team routingTeam
		var raw []byte
		if err := rows.Scan(&team.id, &team.strategy, &raw); err != nil {
			return nil, err
		}
		team.config = map[string]any{}
		if len(raw) > 0 && json.Unmarshal(raw, &team.config) != nil {
			team.config = map[string]any{}
		}
		teams = append(teams, team)
	}
	return teams, rows.Err()
}

func routingTeamMatches(ctx context.Context, tx pgx.Tx, workspaceID string, customerID *string, config map[string]any) (bool, error) {
	if len(config) == 0 || customerID == nil {
		return len(config) == 0, nil
	}
	var language string
	var attributes []byte
	if err := tx.QueryRow(ctx, `SELECT coalesce(language,''), coalesce(attributes,'{}'::jsonb) FROM customers WHERE workspace_id=$1 AND id=$2`, workspaceID, *customerID).Scan(&language, &attributes); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	var customerAttributes map[string]any
	if json.Unmarshal(attributes, &customerAttributes) != nil {
		customerAttributes = map[string]any{}
	}
	if raw, ok := config["languages"].([]any); ok && len(raw) > 0 {
		matched := false
		for _, item := range raw {
			if value, ok := item.(string); ok && strings.EqualFold(strings.TrimSpace(value), language) {
				matched = true
				break
			}
		}
		if !matched {
			return false, nil
		}
	}
	if raw, ok := config["attributes"].(map[string]any); ok {
		for key, expected := range raw {
			actual, exists := customerAttributes[key]
			if !exists || !attributeMatches(actual, expected) {
				return false, nil
			}
		}
	}
	return true, nil
}

func attributeMatches(actual, expected any) bool {
	values := []any{expected}
	if list, ok := expected.([]any); ok {
		values = list
	}
	for _, value := range values {
		if fmt.Sprint(actual) == fmt.Sprint(value) {
			return true
		}
	}
	return false
}

func consumeRoundRobin(ctx context.Context, tx pgx.Tx, teamID string, memberCount int) (int, error) {
	var next int64
	err := tx.QueryRow(ctx, `SELECT next_member_index FROM team_routing_cursors WHERE team_id=$1 FOR UPDATE`, teamID).Scan(&next)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := tx.Exec(ctx, `INSERT INTO team_routing_cursors (team_id, next_member_index) VALUES ($1, 0) ON CONFLICT DO NOTHING`, teamID); err != nil {
			return 0, err
		}
		if err := tx.QueryRow(ctx, `SELECT next_member_index FROM team_routing_cursors WHERE team_id=$1 FOR UPDATE`, teamID).Scan(&next); err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, err
	}
	selected := int(next % int64(memberCount))
	if _, err := tx.Exec(ctx, `UPDATE team_routing_cursors SET next_member_index=next_member_index+1, updated_at=now() WHERE team_id=$1`, teamID); err != nil {
		return 0, err
	}
	return selected, nil
}

func loadRoutingMembers(ctx context.Context, tx pgx.Tx, workspaceID, teamID string) ([]routingMember, error) {
	rows, err := tx.Query(ctx, `
		SELECT tm.member_id, greatest(tm.weight, 1), count(c.id)::int
		FROM team_members tm
		JOIN workspace_members wm ON wm.id=tm.member_id AND wm.workspace_id=$1 AND wm.accepting_conversations
		LEFT JOIN conversations c ON c.team_id=tm.team_id AND c.assignee_id=tm.member_id AND c.state NOT IN ('spam')
		WHERE tm.team_id=$2
		GROUP BY tm.member_id, tm.weight
		ORDER BY tm.member_id
	`, workspaceID, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []routingMember
	for rows.Next() {
		var member routingMember
		if err := rows.Scan(&member.id, &member.weight, &member.assigned); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func chooseRoutingMember(strategy, conversationID string, members []routingMember) string {
	if strategy == "round_robin" || strategy == "least_active" {
		copyMembers := append([]routingMember(nil), members...)
		sort.Slice(copyMembers, func(i, j int) bool {
			if copyMembers[i].assigned != copyMembers[j].assigned {
				return copyMembers[i].assigned < copyMembers[j].assigned
			}
			return copyMembers[i].id < copyMembers[j].id
		})
		return copyMembers[0].id
	}
	if strategy == "weighted" {
		copyMembers := append([]routingMember(nil), members...)
		sort.Slice(copyMembers, func(i, j int) bool {
			left := copyMembers[i].assigned * copyMembers[j].weight
			right := copyMembers[j].assigned * copyMembers[i].weight
			if left != right {
				return left < right
			}
			return copyMembers[i].id < copyMembers[j].id
		})
		return copyMembers[0].id
	}
	// Unknown strategies are rejected when teams are written. The stable
	// fallback keeps a malformed legacy row in a deterministic queue.
	return members[stableIndex(conversationID, len(members))].id
}

func stableIndex(value string, length int) int {
	if length <= 1 {
		return 0
	}
	var hash uint32 = 2166136261
	for i := 0; i < len(value); i++ {
		hash ^= uint32(value[i])
		hash *= 16777619
	}
	return int(hash % uint32(length))
}
