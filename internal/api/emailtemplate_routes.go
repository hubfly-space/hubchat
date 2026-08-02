package api

import (
	"errors"
	"net/http"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/emailtemplate"
	"github.com/hubchat/hubchat/internal/httpserver"
)

func registerEmailTemplateRoutes(mux *http.ServeMux, deps Deps) {
	if deps.EmailTemplates == nil {
		return
	}
	idempotent := Idempotency(deps)
	mux.HandleFunc("GET /v1/email/templates", requireCapability(deps, authorization.IntegrationManage, handleListEmailTemplates(deps)))
	mux.HandleFunc("PUT /v1/email/templates/{key}", requireCapability(deps, authorization.IntegrationManage, idempotent(handleSaveEmailTemplate(deps))))
	mux.HandleFunc("DELETE /v1/email/templates/{key}", requireCapability(deps, authorization.IntegrationManage, idempotent(handleResetEmailTemplate(deps))))
	mux.HandleFunc("POST /v1/email/templates/{key}/preview", requireCapability(deps, authorization.IntegrationManage, handlePreviewEmailTemplate(deps)))
}

func handleListEmailTemplates(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := deps.EmailTemplates.List(r.Context(), actorFromRequest(r).WorkspaceID)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load email templates.")
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
	}
}

func handleSaveEmailTemplate(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input emailtemplate.Input
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			writeEmailTemplateError(w, r, err)
			return
		}
		actor := actorFromRequest(r)
		item, err := deps.EmailTemplates.Save(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("key"), input)
		if err != nil {
			writeEmailTemplateError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}

func handleResetEmailTemplate(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := deps.EmailTemplates.Reset(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("key")); err != nil {
			writeEmailTemplateError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handlePreviewEmailTemplate(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input emailtemplate.Input
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			writeEmailTemplateError(w, r, err)
			return
		}
		if err := emailtemplate.Validate(r.PathValue("key"), input); err != nil {
			writeEmailTemplateError(w, r, err)
			return
		}
		subject, body, err := emailtemplate.Preview(input)
		if err != nil {
			writeEmailTemplateError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]string{"subject": subject, "body": body})
	}
}

func writeEmailTemplateError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, emailtemplate.ErrInvalid) {
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, "The email template is invalid or uses an unsupported variable.")
		return
	}
	if errors.Is(err, emailtemplate.ErrNotFound) {
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Email template not found.")
		return
	}
	httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not save email template.")
}
