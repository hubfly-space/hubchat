//go:build integration

package automation

import (
	"errors"
	"testing"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/database/dbtest"
)

func TestSavedReplyUsageIsWorkspaceScoped(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES
			('wrk_reply_a','Replies A','replies-a'),
			('wrk_reply_b','Replies B','replies-b');
		INSERT INTO saved_replies (id,workspace_id,name,body,scope)
		VALUES ('spr_reply_a','wrk_reply_a','Welcome','Hello {{customer.name}}','workspace')
	`); err != nil {
		t.Fatal(err)
	}

	service := New(pool)
	if err := service.UseSavedReply(ctx, "wrk_reply_a", "spr_reply_a"); err != nil {
		t.Fatalf("use saved reply: %v", err)
	}
	var usage int
	if err := pool.QueryRow(ctx, `SELECT usage_count FROM saved_replies WHERE id='spr_reply_a'`).Scan(&usage); err != nil {
		t.Fatal(err)
	}
	if usage != 1 {
		t.Fatalf("usage count = %d, want 1", usage)
	}

	if err := service.UseSavedReply(ctx, "wrk_reply_b", "spr_reply_a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-workspace use error = %v, want ErrNotFound", err)
	}
}

func TestSavedReplyListPagePreservesNameCursor(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES
			('wrk_reply_page','Replies','replies-page'),
			('wrk_reply_other','Other','replies-other');
		INSERT INTO saved_replies (id,workspace_id,name,body,scope) VALUES
			('spr_reply_b','wrk_reply_page','Beta','B','workspace'),
			('spr_reply_a','wrk_reply_page','Alpha','A','workspace'),
			('spr_reply_other','wrk_reply_other','Other','Other','workspace')
	`); err != nil {
		t.Fatal(err)
	}

	service := New(pool)
	first, err := service.ListSavedRepliesPage(ctx, "wrk_reply_page", "", "", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || first[0].Name != "Alpha" || first[1].Name != "Beta" {
		t.Fatalf("first page = %+v", first)
	}
	second, err := service.ListSavedRepliesPage(ctx, "wrk_reply_page", "", first[1].Name, first[1].ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 0 {
		t.Fatalf("second page = %+v, want empty", second)
	}
}

func TestMacroListingAndExecutionRespectMemberScopeAndCapabilities(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES ('wrk_macro_scope','Macros','macros-scope');
		INSERT INTO users (id,name,email) VALUES
			('usr_macro_a','Macro A','macro-a@example.com'),
			('usr_macro_b','Macro B','macro-b@example.com');
		INSERT INTO workspace_members (id,workspace_id,user_id,role) VALUES
			('mem_macro_a','wrk_macro_scope','usr_macro_a','agent'),
			('mem_macro_b','wrk_macro_scope','usr_macro_b','agent');
		INSERT INTO teams (id,workspace_id,name) VALUES ('team_macro','wrk_macro_scope','Macro team');
		INSERT INTO team_members (team_id,member_id) VALUES ('team_macro','mem_macro_a');
		INSERT INTO macros (id,workspace_id,name,scope,owner_id,team_id,body,actions) VALUES
			('mac_workspace','wrk_macro_scope','Workspace macro','workspace',NULL,NULL,'', '[]'::jsonb),
			('mac_personal','wrk_macro_scope','Personal macro','personal','mem_macro_a',NULL,'', '[]'::jsonb),
			('mac_team','wrk_macro_scope','Team macro','team',NULL,'team_macro','', '[]'::jsonb),
			('mac_action','wrk_macro_scope','Needs assignment','personal','mem_macro_a',NULL,'', '[{"type":"set_priority","params":{"priority":"urgent"}}]'::jsonb)
	`); err != nil {
		t.Fatal(err)
	}

	service := New(pool)
	a, err := service.ListMacrosForMemberPage(ctx, "wrk_macro_scope", "mem_macro_a", "", "", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 4 {
		t.Fatalf("member A macros = %d, want 4", len(a))
	}
	b, err := service.ListMacrosForMemberPage(ctx, "wrk_macro_scope", "mem_macro_b", "", "", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 1 || b[0].ID != "mac_workspace" {
		t.Fatalf("member B macros = %+v, want workspace macro only", b)
	}

	actor := &authorization.Actor{
		MemberID: "mem_macro_a", WorkspaceID: "wrk_macro_scope", Role: "agent",
		Capabilities: map[authorization.Capability]bool{authorization.AutomationManage: true},
	}
	if _, err := service.ExecuteMacro(ctx, "wrk_macro_scope", "mem_macro_a", "mac_action", actor, MacroExecutionRequest{SubjectType: "conversation", SubjectID: "cnv_macro"}); !errors.Is(err, ErrMacroCapability) {
		t.Fatalf("missing action capability error = %v, want ErrMacroCapability", err)
	}

	otherActor := &authorization.Actor{
		MemberID: "mem_macro_b", WorkspaceID: "wrk_macro_scope", Role: "agent",
		Capabilities: map[authorization.Capability]bool{authorization.AutomationManage: true},
	}
	if _, err := service.ExecuteMacro(ctx, "wrk_macro_scope", "mem_macro_b", "mac_personal", otherActor, MacroExecutionRequest{SubjectType: "conversation", SubjectID: "cnv_macro"}); !errors.Is(err, ErrMacroForbidden) {
		t.Fatalf("personal macro access error = %v, want ErrMacroForbidden", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO inboxes (id,workspace_id,name,slug,channels) VALUES ('inb_macro','wrk_macro_scope','Support','macro-support',ARRAY['manual']);
		INSERT INTO conversations (id,workspace_id,inbox_id,channel,subject) VALUES ('cnv_macro','wrk_macro_scope','inb_macro','manual','Macro target');
		INSERT INTO macros (id,workspace_id,name,scope,body,actions) VALUES
			('mac_success','wrk_macro_scope','Raise priority','workspace','', '[{"type":"set_priority","params":{"priority":"urgent"}}]'::jsonb)
	`); err != nil {
		t.Fatal(err)
	}

	executor := New(pool, Options{Conversation: conversation.New(pool, nil, nil)})
	actor.Capabilities[authorization.ConversationAssign] = true
	result, err := executor.ExecuteMacro(ctx, "wrk_macro_scope", "mem_macro_a", "mac_success", actor, MacroExecutionRequest{SubjectType: "conversation", SubjectID: "cnv_macro"})
	if err != nil {
		t.Fatalf("execute macro: %v", err)
	}
	if len(result.ActionsApplied) != 1 || result.ActionsApplied[0].Type != "set_priority" {
		t.Fatalf("applied actions = %+v", result.ActionsApplied)
	}
	var priority string
	if err := pool.QueryRow(ctx, `SELECT priority FROM conversations WHERE id='cnv_macro'`).Scan(&priority); err != nil {
		t.Fatal(err)
	}
	if priority != "urgent" {
		t.Fatalf("priority = %q, want urgent", priority)
	}
}

