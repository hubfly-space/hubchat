// Package knowledgebase owns deterministic, searchable self-service content.
// Articles are stored as Markdown, revisions are append-only, and only
// published rows are visible through the public search surface.
package knowledgebase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
)

var (
	ErrNotFound                = errors.New("knowledgebase: not found")
	ErrInvalidName             = errors.New("knowledgebase: name is required")
	ErrInvalidSlug             = errors.New("knowledgebase: slug must contain lowercase letters, numbers, and hyphens")
	ErrInvalidState            = errors.New("knowledgebase: invalid article state")
	ErrInvalidLanguage         = errors.New("knowledgebase: language is required")
	ErrInvalidArticle          = errors.New("knowledgebase: article title and knowledge base are required")
	ErrInvalidChangelog        = errors.New("knowledgebase: invalid changelog entry")
	ErrFeedbackAlreadyRecorded = errors.New("knowledgebase: article feedback already recorded")
)

const JobPublishScheduled = "knowledgebase.publish_scheduled"

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type Options struct {
	Events *events.Log
}

type Service struct {
	pool   *database.Pool
	events *events.Log
}

type KnowledgeBase struct {
	ID              string    `json:"id"`
	WorkspaceID     string    `json:"workspace_id"`
	Name            string    `json:"name"`
	Slug            string    `json:"slug"`
	DefaultLanguage string    `json:"default_language"`
	Languages       []string  `json:"languages"`
	Visibility      string    `json:"visibility"`
	ArticleCount    int       `json:"article_count"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Collection struct {
	ID              string    `json:"id"`
	WorkspaceID     string    `json:"workspace_id"`
	KnowledgeBaseID string    `json:"knowledge_base_id"`
	ParentID        *string   `json:"parent_id,omitempty"`
	Name            string    `json:"name"`
	Slug            string    `json:"slug"`
	Description     string    `json:"description,omitempty"`
	Icon            string    `json:"icon,omitempty"`
	ArticleCount    int       `json:"article_count"`
	Position        int       `json:"position"`
	CreatedAt       time.Time `json:"created_at"`
}

type Article struct {
	ID              string         `json:"id"`
	WorkspaceID     string         `json:"workspace_id"`
	KnowledgeBaseID string         `json:"knowledge_base_id"`
	CollectionID    *string        `json:"collection_id,omitempty"`
	Title           string         `json:"title"`
	Slug            string         `json:"slug"`
	Excerpt         string         `json:"excerpt"`
	Body            string         `json:"body"`
	State           string         `json:"state"`
	Language        string         `json:"language"`
	AuthorID        *string        `json:"author_id,omitempty"`
	SEO             map[string]any `json:"seo"`
	ViewCount       int            `json:"view_count"`
	HelpfulCount    int            `json:"helpful_count"`
	UnhelpfulCount  int            `json:"unhelpful_count"`
	Version         int            `json:"version"`
	ScheduledAt     *time.Time     `json:"scheduled_at,omitempty"`
	PublishedAt     *time.Time     `json:"published_at,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type ArticleRevision struct {
	ID        string    `json:"id"`
	ArticleID string    `json:"article_id"`
	Version   int       `json:"version"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Excerpt   string    `json:"excerpt"`
	EditedBy  *string   `json:"edited_by,omitempty"`
	Note      *string   `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type ChangelogEntry struct {
	ID             string     `json:"id"`
	WorkspaceID    string     `json:"workspace_id"`
	Title          string     `json:"title"`
	Body           string     `json:"body"`
	Kind           string     `json:"kind"`
	FeedbackItemID *string    `json:"feedback_item_id,omitempty"`
	PublishedAt    *time.Time `json:"published_at,omitempty"`
	CreatedBy      *string    `json:"created_by,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type KnowledgeBaseInput struct {
	Name            string   `json:"name"`
	Slug            string   `json:"slug"`
	DefaultLanguage string   `json:"default_language"`
	Languages       []string `json:"languages"`
	Visibility      string   `json:"visibility"`
}
type CollectionInput struct {
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	ParentID    *string `json:"parent_id"`
	Description string  `json:"description"`
	Icon        string  `json:"icon"`
	Position    int     `json:"position"`
}
type ArticleInput struct {
	KnowledgeBaseID string         `json:"knowledge_base_id"`
	CollectionID    *string        `json:"collection_id"`
	Title           string         `json:"title"`
	Slug            string         `json:"slug"`
	Excerpt         string         `json:"excerpt"`
	Body            string         `json:"body"`
	State           string         `json:"state"`
	Language        string         `json:"language"`
	SEO             map[string]any `json:"seo"`
	ScheduledAt     *time.Time     `json:"scheduled_at"`
}

type ChangelogInput struct {
	Title          string  `json:"title"`
	Body           string  `json:"body"`
	Kind           string  `json:"kind"`
	FeedbackItemID *string `json:"feedback_item_id"`
}

type SearchResult struct {
	Article Article `json:"article"`
	Rank    float32 `json:"rank"`
}

func New(pool *database.Pool, options ...Options) *Service {
	service := &Service{pool: pool}
	if len(options) > 0 {
		service.events = options[0].Events
	}
	return service
}

func (s *Service) CreateKnowledgeBase(ctx context.Context, workspaceID string, input KnowledgeBaseInput) (*KnowledgeBase, error) {
	name, slug := strings.TrimSpace(input.Name), strings.ToLower(strings.TrimSpace(input.Slug))
	if name == "" {
		return nil, ErrInvalidName
	}
	if !slugPattern.MatchString(slug) {
		return nil, ErrInvalidSlug
	}
	language := strings.TrimSpace(input.DefaultLanguage)
	if language == "" {
		language = "en"
	}
	languages := uniqueLanguages(input.Languages)
	if len(languages) == 0 {
		languages = []string{language}
	}
	visibility := input.Visibility
	if visibility == "" {
		visibility = "public"
	}
	if visibility != "public" && visibility != "private" {
		return nil, errors.New("knowledgebase: invalid visibility")
	}
	id := ids.New(ids.PrefixKnowledgeBase)
	_, err := s.pool.Exec(ctx, `INSERT INTO knowledge_bases(id,workspace_id,name,slug,default_language,languages,visibility) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, workspaceID, name, slug, language, languages, visibility)
	if err != nil {
		return nil, fmt.Errorf("knowledgebase: create: %w", err)
	}
	return s.GetKnowledgeBase(ctx, workspaceID, id)
}

