//go:build integration

package conversation_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/workspace"
)

func newTestService(t *testing.T, pool *database.Pool) *conversation.Service {
	t.Helper()
	return conversation.New(pool, events.New(pool), audit.New(pool))
}

// seededWorkspace is what every test in this file needs to exercise a
// conversation: a workspace, its default inbox, and one member to act as.
type seededWorkspace struct {
	WorkspaceID string
	InboxID     string
	MemberID    string
}

func seedWorkspace(t *testing.T, ctx context.Context, pool *database.Pool) seededWorkspace {
	t.Helper()

	wsSvc := workspace.New(pool, events.New(pool), audit.New(pool))

	userID := ids.New(ids.PrefixUser)
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, name, email, password_hash, email_verified_at)
		VALUES ($1, 'Test Owner', $2, 'x', now())
	`, userID, userID+"@example.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	token := ids.New("t")
	ws, err := wsSvc.Bootstrap(ctx, userID, "Acme", "acme-"+token[len(token)-10:])
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	actor, err := wsSvc.ActorForUser(ctx, ws.ID, userID)
	if err != nil {
		t.Fatalf("resolve owner actor: %v", err)
	}

	var inboxID string
	if err := pool.QueryRow(ctx, `SELECT id FROM inboxes WHERE workspace_id = $1 LIMIT 1`, ws.ID).Scan(&inboxID); err != nil {
		t.Fatalf("find default inbox: %v", err)
	}

	return seededWorkspace{WorkspaceID: ws.ID, InboxID: inboxID, MemberID: actor.MemberID}
}

func seedMember(t *testing.T, ctx context.Context, pool *database.Pool, workspaceID string) string {
	t.Helper()

	userID := ids.New(ids.PrefixUser)
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, name, email, password_hash, email_verified_at)
		VALUES ($1, 'Another Agent', $2, 'x', now())
	`, userID, userID+"@example.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	memberID := ids.New(ids.PrefixMember)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspace_members (id, workspace_id, user_id, role)
		VALUES ($1, $2, $3, 'agent')
	`, memberID, workspaceID, userID); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	return memberID
}

func seedTag(t *testing.T, ctx context.Context, pool *database.Pool, workspaceID, name string) string {
	t.Helper()
	id := ids.New(ids.PrefixTag)
	if _, err := pool.Exec(ctx, `
		INSERT INTO tags (id, workspace_id, name) VALUES ($1, $2, $3)
	`, id, workspaceID, name); err != nil {
		t.Fatalf("seed tag: %v", err)
	}
	return id
}

func TestStartCreatesConversationWithOpeningMessage(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)

	conv, msg, err := svc.Start(ctx, ws.WorkspaceID, ws.InboxID, "widget", nil, nil, nil, "Visitor", "Hello, I need help")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if conv.State != "new" || conv.MessageCount != 1 {
		t.Fatalf("expected a fresh conversation with one message, got state=%s count=%d", conv.State, conv.MessageCount)
	}
	if msg.Sequence != 1 {
		t.Fatalf("expected the opening message to be sequence 1, got %d", msg.Sequence)
	}
}

func TestBulkUpdateIsAtomicAndWorkspaceScoped(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	workspaceA := seedWorkspace(t, ctx, pool)
	workspaceB := seedWorkspace(t, ctx, pool)
	secondMember := seedMember(t, ctx, pool, workspaceA.WorkspaceID)
	first, _, err := svc.Start(ctx, workspaceA.WorkspaceID, workspaceA.InboxID, "widget", nil, nil, nil, "Visitor", "First")
	if err != nil {
		t.Fatalf("start first: %v", err)
	}
	second, _, err := svc.Start(ctx, workspaceA.WorkspaceID, workspaceA.InboxID, "widget", nil, nil, nil, "Visitor", "Second")
	if err != nil {
		t.Fatalf("start second: %v", err)
	}
	foreign, _, err := svc.Start(ctx, workspaceB.WorkspaceID, workspaceB.InboxID, "widget", nil, nil, nil, "Visitor", "Foreign")
	if err != nil {
		t.Fatalf("start foreign: %v", err)
	}

	items, err := svc.BulkUpdate(ctx, workspaceA.WorkspaceID, workspaceA.MemberID, []string{second.ID, first.ID, first.ID}, conversation.BulkActionAssign, &secondMember, "")
	if err != nil || len(items) != 2 {
		t.Fatalf("bulk assign = %#v, err=%v", items, err)
	}
	for _, item := range items {
		if item.AssigneeID == nil || *item.AssigneeID != secondMember {
			t.Fatalf("bulk assignment for %s = %v, want %s", item.ID, item.AssigneeID, secondMember)
		}
	}

	if _, err := svc.BulkUpdate(ctx, workspaceA.WorkspaceID, workspaceA.MemberID, []string{first.ID, foreign.ID}, conversation.BulkActionAssign, &workspaceA.MemberID, ""); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatalf("cross-workspace bulk error = %v, want ErrNotFound", err)
	}
	unchanged, err := svc.Get(ctx, workspaceA.WorkspaceID, first.ID)
	if err != nil {
		t.Fatalf("reload unchanged conversation: %v", err)
	}
	if unchanged.AssigneeID == nil || *unchanged.AssigneeID != secondMember {
		t.Fatalf("cross-workspace failure partially changed assignment to %v", unchanged.AssigneeID)
	}

	items, err = svc.BulkUpdate(ctx, workspaceA.WorkspaceID, workspaceA.MemberID, []string{first.ID, second.ID}, conversation.BulkActionState, nil, "resolved")
	if err != nil || len(items) != 2 {
		t.Fatalf("bulk resolve = %#v, err=%v", items, err)
	}
	for _, item := range items {
		if item.State != "resolved" {
			t.Fatalf("bulk state for %s = %s, want resolved", item.ID, item.State)
		}
	}
}

func TestLargeInboxListRemainsTenantScopedUnderConcurrentReads(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	workspaceA := seedWorkspace(t, ctx, pool)
	workspaceB := seedWorkspace(t, ctx, pool)
	for index := 0; index < 160; index++ {
		if _, _, err := svc.Start(ctx, workspaceA.WorkspaceID, workspaceA.InboxID, "widget", nil, nil, nil, "Visitor", fmt.Sprintf("Inbox message %d", index)); err != nil {
			t.Fatalf("seed workspace A conversation %d: %v", index, err)
		}
	}
	if _, _, err := svc.Start(ctx, workspaceB.WorkspaceID, workspaceB.InboxID, "widget", nil, nil, nil, "Other visitor", "Other workspace message"); err != nil {
		t.Fatalf("seed workspace B conversation: %v", err)
	}

	const workers = 8
	const readsPerWorker = 20
	errorsCh := make(chan error, workers*readsPerWorker)
	var group sync.WaitGroup
	group.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer group.Done()
			for read := 0; read < readsPerWorker; read++ {
				items, err := svc.List(ctx, workspaceA.WorkspaceID, conversation.ListFilter{InboxID: workspaceA.InboxID, Limit: 50})
				if err != nil {
					errorsCh <- err
					continue
				}
				if len(items) != 50 {
					errorsCh <- fmt.Errorf("concurrent inbox page returned %d rows, want 50", len(items))
					continue
				}
				for _, item := range items {
					if item.WorkspaceID != workspaceA.WorkspaceID || item.InboxID != workspaceA.InboxID {
						errorsCh <- fmt.Errorf("inbox page leaked conversation %s from workspace %s/inbox %s", item.ID, item.WorkspaceID, item.InboxID)
						break
					}
				}
			}
		}()
	}
	group.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Error(err)
	}
}

func TestStartRoutesThroughInboxTeamRoundRobin(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)
	secondMember := seedMember(t, ctx, pool, ws.WorkspaceID)
	wsSvc := workspace.New(pool, events.New(pool), audit.New(pool))
	team, err := wsSvc.CreateTeam(ctx, ws.WorkspaceID, ws.MemberID, "Support", nil, nil, "round_robin", []string{ws.MemberID, secondMember})
	if err != nil {
		t.Fatalf("create routing team: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inbox_teams (inbox_id, team_id) VALUES ($1, $2)`, ws.InboxID, team.ID); err != nil {
		t.Fatalf("attach team to inbox: %v", err)
	}

	first, _, err := svc.Start(ctx, ws.WorkspaceID, ws.InboxID, "widget", nil, nil, nil, "Visitor", "First")
	if err != nil {
		t.Fatalf("first routed conversation: %v", err)
	}
	second, _, err := svc.Start(ctx, ws.WorkspaceID, ws.InboxID, "widget", nil, nil, nil, "Visitor", "Second")
	if err != nil {
		t.Fatalf("second routed conversation: %v", err)
	}
	if first.TeamID == nil || *first.TeamID != team.ID || second.TeamID == nil || *second.TeamID != team.ID {
		t.Fatalf("team routing = %v, %v; want %s", first.TeamID, second.TeamID, team.ID)
	}
	if first.AssigneeID == nil || second.AssigneeID == nil || *first.AssigneeID == *second.AssigneeID {
		t.Fatalf("round-robin assignments = %v and %v; want two eligible members", first.AssigneeID, second.AssigneeID)
	}
}

