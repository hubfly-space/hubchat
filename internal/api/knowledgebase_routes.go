package api

import (
	"crypto/sha256"
	"errors"
	"net/http"
	"strconv"

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
		entries, err := deps.Knowledgebase.ListPublishedChangelog(r.Context(), r.PathValue("workspaceID"), 100)
		if err != nil {
			writeKnowledgebaseInternal(w, r)
			return
		}
		items := make([]map[string]any, 0, len(entries))
		for _, item := range entries {
			items = append(items, map[string]any{"id": item.ID, "title": item.Title, "body": item.Body, "kind": item.Kind, "published_at": item.PublishedAt})
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
	}
}

func handleListChangelog(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := deps.Knowledgebase.ListChangelog(r.Context(), actorFromRequest(r).WorkspaceID, 200)
		if err != nil {
			writeKnowledgebaseInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
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
		items, err := deps.Knowledgebase.ListKnowledgeBases(r.Context(), actorFromRequest(r).WorkspaceID)
		if err != nil {
			writeKnowledgebaseInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
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
		items, err := deps.Knowledgebase.ListCollections(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeKnowledgebaseError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
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
		limit := 100
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil {
				limit = parsed
			}
		}
		items, err := deps.Knowledgebase.ListArticles(r.Context(), actorFromRequest(r).WorkspaceID, r.URL.Query().Get("state"), r.URL.Query().Get("q"), limit)
		if err != nil {
			writeKnowledgebaseInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
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
		items, err := deps.Knowledgebase.SearchPublished(r.Context(), r.PathValue("workspaceID"), r.URL.Query().Get("knowledge_base"), r.URL.Query().Get("q"), r.URL.Query().Get("language"), r.URL.Query().Get("surface"), 20)
		if err != nil {
			writeKnowledgebaseInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
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