func (s *Service) ListKnowledgeBases(ctx context.Context, workspaceID string) ([]KnowledgeBase, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,workspace_id,name,slug,default_language,languages,visibility,article_count,created_at,updated_at FROM knowledge_bases WHERE workspace_id=$1 ORDER BY created_at DESC,id DESC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]KnowledgeBase, 0)
	for rows.Next() {
		var item KnowledgeBase
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Slug, &item.DefaultLanguage, &item.Languages, &item.Visibility, &item.ArticleCount, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// ListKnowledgeBasesPage returns knowledge bases in the same newest-first
// order as ListKnowledgeBases, with the created timestamp and id as a stable
// cursor pair.
func (s *Service) ListKnowledgeBasesPage(ctx context.Context, workspaceID string, before time.Time, beforeID string, limit int) ([]KnowledgeBase, error) {
	if limit <= 0 || limit > 201 {
		limit = 101
	}
	where := "workspace_id=$1"
	args := []any{workspaceID}
	if !before.IsZero() {
		where += " AND (created_at,id) < ($2,$3)"
		args = append(args, before, beforeID)
	}
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, `SELECT id,workspace_id,name,slug,default_language,languages,visibility,article_count,created_at,updated_at FROM knowledge_bases WHERE `+where+` ORDER BY created_at DESC,id DESC LIMIT $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]KnowledgeBase, 0)
	for rows.Next() {
		var item KnowledgeBase
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Slug, &item.DefaultLanguage, &item.Languages, &item.Visibility, &item.ArticleCount, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) GetKnowledgeBase(ctx context.Context, workspaceID, id string) (*KnowledgeBase, error) {
	var item KnowledgeBase
	err := s.pool.QueryRow(ctx, `SELECT id,workspace_id,name,slug,default_language,languages,visibility,article_count,created_at,updated_at FROM knowledge_bases WHERE workspace_id=$1 AND id=$2`, workspaceID, id).Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Slug, &item.DefaultLanguage, &item.Languages, &item.Visibility, &item.ArticleCount, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Service) CreateCollection(ctx context.Context, workspaceID, kbID string, input CollectionInput) (*Collection, error) {
	if strings.TrimSpace(input.Name) == "" || !slugPattern.MatchString(strings.ToLower(strings.TrimSpace(input.Slug))) {
		return nil, ErrInvalidSlug
	}
	if _, err := s.GetKnowledgeBase(ctx, workspaceID, kbID); err != nil {
		return nil, err
	}
	id := ids.New(ids.PrefixCollection)
	var item Collection
	err := s.pool.QueryRow(ctx, `INSERT INTO article_collections(id,workspace_id,knowledge_base_id,parent_id,name,slug,description,icon,position) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9) RETURNING id,workspace_id,knowledge_base_id,parent_id,name,slug,coalesce(description,''),coalesce(icon,''),article_count,position,created_at`, id, workspaceID, kbID, input.ParentID, strings.TrimSpace(input.Name), strings.ToLower(strings.TrimSpace(input.Slug)), strings.TrimSpace(input.Description), input.Icon, input.Position).Scan(&item.ID, &item.WorkspaceID, &item.KnowledgeBaseID, &item.ParentID, &item.Name, &item.Slug, &item.Description, &item.Icon, &item.ArticleCount, &item.Position, &item.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("knowledgebase: create collection: %w", err)
	}
	return &item, nil
}

func (s *Service) ListCollections(ctx context.Context, workspaceID, kbID string) ([]Collection, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,workspace_id,knowledge_base_id,parent_id,name,slug,coalesce(description,''),coalesce(icon,''),article_count,position,created_at FROM article_collections WHERE workspace_id=$1 AND knowledge_base_id=$2 ORDER BY position,id`, workspaceID, kbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Collection, 0)
	for rows.Next() {
		var item Collection
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.KnowledgeBaseID, &item.ParentID, &item.Name, &item.Slug, &item.Description, &item.Icon, &item.ArticleCount, &item.Position, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// ListCollectionsPage orders by the user-controlled position and id. The
// position is carried as the cursor's non-time value by the HTTP layer.
func (s *Service) ListCollectionsPage(ctx context.Context, workspaceID, kbID string, beforePosition int, beforeID string, hasCursor bool, limit int) ([]Collection, error) {
	if limit <= 0 || limit > 201 {
		limit = 101
	}
	where := "workspace_id=$1 AND knowledge_base_id=$2"
	args := []any{workspaceID, kbID}
	if hasCursor {
		where += " AND (position,id) > ($3,$4)"
		args = append(args, beforePosition, beforeID)
	}
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, `SELECT id,workspace_id,knowledge_base_id,parent_id,name,slug,coalesce(description,''),coalesce(icon,''),article_count,position,created_at FROM article_collections WHERE `+where+` ORDER BY position,id LIMIT $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Collection, 0)
	for rows.Next() {
		var item Collection
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.KnowledgeBaseID, &item.ParentID, &item.Name, &item.Slug, &item.Description, &item.Icon, &item.ArticleCount, &item.Position, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) ListArticles(ctx context.Context, workspaceID, state, query string, limit int) ([]Article, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT id,workspace_id,knowledge_base_id,collection_id,title,slug,excerpt,body,state,language,author_id,seo,view_count,helpful_count,unhelpful_count,version,scheduled_at,published_at,created_at,updated_at FROM articles WHERE workspace_id=$1 AND ($2='' OR state=$2) AND ($3='' OR title ILIKE '%'||$3||'%' OR excerpt ILIKE '%'||$3||'%') ORDER BY updated_at DESC,id DESC LIMIT $4`, workspaceID, state, strings.TrimSpace(query), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanArticles(rows)
}

// ListArticlesPage orders by the mutable update timestamp plus an id
// tiebreaker so a cursor remains deterministic when several articles are
// edited in the same instant.
func (s *Service) ListArticlesPage(ctx context.Context, workspaceID, state, query string, before time.Time, beforeID string, limit int) ([]Article, error) {
	if limit <= 0 || limit > 201 {
		limit = 101
	}
	where := []string{"workspace_id=$1", "($2='' OR state=$2)", "($3='' OR title ILIKE '%'||$3||'%' OR excerpt ILIKE '%'||$3||'%')"}
	args := []any{workspaceID, state, strings.TrimSpace(query)}
	if !before.IsZero() {
		where = append(where, fmt.Sprintf("(updated_at,id) < ($%d,$%d)", len(args)+1, len(args)+2))
		args = append(args, before, beforeID)
	}
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, `SELECT id,workspace_id,knowledge_base_id,collection_id,title,slug,excerpt,body,state,language,author_id,seo,view_count,helpful_count,unhelpful_count,version,scheduled_at,published_at,created_at,updated_at FROM articles WHERE `+strings.Join(where, " AND ")+` ORDER BY updated_at DESC,id DESC LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanArticles(rows)
}

func (s *Service) GetArticle(ctx context.Context, workspaceID, id string) (*Article, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,workspace_id,knowledge_base_id,collection_id,title,slug,excerpt,body,state,language,author_id,seo,view_count,helpful_count,unhelpful_count,version,scheduled_at,published_at,created_at,updated_at FROM articles WHERE workspace_id=$1 AND id=$2`, workspaceID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := scanArticles(rows)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrNotFound
	}
	return &items[0], nil
}

