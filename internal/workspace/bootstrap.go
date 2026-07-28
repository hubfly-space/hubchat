package workspace

import (
	"context"
	"fmt"
	"time"

	"github.com/hubchat/hubchat/internal/authorization"
)

// Bootstrap is everything the dashboard needs before it can render anything.
//
// One endpoint rather than six, because on a cold load the alternative is a
// waterfall: fetch the workspace, then the viewer, then the members the viewer
// list needs, then the teams the member rows reference. Each round trip is a
// frame the agent spends looking at a skeleton, and none of these queries is
// expensive enough to justify its own request.
type Bootstrap struct {
	Workspace  Workspace
	Workspaces []WorkspaceSummary
	Viewer     Viewer
	Members    []MemberProfile
	Teams      []Team
	Tags       []Tag
	Inboxes    []Inbox
}

// WorkspaceSummary is one entry in the workspace switcher.
type WorkspaceSummary struct {
	ID   string
	Name string
	Slug string
	Role string
}

// Viewer is the signed-in member, with the capability set the UI uses to
// decide which controls to render.
//
// The capabilities travel to the browser deliberately, and it is worth being
// explicit about why that is safe: they are an affordance, not a boundary.
// §11.3 re-checks every one of them in the service layer. Sending them lets
// the interface hide a button the server would reject, which is politeness;
// it never lets the interface grant anything.
type Viewer struct {
	MemberID     string
	UserID       string
	Name         string
	Email        string
	AvatarURL    *string
	Role         string
	Capabilities []string
}

// MemberProfile is a workspace member as the directory and assignee pickers
// need them.
type MemberProfile struct {
	ID        string
	UserID    string
	Name      string
	Email     string
	AvatarURL *string
	Role      string
	Presence   string
	Accepting  bool
	TeamIDs    []string
	LastSeenAt *time.Time
	CreatedAt  time.Time
}

type Team struct {
	ID              string
	Name            string
	Description     *string
	LeadID          *string
	RoutingStrategy string
	MemberIDs       []string
	CreatedAt       time.Time
}

type Tag struct {
	ID    string
	Name  string
	Color int
	// UsageCount is populated by ListTags (the standalone tags-settings
	// query); the bootstrap payload's own listTags below leaves it zero,
	// since the opening screen has no use for it and computing it there
	// would cost every cold load a join over six tables for a number nothing
	// on that screen displays.
	UsageCount int
}

