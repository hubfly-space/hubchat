//go:build integration

package workspace_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/workspace"
)

func newTestService(t *testing.T, pool *database.Pool) *workspace.Service {
	t.Helper()
	return workspace.New(pool, events.New(pool), audit.New(pool))
}

// seedOwnerWorkspace creates a workspace with one owner, standing in for what
// Bootstrap produces during setup, without going through the auth module.
func seedOwnerWorkspace(t *testing.T, ctx context.Context, pool *database.Pool, svc *workspace.Service, ownerEmail string) (workspaceID, ownerUserID, ownerMemberID string) {
	t.Helper()

	ownerUserID = seedTestUser(t, ctx, pool, ownerEmail)
	// The slug must be unique per call. ids.New's first characters after the
	// prefix are a ULID timestamp, which two calls in the same test can share
	// down to the millisecond — slicing from there caused exactly that
	// collision. The tail of the id is the random component and does not.
	token := ids.New("t")
	ws, err := svc.Bootstrap(ctx, ownerUserID, "Acme", "acme-"+token[len(token)-10:])
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	actor, err := svc.ActorForUser(ctx, ws.ID, ownerUserID)
	if err != nil {
		t.Fatalf("resolve owner actor: %v", err)
	}
	return ws.ID, ownerUserID, actor.MemberID
}

func seedTestUser(t *testing.T, ctx context.Context, pool *database.Pool, email string) string {
	t.Helper()
	id := ids.New(ids.PrefixUser)
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, name, email, password_hash, email_verified_at)
		VALUES ($1, $2, $3, 'x', now())
	`, id, "Test User", email); err != nil {
		t.Fatalf("seed user %q: %v", email, err)
	}
	return id
}

func addMember(t *testing.T, ctx context.Context, pool *database.Pool, workspaceID, userID, role string) string {
	t.Helper()
	id := ids.New(ids.PrefixMember)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspace_members (id, workspace_id, user_id, role) VALUES ($1, $2, $3, $4)
	`, id, workspaceID, userID, role); err != nil {
		t.Fatalf("add member: %v", err)
	}
	return id
}

// ------------------------------------------------------------- capabilities

// This is the regression test for a real bug: ActorForUser's own repository
// comment promised extra_capabilities would be unioned in by the caller, and
// nothing did it. A per-member grant was silently ignored.
func TestActorForUserUnionsExtraCapabilities(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "owner@example.com")
	analystUserID := seedTestUser(t, ctx, pool, "analyst@example.com")
	analystMemberID := addMember(t, ctx, pool, workspaceID, analystUserID, "analyst")

	actor, err := svc.ActorForUser(ctx, workspaceID, analystUserID)
	if err != nil {
		t.Fatalf("resolve actor: %v", err)
	}
	if actor.Can(authorization.ConversationAssign) {
		t.Fatal("an analyst has conversation.assign by default; the fixture is wrong")
	}

	if err := svc.SetExtraCapabilities(ctx, workspaceID, ownerMemberID, analystMemberID,
		[]authorization.Capability{authorization.ConversationAssign}); err != nil {
		t.Fatalf("grant capability: %v", err)
	}

	actor, err = svc.ActorForUser(ctx, workspaceID, analystUserID)
	if err != nil {
		t.Fatalf("resolve actor after grant: %v", err)
	}
	if !actor.Can(authorization.ConversationAssign) {
		t.Fatal("granted capability was not honoured — extra_capabilities is not being unioned in")
	}

	// The role's own defaults must be untouched by the grant.
	if !actor.Can(authorization.ReportRead) {
		t.Fatal("granting one extra capability lost the analyst's role-default capabilities")
	}
}

// ----------------------------------------------------------------- roles

func TestSetMemberRoleCannotGrantOwnership(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "owner@example.com")
	agentUserID := seedTestUser(t, ctx, pool, "agent@example.com")
	agentMemberID := addMember(t, ctx, pool, workspaceID, agentUserID, "agent")

	err := svc.SetMemberRole(ctx, workspaceID, ownerMemberID, agentMemberID, "owner")
	if !errors.Is(err, workspace.ErrCannotDemoteOwner) {
		t.Fatalf("granting owner via SetMemberRole: got %v, want ErrCannotDemoteOwner", err)
	}
}