func (s *Service) ListArticleRevisionsPage(ctx context.Context, workspaceID, articleID string, before time.Time, beforeID string, limit int) ([]ArticleRevision, error) {
	if limit <= 0 || limit > 201 {
		limit = 101
	}
	where := "a.workspace_id=$1 AND r.article_id=$2"
	args := []any{workspaceID, articleID}
	if !before.IsZero() {
		where += " AND (r.created_at,r.id)<($3,$4)"
		args = append(args, before, beforeID)
	}
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.article_id, r.version, r.title, r.body, r.excerpt, r.edited_by, r.note, r.created_at
		FROM article_revisions r JOIN articles a ON a.id=r.article_id
		WHERE `+where+` ORDER BY r.created_at DESC, r.id DESC LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("knowledgebase: list article revisions: %w", err)
	}
	defer rows.Close()
	revisions := make([]ArticleRevision, 0)
	for rows.Next() {
		var revision ArticleRevision
		if err := rows.Scan(&revision.ID, &revision.ArticleID, &revision.Version, &revision.Title, &revision.Body, &revision.Excerpt, &revision.EditedBy, &revision.Note, &revision.CreatedAt); err != nil {
			return nil, err
		}
		revisions = append(revisions, revision)
	}
	return revisions, rows.Err()
}

