//go:build integration

package workspace_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/workspace"
)

func TestCreateTagRejectsDuplicateNames(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "owner@example.com")

	if _, err := svc.CreateTag(ctx, workspaceID, ownerMemberID, "Billing", 1); err != nil {
		t.Fatalf("first tag: %v", err)
	}
	_, err := svc.CreateTag(ctx, workspaceID, ownerMemberID, "Billing", 2)
	if !errors.Is(err, workspace.ErrTagNameTaken) {
		t.Fatalf("got %v, want ErrTagNameTaken", err)
	}
}

func TestCreateTagRejectsColorOutOfRange(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "owner@example.com")

	if _, err := svc.CreateTag(ctx, workspaceID, ownerMemberID, "Billing", 7); !errors.Is(err, workspace.ErrInvalidColor) {
		t.Fatalf("got %v, want ErrInvalidColor", err)
	}
	if _, err := svc.CreateTag(ctx, workspaceID, ownerMemberID, "Billing", 0); !errors.Is(err, workspace.ErrInvalidColor) {
		t.Fatalf("got %v, want ErrInvalidColor", err)
	}
}

func TestDeleteTagCascadesToConversationTags(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "owner@example.com")
	tag, err := svc.CreateTag(ctx, workspaceID, ownerMemberID, "Billing", 1)
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}

	conversationID := seedConversation(t, ctx, pool, workspaceID)
	tagConversation(t, ctx, pool, conversationID, tag.ID)

	if err := svc.DeleteTag(ctx, workspaceID, ownerMemberID, tag.ID); err != nil {
		t.Fatalf("delete tag: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM conversation_tags WHERE tag_id = $1`, tag.ID).Scan(&count); err != nil {
		t.Fatalf("count conversation_tags: %v", err)
	}
	if count != 0 {
		t.Fatalf("conversation_tags still references a deleted tag: %d rows", count)
	}
}

// This is the exact case that broke my first draft of the merge query: a
// self-join with no entity-column condition would delete every source-tagged
// row workspace-wide the moment *any* row anywhere had the target tag,
// instead of only the rows for entities that had both. Two conversations —
// one double-tagged, one only with the source — distinguish the bug from a
// correct implementation.
func TestMergeTagsReassignsUsageAndHandlesOverlap(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "owner@example.com")

	source, err := svc.CreateTag(ctx, workspaceID, ownerMemberID, "Bug", 1)
	if err != nil {
		t.Fatalf("create source tag: %v", err)
	}
	target, err := svc.CreateTag(ctx, workspaceID, ownerMemberID, "Defect", 2)
	if err != nil {
		t.Fatalf("create target tag: %v", err)
	}

	onlySource := seedConversation(t, ctx, pool, workspaceID)
	tagConversation(t, ctx, pool, onlySource, source.ID)

	both := seedConversation(t, ctx, pool, workspaceID)
	tagConversation(t, ctx, pool, both, source.ID)
	tagConversation(t, ctx, pool, both, target.ID)

	untouched := seedConversation(t, ctx, pool, workspaceID)
	tagConversation(t, ctx, pool, untouched, target.ID)

	if err := svc.MergeTags(ctx, workspaceID, ownerMemberID, source.ID, target.ID); err != nil {
		t.Fatalf("merge tags: %v", err)
	}

	// The source tag itself must be gone.
	tags, err := svc.ListTags(ctx, workspaceID)
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}
	if len(tags) != 1 || tags[0].ID != target.ID {
		t.Fatalf("expected only the target tag to remain, got %+v", tags)
	}

	assertConversationTags(t, ctx, pool, onlySource, target.ID)
	assertConversationTags(t, ctx, pool, both, target.ID)      // exactly once, not twice
	assertConversationTags(t, ctx, pool, untouched, target.ID) // must be unaffected, not duplicated
}

func TestMergeTagsRefusesSelfMerge(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "owner@example.com")
	tag, err := svc.CreateTag(ctx, workspaceID, ownerMemberID, "Billing", 1)
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}

	if err := svc.MergeTags(ctx, workspaceID, ownerMemberID, tag.ID, tag.ID); err == nil {
		t.Fatal("merging a tag into itself was accepted")
	}
}

// Regression test: recordAudit's call sites all pass ActorID as a member id
// and leave ActorName unset. Without the resolution in recordAudit itself,
// every audit entry from this package would carry a blank name — the exact
// live symptom that surfaced this bug.
func TestAuditEntriesRecordTheActorsName(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "owner@example.com")

	if _, err := svc.CreateTag(ctx, workspaceID, ownerMemberID, "Billing", 1); err != nil {
		t.Fatalf("create tag: %v", err)
	}

	var name string
	err := pool.QueryRow(ctx, `
		SELECT actor_name FROM audit_logs
		WHERE workspace_id = $1 AND action = 'tag.created'
	`, workspaceID).Scan(&name)
	if err != nil {
		t.Fatalf("read audit entry: %v", err)
	}
	if name == "" {
		t.Fatal("audit entry has a blank actor_name; it will read as anonymous once the member is removed")
	}
}