func TestSetMemberRoleCannotDemoteTheSoleOwner(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "owner@example.com")

	// The unique index only prevents a *second* owner; nothing at the schema
	// level stops the sole owner being demoted to zero owners. That has to be
	// caught here.
	err := svc.SetMemberRole(ctx, workspaceID, ownerMemberID, ownerMemberID, "admin")
	if !errors.Is(err, workspace.ErrCannotDemoteOwner) {
		t.Fatalf("demoting the sole owner: got %v, want ErrCannotDemoteOwner", err)
	}
}

func TestSetMemberRoleRejectsUnknownRole(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "owner@example.com")
	agentUserID := seedTestUser(t, ctx, pool, "agent@example.com")
	agentMemberID := addMember(t, ctx, pool, workspaceID, agentUserID, "agent")

	err := svc.SetMemberRole(ctx, workspaceID, ownerMemberID, agentMemberID, "superadmin")
	if !errors.Is(err, workspace.ErrInvalidRole) {
		t.Fatalf("got %v, want ErrInvalidRole", err)
	}
}

// ----------------------------------------------------------------- removal

func TestRemoveMemberCannotRemoveTheOwner(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "owner@example.com")
	adminUserID := seedTestUser(t, ctx, pool, "admin@example.com")
	adminMemberID := addMember(t, ctx, pool, workspaceID, adminUserID, "admin")

	err := svc.RemoveMember(ctx, workspaceID, adminMemberID, ownerMemberID)
	if !errors.Is(err, workspace.ErrLastOwner) {
		t.Fatalf("removing the owner: got %v, want ErrLastOwner", err)
	}
}

func TestRemoveMemberRefusesSelfRemoval(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceID, _, _ := seedOwnerWorkspace(t, ctx, pool, svc, "owner@example.com")
	adminUserID := seedTestUser(t, ctx, pool, "admin@example.com")
	adminMemberID := addMember(t, ctx, pool, workspaceID, adminUserID, "admin")

	err := svc.RemoveMember(ctx, workspaceID, adminMemberID, adminMemberID)
	if !errors.Is(err, workspace.ErrSelfRemoval) {
		t.Fatalf("got %v, want ErrSelfRemoval", err)
	}
}

func TestRemoveMemberActuallyRemovesAccess(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "owner@example.com")
	agentUserID := seedTestUser(t, ctx, pool, "agent@example.com")
	agentMemberID := addMember(t, ctx, pool, workspaceID, agentUserID, "agent")

	if err := svc.RemoveMember(ctx, workspaceID, ownerMemberID, agentMemberID); err != nil {
		t.Fatalf("remove member: %v", err)
	}

	if _, err := svc.ActorForUser(ctx, workspaceID, agentUserID); err == nil {
		t.Fatal("a removed member can still resolve an actor for this workspace")
	}
}

// -------------------------------------------------------------- invites

func TestInviteRoundTripCreatesMembership(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "owner@example.com")

	token, invite, err := svc.IssueInvite(ctx, workspaceID, ownerMemberID, "New.Agent@Example.com", "agent")
	if err != nil {
		t.Fatalf("issue invite: %v", err)
	}
	if invite.Email != "new.agent@example.com" {
		t.Fatalf("invite email not normalised: %q", invite.Email)
	}

	details, err := svc.LookupInvite(ctx, token)
	if err != nil {
		t.Fatalf("lookup invite: %v", err)
	}
	if details.Role != "agent" {
		t.Fatalf("got role %q, want agent", details.Role)
	}

	newUserID := seedTestUser(t, ctx, pool, "new.agent@example.com")
	ws, err := svc.RedeemInvite(ctx, token, newUserID, "new.agent@example.com")
	if err != nil {
		t.Fatalf("redeem invite: %v", err)
	}
	if ws.ID != workspaceID {
		t.Fatalf("redeemed into the wrong workspace: %s", ws.ID)
	}

	actor, err := svc.ActorForUser(ctx, workspaceID, newUserID)
	if err != nil {
		t.Fatalf("the invited user did not receive membership: %v", err)
	}
	if actor.Role != "agent" {
		t.Fatalf("got role %q, want agent", actor.Role)
	}
}

