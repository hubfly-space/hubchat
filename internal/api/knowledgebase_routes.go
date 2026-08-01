package api

import (
	"crypto/sha256"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/knowledgebase"
)

func registerKnowledgeBaseRoutes(mux *http.ServeMux, deps Deps) {
	idempotent := Idempotency(deps)
	mux.HandleFunc("GET /v1/knowledge-bases", requireCapability(deps, authorization.KnowledgebaseManage, handleListKnowledgeBases(deps)))
	mux.HandleFunc("POST /v1/knowledge-bases", requireCapability(deps, authorization.KnowledgebaseManage, Idempotency(deps)(handleCreateKnowledgeBase(deps))))
	mux.HandleFunc("GET /v1/knowledge-bases/{id}/collections", requireCapability(deps, authorization.KnowledgebaseManage, handleListCollections(deps)))
	mux.HandleFunc("POST /v1/knowledge-bases/{id}/collections", requireCapability(deps, authorization.KnowledgebaseManage, Idempotency(deps)(handleCreateCollection(deps))))
	mux.HandleFunc("GET /v1/articles", requireCapability(deps, authorization.KnowledgebaseManage, handleListArticles(deps)))
	mux.HandleFunc("POST /v1/articles", requireCapability(deps, authorization.KnowledgebaseManage, Idempotency(deps)(handleCreateArticle(deps))))
	mux.HandleFunc("GET /v1/articles/{id}", requireCapability(deps, authorization.KnowledgebaseManage, handleGetArticle(deps)))
	mux.HandleFunc("GET /v1/articles/{id}/revisions", requireCapability(deps, authorization.KnowledgebaseManage, handleListArticleRevisions(deps)))
	mux.HandleFunc("PATCH /v1/articles/{id}", requireCapability(deps, authorization.KnowledgebaseManage, idempotent(handleUpdateArticle(deps))))
	mux.HandleFunc("POST /v1/articles/{id}/publish", requireCapability(deps, authorization.KnowledgebaseManage, Idempotency(deps)(handlePublishArticle(deps))))
	mux.HandleFunc("GET /v1/changelog", requireCapability(deps, authorization.KnowledgebaseManage, handleListChangelog(deps)))
	mux.HandleFunc("POST /v1/changelog", requireCapability(deps, authorization.KnowledgebaseManage, Idempotency(deps)(handleCreateChangelog(deps))))
	mux.HandleFunc("GET /v1/changelog/{id}", requireCapability(deps, authorization.KnowledgebaseManage, handleGetChangelog(deps)))
	mux.HandleFunc("PATCH /v1/changelog/{id}", requireCapability(deps, authorization.KnowledgebaseManage, idempotent(handleUpdateChangelog(deps))))
	mux.HandleFunc("POST /v1/changelog/{id}/publish", requireCapability(deps, authorization.KnowledgebaseManage, Idempotency(deps)(handlePublishChangelog(deps))))

	// Public search only exposes published articles and records no-result
	// searches for content-gap reporting.
	mux.HandleFunc("GET /v1/public/knowledge-bases/{workspaceID}/search", handlePublicArticleSearch(deps))
	mux.HandleFunc("GET /v1/public/knowledge-bases/{workspaceID}/articles/{slug}", handlePublicArticle(deps))
	mux.HandleFunc("POST /v1/public/knowledge-bases/{workspaceID}/articles/{slug}/feedback", Idempotency(deps)(handlePublicArticleFeedback(deps)))
	mux.HandleFunc("GET /v1/public/changelog/{workspaceID}", handlePublicChangelog(deps))
}

func handlePublicChangelog(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed changelog cursor.")
			return
		}
		entries, err := deps.Knowledgebase.ListPublishedChangelogPage(r.Context(), r.PathValue("workspaceID"), cursor.At, cursor.ID, limit+1)
		if err != nil {
			writeKnowledgebaseInternal(w, r)
			return
		}
		items := make([]map[string]any, 0, len(entries))
		for _, item := range entries {
			items = append(items, map[string]any{"id": item.ID, "title": item.Title, "body": item.Body, "kind": item.Kind, "published_at": item.PublishedAt})
		}
		page := NewPage(entries, limit, func(item knowledgebase.ChangelogEntry) Cursor {
			at := time.Time{}
			if item.PublishedAt != nil {
				at = *item.PublishedAt
			}
			return Cursor{At: at, ID: item.ID}
		})
		pageItems := make([]map[string]any, 0, len(page.Data))
		for _, item := range page.Data {
			pageItems = append(pageItems, map[string]any{"id": item.ID, "title": item.Title, "body": item.Body, "kind": item.Kind, "published_at": item.PublishedAt})
		}
		httpserver.WriteJSON(w, http.StatusOK, Page[map[string]any]{Data: pageItems, NextCursor: page.NextCursor, HasMore: page.HasMore})
	}
}