// LoadBootstrap assembles the dashboard's opening payload for one member.
//
// Every query here is workspace-scoped except the workspace list, which is
// scoped to the user's memberships — the two scopes a tenant boundary has
// (§11.3).
func (s *Service) LoadBootstrap(ctx context.Context, workspaceID, userID string) (*Bootstrap, error) {
	actor, err := s.ActorForUser(ctx, workspaceID, userID)
	if err != nil {
		return nil, err
	}

	ws, err := s.repo.byID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	viewer, err := s.repo.viewerProfile(ctx, workspaceID, userID)
	if err != nil {
		return nil, err
	}
	viewer.Capabilities = capabilityNames(actor)

	workspaces, err := s.repo.listForUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	members, err := s.repo.listMembers(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	teams, err := s.repo.listTeams(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	tags, err := s.repo.listTags(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	inboxes, err := s.repo.listInboxes(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	return &Bootstrap{
		Workspace:  *ws,
		Workspaces: workspaces,
		Viewer:     *viewer,
		Members:    members,
		Teams:      teams,
		Tags:       tags,
		Inboxes:    inboxes,
	}, nil
}

// capabilityNames flattens the actor's capability set for the wire.
//
// The owner role is expanded here rather than sent as a flag. Actor.Can
// short-circuits owners in Go, but the browser has no such rule — sending
// `role: "owner"` and expecting every `can()` call site to remember it is how
// one screen ends up hiding a control from the person who owns the workspace.
func capabilityNames(actor *authorization.Actor) []string {
	if actor.Role == "owner" {
		return authorization.AllCapabilityNames()
	}

	names := make([]string, 0, len(actor.Capabilities))
	for capability, granted := range actor.Capabilities {
		if granted {
			names = append(names, string(capability))
		}
	}
	return names
}

func (r *repository) viewerProfile(ctx context.Context, workspaceID, userID string) (*Viewer, error) {
	var v Viewer
	err := r.pool.QueryRow(ctx, `
		SELECT m.id, u.id, u.name, u.email::text, u.avatar_url, m.role
		FROM workspace_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.workspace_id = $1 AND m.user_id = $2
	`, workspaceID, userID).Scan(
		&v.MemberID, &v.UserID, &v.Name, &v.Email, &v.AvatarURL, &v.Role,
	)
	if err != nil {
		return nil, fmt.Errorf("workspace: load viewer: %w", err)
	}
	return &v, nil
}

func (r *repository) listForUser(ctx context.Context, userID string) ([]WorkspaceSummary, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT w.id, w.name, w.slug::text, m.role
		FROM workspace_members m
		JOIN workspaces w ON w.id = m.workspace_id
		WHERE m.user_id = $1
		ORDER BY m.created_at
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("workspace: list for user: %w", err)
	}
	defer rows.Close()

	out := []WorkspaceSummary{}
	for rows.Next() {
		var s WorkspaceSummary
		if err := rows.Scan(&s.ID, &s.Name, &s.Slug, &s.Role); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// listMembers returns the workspace directory with team membership folded in.
//
// The team ids come from an aggregate rather than a second query and a join in
// Go: a member list of any size would otherwise be N+1, and the assignee
// picker opens on every conversation.
func (r *repository) listMembers(ctx context.Context, workspaceID string) ([]MemberProfile, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT m.id, u.id, u.name, u.email::text, u.avatar_url, m.role,
		       m.presence, m.accepting_conversations, m.last_seen_at, m.created_at,
		       coalesce(
		           array_agg(tm.team_id) FILTER (WHERE tm.team_id IS NOT NULL),
		           '{}'
		       ) AS team_ids
		FROM workspace_members m
		JOIN users u ON u.id = m.user_id
		LEFT JOIN team_members tm ON tm.member_id = m.id
		WHERE m.workspace_id = $1
		GROUP BY m.id, u.id, u.name, u.email, u.avatar_url, m.role,
		         m.presence, m.accepting_conversations, m.last_seen_at, m.created_at
		ORDER BY u.name
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("workspace: list members: %w", err)
	}
	defer rows.Close()

	out := []MemberProfile{}
	for rows.Next() {
		var m MemberProfile
		if err := rows.Scan(
			&m.ID, &m.UserID, &m.Name, &m.Email, &m.AvatarURL, &m.Role,
			&m.Presence, &m.Accepting, &m.LastSeenAt, &m.CreatedAt, &m.TeamIDs,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *repository) listTeams(ctx context.Context, workspaceID string) ([]Team, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT t.id, t.name, t.description, t.lead_id, t.routing_strategy, t.created_at,
		       coalesce(
		           array_agg(tm.member_id) FILTER (WHERE tm.member_id IS NOT NULL),
		           '{}'
		       ) AS member_ids
		FROM teams t
		LEFT JOIN team_members tm ON tm.team_id = t.id
		WHERE t.workspace_id = $1
		GROUP BY t.id, t.name, t.description, t.lead_id, t.routing_strategy, t.created_at
		ORDER BY t.name
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("workspace: list teams: %w", err)
	}
	defer rows.Close()

	out := []Team{}
	for rows.Next() {
		var t Team
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.LeadID,
			&t.RoutingStrategy, &t.CreatedAt, &t.MemberIDs); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *repository) listTags(ctx context.Context, workspaceID string) ([]Tag, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name::text, color
		FROM tags
		WHERE workspace_id = $1
		ORDER BY name
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("workspace: list tags: %w", err)
	}
	defer rows.Close()

	out := []Tag{}
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Color); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// WorkspaceIDsForUser returns every workspace the user is a member of.
//
// Used by account-level auditing: `audit_logs` is workspace-scoped, but
// changing a password is not, so the entry is written to each workspace whose
// administrators could legitimately need to see it.
func (s *Service) WorkspaceIDsForUser(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.repo.pool.Query(ctx, `
		SELECT workspace_id FROM workspace_members WHERE user_id = $1
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("workspace: list memberships: %w", err)
	}
	defer rows.Close()

	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
