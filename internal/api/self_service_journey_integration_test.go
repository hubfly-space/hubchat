//go:build integration

package api

import (
	"errors"
	"testing"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/feedback"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/knowledgebase"
	"github.com/hubchat/hubchat/internal/survey"
	"github.com/hubchat/hubchat/internal/workspace"
)

// TestSelfServiceJourney exercises the public feedback, knowledge-base, and
// survey surfaces together. The individual packages have deeper invariant
// tests; this journey proves their tenant and customer contracts compose on
// the same PostgreSQL workspace.
func TestSelfServiceJourney(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	ownerID := ids.New(ids.PrefixUser)
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, name, email, password_hash, email_verified_at)
		VALUES ($1, 'Self-service Owner', $2, 'x', now())
	`, ownerID, ownerID+"@example.com"); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	eventLog := events.New(pool)
	auditLog := audit.New(pool)
	workspaceService := workspace.New(pool, eventLog, auditLog)
	createdWorkspace, err := workspaceService.Bootstrap(ctx, ownerID, "Self-service workspace", "self-service-"+ownerID[len(ownerID)-10:])
	if err != nil {
		t.Fatalf("bootstrap workspace: %v", err)
	}
	actor, err := workspaceService.ActorForUser(ctx, createdWorkspace.ID, ownerID)
	if err != nil {
		t.Fatalf("resolve owner actor: %v", err)
	}

	customerID := ids.New(ids.PrefixCustomer)
	if _, err := pool.Exec(ctx, `
		INSERT INTO customers (id, workspace_id, name, email)
		VALUES ($1, $2, 'Self-service Customer', 'self-service-customer@example.com')
	`, customerID, createdWorkspace.ID); err != nil {
		t.Fatalf("seed customer: %v", err)
	}

	feedbackService := feedback.New(pool, eventLog, auditLog)
	board, err := feedbackService.CreateBoard(ctx, createdWorkspace.ID, feedback.BoardInput{
		Name: "Product feedback", Slug: "product-feedback", AllowComments: boolPtr(true), AllowVoting: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create feedback board: %v", err)
	}
	item, err := feedbackService.CreateItem(ctx, createdWorkspace.ID, board.ID, actor.MemberID, feedback.ItemInput{
		Title: "Add keyboard shortcuts", Description: "Make frequent support actions faster.", Type: "feature_request",
	}, customerID)
	if err != nil {
		t.Fatalf("submit feedback item: %v", err)
	}
	if err := feedbackService.Vote(ctx, createdWorkspace.ID, item.ID, customerID); err != nil {
		t.Fatalf("vote for feedback item: %v", err)
	}
	if _, err := feedbackService.AddComment(ctx, createdWorkspace.ID, item.ID, "customer", customerID, "Self-service Customer", "This would save our team time.", false); err != nil {
		t.Fatalf("comment on feedback item: %v", err)
	}
	item, err = feedbackService.SetStatus(ctx, createdWorkspace.ID, item.ID, actor.MemberID, "planned", "Accepted for a future release")
	if err != nil {
		t.Fatalf("moderate feedback item: %v", err)
	}
	if item.Status != "planned" || item.VoteCount != 1 || item.CommentCount != 1 || item.SubmitterID == nil || *item.SubmitterID != customerID {
		t.Fatalf("feedback item = %+v, want planned item with customer attribution and counters", item)
	}

	kbService := knowledgebase.New(pool, knowledgebase.Options{Events: eventLog})
	kb, err := kbService.CreateKnowledgeBase(ctx, createdWorkspace.ID, knowledgebase.KnowledgeBaseInput{
		Name: "Help center", Slug: "help", DefaultLanguage: "en", Languages: []string{"en"}, Visibility: "public",
	})
	if err != nil {
		t.Fatalf("create knowledge base: %v", err)
	}
	collection, err := kbService.CreateCollection(ctx, createdWorkspace.ID, kb.ID, knowledgebase.CollectionInput{
		Name: "Getting started", Slug: "getting-started",
	})
	if err != nil {
		t.Fatalf("create knowledge-base collection: %v", err)
	}
	article, err := kbService.SaveArticle(ctx, createdWorkspace.ID, actor.MemberID, "", knowledgebase.ArticleInput{
		KnowledgeBaseID: kb.ID, CollectionID: &collection.ID, Title: "Keyboard shortcuts", Slug: "keyboard-shortcuts",
		Excerpt: "Speed up support work.", Body: "Use **keyboard shortcuts** to move through support work faster.", Language: "en", State: "draft",
	})
	if err != nil {
		t.Fatalf("save article draft: %v", err)
	}
	article, err = kbService.PublishArticle(ctx, createdWorkspace.ID, article.ID)
	if err != nil {
		t.Fatalf("publish article: %v", err)
	}
	results, err := kbService.SearchPublished(ctx, createdWorkspace.ID, kb.Slug, collection.Slug, "keyboard shortcuts", "en", "portal", 10)
	if err != nil {
		t.Fatalf("search published article: %v", err)
	}
	if len(results) != 1 || results[0].Article.ID != article.ID || results[0].Article.State != "published" {
		t.Fatalf("published search results = %+v, want article %q", results, article.ID)
	}
	if err := kbService.RecordArticleFeedback(ctx, createdWorkspace.ID, article.ID, true, "Clear and useful", customerID, []byte("self-service-article-fingerprint")); err != nil {
		t.Fatalf("record article feedback: %v", err)
	}
	updatedArticle, err := kbService.GetArticle(ctx, createdWorkspace.ID, article.ID)
	if err != nil {
		t.Fatalf("reload article: %v", err)
	}
	if updatedArticle.HelpfulCount != 1 || updatedArticle.UnhelpfulCount != 0 {
		t.Fatalf("article helpfulness = %d/%d, want 1/0", updatedArticle.HelpfulCount, updatedArticle.UnhelpfulCount)
	}

	surveyService := survey.New(pool, survey.Options{Events: eventLog})
	createdSurvey, err := surveyService.Create(ctx, createdWorkspace.ID, survey.Input{
		Name: "Support satisfaction", Type: "csat", Delivery: []string{"portal"},
		Questions: []survey.QuestionInput{{Prompt: "How was this help?", Type: "number", Required: true}},
	})
	if err != nil {
		t.Fatalf("create survey: %v", err)
	}
	questionID := createdSurvey.Questions[0].ID
	response, err := surveyService.Submit(ctx, createdWorkspace.ID, createdSurvey.ID, customerID, survey.ResponseInput{
		Answers: map[string]any{questionID: float64(5)}, Comment: "Very helpful",
	})
	if err != nil {
		t.Fatalf("submit survey response: %v", err)
	}
	if response.CustomerID == nil || *response.CustomerID != customerID || response.Score == nil || *response.Score != 5 {
		t.Fatalf("survey response = %+v, want identified score 5", response)
	}
	summary, err := surveyService.Summary(ctx, createdWorkspace.ID, createdSurvey.ID)
	if err != nil {
		t.Fatalf("summarize survey: %v", err)
	}
	if summary.ResponseCount != 1 || summary.AverageScore == nil || *summary.AverageScore != 5 || summary.CommentCount != 1 {
		t.Fatalf("survey summary = %+v, want one score-5 response with comment", summary)
	}

	otherWorkspaceID := ids.New(ids.PrefixWorkspace)
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id, name, slug) VALUES ($1, 'Other workspace', $2)`, otherWorkspaceID, "other-"+otherWorkspaceID[len(otherWorkspaceID)-10:]); err != nil {
		t.Fatalf("seed other workspace: %v", err)
	}
	otherBoards, err := feedbackService.ListPublicBoardsPage(ctx, otherWorkspaceID, nil, "", 10)
	if err != nil || len(otherBoards) != 0 {
		t.Fatalf("cross-workspace feedback boards = %+v, err=%v", otherBoards, err)
	}
	otherArticles, err := kbService.SearchPublished(ctx, otherWorkspaceID, kb.Slug, "", "keyboard", "en", "portal", 10)
	if err != nil || len(otherArticles) != 0 {
		t.Fatalf("cross-workspace articles = %+v, err=%v", otherArticles, err)
	}
	if _, err := surveyService.Get(ctx, otherWorkspaceID, createdSurvey.ID); !errors.Is(err, survey.ErrNotFound) {
		t.Fatalf("cross-workspace survey lookup error = %v, want ErrNotFound", err)
	}
}

func boolPtr(value bool) *bool {
	return &value
}