// FindArticleBySlug resolves an article inside one workspace and knowledge
// base. The language predicate keeps translated articles independent.
func (s *Service) FindArticleBySlug(ctx context.Context, workspaceID, knowledgeBaseID, language, slug string) (*Article, error) {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT id FROM articles WHERE workspace_id=$1 AND knowledge_base_id=$2 AND language=$3 AND slug=$4`, workspaceID, knowledgeBaseID, language, strings.ToLower(strings.TrimSpace(slug))).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.GetArticle(ctx, workspaceID, id)
}

func (s *Service) GetPublishedBySlug(ctx context.Context, workspaceID, slug string) (*Article, error) {
	return s.GetPublishedBySlugSurface(ctx, workspaceID, slug, "")
}

// GetPublishedBySlugSurface increments the published article view counter and
// records the same committed action in the workspace event log. The surface is
// an allowlisted reporting dimension (for example, portal or widget), not
// arbitrary customer payload.
func (s *Service) GetPublishedBySlugSurface(ctx context.Context, workspaceID, slug, surface string) (*Article, error) {
	surface = normalizeSurface(surface)
	var id string
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `UPDATE articles SET view_count=view_count+1 WHERE workspace_id=$1 AND slug=$2 AND state='published' RETURNING id`, workspaceID, strings.ToLower(strings.TrimSpace(slug))).Scan(&id); err != nil {
			return err
		}
		if s.events == nil {
			return nil
		}
		_, err := s.events.Append(ctx, tx, events.Event{
			WorkspaceID: workspaceID,
			Type:        events.ArticleViewed,
			EntityType:  "article",
			EntityID:    id,
			ActorType:   events.ActorVisitor,
			Data:        map[string]any{"surface": strings.TrimSpace(surface)},
		})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.GetArticle(ctx, workspaceID, id)
}

func (s *Service) RecordArticleFeedback(ctx context.Context, workspaceID, articleID string, helpful bool, comment, customerID string, fingerprint []byte) error {
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM articles WHERE workspace_id=$1 AND id=$2 AND state='published')`, workspaceID, articleID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		_, err := tx.Exec(ctx, `INSERT INTO article_feedback(id,workspace_id,article_id,helpful,comment,customer_id,fingerprint) VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),$7)`, ids.New(ids.PrefixArticleFeedback), workspaceID, articleID, helpful, strings.TrimSpace(comment), customerID, fingerprint)
		if err != nil {
			if strings.Contains(err.Error(), "article_feedback_one_per_fingerprint") || strings.Contains(err.Error(), "duplicate key") {
				return ErrFeedbackAlreadyRecorded
			}
			return err
		}
		column := "unhelpful_count"
		if helpful {
			column = "helpful_count"
		}
		if _, err = tx.Exec(ctx, `UPDATE articles SET `+column+`=`+column+`+1,updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, articleID); err != nil {
			return err
		}
		if s.events == nil {
			return nil
		}
		_, err = s.events.Append(ctx, tx, events.Event{
			WorkspaceID: workspaceID,
			Type:        events.ArticleFeedbackRecorded,
			EntityType:  "article",
			EntityID:    articleID,
			ActorType:   events.ActorVisitor,
			Data:        map[string]any{"helpful": helpful},
		})
		return err
	})
}

func (s *Service) SaveArticle(ctx context.Context, workspaceID, authorID, id string, input ArticleInput) (*Article, error) {
	if strings.TrimSpace(input.Title) == "" || input.KnowledgeBaseID == "" {
		return nil, ErrInvalidArticle
	}
	if !slugPattern.MatchString(strings.ToLower(strings.TrimSpace(input.Slug))) {
		return nil, ErrInvalidSlug
	}
	if input.Language == "" {
		input.Language = "en"
	}
	if input.State == "" {
		input.State = "draft"
	}
	if !validState(input.State) {
		return nil, ErrInvalidState
	}
	if _, err := s.GetKnowledgeBase(ctx, workspaceID, input.KnowledgeBaseID); err != nil {
		return nil, err
	}
	if input.CollectionID != nil {
		collectionID := strings.TrimSpace(*input.CollectionID)
		if collectionID == "" {
			input.CollectionID = nil
		} else {
			var exists bool
			if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM article_collections WHERE workspace_id=$1 AND knowledge_base_id=$2 AND id=$3)`, workspaceID, input.KnowledgeBaseID, collectionID).Scan(&exists); err != nil {
				return nil, err
			}
			if !exists {
				return nil, ErrNotFound
			}
			input.CollectionID = &collectionID
		}
	}
	seo := input.SEO
	if seo == nil {
		seo = map[string]any{}
	}
	seoJSON, _ := json.Marshal(seo)
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if id == "" {
			id = ids.New(ids.PrefixArticle)
			if _, err := tx.Exec(ctx, `INSERT INTO articles(id,workspace_id,knowledge_base_id,collection_id,title,slug,excerpt,body,state,language,author_id,seo,scheduled_at,published_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),$12,$13,CASE WHEN $9='published' THEN now() ELSE NULL END)`, id, workspaceID, input.KnowledgeBaseID, input.CollectionID, strings.TrimSpace(input.Title), strings.ToLower(strings.TrimSpace(input.Slug)), input.Excerpt, input.Body, input.State, input.Language, authorID, seoJSON, input.ScheduledAt); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO article_revisions(id,article_id,version,title,body,excerpt,edited_by,note) VALUES($1,$2,1,$3,$4,$5,NULLIF($6,''),'created')`, ids.New(ids.PrefixArticleRevision), id, input.Title, input.Body, input.Excerpt, authorID); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE knowledge_bases SET article_count=article_count+1,updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, input.KnowledgeBaseID)
			return err
		}
		var oldVersion int
		err := tx.QueryRow(ctx, `SELECT version FROM articles WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspaceID, id).Scan(&oldVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		version := oldVersion + 1
		if _, err := tx.Exec(ctx, `UPDATE articles SET knowledge_base_id=$3,collection_id=$4,title=$5,slug=$6,excerpt=$7,body=$8,state=$9,language=$10,seo=$11,scheduled_at=$12,version=$13,published_at=CASE WHEN $9='published' THEN coalesce(published_at,now()) ELSE published_at END,updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, id, input.KnowledgeBaseID, input.CollectionID, input.Title, strings.ToLower(input.Slug), input.Excerpt, input.Body, input.State, input.Language, seoJSON, input.ScheduledAt, version); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO article_revisions(id,article_id,version,title,body,excerpt,edited_by,note) VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,''),'updated')`, ids.New(ids.PrefixArticleRevision), id, version, input.Title, input.Body, input.Excerpt, authorID)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("knowledgebase: save article: %w", err)
	}
	return s.GetArticle(ctx, workspaceID, id)
}

