//go:build integration

package customer_test

import (
	"testing"

	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/ids"
)

func TestCustomer360CombinesRelatedRecordsAndPreservesTenantIsolation(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	workspaceID, memberID := seedWorkspace(t, ctx, pool)
	otherWorkspaceID, _ := seedWorkspace(t, ctx, pool)
	customerID := seedCustomer(t, ctx, pool, workspaceID, "Ada")
	loserID := seedCustomer(t, ctx, pool, workspaceID, "Merged identity")
	otherCustomerID := seedCustomer(t, ctx, pool, otherWorkspaceID, "Other")

	emailID := ids.New(ids.PrefixCustomerEmail)
	phoneID := ids.New(ids.PrefixCustomerPhone)
	boardID := ids.New(ids.PrefixFeedbackBoard)
	itemID := ids.New(ids.PrefixFeedbackItem)
	surveyID := ids.New(ids.PrefixSurvey)
	responseID := ids.New(ids.PrefixSurveyResponse)
	knowledgeBaseID := ids.New(ids.PrefixKnowledgeBase)
	articleID := ids.New(ids.PrefixArticle)
	articleFeedbackID := ids.New(ids.PrefixArticleFeedback)
	mergeID := ids.New(ids.PrefixMerge)
	companyID := ids.New(ids.PrefixCompany)
	conversationID := ids.New(ids.PrefixConversation)
	ticketID := ids.New(ids.PrefixTicket)
	eventID := ids.New(ids.PrefixCustomerEvent)
	sessionID := ids.New(ids.PrefixContactSession)
	var inboxID string
	if err := pool.QueryRow(ctx, `SELECT id FROM inboxes WHERE workspace_id=$1 ORDER BY id LIMIT 1`, workspaceID).Scan(&inboxID); err != nil {
		t.Fatalf("find default inbox: %v", err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("seed customer 360 records: %v", err)
		}
	}
	exec(`INSERT INTO customer_emails (id,workspace_id,customer_id,email,verified_at,is_primary) VALUES ($1,$2,$3,'ada@example.com',now(),true)`, emailID, workspaceID, customerID)
	exec(`INSERT INTO customer_phones (id,workspace_id,customer_id,phone,verified_at) VALUES ($1,$2,$3,'+250788000000',now())`, phoneID, workspaceID, customerID)
	exec(`INSERT INTO feedback_boards (id,workspace_id,name,slug) VALUES ($1,$2,'Roadmap','roadmap')`, boardID, workspaceID)
	exec(`INSERT INTO feedback_items (id,workspace_id,board_id,title,submitter_id,status) VALUES ($1,$2,$3,'Exportable customer data',$4,'planned')`, itemID, workspaceID, boardID, customerID)
	exec(`INSERT INTO feedback_votes (item_id,customer_id,workspace_id) VALUES ($1,$2,$3)`, itemID, customerID, workspaceID)
	exec(`INSERT INTO surveys (id,workspace_id,name,type) VALUES ($1,$2,'Post-resolution CSAT','csat')`, surveyID, workspaceID)
	exec(`INSERT INTO survey_responses (id,workspace_id,survey_id,customer_id,score,comment,submitted_at) VALUES ($1,$2,$3,$4,5,'Excellent',now())`, responseID, workspaceID, surveyID, customerID)
	exec(`INSERT INTO knowledge_bases (id,workspace_id,name,slug) VALUES ($1,$2,'Help','help')`, knowledgeBaseID, workspaceID)
	exec(`INSERT INTO articles (id,workspace_id,knowledge_base_id,title,slug,state,published_at) VALUES ($1,$2,$3,'Export guide','export-guide','published',now())`, articleID, workspaceID, knowledgeBaseID)
	exec(`INSERT INTO article_feedback (id,workspace_id,article_id,customer_id,helpful,comment) VALUES ($1,$2,$3,$4,true,'Clear and useful')`, articleFeedbackID, workspaceID, articleID, customerID)
	exec(`INSERT INTO identity_merge_history (id,workspace_id,winner_id,loser_id,loser_snapshot,moved_counts,merged_by,reversible_until) VALUES ($1,$2,$3,$4,'{}'::jsonb,'{"conversations":2}'::jsonb,$5,now()+interval '30 days')`, mergeID, workspaceID, customerID, loserID, memberID)
	exec(`INSERT INTO companies (id,workspace_id,name,domain,tier) VALUES ($1,$2,'Acme','acme.example','growth')`, companyID, workspaceID)
	exec(`INSERT INTO company_customers (company_id,customer_id) VALUES ($1,$2)`, companyID, customerID)
	exec(`INSERT INTO conversations (id,workspace_id,inbox_id,channel,subject,state,customer_id,last_message_preview) VALUES ($1,$2,$3,'widget','Checkout question','open',$4,'Can you help with checkout?')`, conversationID, workspaceID, inboxID, customerID)
	exec(`INSERT INTO tickets (id,workspace_id,number,prefix,title,status,priority,customer_id,conversation_id) VALUES ($1,$2,1001,'SUP','Checkout follow-up','pending','high',$3,$4)`, ticketID, workspaceID, customerID, conversationID)
	exec(`INSERT INTO customer_events (id,workspace_id,customer_id,type,source,url,request_id,payload) VALUES ($1,$2,$3,'checkout.started','js_sdk','https://acme.example/checkout','req_360','{"order_id":"ord_1"}'::jsonb)`, eventID, workspaceID, customerID)
	exec(`INSERT INTO contact_sessions (id,workspace_id,customer_id,device,browser,os,current_url,page_views) VALUES ($1,$2,$3,'desktop','Firefox','Linux','https://acme.example/checkout',3)`, sessionID, workspaceID, customerID)

	context, err := svc.Customer360(ctx, workspaceID, customerID)
	if err != nil {
		t.Fatalf("customer 360: %v", err)
	}
	if len(context.Feedback) != 1 || context.Feedback[0].Title != "Exportable customer data" {
		t.Fatalf("feedback context = %+v", context.Feedback)
	}
	if len(context.Surveys) != 1 || context.Surveys[0].Score == nil || *context.Surveys[0].Score != 5 {
		t.Fatalf("survey context = %+v", context.Surveys)
	}
	if len(context.Articles) != 1 || context.Articles[0].Slug != "export-guide" {
		t.Fatalf("article context = %+v", context.Articles)
	}
	if len(context.Identities) != 2 || len(context.Merges) != 1 || context.Merges[0].MovedCounts["conversations"] != float64(2) {
		t.Fatalf("identity context = %+v, merges = %+v", context.Identities, context.Merges)
	}
	if len(context.Companies) != 1 || context.Companies[0].Name != "Acme" {
		t.Fatalf("company context = %+v", context.Companies)
	}
	if len(context.Conversations) != 1 || context.Conversations[0].ID != conversationID {
		t.Fatalf("conversation context = %+v", context.Conversations)
	}
	if len(context.Tickets) != 1 || context.Tickets[0].Number != 1001 {
		t.Fatalf("ticket context = %+v", context.Tickets)
	}
	if len(context.Events) != 1 || context.Events[0].RequestID == nil || *context.Events[0].RequestID != "req_360" {
		t.Fatalf("event context = %+v", context.Events)
	}
	if len(context.Sessions) != 1 || context.Sessions[0].PageViews != 3 {
		t.Fatalf("session context = %+v", context.Sessions)
	}
	if context.FeedbackTruncated || context.SurveysTruncated || context.ArticlesTruncated || context.IdentitiesTruncated || context.MergesTruncated {
		t.Fatal("small customer 360 result was unexpectedly truncated")
	}
	if context.CompaniesTruncated || context.ConversationsTruncated || context.TicketsTruncated || context.EventsTruncated || context.SessionsTruncated {
		t.Fatal("small customer 360 support result was unexpectedly truncated")
	}

	if _, err := svc.Customer360(ctx, otherWorkspaceID, customerID); err == nil {
		t.Fatal("customer 360 crossed workspace boundary")
	}
	if _, err := svc.Customer360(ctx, workspaceID, otherCustomerID); err == nil {
		t.Fatal("customer 360 returned a foreign customer")
	}

}