// ------------------------------------------------------------- settings

func TestSettingsRoundTripPerGroup(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "owner@example.com")

	// A workspace nobody has configured yet gets sensible defaults, not a
	// blank struct that looks like every protection is off.
	initial, err := svc.GetSettings(ctx, workspaceID)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if initial.Privacy.IPStorage != "full" {
		t.Fatalf("default ip_storage = %q, want full", initial.Privacy.IPStorage)
	}

	if _, err := svc.UpdateBranding(ctx, workspaceID, ownerMemberID, nil, nil, workspace.BrandingSettings{
		AccentColor: "#112233", EmailFooter: "Sent with love",
	}); err != nil {
		t.Fatalf("update branding: %v", err)
	}

	if _, err := svc.UpdateSecuritySettings(ctx, workspaceID, ownerMemberID, workspace.SecuritySettings{
		RequireTwoFactor: true, AllowedEmailDomains: []string{"example.com"},
	}); err != nil {
		t.Fatalf("update security: %v", err)
	}

	settings, err := svc.GetSettings(ctx, workspaceID)
	if err != nil {
		t.Fatalf("get settings after updates: %v", err)
	}

	// Updating security must not have clobbered the branding written just
	// before it — this is what mergeSettings' read-modify-write exists for.
	if settings.Branding.AccentColor != "#112233" {
		t.Fatalf("branding lost after a later security update: %+v", settings.Branding)
	}
	if !settings.Security.RequireTwoFactor || len(settings.Security.AllowedEmailDomains) != 1 {
		t.Fatalf("security settings not applied: %+v", settings.Security)
	}
}

func TestUpdatePrivacyRejectsUnknownIPStorage(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "owner@example.com")

	_, err := svc.UpdatePrivacySettings(ctx, workspaceID, ownerMemberID, workspace.PrivacySettings{
		IPStorage: "everything",
	})
	if !errors.Is(err, workspace.ErrInvalidSettings) {
		t.Fatalf("got %v, want ErrInvalidSettings", err)
	}
}

func TestUpdateGeneralValidatesTicketPrefix(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)

	workspaceID, _, ownerMemberID := seedOwnerWorkspace(t, ctx, pool, svc, "owner@example.com")

	_, err := svc.UpdateGeneral(ctx, workspaceID, ownerMemberID, "Acme Support", "1", "UTC", "en")
	if !errors.Is(err, workspace.ErrInvalidSettings) {
		t.Fatalf("got %v, want ErrInvalidSettings for a numeric ticket prefix", err)
	}

	ws, err := svc.UpdateGeneral(ctx, workspaceID, ownerMemberID, "Acme Support", "sup", "America/New_York", "fr")
	if err != nil {
		t.Fatalf("valid update: %v", err)
	}
	if ws.TicketPrefix != "SUP" {
		t.Fatalf("ticket prefix not normalised to uppercase: %q", ws.TicketPrefix)
	}
	if ws.Timezone != "America/New_York" || ws.DefaultLanguage != "fr" {
		t.Fatalf("unexpected workspace after update: %+v", ws)
	}
}

// ------------------------------------------------------------------ helpers

func seedConversation(t *testing.T, ctx context.Context, pool *database.Pool, workspaceID string) string {
	t.Helper()

	var inboxID string
	if err := pool.QueryRow(ctx,
		`SELECT id FROM inboxes WHERE workspace_id = $1 LIMIT 1`, workspaceID).Scan(&inboxID); err != nil {
		t.Fatalf("find default inbox: %v", err)
	}

	id := ids.New(ids.PrefixConversation)
	if _, err := pool.Exec(ctx, `
		INSERT INTO conversations (id, workspace_id, inbox_id, channel, state, priority)
		VALUES ($1, $2, $3, 'widget', 'open', 'normal')
	`, id, workspaceID, inboxID); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}
	return id
}

func tagConversation(t *testing.T, ctx context.Context, pool *database.Pool, conversationID, tagID string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO conversation_tags (conversation_id, tag_id) VALUES ($1, $2)
	`, conversationID, tagID); err != nil {
		t.Fatalf("tag conversation: %v", err)
	}
}

func assertConversationTags(t *testing.T, ctx context.Context, pool *database.Pool, conversationID, wantTagID string) {
	t.Helper()

	rows, err := pool.Query(ctx,
		`SELECT tag_id FROM conversation_tags WHERE conversation_id = $1`, conversationID)
	if err != nil {
		t.Fatalf("read conversation_tags: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var tagID string
		if err := rows.Scan(&tagID); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, tagID)
	}

	if len(got) != 1 || got[0] != wantTagID {
		t.Fatalf("conversation %s tags = %v, want exactly [%s]", conversationID, got, wantTagID)
	}
}
