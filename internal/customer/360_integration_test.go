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
	if context.FeedbackTruncated || context.SurveysTruncated || context.ArticlesTruncated || context.IdentitiesTruncated || context.MergesTruncated {
		t.Fatal("small customer 360 result was unexpectedly truncated")
	}

	if _, err := svc.Customer360(ctx, otherWorkspaceID, customerID); err == nil {
		t.Fatal("customer 360 crossed workspace boundary")
	}
	if _, err := svc.Customer360(ctx, workspaceID, otherCustomerID); err == nil {
		t.Fatal("customer 360 returned a foreign customer")
	}

}