func TestStartSelectsInboxTeamByCustomerLanguage(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)
	secondMember := seedMember(t, ctx, pool, ws.WorkspaceID)
	wsSvc := workspace.New(pool, events.New(pool), audit.New(pool))
	enTeam, err := wsSvc.CreateTeamWithRouting(ctx, ws.WorkspaceID, ws.MemberID, "English", nil, nil, "team_queue", []string{ws.MemberID}, map[string]any{"languages": []any{"en"}})
	if err != nil {
		t.Fatalf("create English team: %v", err)
	}
	frTeam, err := wsSvc.CreateTeamWithRouting(ctx, ws.WorkspaceID, ws.MemberID, "French", nil, nil, "team_queue", []string{secondMember}, map[string]any{"languages": []any{"fr"}})
	if err != nil {
		t.Fatalf("create French team: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inbox_teams (inbox_id, team_id) VALUES ($1, $2), ($1, $3)`, ws.InboxID, enTeam.ID, frTeam.ID); err != nil {
		t.Fatalf("attach language teams: %v", err)
	}
	enCustomer := ids.New(ids.PrefixCustomer)
	frCustomer := ids.New(ids.PrefixCustomer)
	if _, err := pool.Exec(ctx, `INSERT INTO customers (id,workspace_id,name,email,language,attributes) VALUES ($1,$3,'English','en-'||$1||'@example.com','en','{}'), ($2,$3,'French','fr-'||$2||'@example.com','fr','{}')`, enCustomer, frCustomer, ws.WorkspaceID); err != nil {
		t.Fatalf("seed language customers: %v", err)
	}
	enConversation, _, err := svc.Start(ctx, ws.WorkspaceID, ws.InboxID, "widget", nil, &enCustomer, nil, "English customer", "Hello")
	if err != nil {
		t.Fatalf("start English conversation: %v", err)
	}
	frConversation, _, err := svc.Start(ctx, ws.WorkspaceID, ws.InboxID, "widget", nil, &frCustomer, nil, "French customer", "Bonjour")
	if err != nil {
		t.Fatalf("start French conversation: %v", err)
	}
	if enConversation.TeamID == nil || *enConversation.TeamID != enTeam.ID {
		t.Fatalf("English team = %v, want %s", enConversation.TeamID, enTeam.ID)
	}
	if frConversation.TeamID == nil || *frConversation.TeamID != frTeam.ID {
		t.Fatalf("French team = %v, want %s", frConversation.TeamID, frTeam.ID)
	}
}