func handleListChangelog(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed changelog cursor.")
			return
		}
		items, err := deps.Knowledgebase.ListChangelogPage(r.Context(), actorFromRequest(r).WorkspaceID, cursor.At, cursor.ID, limit+1)
		if err != nil {
			writeKnowledgebaseInternal(w, r)
			return
		}
		page := NewPage(items, limit, func(item knowledgebase.ChangelogEntry) Cursor {
			at := item.CreatedAt
			if item.PublishedAt != nil {
				at = *item.PublishedAt
			}
			return Cursor{At: at, ID: item.ID}
		})
		httpserver.WriteJSON(w, http.StatusOK, page)
	}
}

func handleCreateChangelog(deps Deps) http.HandlerFunc {
	return saveChangelog(deps, "")
}

func handleUpdateChangelog(deps Deps) http.HandlerFunc {
	return saveChangelog(deps, "update")
}

func saveChangelog(deps Deps, mode string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input knowledgebase.ChangelogInput
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			writeKnowledgebaseValidation(w, r, err)
			return
		}
		actor := actorFromRequest(r)
		id := ""
		if mode == "update" {
			id = r.PathValue("id")
		}
		item, err := deps.Knowledgebase.SaveChangelog(r.Context(), actor.WorkspaceID, actor.MemberID, id, input)
		if err != nil {
			writeKnowledgebaseError(w, r, err)
			return
		}
		status := http.StatusOK
		if id == "" {
			status = http.StatusCreated
		}
		httpserver.WriteJSON(w, status, item)
	}
}

func handleGetChangelog(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := deps.Knowledgebase.GetChangelog(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeKnowledgebaseError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}

func handlePublishChangelog(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		item, err := deps.Knowledgebase.PublishChangelog(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"))
		if err != nil {
			writeKnowledgebaseError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}

func handleListKnowledgeBases(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed knowledge-base cursor.")
			return
		}
		items, err := deps.Knowledgebase.ListKnowledgeBasesPage(r.Context(), actorFromRequest(r).WorkspaceID, cursor.At, cursor.ID, limit+1)
		if err != nil {
			writeKnowledgebaseInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, NewPage(items, limit, func(item knowledgebase.KnowledgeBase) Cursor {
			return Cursor{At: item.CreatedAt, ID: item.ID}
		}))
	}
}

func handleCreateKnowledgeBase(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input knowledgebase.KnowledgeBaseInput
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			writeKnowledgebaseValidation(w, r, err)
			return
		}
		item, err := deps.Knowledgebase.CreateKnowledgeBase(r.Context(), actorFromRequest(r).WorkspaceID, input)
		if err != nil {
			writeKnowledgebaseError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, item)
	}
}

func handleListCollections(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed collection cursor.")
			return
		}
		beforePosition := 0
		hasCursor := !cursor.IsZero()
		if hasCursor {
			beforePosition, err = strconv.Atoi(cursor.Value)
			if err != nil || cursor.ID == "" {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed collection cursor.")
				return
			}
		}
		items, err := deps.Knowledgebase.ListCollectionsPage(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"), beforePosition, cursor.ID, hasCursor, limit+1)
		if err != nil {
			writeKnowledgebaseError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, NewPage(items, limit, func(item knowledgebase.Collection) Cursor {
			return Cursor{Value: strconv.Itoa(item.Position), ID: item.ID}
		}))
	}
}

func handleCreateCollection(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input knowledgebase.CollectionInput
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			writeKnowledgebaseValidation(w, r, err)
			return
		}
		item, err := deps.Knowledgebase.CreateCollection(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"), input)
		if err != nil {
			writeKnowledgebaseError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, item)
	}
}

func handleListArticles(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed pagination parameters.")
			return
		}
		items, err := deps.Knowledgebase.ListArticlesPage(r.Context(), actorFromRequest(r).WorkspaceID, r.URL.Query().Get("state"), r.URL.Query().Get("q"), cursor.At, cursor.ID, limit+1)
		if err != nil {
			writeKnowledgebaseInternal(w, r)
			return
		}
		page := NewPage(items, limit, func(item knowledgebase.Article) Cursor { return Cursor{At: item.UpdatedAt, ID: item.ID} })
		httpserver.WriteJSON(w, http.StatusOK, page)
	}
}