// A replayed invite token must not create a second membership or error in a
// way that looks like success.
func TestInviteTokenIsSingleUse(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "owner@example.com")
	token, _, err := svc.IssueInvite(ctx, workspaceID, ownerMemberID, "once@example.com", "agent")
	if err != nil {
		t.Fatalf("issue invite: %v", err)
	}

	userID := seedTestUser(t, ctx, pool, "once@example.com")
	if _, err := svc.RedeemInvite(ctx, token, userID, "once@example.com"); err != nil {
		t.Fatalf("first redeem: %v", err)
	}

	secondUserID := seedTestUser(t, ctx, pool, "second@example.com")
	if _, err := svc.RedeemInvite(ctx, token, secondUserID, "second@example.com"); !errors.Is(err, workspace.ErrInviteNotFound) {
		t.Fatalf("replayed token: got %v, want ErrInviteNotFound", err)
	}
}

// The invite names a specific address; redeeming it under a different account
// must be refused, or a forwarded invite link becomes a bearer token for
// "anyone gets a seat" (§6.9-adjacent: never grant on a weak signal).
func TestInviteCannotBeRedeemedByAnotherAddress(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "owner@example.com")
	token, _, err := svc.IssueInvite(ctx, workspaceID, ownerMemberID, "intended@example.com", "agent")
	if err != nil {
		t.Fatalf("issue invite: %v", err)
	}

	interloperID := seedTestUser(t, ctx, pool, "interloper@example.com")
	_, err = svc.RedeemInvite(ctx, token, interloperID, "interloper@example.com")
	if !errors.Is(err, workspace.ErrInviteEmailMismatch) {
		t.Fatalf("got %v, want ErrInviteEmailMismatch", err)
	}
}

func TestCannotInviteAnExistingMember(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceID, ownerUserID, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "owner@example.com")

	var email string
	if err := pool.QueryRow(ctx, `SELECT email::text FROM users WHERE id = $1`, ownerUserID).Scan(&email); err != nil {
		t.Fatalf("read owner email: %v", err)
	}

	_, _, err := svc.IssueInvite(ctx, workspaceID, ownerMemberID, email, "agent")
	if !errors.Is(err, workspace.ErrAlreadyMember) {
		t.Fatalf("got %v, want ErrAlreadyMember", err)
	}
}

func TestDuplicateInviteToTheSameAddressIsRefused(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "owner@example.com")

	if _, _, err := svc.IssueInvite(ctx, workspaceID, ownerMemberID, "pending@example.com", "agent"); err != nil {
		t.Fatalf("first invite: %v", err)
	}
	_, _, err := svc.IssueInvite(ctx, workspaceID, ownerMemberID, "pending@example.com", "manager")
	if !errors.Is(err, workspace.ErrInviteExists) {
		t.Fatalf("second invite to the same address: got %v, want ErrInviteExists", err)
	}
}

func TestRevokedInviteCannotBeRedeemed(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "owner@example.com")
	token, invite, err := svc.IssueInvite(ctx, workspaceID, ownerMemberID, "revoke-me@example.com", "agent")
	if err != nil {
		t.Fatalf("issue invite: %v", err)
	}

	if err := svc.RevokeInvite(ctx, workspaceID, ownerMemberID, invite.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	userID := seedTestUser(t, ctx, pool, "revoke-me@example.com")
	_, err = svc.RedeemInvite(ctx, token, userID, "revoke-me@example.com")
	if !errors.Is(err, workspace.ErrInviteNotFound) {
		t.Fatalf("redeeming a revoked invite: got %v, want ErrInviteNotFound", err)
	}
}

// §11.3/§11.6: an invite from one workspace must not be discoverable or
// redeemable against another.
func TestInviteRevocationIsScopedToItsWorkspace(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceA, _, ownerA := seedOwnerWorkspace(t, ctx, pool, svc, "owner-a@example.com")
	workspaceB, _, ownerB := seedOwnerWorkspace(t, ctx, pool, svc, "owner-b@example.com")

	_, invite, err := svc.IssueInvite(ctx, workspaceA, ownerA, "target@example.com", "agent")
	if err != nil {
		t.Fatalf("issue invite: %v", err)
	}

	// Workspace B's owner "revoking" workspace A's invite by guessing its id
	// is scoped out by the WHERE clause and affects nothing — a harmless
	// no-op, not an error, and specifically not a delete that reaches across
	// the tenant boundary.
	if err := svc.RevokeInvite(ctx, workspaceB, ownerB, invite.ID); err != nil {
		t.Fatalf("cross-tenant revoke attempt errored instead of no-op: %v", err)
	}

	invites, err := svc.ListInvites(ctx, workspaceA)
	if err != nil {
		t.Fatalf("list invites: %v", err)
	}
	if len(invites) != 1 || invites[0].AcceptedAt != nil {
		t.Fatalf("workspace A's invite was affected by a cross-tenant revoke attempt")
	}
}