func TestPostMessageIsIdempotentByClientID(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)

	conv, _, err := svc.Start(ctx, ws.WorkspaceID, ws.InboxID, "widget", nil, nil, nil, "Visitor", "Hi")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	clientID := "retry-key-1"
	first, err := svc.PostMessage(ctx, ws.WorkspaceID, conv.ID, &clientID, "reply", "agent", &ws.MemberID, "Agent", "On it")
	if err != nil {
		t.Fatalf("first post: %v", err)
	}
	second, err := svc.PostMessage(ctx, ws.WorkspaceID, conv.ID, &clientID, "reply", "agent", &ws.MemberID, "Agent", "On it")
	if err != nil {
		t.Fatalf("retried post: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected the retried send to return the same message, got %s and %s", first.ID, second.ID)
	}

	reloaded, err := svc.Get(ctx, ws.WorkspaceID, conv.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if reloaded.MessageCount != 2 {
		t.Fatalf("expected exactly 2 messages (open + one reply, not two), got %d", reloaded.MessageCount)
	}
}

func TestPostMessageMentionsAreWorkspaceScopedAndDurable(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	eventLog := events.New(pool)
	svc := conversation.New(pool, eventLog, audit.New(pool))
	wsA := seedWorkspace(t, ctx, pool)
	wsB := seedWorkspace(t, ctx, pool)
	memberA := seedMember(t, ctx, pool, wsA.WorkspaceID)

	conv, _, err := svc.Start(ctx, wsA.WorkspaceID, wsA.InboxID, "widget", nil, nil, nil, "Visitor", "Hi")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := svc.PostMessageWithMentions(ctx, wsA.WorkspaceID, conv.ID, nil, "note", "agent", &wsA.MemberID, "Agent", "Please review", []string{wsB.MemberID}); !errors.Is(err, conversation.ErrInvalidMention) {
		t.Fatalf("cross-workspace mention: got %v, want ErrInvalidMention", err)
	}
	if _, err := svc.PostMessageWithMentions(ctx, wsA.WorkspaceID, conv.ID, nil, "note", "agent", &wsA.MemberID, "Agent", "Please review", []string{memberA}); err != nil {
		t.Fatalf("same-workspace mention: %v", err)
	}

	records, err := eventLog.Since(ctx, wsA.WorkspaceID, 0, 50)
	if err != nil {
		t.Fatalf("read event log: %v", err)
	}
	var found bool
	for _, record := range records {
		if record.Type != events.MessageCreated {
			continue
		}
		var payload conversation.MessageEvent
		if err := json.Unmarshal(record.Data, &payload); err != nil {
			t.Fatalf("decode message event: %v", err)
		}
		for _, mentioned := range payload.MentionedMemberIDs {
			if mentioned == memberA {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("message event did not preserve the validated mentioned member")
	}
}

func TestListAndCountsExposeMentionAndSLAQueues(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)
	approaching, _, err := svc.Start(ctx, ws.WorkspaceID, ws.InboxID, "widget", nil, nil, nil, "Visitor", "Approaching")
	if err != nil {
		t.Fatalf("start approaching: %v", err)
	}
	breached, _, err := svc.Start(ctx, ws.WorkspaceID, ws.InboxID, "widget", nil, nil, nil, "Visitor", "Breached")
	if err != nil {
		t.Fatalf("start breached: %v", err)
	}
	ordinary, _, err := svc.Start(ctx, ws.WorkspaceID, ws.InboxID, "widget", nil, nil, nil, "Visitor", "Ordinary")
	if err != nil {
		t.Fatalf("start ordinary: %v", err)
	}

	policyID := ids.New("sla")
	if _, err := pool.Exec(ctx, `INSERT INTO sla_policies (id,workspace_id,name) VALUES ($1,$2,'Queue filter policy')`, policyID, ws.WorkspaceID); err != nil {
		t.Fatalf("seed SLA policy: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO sla_instances (id,workspace_id,policy_id,conversation_id,kind,state,target_minutes,deadline_at,warned_at)
		VALUES
			($3,$2,$1,$4,'first_response','active',30,now()+interval '5 minutes',now()),
			($5,$2,$1,$6,'resolution','breached',60,now()-interval '5 minutes',now()-interval '5 minutes')
	`, policyID, ws.WorkspaceID, ids.New("sla"), approaching.ID, ids.New("sla"), breached.ID); err != nil {
		t.Fatalf("seed SLA instances: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO notifications (id,workspace_id,member_id,type,title,body,entity_type,entity_id,url)
		VALUES ($1,$2,$3,'mention','Mentioned','You were mentioned.','conversation',$4,'/inbox')
	`, ids.New("ntf"), ws.WorkspaceID, ws.MemberID, approaching.ID); err != nil {
		t.Fatalf("seed mention notification: %v", err)
	}
	if _, err := svc.SetState(ctx, ws.WorkspaceID, ws.MemberID, ordinary.ID, "resolved"); err != nil {
		t.Fatalf("resolve ordinary conversation: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO sla_instances (id,workspace_id,policy_id,conversation_id,kind,state,target_minutes,deadline_at,warned_at)
		VALUES ($1,$2,$3,$4,'resolution','breached',60,now()-interval '5 minutes',now()-interval '5 minutes')
	`, ids.New("sla"), ws.WorkspaceID, policyID, ordinary.ID); err != nil {
		t.Fatalf("seed resolved SLA instance: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO notifications (id,workspace_id,member_id,type,title,body,entity_type,entity_id,url)
		VALUES ($1,$2,$3,'mention','Mentioned','You were mentioned.','conversation',$4,'/inbox')
	`, ids.New("ntf"), ws.WorkspaceID, ws.MemberID, ordinary.ID); err != nil {
		t.Fatalf("seed resolved mention notification: %v", err)
	}

	items, err := svc.List(ctx, ws.WorkspaceID, conversation.ListFilter{SLAState: "approaching", Limit: 10})
	if err != nil || len(items) != 1 || items[0].ID != approaching.ID {
		t.Fatalf("approaching queue = %+v, err=%v", items, err)
	}
	items, err = svc.List(ctx, ws.WorkspaceID, conversation.ListFilter{SLAState: "breached", Limit: 10})
	if err != nil || len(items) != 1 || items[0].ID != breached.ID {
		t.Fatalf("breached queue = %+v, err=%v", items, err)
	}
	items, err = svc.List(ctx, ws.WorkspaceID, conversation.ListFilter{MentionedMemberID: ws.MemberID, Limit: 10})
	if err != nil || len(items) != 1 || items[0].ID != approaching.ID {
		t.Fatalf("mentioned queue = %+v, err=%v", items, err)
	}

	counts, err := svc.Counts(ctx, ws.WorkspaceID, ws.MemberID)
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if counts.Mentioned != 1 || counts.SLAApproaching != 1 || counts.SLABreached != 1 {
		t.Fatalf("queue counts = %+v", counts)
	}
}

func TestSetAssigneeRejectsAMemberFromAnotherWorkspace(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	wsA := seedWorkspace(t, ctx, pool)
	wsB := seedWorkspace(t, ctx, pool)

	conv, _, err := svc.Start(ctx, wsA.WorkspaceID, wsA.InboxID, "widget", nil, nil, nil, "Visitor", "Hi")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	if _, err := svc.SetAssignee(ctx, wsA.WorkspaceID, wsA.MemberID, conv.ID, &wsB.MemberID); !errors.Is(err, conversation.ErrInvalidAssignee) {
		t.Fatalf("cross-workspace assignee: got %v, want ErrInvalidAssignee", err)
	}

	updated, err := svc.SetAssignee(ctx, wsA.WorkspaceID, wsA.MemberID, conv.ID, &wsA.MemberID)
	if err != nil {
		t.Fatalf("same-workspace assignee: %v", err)
	}
	if updated.AssigneeID == nil || *updated.AssigneeID != wsA.MemberID {
		t.Fatalf("expected assignee to be set, got %v", updated.AssigneeID)
	}
}

func TestSetStateRejectsSnoozedAndRecordsHistory(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)

	conv, _, err := svc.Start(ctx, ws.WorkspaceID, ws.InboxID, "widget", nil, nil, nil, "Visitor", "Hi")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	if _, err := svc.SetState(ctx, ws.WorkspaceID, ws.MemberID, conv.ID, "snoozed"); !errors.Is(err, conversation.ErrInvalidState) {
		t.Fatalf("direct snoozed transition: got %v, want ErrInvalidState", err)
	}

	updated, err := svc.SetState(ctx, ws.WorkspaceID, ws.MemberID, conv.ID, "resolved")
	if err != nil {
		t.Fatalf("set state: %v", err)
	}
	if updated.State != "resolved" {
		t.Fatalf("expected resolved, got %s", updated.State)
	}

	var fromState, toState string
	err = pool.QueryRow(ctx, `
		SELECT from_state, to_state FROM conversation_status_history
		WHERE conversation_id = $1 ORDER BY occurred_at DESC LIMIT 1
	`, conv.ID).Scan(&fromState, &toState)
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if fromState != "new" || toState != "resolved" {
		t.Fatalf("expected history new->resolved, got %s->%s", fromState, toState)
	}
}

func TestSnoozeRejectsPastTimesAndWakeSnoozedReopens(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)

	conv, _, err := svc.Start(ctx, ws.WorkspaceID, ws.InboxID, "widget", nil, nil, nil, "Visitor", "Hi")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	if _, err := svc.Snooze(ctx, ws.WorkspaceID, ws.MemberID, conv.ID, time.Now().Add(-time.Hour)); !errors.Is(err, conversation.ErrSnoozeInPast) {
		t.Fatalf("snooze in the past: got %v, want ErrSnoozeInPast", err)
	}

	// Snooze one second in the future so WakeSnoozed's "already elapsed"
	// query has something to find without a sleep in the test.
	snoozed, err := svc.Snooze(ctx, ws.WorkspaceID, ws.MemberID, conv.ID, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("snooze: %v", err)
	}
	if snoozed.State != "snoozed" || snoozed.SnoozedUntil == nil {
		t.Fatalf("expected snoozed state with a wake time, got %+v", snoozed)
	}

	time.Sleep(1100 * time.Millisecond)

	woken, err := svc.WakeSnoozed(ctx)
	if err != nil {
		t.Fatalf("wake snoozed: %v", err)
	}
	if woken != 1 {
		t.Fatalf("expected exactly one conversation woken, got %d", woken)
	}

	reloaded, err := svc.Get(ctx, ws.WorkspaceID, conv.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if reloaded.State != "open" || reloaded.SnoozedUntil != nil {
		t.Fatalf("expected the conversation to reopen with no snooze time, got state=%s until=%v", reloaded.State, reloaded.SnoozedUntil)
	}
}

func TestAddTagRejectsATagFromAnotherWorkspace(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	wsA := seedWorkspace(t, ctx, pool)
	wsB := seedWorkspace(t, ctx, pool)

	conv, _, err := svc.Start(ctx, wsA.WorkspaceID, wsA.InboxID, "widget", nil, nil, nil, "Visitor", "Hi")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	tagB := seedTag(t, ctx, pool, wsB.WorkspaceID, "foreign")
	if err := svc.AddTag(ctx, wsA.WorkspaceID, wsA.MemberID, conv.ID, tagB); !errors.Is(err, conversation.ErrTagNotFound) {
		t.Fatalf("cross-workspace tag: got %v, want ErrTagNotFound", err)
	}

	tagA := seedTag(t, ctx, pool, wsA.WorkspaceID, "vip")
	if err := svc.AddTag(ctx, wsA.WorkspaceID, wsA.MemberID, conv.ID, tagA); err != nil {
		t.Fatalf("same-workspace tag: %v", err)
	}

	tags, err := svc.Tags(ctx, wsA.WorkspaceID, conv.ID)
	if err != nil || len(tags) != 1 || tags[0] != tagA {
		t.Fatalf("expected exactly [vip] tag, got %v, %v", tags, err)
	}

	if err := svc.RemoveTag(ctx, wsA.WorkspaceID, wsA.MemberID, conv.ID, tagA); err != nil {
		t.Fatalf("remove tag: %v", err)
	}
	tags, err = svc.Tags(ctx, wsA.WorkspaceID, conv.ID)
	if err != nil || len(tags) != 0 {
		t.Fatalf("expected no tags after removal, got %v, %v", tags, err)
	}
}

func TestListFiltersByStateAndAssigneeAndScopesToWorkspace(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	wsA := seedWorkspace(t, ctx, pool)
	wsB := seedWorkspace(t, ctx, pool)
	otherMember := seedMember(t, ctx, pool, wsA.WorkspaceID)

	open, _, err := svc.Start(ctx, wsA.WorkspaceID, wsA.InboxID, "widget", nil, nil, nil, "Visitor", "Open one")
	if err != nil {
		t.Fatalf("start open: %v", err)
	}
	if _, err := svc.SetAssignee(ctx, wsA.WorkspaceID, wsA.MemberID, open.ID, &otherMember); err != nil {
		t.Fatalf("assign: %v", err)
	}

	resolved, _, err := svc.Start(ctx, wsA.WorkspaceID, wsA.InboxID, "widget", nil, nil, nil, "Visitor", "Resolved one")
	if err != nil {
		t.Fatalf("start resolved: %v", err)
	}
	if _, err := svc.SetState(ctx, wsA.WorkspaceID, wsA.MemberID, resolved.ID, "resolved"); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if _, _, err := svc.Start(ctx, wsB.WorkspaceID, wsB.InboxID, "widget", nil, nil, nil, "Visitor", "Other workspace"); err != nil {
		t.Fatalf("start in other workspace: %v", err)
	}

	// Default filter (no explicit states): everything not closed/spam, so
	// both A conversations show but nothing from workspace B.
	defaultView, err := svc.List(ctx, wsA.WorkspaceID, conversation.ListFilter{InboxID: wsA.InboxID, Limit: 50})
	if err != nil {
		t.Fatalf("list default: %v", err)
	}
	if len(defaultView) != 2 {
		t.Fatalf("expected 2 conversations in the default view, got %d", len(defaultView))
	}

	assignedToOther, err := svc.List(ctx, wsA.WorkspaceID, conversation.ListFilter{
		InboxID: wsA.InboxID, AssigneeID: otherMember, Limit: 50,
	})
	if err != nil {
		t.Fatalf("list by assignee: %v", err)
	}
	if len(assignedToOther) != 1 || assignedToOther[0].ID != open.ID {
		t.Fatalf("expected exactly the assigned conversation, got %+v", assignedToOther)
	}

	resolvedOnly, err := svc.List(ctx, wsA.WorkspaceID, conversation.ListFilter{
		InboxID: wsA.InboxID, States: []string{"resolved"}, Limit: 50,
	})
	if err != nil {
		t.Fatalf("list resolved: %v", err)
	}
	if len(resolvedOnly) != 1 || resolvedOnly[0].ID != resolved.ID {
		t.Fatalf("expected exactly the resolved conversation, got %+v", resolvedOnly)
	}
}

func TestMarkReadReflectsInIsRead(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)

	conv, _, err := svc.Start(ctx, ws.WorkspaceID, ws.InboxID, "widget", nil, nil, nil, "Visitor", "Hi")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	read, err := svc.IsRead(ctx, ws.WorkspaceID, conv.ID, ws.MemberID)
	if err != nil {
		t.Fatalf("is read: %v", err)
	}
	if read {
		t.Fatalf("expected unread before MarkRead")
	}

	if err := svc.MarkRead(ctx, ws.WorkspaceID, conv.ID, ws.MemberID); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	read, err = svc.IsRead(ctx, ws.WorkspaceID, conv.ID, ws.MemberID)
	if err != nil {
		t.Fatalf("is read after mark: %v", err)
	}
	if !read {
		t.Fatalf("expected read after MarkRead")
	}

	// A new customer message should flip it back to unread for that member.
	if _, err := svc.PostMessage(ctx, ws.WorkspaceID, conv.ID, nil, "reply", "customer", nil, "Visitor", "Are you there?"); err != nil {
		t.Fatalf("post customer message: %v", err)
	}
	read, err = svc.IsRead(ctx, ws.WorkspaceID, conv.ID, ws.MemberID)
	if err != nil {
		t.Fatalf("is read after new message: %v", err)
	}
	if read {
		t.Fatalf("expected unread again after a new message")
	}
}

func TestEditMessageOnlyAllowsTheOriginalAuthor(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)
	otherMember := seedMember(t, ctx, pool, ws.WorkspaceID)

	conv, _, err := svc.Start(ctx, ws.WorkspaceID, ws.InboxID, "widget", nil, nil, nil, "Visitor", "Hi")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	msg, err := svc.PostMessage(ctx, ws.WorkspaceID, conv.ID, nil, "reply", "agent", &ws.MemberID, "Agent", "Origianl typo")
	if err != nil {
		t.Fatalf("post: %v", err)
	}

	if _, err := svc.EditMessage(ctx, ws.WorkspaceID, otherMember, conv.ID, msg.ID, "Hijacked"); !errors.Is(err, conversation.ErrNotMessageAuthor) {
		t.Fatalf("edit by non-author: got %v, want ErrNotMessageAuthor", err)
	}

	edited, err := svc.EditMessage(ctx, ws.WorkspaceID, ws.MemberID, conv.ID, msg.ID, "Original typo fixed")
	if err != nil {
		t.Fatalf("edit by author: %v", err)
	}
	if edited.Body != "Original typo fixed" || edited.EditedAt == nil {
		t.Fatalf("expected updated body and edited_at set, got body=%q edited_at=%v", edited.Body, edited.EditedAt)
	}

	var revisionBody string
	if err := pool.QueryRow(ctx, `SELECT body FROM message_revisions WHERE message_id = $1`, msg.ID).Scan(&revisionBody); err != nil {
		t.Fatalf("read revision: %v", err)
	}
	if revisionBody != "Origianl typo" {
		t.Fatalf("expected the revision to keep the original body, got %q", revisionBody)
	}
}

