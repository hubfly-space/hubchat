//go:build integration

package knowledgebase

import (
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/workspace"
)

func TestPublishScheduledPromotesDueArticlesAndAppendsEvent(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	userID := ids.New(ids.PrefixUser)
	if _, err := pool.Exec(ctx, `INSERT INTO users (id,name,email,password_hash,email_verified_at) VALUES ($1,'KB scheduler owner',$2,'x',now())`, userID, userID+"@example.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	workspaceService := workspace.New(pool, events.New(pool), audit.New(pool))
	createdWorkspace, err := workspaceService.Bootstrap(ctx, userID, "KB scheduler", "kb-scheduler-"+userID[len(userID)-10:])
	if err != nil {
		t.Fatalf("bootstrap workspace: %v", err)
	}
	actor, err := workspaceService.ActorForUser(ctx, createdWorkspace.ID, userID)
	if err != nil {
		t.Fatalf("resolve actor: %v", err)
	}
	eventLog := events.New(pool)
	service := New(pool, Options{Events: eventLog})
	kb, err := service.CreateKnowledgeBase(ctx, createdWorkspace.ID, KnowledgeBaseInput{Name: "Help", Slug: "help"})
	if err != nil {
		t.Fatalf("create knowledge base: %v", err)
	}
	due := time.Now().UTC().Add(-time.Minute)
	article, err := service.SaveArticle(ctx, createdWorkspace.ID, actor.MemberID, "", ArticleInput{
		KnowledgeBaseID: kb.ID, Title: "Scheduled article", Slug: "scheduled-article", Body: "Published on time.",
		State: "scheduled", Language: "en", ScheduledAt: &due,
	})
	if err != nil {
		t.Fatalf("create scheduled article: %v", err)
	}
	count, err := service.PublishScheduled(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("publish scheduled articles: %v", err)
	}
	if count != 1 {
		t.Fatalf("published count = %d, want 1", count)
	}
	updated, err := service.GetArticle(ctx, createdWorkspace.ID, article.ID)
	if err != nil {
		t.Fatalf("read published article: %v", err)
	}
	if updated.State != "published" || updated.PublishedAt == nil {
		t.Fatalf("article state=%q published_at=%v, want published timestamp", updated.State, updated.PublishedAt)
	}
	eventsForArticle, err := eventLog.ForEntity(ctx, createdWorkspace.ID, "article", article.ID, 10)
	if err != nil {
		t.Fatalf("read article events: %v", err)
	}
	if len(eventsForArticle) != 1 || eventsForArticle[0].Type != events.ArticlePublished {
		t.Fatalf("unexpected article events: %+v", eventsForArticle)
	}
	count, err = service.PublishScheduled(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("repeat scheduled publish: %v", err)
	}
	if count != 0 {
		t.Fatalf("repeat published count = %d, want 0", count)
	}
}

func TestListChangelogPageIsWorkspaceScopedAndStable(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	for _, workspace := range []struct{ id, name, slug string }{
		{"wrk_changelog_page_a", "Changelog A", "changelog-page-a"},
		{"wrk_changelog_page_b", "Changelog B", "changelog-page-b"},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id,name,slug) VALUES ($1,$2,$3)`, workspace.id, workspace.name, workspace.slug); err != nil {
			t.Fatal(err)
		}
	}
	for _, entry := range []struct {
		id, workspace, title string
		published            bool
	}{
		{"clg_page_a1", "wrk_changelog_page_a", "First", true},
		{"clg_page_a2", "wrk_changelog_page_a", "Second", false},
		{"clg_page_a3", "wrk_changelog_page_a", "Third", true},
		{"clg_page_b1", "wrk_changelog_page_b", "Other workspace", true},
	} {
		var published any
		if entry.published {
			published = time.Now().UTC()
		}
		if _, err := pool.Exec(ctx, `INSERT INTO changelog_entries (id,workspace_id,title,published_at) VALUES ($1,$2,$3,$4)`, entry.id, entry.workspace, entry.title, published); err != nil {
			t.Fatal(err)
		}
	}

	service := New(pool, Options{})
	first, err := service.ListChangelogPage(ctx, "wrk_changelog_page_a", time.Time{}, "", 2)
	if err != nil || len(first) != 2 {
		t.Fatalf("first changelog page = %d rows, err=%v", len(first), err)
	}
	before := first[1].CreatedAt
	if first[1].PublishedAt != nil {
		before = *first[1].PublishedAt
	}
	second, err := service.ListChangelogPage(ctx, "wrk_changelog_page_a", before, first[1].ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].WorkspaceID != "wrk_changelog_page_a" || second[0].ID == first[1].ID {
		t.Fatalf("second changelog page = %+v", second)
	}
}