// ------------------------------------------------------------------- teams

func TestTeamLifecycle(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "owner@example.com")
	agentUserID := seedTestUser(t, ctx, pool, "agent@example.com")
	agentMemberID := addMember(t, ctx, pool, workspaceID, agentUserID, "agent")

	team, err := svc.CreateTeam(ctx, workspaceID, ownerMemberID, "Support", nil, nil, "round_robin", []string{agentMemberID})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	if len(team.MemberIDs) != 1 || team.MemberIDs[0] != agentMemberID {
		t.Fatalf("team members = %v, want [%s]", team.MemberIDs, agentMemberID)
	}

	newUserID := seedTestUser(t, ctx, pool, "second@example.com")
	newMemberID := addMember(t, ctx, pool, workspaceID, newUserID, "agent")
	if err := svc.AddTeamMember(ctx, workspaceID, team.ID, newMemberID); err != nil {
		t.Fatalf("add team member: %v", err)
	}
	if err := svc.RemoveTeamMember(ctx, workspaceID, team.ID, agentMemberID); err != nil {
		t.Fatalf("remove team member: %v", err)
	}

	teams, err := svc.ListTeams(ctx, workspaceID)
	if err != nil {
		t.Fatalf("list teams: %v", err)
	}
	if len(teams) != 1 || len(teams[0].MemberIDs) != 1 || teams[0].MemberIDs[0] != newMemberID {
		t.Fatalf("unexpected team roster after add/remove: %+v", teams)
	}

	if err := svc.DeleteTeam(ctx, workspaceID, ownerMemberID, team.ID); err != nil {
		t.Fatalf("delete team: %v", err)
	}
	teams, err = svc.ListTeams(ctx, workspaceID)
	if err != nil {
		t.Fatalf("list teams after delete: %v", err)
	}
	if len(teams) != 0 {
		t.Fatalf("team still present after delete: %+v", teams)
	}
}

func TestDuplicateTeamNameIsRefused(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "owner@example.com")

	if _, err := svc.CreateTeam(ctx, workspaceID, ownerMemberID, "Support", nil, nil, "manual", nil); err != nil {
		t.Fatalf("first team: %v", err)
	}
	_, err := svc.CreateTeam(ctx, workspaceID, ownerMemberID, "Support", nil, nil, "manual", nil)
	if !errors.Is(err, workspace.ErrTeamNameTaken) {
		t.Fatalf("got %v, want ErrTeamNameTaken", err)
	}
}

// A team id from another workspace must not be addable-to via this one —
// otherwise a workspace could attach its own member to a stranger's team by
// guessing an id.
func TestAddTeamMemberIsScopedToItsWorkspace(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceA, _, ownerA := seedOwnerWorkspace(t, ctx, pool, svc, "owner-a@example.com")
	workspaceB, _, _ := seedOwnerWorkspace(t, ctx, pool, svc, "owner-b@example.com")

	teamA, err := svc.CreateTeam(ctx, workspaceA, ownerA, "Team A", nil, nil, "manual", nil)
	if err != nil {
		t.Fatalf("create team: %v", err)
	}

	bUserID := seedTestUser(t, ctx, pool, "b-member@example.com")
	bMemberID := addMember(t, ctx, pool, workspaceB, bUserID, "agent")

	err = svc.AddTeamMember(ctx, workspaceB, teamA.ID, bMemberID)
	if !errors.Is(err, workspace.ErrTeamNotFound) {
		t.Fatalf("cross-tenant add: got %v, want ErrTeamNotFound", err)
	}

	teamsA, _ := svc.ListTeams(ctx, workspaceA)
	if len(teamsA) != 1 || len(teamsA[0].MemberIDs) != 0 {
		t.Fatalf("cross-tenant add leaked into workspace A's team: %+v", teamsA)
	}
}
