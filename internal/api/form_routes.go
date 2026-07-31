package api

import (
	"errors"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/file"
	"github.com/hubchat/hubchat/internal/form"
	"github.com/hubchat/hubchat/internal/httpserver"
)

func registerFormRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/forms", requireCapability(deps, authorization.TicketManage, handleListForms(deps)))
	mux.HandleFunc("POST /v1/forms", requireCapability(deps, authorization.TicketManage, Idempotency(deps)(handleCreateForm(deps))))
	mux.HandleFunc("GET /v1/forms/{id}", requireCapability(deps, authorization.TicketManage, handleGetForm(deps)))
	mux.HandleFunc("PATCH /v1/forms/{id}", requireCapability(deps, authorization.TicketManage, Idempotency(deps)(handleUpdateForm(deps))))
	mux.HandleFunc("DELETE /v1/forms/{id}", requireCapability(deps, authorization.TicketManage, Idempotency(deps)(handleDeleteForm(deps))))

	// Public forms carry the opaque workspace id in the embed URL. The form
	// itself is still looked up with workspace + slug, and only enabled forms
	// are returned, so this path cannot become a cross-tenant directory read.
	mux.HandleFunc("GET /v1/public/forms/{workspaceID}/{slug}", handleGetPublicForm(deps))
	mux.HandleFunc("POST /v1/public/forms/{workspaceID}/{slug}/files", Idempotency(deps)(handleUploadPublicFormFile(deps)))
	mux.HandleFunc("POST /v1/public/forms/{workspaceID}/{slug}/submissions", Idempotency(deps)(handleSubmitPublicForm(deps)))
}

func handleListForms(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}
		items, err := deps.Form.ListPage(r.Context(), actor.WorkspaceID, cursor.At, cursor.ID, limit+1)
		if err != nil {
			writeFormInternalError(w, r)
			return
		}
		page := NewPage(items, limit, func(item form.Form) Cursor {
			return Cursor{At: item.CreatedAt, ID: item.ID}
		})
		out := make([]map[string]any, 0, len(page.Data))
		for _, item := range page.Data {
			out = append(out, formJSON(item, true))
		}
		httpserver.WriteJSON(w, http.StatusOK, Page[map[string]any]{Data: out, NextCursor: page.NextCursor, HasMore: page.HasMore})
	}
}

func handleCreateForm(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var input form.CreateInput
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			writeFormValidationError(w, r, err)
			return
		}
		created, err := deps.Form.Create(r.Context(), actor.WorkspaceID, input)
		if err != nil {
			writeFormError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, formJSON(*created, true))
	}
}

func handleGetForm(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		item, err := deps.Form.Get(r.Context(), actor.WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeFormError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, formJSON(*item, true))
	}
}

func handleUpdateForm(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var input form.UpdateInput
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			writeFormValidationError(w, r, err)
			return
		}
		updated, err := deps.Form.Update(r.Context(), actor.WorkspaceID, r.PathValue("id"), input)
		if err != nil {
			writeFormError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, formJSON(*updated, true))
	}
}

func handleDeleteForm(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if err := deps.Form.Delete(r.Context(), actor.WorkspaceID, r.PathValue("id")); err != nil {
			writeFormError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleGetPublicForm(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := deps.Form.GetPublic(r.Context(), r.PathValue("workspaceID"), r.PathValue("slug"))
		if err != nil {
			writePublicFormNotFound(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, formJSON(*item, false))
	}
}

func handleSubmitPublicForm(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		workspaceID := r.PathValue("workspaceID")
		slug := r.PathValue("slug")
		definition, err := deps.Form.GetPublic(r.Context(), workspaceID, slug)
		if err != nil {
			writePublicFormNotFound(w, r)
			return
		}
		var input form.SubmissionInput
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			writeFormValidationError(w, r, err)
			return
		}
		input.SourceURL = strings.TrimSpace(input.SourceURL)
		if input.SourceURL != "" {
			if parsed, err := url.Parse(input.SourceURL); err != nil || parsed.Scheme == "" || parsed.Host == "" {
				writeFormValidationError(w, r, errors.New("source_url must be an absolute URL"))
				return
			}
		}
		input.IP = clientIP(r)
		input.UserAgent = r.UserAgent()
		if definition.Access == "authenticated" {
			if deps.Portal == nil || httpserver.PortalSessionToken(r) == "" {
				httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeUnauthorized, "Sign in to submit this form.")
				return
			}
			session, sessionErr := deps.Portal.Session(r.Context(), httpserver.PortalSessionToken(r), portalIdentifier(r))
			if sessionErr != nil || session.WorkspaceID != workspaceID {
				httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeUnauthorized, "Sign in to submit this form.")
				return
			}
			input.CustomerID = session.CustomerID
		}
		id, err := deps.Form.Submit(r.Context(), workspaceID, slug, input)
		if err != nil {
			writeFormError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, map[string]any{"id": id, "status": "accepted"})
	}
}

