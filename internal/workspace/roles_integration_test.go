//go:build integration

package workspace_test

import (
	"errors"
	"testing"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/workspace"
)

func TestCustomRoleLifecycleIsWorkspaceScopedAndDrivesAuthorization(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)
	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "roles-owner@example.com")

	description := "Handles billing without changing configuration."
	role, err := svc.CreateRole(ctx, workspaceID, ownerMemberID, workspace.RoleInput{
		Key: "billing_specialist", Name: "Billing specialist", Description: &description,
		Capabilities: []authorization.Capability{authorization.CustomerRead, authorization.ConversationRead},
	})
	if err != nil {
		t.Fatalf("create custom role: %v", err)
	}
	if role.IsBuiltin || role.WorkspaceID != workspaceID || role.Key != "billing_specialist" {
		t.Fatalf("unexpected custom role: %+v", role)
	}

	roles, err := svc.ListRoles(ctx, workspaceID)
	if err != nil {
		t.Fatalf("list roles: %v", err)
	}
	found := false
	for _, item := range roles {
		if item.ID == role.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("workspace role was not returned by the role catalog")
	}
	inviteToken, invite, err := svc.IssueInvite(ctx, workspaceID, ownerMemberID, "billing-invite@example.com", role.Key)
	if err != nil || invite == nil || inviteToken == "" {
		t.Fatalf("custom-role invite: invite=%+v token=%q err=%v", invite, inviteToken, err)
	}
	if err := svc.DeleteRole(ctx, workspaceID, ownerMemberID, role.ID); !errors.Is(err, workspace.ErrRoleInUse) {
		t.Fatalf("delete role with pending invite = %v, want ErrRoleInUse", err)
	}
	if err := svc.RevokeInvite(ctx, workspaceID, ownerMemberID, invite.ID); err != nil {
		t.Fatalf("revoke custom-role invite: %v", err)
	}

	memberUserID := seedTestUser(t, ctx, pool, "billing-member@example.com")
	memberID := addMember(t, ctx, pool, workspaceID, memberUserID, "billing_specialist")
	actor, err := svc.ActorForUser(ctx, workspaceID, memberUserID)
	if err != nil {
		t.Fatalf("resolve custom-role actor: %v", err)
	}
	if !actor.Can(authorization.CustomerRead) || actor.Can(authorization.ConversationReply) {
		t.Fatalf("custom capabilities were not applied: %+v", actor.Capabilities)
	}

	updated, err := svc.UpdateRole(ctx, workspaceID, ownerMemberID, role.ID, workspace.RoleUpdateInput{
		Name: "Billing lead", Capabilities: []authorization.Capability{authorization.CustomerRead, authorization.ConversationRead, authorization.ConversationReply},
	})
	if err != nil {
		t.Fatalf("update custom role: %v", err)
	}
	if updated.Name != "Billing lead" || len(updated.Capabilities) != 3 {
		t.Fatalf("unexpected updated role: %+v", updated)
	}
	actor, err = svc.ActorForUser(ctx, workspaceID, memberUserID)
	if err != nil || !actor.Can(authorization.ConversationReply) {
		t.Fatalf("updated capabilities not visible to member: actor=%+v err=%v", actor, err)
	}

	if err := svc.DeleteRole(ctx, workspaceID, ownerMemberID, role.ID); !errors.Is(err, workspace.ErrRoleInUse) {
		t.Fatalf("delete assigned role = %v, want ErrRoleInUse", err)
	}
	if err := svc.RemoveMember(ctx, workspaceID, ownerMemberID, memberID); err != nil {
		t.Fatalf("remove role member: %v", err)
	}
	if err := svc.DeleteRole(ctx, workspaceID, ownerMemberID, role.ID); err != nil {
		t.Fatalf("delete unused role: %v", err)
	}

	otherWorkspaceID, _, otherOwnerID := seedOwnerWorkspace(t, ctx, pool, svc, "roles-other-owner@example.com")
	if _, err := svc.CreateRole(ctx, otherWorkspaceID, otherOwnerID, workspace.RoleInput{Key: "billing_specialist", Name: "Other billing", Capabilities: []authorization.Capability{authorization.ReportRead}}); err != nil {
		t.Fatalf("same key in another workspace: %v", err)
	}
	if err := svc.DeleteRole(ctx, otherWorkspaceID, otherOwnerID, role.ID); !errors.Is(err, workspace.ErrRoleNotFound) {
		t.Fatalf("cross-workspace role delete = %v", err)
	}
}

func TestCustomRoleValidationRejectsReservedAndUnknownCapabilities(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)
	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "roles-validation-owner@example.com")

	for name, input := range map[string]workspace.RoleInput{
		"reserved":           {Key: "admin", Name: "Not admin"},
		"invalid capability": {Key: "custom_ops", Name: "Custom", Capabilities: []authorization.Capability{"not.real"}},
		"invalid key":        {Key: "Bad Key", Name: "Bad"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.CreateRole(ctx, workspaceID, ownerMemberID, input)
			if name == "reserved" && !errors.Is(err, workspace.ErrRoleKeyReserved) {
				t.Fatalf("error = %v, want reserved key", err)
			}
			if name == "invalid capability" && !errors.Is(err, workspace.ErrInvalidCapability) {
				t.Fatalf("error = %v, want invalid capability", err)
			}
			if name == "invalid key" && !errors.Is(err, workspace.ErrRoleKeyInvalid) {
				t.Fatalf("error = %v, want invalid key", err)
			}
		})
	}
}