func handleCreateArticle(deps Deps) http.HandlerFunc { return saveArticle(deps, "") }
func handleUpdateArticle(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { saveArticle(deps, r.PathValue("id"))(w, r) }
}
func saveArticle(deps Deps, id string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input knowledgebase.ArticleInput
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			writeKnowledgebaseValidation(w, r, err)
			return
		}
		actor := actorFromRequest(r)
		item, err := deps.Knowledgebase.SaveArticle(r.Context(), actor.WorkspaceID, actor.MemberID, id, input)
		if err != nil {
			writeKnowledgebaseError(w, r, err)
			return
		}
		status := http.StatusOK
		if id == "" {
			status = http.StatusCreated
		}
		httpserver.WriteJSON(w, status, item)
	}
}

func handleGetArticle(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := deps.Knowledgebase.GetArticle(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeKnowledgebaseError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}

func handleListArticleRevisions(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed article revision cursor.")
			return
		}
		revisions, err := deps.Knowledgebase.ListArticleRevisionsPage(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"), cursor.At, cursor.ID, limit+1)
		if err != nil {
			writeKnowledgebaseError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, NewPage(revisions, limit, func(item knowledgebase.ArticleRevision) Cursor {
			return Cursor{At: item.CreatedAt, ID: item.ID}
		}))
	}
}

func handlePublishArticle(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := deps.Knowledgebase.PublishArticle(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeKnowledgebaseError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}

func handlePublicArticleSearch(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed article cursor.")
			return
		}
		pageLimit := limit
		if pageLimit > 50 {
			pageLimit = 50
		}
		var beforeRank *float32
		if !cursor.IsZero() {
			value, parseErr := strconv.ParseFloat(cursor.Value, 32)
			if parseErr != nil {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed article cursor.")
				return
			}
			rank := float32(value)
			beforeRank = &rank
		}
		items, err := deps.Knowledgebase.SearchPublishedPage(r.Context(), r.PathValue("workspaceID"), r.URL.Query().Get("knowledge_base"), r.URL.Query().Get("collection"), r.URL.Query().Get("q"), r.URL.Query().Get("language"), beforeRank, cursor.At, cursor.ID, pageLimit+1)
		if err != nil {
			writeKnowledgebaseInternal(w, r)
			return
		}
		page := NewPage(items, pageLimit, func(item knowledgebase.SearchResult) Cursor {
			at := time.Time{}
			if item.Article.PublishedAt != nil {
				at = *item.Article.PublishedAt
			}
			return Cursor{Value: strconv.FormatFloat(float64(item.Rank), 'g', -1, 32), At: at, ID: item.Article.ID}
		})
		if cursor.IsZero() {
			deps.Knowledgebase.RecordSearch(r.Context(), r.PathValue("workspaceID"), r.URL.Query().Get("q"), r.URL.Query().Get("language"), r.URL.Query().Get("surface"), len(page.Data))
		}
		httpserver.WriteJSON(w, http.StatusOK, page)
	}
}

func handlePublicArticle(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := deps.Knowledgebase.GetPublishedBySlug(r.Context(), r.PathValue("workspaceID"), r.PathValue("slug"))
		if errors.Is(err, knowledgebase.ErrNotFound) {
			httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Article not found.")
			return
		}
		if err != nil {
			writeKnowledgebaseInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}

func handlePublicArticleFeedback(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Helpful bool   `json:"helpful"`
			Comment string `json:"comment"`
		}
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			writeKnowledgebaseValidation(w, r, err)
			return
		}
		article, err := deps.Knowledgebase.GetPublishedBySlug(r.Context(), r.PathValue("workspaceID"), r.PathValue("slug"))
		if err != nil {
			writeKnowledgebaseError(w, r, err)
			return
		}
		fingerprint := sha256.Sum256([]byte(clientIP(r) + "|" + r.UserAgent()))
		if err := deps.Knowledgebase.RecordArticleFeedback(r.Context(), r.PathValue("workspaceID"), article.ID, input.Helpful, input.Comment, "", fingerprint[:]); err != nil {
			if errors.Is(err, knowledgebase.ErrFeedbackAlreadyRecorded) {
				httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, "Feedback was already recorded for this article.")
				return
			}
			writeKnowledgebaseError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, map[string]any{"status": "recorded"})
	}
}

func writeKnowledgebaseError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, knowledgebase.ErrNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Knowledge-base resource not found.")
	case errors.Is(err, knowledgebase.ErrInvalidName), errors.Is(err, knowledgebase.ErrInvalidSlug), errors.Is(err, knowledgebase.ErrInvalidState), errors.Is(err, knowledgebase.ErrInvalidLanguage), errors.Is(err, knowledgebase.ErrInvalidArticle), errors.Is(err, knowledgebase.ErrInvalidChangelog):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	default:
		writeKnowledgebaseInternal(w, r)
	}
}

func writeKnowledgebaseValidation(w http.ResponseWriter, r *http.Request, err error) {
	httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
}
func writeKnowledgebaseInternal(w http.ResponseWriter, r *http.Request) {
	httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load knowledge-base content.")
}