func (s *Service) PublishArticle(ctx context.Context, workspaceID, id string) (*Article, error) {
	var exists string
	err := s.pool.QueryRow(ctx, `UPDATE articles SET state='published',published_at=coalesce(published_at,now()),updated_at=now() WHERE workspace_id=$1 AND id=$2 RETURNING id`, workspaceID, id).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.GetArticle(ctx, workspaceID, id)
}

// PublishScheduled promotes every due article in one transaction. The event
// is committed with the state transition, so a worker retry cannot expose a
// published article without notifying the normal webhook, analytics, and
// realtime consumers.
func (s *Service) PublishScheduled(ctx context.Context, now time.Time) (int, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	count := 0
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			UPDATE articles
			SET state='published', published_at=coalesce(published_at,$1), updated_at=$1
			WHERE state='scheduled' AND scheduled_at IS NOT NULL AND scheduled_at <= $1
			RETURNING id,workspace_id,title
		`, now)
		if err != nil {
			return err
		}
		type publishedArticle struct {
			id, workspaceID, title string
		}
		published := make([]publishedArticle, 0)
		for rows.Next() {
			var item publishedArticle
			if err := rows.Scan(&item.id, &item.workspaceID, &item.title); err != nil {
				rows.Close()
				return err
			}
			published = append(published, item)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if s.events != nil {
			for _, item := range published {
				if _, err := s.events.Append(ctx, tx, events.Event{
					WorkspaceID: item.workspaceID, Type: events.ArticlePublished,
					EntityType: "article", EntityID: item.id, ActorType: events.ActorSystem,
					Data: map[string]any{"id": item.id, "title": item.title},
				}); err != nil {
					return err
				}
			}
		}
		count = len(published)
		return nil
	})
	return count, err
}

func (s *Service) SaveChangelog(ctx context.Context, workspaceID, memberID, id string, input ChangelogInput) (*ChangelogEntry, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, ErrInvalidName
	}
	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	if kind == "" {
		kind = "added"
	}
	if !validChangelogKind(kind) {
		return nil, ErrInvalidChangelog
	}
	if input.FeedbackItemID != nil && strings.TrimSpace(*input.FeedbackItemID) != "" {
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM feedback_items WHERE workspace_id=$1 AND id=$2)`, workspaceID, strings.TrimSpace(*input.FeedbackItemID)).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrNotFound
		}
	}
	if id == "" {
		id = ids.New(ids.PrefixChangelogEntry)
		if _, err := s.pool.Exec(ctx, `INSERT INTO changelog_entries(id,workspace_id,title,body,kind,feedback_item_id,created_by) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''))`, id, workspaceID, title, strings.TrimSpace(input.Body), kind, nullableString(input.FeedbackItemID), memberID); err != nil {
			return nil, fmt.Errorf("knowledgebase: create changelog entry: %w", err)
		}
	} else {
		result, err := s.pool.Exec(ctx, `UPDATE changelog_entries SET title=$3,body=$4,kind=$5,feedback_item_id=NULLIF($6,''),updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, id, title, strings.TrimSpace(input.Body), kind, nullableString(input.FeedbackItemID))
		if err != nil {
			return nil, fmt.Errorf("knowledgebase: update changelog entry: %w", err)
		}
		if result.RowsAffected() == 0 {
			return nil, ErrNotFound
		}
	}
	return s.GetChangelog(ctx, workspaceID, id)
}

func (s *Service) ListChangelog(ctx context.Context, workspaceID string, limit int) ([]ChangelogEntry, error) {
	return s.ListChangelogPage(ctx, workspaceID, time.Time{}, "", limit)
}

// ListChangelogPage returns drafts and published entries in one stable stream.
// The effective publication/creation timestamp plus id is the cursor because
// drafts do not have a published_at value yet.
func (s *Service) ListChangelogPage(ctx context.Context, workspaceID string, before time.Time, beforeID string, limit int) ([]ChangelogEntry, error) {
	if limit <= 0 || limit > 201 {
		limit = 100
	}
	query := `
		SELECT id,workspace_id,title,body,kind,feedback_item_id,published_at,created_by,created_at,updated_at
		FROM changelog_entries WHERE workspace_id=$1`
	args := []any{workspaceID}
	if !before.IsZero() {
		query += ` AND (coalesce(published_at,created_at),id) < ($2,$3)`
		args = append(args, before, beforeID)
	}
	query += fmt.Sprintf(" ORDER BY coalesce(published_at,created_at) DESC,id DESC LIMIT $%d", len(args)+1)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanChangelog(rows)
}

func (s *Service) GetChangelog(ctx context.Context, workspaceID, id string) (*ChangelogEntry, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id,workspace_id,title,body,kind,feedback_item_id,published_at,created_by,created_at,updated_at
		FROM changelog_entries WHERE workspace_id=$1 AND id=$2
	`, workspaceID, id)
	item, err := scanOneChangelog(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return item, err
}

func (s *Service) ListPublishedChangelog(ctx context.Context, workspaceID string, limit int) ([]ChangelogEntry, error) {
	return s.ListPublishedChangelogPage(ctx, workspaceID, time.Time{}, "", limit)
}

func (s *Service) ListPublishedChangelogPage(ctx context.Context, workspaceID string, before time.Time, beforeID string, limit int) ([]ChangelogEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := `
		SELECT id,workspace_id,title,body,kind,feedback_item_id,published_at,created_by,created_at,updated_at
		FROM changelog_entries
		WHERE workspace_id=$1 AND published_at IS NOT NULL AND published_at <= now()`
	args := []any{workspaceID}
	if !before.IsZero() {
		query += " AND (published_at,id) < ($2,$3)"
		args = append(args, before, beforeID)
	}
	args = append(args, limit)
	query += fmt.Sprintf(" ORDER BY published_at DESC,id DESC LIMIT $%d", len(args))
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanChangelog(rows)
}

func (s *Service) PublishChangelog(ctx context.Context, workspaceID, memberID, id string) (*ChangelogEntry, error) {
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var publishedAt *time.Time
		if err := tx.QueryRow(ctx, `SELECT published_at FROM changelog_entries WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspaceID, id).Scan(&publishedAt); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if publishedAt != nil {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE changelog_entries SET published_at=now(),updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, id); err != nil {
			return err
		}
		if s.events == nil {
			return nil
		}
		_, err := s.events.Append(ctx, tx, events.Event{
			WorkspaceID: workspaceID,
			Type:        events.ChangelogPublished,
			EntityType:  "changelog_entry",
			EntityID:    id,
			ActorType:   events.ActorUser,
			ActorID:     memberID,
			Data:        map[string]any{"id": id},
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetChangelog(ctx, workspaceID, id)
}

func validChangelogKind(kind string) bool {
	switch kind {
	case "added", "improved", "fixed", "removed":
		return true
	default:
		return false
	}
}

func nullableString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

type changelogScanner interface{ Scan(...any) error }

func scanOneChangelog(row changelogScanner) (*ChangelogEntry, error) {
	var item ChangelogEntry
	err := row.Scan(&item.ID, &item.WorkspaceID, &item.Title, &item.Body, &item.Kind, &item.FeedbackItemID, &item.PublishedAt, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func scanChangelog(rows pgx.Rows) ([]ChangelogEntry, error) {
	result := make([]ChangelogEntry, 0)
	for rows.Next() {
		item, err := scanOneChangelog(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *item)
	}
	return result, rows.Err()
}

func (s *Service) SearchPublished(ctx context.Context, workspaceID, kbSlug, collectionSlug, query, language, surface string, limit int) ([]SearchResult, error) {
	result, err := s.SearchPublishedPage(ctx, workspaceID, kbSlug, collectionSlug, query, language, nil, time.Time{}, "", limit)
	if err != nil {
		return nil, err
	}
	if surface == "" {
		surface = "portal"
	}
	s.RecordSearch(ctx, workspaceID, query, language, surface, len(result))
	return result, nil
}

// RecordSearch records one logical public search. Paginated follow-up requests
// deliberately do not create duplicate analytics rows.
func (s *Service) RecordSearch(ctx context.Context, workspaceID, query, language, surface string, resultCount int) {
	surface = normalizeSurface(surface)
	_ = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO article_searches(id,workspace_id,query,result_count,language,surface) VALUES($1,$2,$3,$4,NULLIF($5,''),$6)`, ids.New(ids.PrefixArticleSearch), workspaceID, strings.TrimSpace(query), resultCount, language, surface); err != nil {
			return err
		}
		if s.events == nil {
			return nil
		}
		_, err := s.events.Append(ctx, tx, events.Event{
			WorkspaceID: workspaceID,
			Type:        events.ArticleSearchRecorded,
			EntityType:  "article_search",
			ActorType:   events.ActorVisitor,
			Data:        map[string]any{"result_count": resultCount, "surface": surface},
		})
		return err
	})
}