func handleUploadPublicFormFile(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		workspaceID := r.PathValue("workspaceID")
		if _, err := deps.Form.GetPublic(r.Context(), workspaceID, r.PathValue("slug")); err != nil {
			writePublicFormNotFound(w, r)
			return
		}
		if deps.File == nil {
			writeFormInternalError(w, r)
			return
		}
		uploaded, err := uploadFormFile(r, deps.File, workspaceID, "visitor", "")
		if err != nil {
			writeFileError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, fileJSON(*uploaded))
	}
}

func uploadFormFile(r *http.Request, files *file.Service, workspaceID, uploadedByType, uploadedByID string) (*file.Record, error) {
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		return nil, err
	}
	parts := r.MultipartForm.File["file"]
	if len(parts) != 1 {
		return nil, errors.New("upload exactly one file")
	}
	part := parts[0]
	opened, err := part.Open()
	if err != nil {
		return nil, err
	}
	defer opened.Close()
	mimeType := part.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(part.Filename))
	}
	return files.Create(r.Context(), workspaceID, file.UploadInput{
		Name: filepath.Base(part.Filename), MIMEType: mimeType, SizeBytes: part.Size, Body: opened,
		OwnerType: "workspace", OwnerID: workspaceID, UploadedByType: uploadedByType, UploadedByID: uploadedByID,
	})
}

func formJSON(item form.Form, includeInternal bool) map[string]any {
	fields := make([]map[string]any, 0, len(item.Fields))
	for _, field := range item.Fields {
		fields = append(fields, map[string]any{
			"id": field.ID, "key": field.Key, "label": field.Label, "type": field.Type,
			"placeholder": field.Placeholder, "description": field.Description, "options": orEmpty(field.Options),
			"required": field.Required, "default_value": field.DefaultValue, "condition": field.Condition,
			"validation": field.Validation, "position": field.Position,
		})
	}
	result := map[string]any{
		"id": item.ID, "workspace_id": item.WorkspaceID, "name": item.Name, "slug": item.Slug,
		"description": item.Description, "purpose": item.Purpose, "routing": item.Routing,
		"confirmation": item.Confirmation, "access": item.Access, "fields": fields,
		"enabled": item.Enabled, "max_submissions": item.MaxSubmissions, "updated_at": item.UpdatedAt, "created_at": item.CreatedAt,
	}
	if includeInternal {
		result["spam_protection"] = item.SpamProtection
		result["submission_count"] = item.SubmissionCount
	}
	return result
}

func writeFormError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, form.ErrNotFound), errors.Is(err, form.ErrDisabled):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Form not found.")
	case errors.Is(err, form.ErrInvalidName), errors.Is(err, form.ErrInvalidSlug), errors.Is(err, form.ErrInvalidPurpose),
		errors.Is(err, form.ErrInvalidAccess), errors.Is(err, form.ErrInvalidField), errors.Is(err, form.ErrInvalidLimit), errors.Is(err, form.ErrInvalidSubmission):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	case errors.Is(err, form.ErrSubmissionLimit):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, "This form is no longer accepting submissions.")
	case errors.Is(err, form.ErrRateLimited):
		httpserver.WriteError(w, r, http.StatusTooManyRequests, httpserver.CodeRateLimited, "Please wait before submitting this form again.")
	default:
		writeFormInternalError(w, r)
	}
}

func writeFormValidationError(w http.ResponseWriter, r *http.Request, err error) {
	httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
}

func writePublicFormNotFound(w http.ResponseWriter, r *http.Request) {
	httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Form not found.")
}

func writeFormInternalError(w http.ResponseWriter, r *http.Request) {
	httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load the form.")
}
