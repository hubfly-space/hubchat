//go:build integration

package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/analytics"
	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/auth"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/automation"
	"github.com/hubchat/hubchat/internal/config"
	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/feedback"
	filemodule "github.com/hubchat/hubchat/internal/file"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/inbox"
	"github.com/hubchat/hubchat/internal/jobs"
	"github.com/hubchat/hubchat/internal/knowledgebase"
	"github.com/hubchat/hubchat/internal/notification"
	"github.com/hubchat/hubchat/internal/search"
	"github.com/hubchat/hubchat/internal/task"
	"github.com/hubchat/hubchat/internal/ticket"
	"github.com/hubchat/hubchat/internal/workspace"
)

func productHarness(t *testing.T) (Deps, string, string) {
	t.Helper()
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	userID := ids.New(ids.PrefixUser)
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,name,email,password_hash,email_verified_at) VALUES($1,'Product owner',$2,'unused',now())`, userID, userID+"@example.test"); err != nil {
		t.Fatal(err)
	}
	eventLog := events.New(pool)
	auditLog := audit.New(pool)
	ws := workspace.New(pool, eventLog, auditLog)
	created, err := ws.Bootstrap(ctx, userID, "Product test", "product-"+userID[len(userID)-10:])
	if err != nil {
		t.Fatal(err)
	}
	a := auth.New(pool, auth.Options{SessionLifetime: time.Hour})
	session, err := a.CreateSessionWithMethod(ctx, userID, "product-test", "", auth.AuthMethodPassword)
	if err != nil {
		t.Fatal(err)
	}
	c := conversation.New(pool, eventLog, auditLog)
	customers := customer.New(pool, eventLog, auditLog, config.Default().Limits)
	tickets := ticket.New(pool, ws, eventLog, auditLog)
	kb := knowledgebase.New(pool, knowledgebase.Options{Events: eventLog})
	f := feedback.New(pool, eventLog, auditLog)
	queue := jobs.NewClient(pool)
	notifications := notification.New(pool, queue)
	store, err := filemodule.NewLocalStore(t.TempDir(), 10<<20, nil)
	if err != nil {
		t.Fatal(err)
	}
	deps := Deps{Pool: pool, Auth: a, Workspace: ws, Conversation: c, Customer: customers, Ticket: tickets, Knowledgebase: kb, Feedback: f, Inbox: inbox.New(pool, eventLog, auditLog), File: filemodule.New(pool, store), Analytics: analytics.New(pool), Notification: notifications, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	deps.Automation = automation.New(pool, automation.Options{Conversation: c, Ticket: tickets, Jobs: queue, Tasks: task.New(pool), Notification: notifications})
	deps.Search = search.New(c, customers, search.Provider{Kind: "company", Capability: authorization.CustomerRead, Fetch: customers.SearchCompanies}, search.Provider{Kind: "ticket", Capability: authorization.TicketManage, Fetch: tickets.SearchMatches}, search.Provider{Kind: "article", Capability: authorization.KnowledgebaseManage, Fetch: kb.SearchMatches}, search.Provider{Kind: "feedback", Capability: authorization.FeedbackModerate, Fetch: f.SearchMatches})
	return deps, created.ID, session.Token
}
func productRequest(t *testing.T, deps Deps, workspaceID, token, method, path string, input any) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(input)
	req := httptest.NewRequest(method, path, strings.NewReader(string(body)))
	req.AddCookie(&http.Cookie{Name: httpserver.SessionCookieName, Value: token})
	req.Header.Set("Hubchat-Workspace-Id", workspaceID)
	if method != "GET" {
		req.Header.Set("Idempotency-Key", ids.New("test"))
	}
	response := httptest.NewRecorder()
	New(deps).ServeHTTP(response, req)
	return response
}
func TestManualCustomersAndArticlePublicationProtectConflicts(t *testing.T) {
	deps, ws, token := productHarness(t)
	create := productRequest(t, deps, ws, token, "POST", "/v1/customers", map[string]any{"name": "Ada", "email": "ada@example.test"})
	if create.Code != 201 {
		t.Fatalf("create customer: %d %s", create.Code, create.Body)
	}
	var profile map[string]any
	if err := json.Unmarshal(create.Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	if profile["verification"] != "unverified" {
		t.Fatal("manual creation verified an unproven email")
	}
	duplicate := productRequest(t, deps, ws, token, "POST", "/v1/customers", map[string]any{"name": "Ada again", "email": "ADA@example.test"})
	if duplicate.Code != 409 {
		t.Fatalf("duplicate: %d %s", duplicate.Code, duplicate.Body)
	}
	base := productRequest(t, deps, ws, token, "POST", "/v1/knowledge-bases", map[string]any{"name": "Guides", "slug": "guides", "default_language": "en", "languages": []string{"en"}, "visibility": "public"})
	if base.Code != 201 {
		t.Fatalf("base: %d %s", base.Code, base.Body)
	}
	var baseRecord knowledgebase.KnowledgeBase
	if err := json.Unmarshal(base.Body.Bytes(), &baseRecord); err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"knowledge_base_id": baseRecord.ID, "title": "Answer", "slug": "answer", "body": "OLD BODY", "state": "draft", "language": "en", "seo": map[string]any{"title": "SEO title"}}
	created := productRequest(t, deps, ws, token, "POST", "/v1/articles", input)
	if created.Code != 201 {
		t.Fatalf("article: %d %s", created.Code, created.Body)
	}
	var article knowledgebase.Article
	if err := json.Unmarshal(created.Body.Bytes(), &article); err != nil {
		t.Fatal(err)
	}
	input["body"] = "CURRENT EDITS"
	input["state"] = "published"
	input["expected_version"] = 1
	saved := productRequest(t, deps, ws, token, "PATCH", "/v1/articles/"+article.ID, input)
	if saved.Code != 200 {
		t.Fatalf("publish edits: %d %s", saved.Code, saved.Body)
	}
	input["body"] = "STALE OVERWRITE"
	conflict := productRequest(t, deps, ws, token, "PATCH", "/v1/articles/"+article.ID, input)
	if conflict.Code != 409 {
		t.Fatalf("stale write: %d %s", conflict.Code, conflict.Body)
	}
	latest, err := deps.Knowledgebase.GetArticle(dbtest.Context(t), ws, article.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Body != "CURRENT EDITS" || latest.State != "published" || latest.SEO["title"] != "SEO title" {
		t.Fatalf("lost current edits: %+v", latest)
	}
}
func TestInboxVisibilityProtectsListsSearchFilesAndRealtime(t *testing.T) {
	deps, ws, ownerToken := productHarness(t)
	ctx := dbtest.Context(t)
	owner, err := deps.Auth.UserForSession(ctx, ownerToken)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := deps.Workspace.ActorForUser(ctx, ws, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	private, err := deps.Inbox.Create(ctx, ws, actor.MemberID, "Private", "private", nil, []string{"manual"}, nil, "teams")
	if err != nil {
		t.Fatal(err)
	}
	var publicID string
	if err := deps.Pool.QueryRow(ctx, `SELECT id FROM inboxes WHERE workspace_id=$1 AND is_default`, ws).Scan(&publicID); err != nil {
		t.Fatal(err)
	}
	hidden, _, err := deps.Conversation.Start(ctx, ws, private.ID, "manual", nil, nil, nil, "Agent", "classified secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := deps.Conversation.Start(ctx, ws, publicID, "manual", nil, nil, nil, "Agent", "Public message"); err != nil {
		t.Fatal(err)
	}
	file, err := deps.File.Create(ctx, ws, filemodule.UploadInput{Name: "private.txt", MIMEType: "text/plain", SizeBytes: 6, Body: strings.NewReader("secret"), OwnerType: "conversation", OwnerID: hidden.ID})
	if err != nil {
		t.Fatal(err)
	}
	uid := ids.New(ids.PrefixUser)
	mid := ids.New(ids.PrefixMember)
	if _, err := deps.Pool.Exec(ctx, `INSERT INTO users(id,name,email,password_hash) VALUES($1,'Agent',$2,'unused')`, uid, uid+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.Pool.Exec(ctx, `INSERT INTO workspace_members(id,workspace_id,user_id,role) VALUES($1,$2,$3,'agent')`, mid, ws, uid); err != nil {
		t.Fatal(err)
	}
	session, err := deps.Auth.CreateSessionWithMethod(ctx, uid, "agent-test", "", auth.AuthMethodPassword)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/v1/conversations/" + hidden.ID, "/v1/conversations/" + hidden.ID + "/messages", "/v1/files/" + file.ID} {
		r := productRequest(t, deps, ws, session.Token, "GET", path, nil)
		if r.Code != 404 {
			t.Fatalf("private path %s: %d %s", path, r.Code, r.Body)
		}
	}
	list := productRequest(t, deps, ws, session.Token, "GET", "/v1/conversations", nil)
	if list.Code != 200 || strings.Contains(list.Body.String(), hidden.ID) {
		t.Fatalf("private list leak: %d %s", list.Code, list.Body)
	}
	results := productRequest(t, deps, ws, session.Token, "GET", "/v1/search?q=classified", nil)
	if results.Code != 200 || strings.Contains(results.Body.String(), hidden.ID) {
		t.Fatalf("private search leak: %d %s", results.Code, results.Body)
	}
	scoped, err := deps.Workspace.ActorForUser(ctx, ws, uid)
	if err != nil {
		t.Fatal(err)
	}
	grant := agentRealtimeGrant(ctx, deps, session.Token, scoped)
	if _, allowed := grant.Filter(events.Record{EntityType: "conversation", EntityID: hidden.ID, Type: events.MessageCreated}); allowed {
		t.Fatal("private realtime payload leaked")
	}
}

func TestExpandedSearchCardsReportsAndFollowUps(t *testing.T) {
	deps, ws, token := productHarness(t)
	ctx := dbtest.Context(t)
	user, err := deps.Auth.UserForSession(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := deps.Workspace.ActorForUser(ctx, ws, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	company, err := deps.Customer.CreateCompany(ctx, ws, actor.MemberID, "Expansion company", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := productRequest(t, deps, ws, token, "GET", "/v1/search?q=Expansion&kind=company", nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), company.ID) {
		t.Fatalf("company search: %d %s", response.Code, response.Body)
	}
	var inboxID string
	if err := deps.Pool.QueryRow(ctx, `SELECT id FROM inboxes WHERE workspace_id=$1 AND is_default`, ws).Scan(&inboxID); err != nil {
		t.Fatal(err)
	}
	createdTicket := productRequest(t, deps, ws, token, "POST", "/v1/tickets", map[string]any{"title": "Expansion ticket", "inbox_id": inboxID})
	if createdTicket.Code != 201 {
		t.Fatalf("ticket create: %d %s", createdTicket.Code, createdTicket.Body)
	}
	var ticketRecord struct {
		ID     string `json:"id"`
		Number int    `json:"number"`
		Prefix string `json:"prefix"`
	}
	if err := json.Unmarshal(createdTicket.Body.Bytes(), &ticketRecord); err != nil {
		t.Fatal(err)
	}
	response = productRequest(t, deps, ws, token, "GET", "/v1/search?q=Expansion&kind=ticket", nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), ticketRecord.ID) {
		t.Fatalf("ticket search: %d %s", response.Code, response.Body)
	}
	if _, err := deps.Pool.Exec(ctx, `INSERT INTO attribute_definitions(id,workspace_id,entity_type,key,label,type) VALUES('atr_context_test',$1,'customer','plan','Plan','string')`, ws); err != nil {
		t.Fatal(err)
	}
	response = productRequest(t, deps, ws, token, "POST", "/v1/customer-context/cards", map[string]any{"title": "Account", "fields": []map[string]string{{"key": "plan", "label": "Plan", "format": "text"}}})
	if response.Code != 201 {
		t.Fatalf("context card: %d %s", response.Code, response.Body)
	}
	name, email := "Customer", "expansion@example.test"
	person, err := deps.Customer.Identify(ctx, ws, nil, customer.Identity{Name: &name, Email: &email}, false)
	if err != nil {
		t.Fatal(err)
	}
	conv, first, err := deps.Conversation.Start(ctx, ws, inboxID, "manual", nil, &person.ID, nil, name, "Expansion question")
	if err != nil {
		t.Fatal(err)
	}
	reply, err := deps.Conversation.PostMessage(ctx, ws, conv.ID, nil, "reply", "agent", &actor.MemberID, "Agent", "Expansion answer")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deps.Pool.Exec(ctx, `UPDATE messages SET created_at=CASE WHEN id=$1 THEN now()-interval '5 minutes' ELSE now()-interval '4 minutes' END WHERE id=ANY($2::text[])`, first.ID, []string{first.ID, reply.ID}); err != nil {
		t.Fatal(err)
	}
	response = productRequest(t, deps, ws, token, "GET", "/v1/analytics/drilldown?group_by=inbox", nil)
	if response.Code != 200 {
		t.Fatalf("report: %d %s", response.Code, response.Body)
	}
	var report struct {
		Data []analytics.DrilldownRow `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Data) != 1 || report.Data[0].Current != 1 || report.Data[0].FirstResponseP50 == nil || *report.Data[0].FirstResponseP50 < 59 || *report.Data[0].FirstResponseP50 > 61 {
		t.Fatalf("incorrect cohort/percentile: %+v", report.Data)
	}
	reminder := productRequest(t, deps, ws, token, "POST", "/v1/conversations/"+conv.ID+"/follow-ups", map[string]any{"body": "Review the answer", "scheduled_for": time.Now().Add(time.Hour)})
	if reminder.Code != 201 {
		t.Fatalf("follow-up: %d %s", reminder.Code, reminder.Body)
	}
	var followup automation.FollowUp
	if err := json.Unmarshal(reminder.Body.Bytes(), &followup); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.Pool.Exec(ctx, `UPDATE scheduled_actions SET scheduled_for=now()-interval '1 minute' WHERE id=$1`, followup.ID); err != nil {
		t.Fatal(err)
	}
	executed, failed, err := deps.Automation.RunScheduledActions(ctx)
	if err != nil || executed != 1 || failed != 0 {
		t.Fatalf("execute reminder: %d %d %v", executed, failed, err)
	}
	if executed, _, err := deps.Automation.RunScheduledActions(ctx); err != nil || executed != 0 {
		t.Fatalf("reminder ran twice: %d %v", executed, err)
	}
	var count int
	if err := deps.Pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE workspace_id=$1 AND source_event_id=$2`, ws, "follow-up:"+followup.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("reminder notifications=%d", count)
	}
}