func TestRedactMessageClearsBodyAndKeepsRevision(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)

	conv, _, err := svc.Start(ctx, ws.WorkspaceID, ws.InboxID, "widget", nil, nil, nil, "Visitor", "My card number is 4242 4242 4242 4242")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	messages, err := svc.Messages(ctx, ws.WorkspaceID, conv.ID, 0)
	if err != nil || len(messages) != 1 {
		t.Fatalf("messages: %v, %v", messages, err)
	}

	redacted, err := svc.RedactMessage(ctx, ws.WorkspaceID, ws.MemberID, conv.ID, messages[0].ID)
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if redacted.Body != "" || redacted.RedactedAt == nil {
		t.Fatalf("expected an empty, redacted message, got body=%q redacted_at=%v", redacted.Body, redacted.RedactedAt)
	}

	var revisionBody string
	if err := pool.QueryRow(ctx, `SELECT body FROM message_revisions WHERE message_id = $1`, messages[0].ID).Scan(&revisionBody); err != nil {
		t.Fatalf("read revision: %v", err)
	}
	if !strings.Contains(revisionBody, "4242") {
		t.Fatalf("expected the original card number preserved in the revision, got %q", revisionBody)
	}
}

func TestListMessagesPageReturnsNewestWindowAndOlderCursors(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)

	conv, _, err := svc.Start(ctx, ws.WorkspaceID, ws.InboxID, "widget", nil, nil, nil, "Visitor", "message 1")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	for index := 2; index <= 5; index++ {
		if _, err := svc.PostMessage(ctx, ws.WorkspaceID, conv.ID, nil, "reply", "customer", nil, "Visitor", fmt.Sprintf("message %d", index)); err != nil {
			t.Fatalf("post message %d: %v", index, err)
		}
	}

	latest, hasMore, err := svc.ListMessagesPage(ctx, ws.WorkspaceID, conv.ID, 0, 0, 2)
	if err != nil || !hasMore || len(latest) != 2 {
		t.Fatalf("latest page = %d messages, has_more=%v, err=%v", len(latest), hasMore, err)
	}
	if latest[0].Sequence != 4 || latest[1].Sequence != 5 {
		t.Fatalf("latest sequences = %d,%d, want 4,5", latest[0].Sequence, latest[1].Sequence)
	}

	older, hasMore, err := svc.ListMessagesPage(ctx, ws.WorkspaceID, conv.ID, latest[0].Sequence, 0, 2)
	if err != nil || !hasMore || len(older) != 2 {
		t.Fatalf("older page = %d messages, has_more=%v, err=%v", len(older), hasMore, err)
	}
	if older[0].Sequence != 2 || older[1].Sequence != 3 {
		t.Fatalf("older sequences = %d,%d, want 2,3", older[0].Sequence, older[1].Sequence)
	}

	oldest, hasMore, err := svc.ListMessagesPage(ctx, ws.WorkspaceID, conv.ID, older[0].Sequence, 0, 2)
	if err != nil || hasMore || len(oldest) != 1 || oldest[0].Sequence != 1 {
		t.Fatalf("oldest page = %+v, has_more=%v, err=%v", oldest, hasMore, err)
	}
}