func TestMacroCrudAndSavedReplyVisibilityRespectMemberScope(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES ('wrk_content_scope','Content','content-scope');
		INSERT INTO users (id,name,email) VALUES
			('usr_content_a','Content A','content-a@example.com'),
			('usr_content_b','Content B','content-b@example.com');
		INSERT INTO workspace_members (id,workspace_id,user_id,role) VALUES
			('mem_content_a','wrk_content_scope','usr_content_a','agent'),
			('mem_content_b','wrk_content_scope','usr_content_b','agent');
		INSERT INTO teams (id,workspace_id,name) VALUES ('team_content','wrk_content_scope','Content team');
		INSERT INTO team_members (team_id,member_id) VALUES ('team_content','mem_content_a');
		INSERT INTO macros (id,workspace_id,name,scope,owner_id,body,actions) VALUES
			('mac_content_personal','wrk_content_scope','Personal','personal','mem_content_a','', '[]'::jsonb),
			('mac_content_workspace','wrk_content_scope','Workspace','workspace',NULL,'', '[]'::jsonb);
		INSERT INTO saved_replies (id,workspace_id,name,scope,owner_id,team_id,body) VALUES
			('spr_content_personal','wrk_content_scope','Personal','personal','mem_content_a',NULL,'Personal'),
			('spr_content_team','wrk_content_scope','Team','team',NULL,'team_content','Team'),
			('spr_content_workspace','wrk_content_scope','Workspace','workspace',NULL,NULL,'Workspace');
	`); err != nil {
		t.Fatal(err)
	}

	service := New(pool)
	a, err := service.ListSavedRepliesForMemberPage(ctx, "wrk_content_scope", "mem_content_a", "", "", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 3 {
		t.Fatalf("member A replies = %d, want 3", len(a))
	}
	b, err := service.ListSavedRepliesForMemberPage(ctx, "wrk_content_scope", "mem_content_b", "", "", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 1 || b[0].ID != "spr_content_workspace" {
		t.Fatalf("member B replies = %+v, want workspace reply only", b)
	}
	if err := service.UseSavedReplyForMember(ctx, "wrk_content_scope", "mem_content_b", "spr_content_personal"); !errors.Is(err, ErrSavedReplyForbidden) {
		t.Fatalf("personal reply access error = %v, want ErrSavedReplyForbidden", err)
	}
	if err := service.UseSavedReplyForMember(ctx, "wrk_content_scope", "mem_content_a", "spr_content_personal"); err != nil {
		t.Fatalf("owner reply use: %v", err)
	}

	updated, err := service.UpdateMacro(ctx, "wrk_content_scope", "mem_content_a", "mac_content_personal", MacroInput{
		Name: "Team macro", Scope: "team", TeamID: "team_content", Body: "Updated", Actions: []Action{{Type: "set_priority", Params: map[string]any{"priority": "high"}}},
	})
	if err != nil {
		t.Fatalf("update macro: %v", err)
	}
	if updated.Scope != "team" || updated.TeamID == nil || *updated.TeamID != "team_content" || len(updated.Actions) != 1 {
		t.Fatalf("updated macro = %+v", updated)
	}
	if _, err := service.GetMacroForMember(ctx, "wrk_content_scope", "mem_content_b", "mac_content_personal"); !errors.Is(err, ErrMacroForbidden) {
		t.Fatalf("updated team macro access error = %v, want ErrMacroForbidden", err)
	}
	if err := service.DeleteMacro(ctx, "wrk_content_scope", "mem_content_a", "mac_content_personal"); err != nil {
		t.Fatalf("delete macro: %v", err)
	}
	if _, err := service.GetMacro(ctx, "wrk_content_scope", "mac_content_personal"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted macro lookup error = %v, want ErrNotFound", err)
	}
}