func normalizeSurface(surface string) string {
	switch strings.ToLower(strings.TrimSpace(surface)) {
	case "widget":
		return "widget"
	default:
		return "portal"
	}
}

func (s *Service) SearchPublishedPage(ctx context.Context, workspaceID, kbSlug, collectionSlug, query, language string, beforeRank *float32, before time.Time, beforeID string, limit int) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rankExpr := "ts_rank(setweight(to_tsvector('english',a.title),'A')||setweight(to_tsvector('english',a.excerpt),'B')||setweight(to_tsvector('english',a.body),'C'),websearch_to_tsquery('english',$4))"
	args := []any{workspaceID, kbSlug, collectionSlug, query, language}
	querySQL := `SELECT a.id,a.workspace_id,a.knowledge_base_id,a.collection_id,a.title,a.slug,a.excerpt,a.body,a.state,a.language,a.author_id,a.seo,a.view_count,a.helpful_count,a.unhelpful_count,a.version,a.scheduled_at,a.published_at,a.created_at,a.updated_at,` + rankExpr + ` FROM articles a JOIN knowledge_bases k ON k.id=a.knowledge_base_id LEFT JOIN article_collections c ON c.id=a.collection_id AND c.workspace_id=a.workspace_id AND c.knowledge_base_id=a.knowledge_base_id WHERE a.workspace_id=$1 AND ($2='' OR k.slug=$2) AND ($3='' OR c.slug=$3) AND a.state='published' AND ($5='' OR a.language=$5) AND ($4='' OR (setweight(to_tsvector('english',a.title),'A')||setweight(to_tsvector('english',a.excerpt),'B')||setweight(to_tsvector('english',a.body),'C')) @@ websearch_to_tsquery('english',$4))`
	if beforeRank != nil {
		args = append(args, *beforeRank, before, beforeID)
		querySQL += ` AND (` + rankExpr + `,a.published_at,a.id) < ($6,$7,$8)`
	}
	args = append(args, limit)
	querySQL += ` ORDER BY ` + rankExpr + ` DESC,a.published_at DESC,a.id DESC LIMIT $` + fmt.Sprint(len(args))
	rows, err := s.pool.Query(ctx, querySQL, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]SearchResult, 0)
	for rows.Next() {
		var item Article
		var rank float32
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.KnowledgeBaseID, &item.CollectionID, &item.Title, &item.Slug, &item.Excerpt, &item.Body, &item.State, &item.Language, &item.AuthorID, &item.SEO, &item.ViewCount, &item.HelpfulCount, &item.UnhelpfulCount, &item.Version, &item.ScheduledAt, &item.PublishedAt, &item.CreatedAt, &item.UpdatedAt, &rank); err != nil {
			return nil, err
		}
		result = append(result, SearchResult{Article: item, Rank: rank})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// ListPublished returns the small ordered article set used by surface
