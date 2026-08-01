//go:build integration

package workspace_test

import (
	"errors"
	"testing"

	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/workspace"
)

func TestSCIMProvisioningIsIdempotentAndReversible(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)
	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "scim-owner@example.com")

	item, created, err := svc.ProvisionSCIMUser(ctx, workspaceID, ownerMemberID, workspace.SCIMProvisionInput{
		ExternalID: "directory-1", UserName: "Agent@Example.com", DisplayName: "Directory Agent", Role: "agent",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if !created || item == nil || !item.Active || item.ExternalID != "directory-1" {
		t.Fatalf("unexpected provision result: created=%v item=%+v", created, item)
	}

	retry, created, err := svc.ProvisionSCIMUser(ctx, workspaceID, ownerMemberID, workspace.SCIMProvisionInput{
		ExternalID: "directory-1", UserName: "agent@example.com", DisplayName: "Directory Agent",
	})
	if err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
	if created || retry.ID != item.ID || retry.Role != "agent" {
		t.Fatalf("retry changed the membership: created=%v retry=%+v original=%+v", created, retry, item)
	}

	active := false
	inactive, _, err := svc.ProvisionSCIMUser(ctx, workspaceID, ownerMemberID, workspace.SCIMProvisionInput{
		ExternalID: "directory-1", UserName: "agent@example.com", DisplayName: "Directory Agent", Active: &active,
	})
	if err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if inactive.Active {
		t.Fatal("deactivated SCIM member is still active")
	}
	if _, err := svc.ActorForUser(ctx, workspaceID, inactive.UserID); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("inactive member resolved an actor: %v", err)
	}

	active = true
	reactivated, _, err := svc.ProvisionSCIMUser(ctx, workspaceID, ownerMemberID, workspace.SCIMProvisionInput{
		ExternalID: "directory-1", UserName: "agent@example.com", DisplayName: "Directory Agent", Active: &active,
	})
	if err != nil || !reactivated.Active {
		t.Fatalf("reactivate: item=%+v err=%v", reactivated, err)
	}
	if _, err := svc.ActorForUser(ctx, workspaceID, reactivated.UserID); err != nil {
		t.Fatalf("reactivated member did not regain access: %v", err)
	}

	otherWorkspaceID, _, otherOwnerID := seedOwnerWorkspace(t, ctx, pool, svc, "scim-other-owner@example.com")
	if _, err := svc.GetSCIMUser(ctx, otherWorkspaceID, item.ID); !errors.Is(err, workspace.ErrSCIMUserNotFound) {
		t.Fatalf("cross-workspace SCIM lookup returned %v", err)
	}
	if _, _, err := svc.ProvisionSCIMUser(ctx, workspaceID, otherOwnerID, workspace.SCIMProvisionInput{
		ExternalID: "directory-1", UserName: "other@example.com", DisplayName: "Other",
	}); !errors.Is(err, workspace.ErrSCIMExternalIDConflict) {
		t.Fatalf("external id conflict = %v", err)
	}
}

func TestSCIMCannotDeactivateWorkspaceOwner(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)
	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "scim-owner-protect@example.com")

	active := false
	_, _, err := svc.ProvisionSCIMUser(ctx, workspaceID, ownerMemberID, workspace.SCIMProvisionInput{
		ExternalID: "owner-directory", UserName: "scim-owner-protect@example.com", DisplayName: "Owner", Active: &active,
	})
	if !errors.Is(err, workspace.ErrSCIMOwnerDeactivation) {
		t.Fatalf("owner deactivation = %v", err)
	}
}

func TestSCIMListUsesBoundedStartIndexWindow(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)
	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "scim-list-owner@example.com")
	for i, email := range []string{"scim-list-a@example.com", "scim-list-b@example.com"} {
		_, _, err := svc.ProvisionSCIMUser(ctx, workspaceID, ownerMemberID, workspace.SCIMProvisionInput{
			ExternalID: ids.New("dir"), UserName: email, DisplayName: email,
		})
		if err != nil {
			t.Fatalf("provision %d: %v", i, err)
		}
	}
	items, total, err := svc.ListSCIMUsers(ctx, workspaceID, "", "", 2, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 3 || len(items) != 1 {
		t.Fatalf("list window = total %d items %d, want total 3 items 1", total, len(items))
	}
}