func TestMergeMovesMessagesAndClosesTheSource(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)

	source, _, err := svc.Start(ctx, ws.WorkspaceID, ws.InboxID, "widget", nil, nil, nil, "Visitor", "Duplicate thread")
	if err != nil {
		t.Fatalf("start source: %v", err)
	}
	target, _, err := svc.Start(ctx, ws.WorkspaceID, ws.InboxID, "widget", nil, nil, nil, "Visitor", "Main thread")
	if err != nil {
		t.Fatalf("start target: %v", err)
	}
	if _, err := svc.PostMessage(ctx, ws.WorkspaceID, source.ID, nil, "reply", "agent", &ws.MemberID, "Agent", "Reply on the duplicate"); err != nil {
		t.Fatalf("post on source: %v", err)
	}

	if err := svc.Merge(ctx, ws.WorkspaceID, ws.MemberID, source.ID, target.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}

	mergedSource, err := svc.Get(ctx, ws.WorkspaceID, source.ID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if mergedSource.State != "closed" {
		t.Fatalf("expected the source to be closed, got %s", mergedSource.State)
	}

	targetMessages, err := svc.Messages(ctx, ws.WorkspaceID, target.ID, 0)
	if err != nil {
		t.Fatalf("target messages: %v", err)
	}
	if len(targetMessages) != 3 {
		t.Fatalf("expected target to hold its own message plus source's 2, got %d", len(targetMessages))
	}
	for i := 1; i < len(targetMessages); i++ {
		if targetMessages[i].Sequence <= targetMessages[i-1].Sequence {
			t.Fatalf("expected strictly increasing sequence after merge, got %+v", targetMessages)
		}
	}

	reloadedTarget, err := svc.Get(ctx, ws.WorkspaceID, target.ID)
	if err != nil {
		t.Fatalf("get target: %v", err)
	}
	if reloadedTarget.MessageCount != 3 {
		t.Fatalf("expected target message_count to include the moved messages, got %d", reloadedTarget.MessageCount)
	}
}