// bootstraps. Unlike SearchPublished it does not write a search analytics row
// for an empty query on every widget or portal load.
func (s *Service) ListPublished(ctx context.Context, workspaceID, language string, limit int) ([]Article, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `SELECT id,workspace_id,knowledge_base_id,collection_id,title,slug,excerpt,body,state,language,author_id,seo,view_count,helpful_count,unhelpful_count,version,scheduled_at,published_at,created_at,updated_at FROM articles WHERE workspace_id=$1 AND state='published' AND ($2='' OR language=$2) ORDER BY published_at DESC,id DESC LIMIT $3`, workspaceID, language, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanArticles(rows)
}

func scanArticles(rows pgx.Rows) ([]Article, error) {
	result := make([]Article, 0)
	for rows.Next() {
		var item Article
		var seoBytes []byte
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.KnowledgeBaseID, &item.CollectionID, &item.Title, &item.Slug, &item.Excerpt, &item.Body, &item.State, &item.Language, &item.AuthorID, &seoBytes, &item.ViewCount, &item.HelpfulCount, &item.UnhelpfulCount, &item.Version, &item.ScheduledAt, &item.PublishedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if len(seoBytes) > 0 {
			if err := json.Unmarshal(seoBytes, &item.SEO); err != nil {
				return nil, err
			}
		}
		if item.SEO == nil {
			item.SEO = map[string]any{}
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
func uniqueLanguages(input []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(input))
	for _, v := range input {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
func validState(state string) bool {
	switch state {
	case "draft", "in_review", "scheduled", "published", "archived":
		return true
	}
	return false
}