func TestTranscriptIncludesNotesAndRedactsAsExpected(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)

	conv, _, err := svc.Start(ctx, ws.WorkspaceID, ws.InboxID, "widget", nil, nil, nil, "Visitor", "Public message")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := svc.PostMessage(ctx, ws.WorkspaceID, conv.ID, nil, "note", "agent", &ws.MemberID, "Agent", "Escalate this one"); err != nil {
		t.Fatalf("post note: %v", err)
	}

	transcript, err := svc.Transcript(ctx, ws.WorkspaceID, conv.ID)
	if err != nil {
		t.Fatalf("transcript: %v", err)
	}
	if !strings.Contains(transcript, "Public message") {
		t.Fatalf("expected the public message in the transcript, got:\n%s", transcript)
	}
	if !strings.Contains(transcript, "internal note") || !strings.Contains(transcript, "Escalate this one") {
		t.Fatalf("expected the internal note marked and included, got:\n%s", transcript)
	}
}

func TestCountsMatchTheSidebarBadges(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)

	if _, _, err := svc.Start(ctx, ws.WorkspaceID, ws.InboxID, "widget", nil, nil, nil, "Visitor", "Unassigned one"); err != nil {
		t.Fatalf("start unassigned: %v", err)
	}

	mine, _, err := svc.Start(ctx, ws.WorkspaceID, ws.InboxID, "widget", nil, nil, nil, "Visitor", "Assigned to me")
	if err != nil {
		t.Fatalf("start mine: %v", err)
	}
	if _, err := svc.SetAssignee(ctx, ws.WorkspaceID, ws.MemberID, mine.ID, &ws.MemberID); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if err := svc.Follow(ctx, ws.WorkspaceID, mine.ID, ws.MemberID); err != nil {
		t.Fatalf("follow: %v", err)
	}
	followerPage, err := svc.FollowersPage(ctx, ws.WorkspaceID, mine.ID, "", 1)
	if err != nil {
		t.Fatalf("followers page: %v", err)
	}
	if len(followerPage) != 1 || followerPage[0] != ws.MemberID {
		t.Fatalf("expected one cursor-paged follower, got %v", followerPage)
	}

	spam, _, err := svc.Start(ctx, ws.WorkspaceID, ws.InboxID, "widget", nil, nil, nil, "Visitor", "Spam one")
	if err != nil {
		t.Fatalf("start spam: %v", err)
	}
	if _, err := svc.SetState(ctx, ws.WorkspaceID, ws.MemberID, spam.ID, "spam"); err != nil {
		t.Fatalf("mark spam: %v", err)
	}

	counts, err := svc.Counts(ctx, ws.WorkspaceID, ws.MemberID)
	if err != nil {
		t.Fatalf("counts: %v", err)
	}

	if counts.All != 2 {
		t.Fatalf("expected 2 active (spam excluded), got %d", counts.All)
	}
	if counts.Unassigned != 1 {
		t.Fatalf("expected 1 unassigned, got %d", counts.Unassigned)
	}
	if counts.Mine != 1 {
		t.Fatalf("expected 1 mine, got %d", counts.Mine)
	}
	if counts.Following != 1 {
		t.Fatalf("expected 1 following, got %d", counts.Following)
	}
	if counts.Spam != 1 {
		t.Fatalf("expected 1 spam, got %d", counts.Spam)
	}
}

func TestSetInboxRejectsAnInboxFromAnotherWorkspace(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	wsA := seedWorkspace(t, ctx, pool)
	wsB := seedWorkspace(t, ctx, pool)

	conv, _, err := svc.Start(ctx, wsA.WorkspaceID, wsA.InboxID, "widget", nil, nil, nil, "Visitor", "Hi")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	if _, err := svc.SetInbox(ctx, wsA.WorkspaceID, wsA.MemberID, conv.ID, wsB.InboxID); !errors.Is(err, conversation.ErrInvalidInbox) {
		t.Fatalf("cross-workspace inbox: got %v, want ErrInvalidInbox", err)
	}
}
