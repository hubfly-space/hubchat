package api

import (
	"errors"
	"io"
	"net/http"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/emailchannel"
	"github.com/hubchat/hubchat/internal/httpserver"
)

func registerEmailChannelRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/email/mailboxes", requireCapability(deps, authorization.IntegrationManage, handleListMailboxes(deps)))
	mux.HandleFunc("POST /v1/email/mailboxes", requireCapability(deps, authorization.IntegrationManage, Idempotency(deps)(handleCreateMailbox(deps))))
	mux.HandleFunc("PATCH /v1/email/mailboxes/{id}", requireCapability(deps, authorization.IntegrationManage, handleUpdateMailbox(deps)))
	mux.HandleFunc("DELETE /v1/email/mailboxes/{id}", requireCapability(deps, authorization.IntegrationManage, handleDeleteMailbox(deps)))

	// Providers post their normalized JSON payload here. Authentication is the
	// mailbox's HMAC secret, not a dashboard session.
	mux.HandleFunc("POST /v1/email/inbound/{provider}", handleInboundEmail(deps))
}

func handleListMailboxes(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := deps.EmailChannel.List(r.Context(), actorFromRequest(r).WorkspaceID)
		if err != nil {
			writeEmailChannelInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
	}
}

func handleCreateMailbox(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input emailchannel.CreateInput
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			writeEmailChannelValidation(w, r, err)
			return
		}
		created, err := deps.EmailChannel.Create(r.Context(), actorFromRequest(r).WorkspaceID, input)
		if err != nil {
			writeEmailChannelError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, created)
	}
}

func handleUpdateMailbox(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input emailchannel.UpdateInput
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			writeEmailChannelValidation(w, r, err)
			return
		}
		item, err := deps.EmailChannel.Update(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"), input)
		if err != nil {
			writeEmailChannelError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}

func handleDeleteMailbox(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := deps.EmailChannel.Delete(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id")); err != nil {
			writeEmailChannelError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleInboundEmail(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Inbound email payload is too large or malformed.")
			return
		}
		input, err := emailchannel.UnmarshalProviderPayload(body)
		if err != nil {
			writeEmailChannelValidation(w, r, err)
			return
		}
		result, err := deps.EmailChannel.Ingest(r.Context(), body, r.Header.Get("X-Hubchat-Signature"), input)
		if err != nil {
			writeEmailChannelInboundError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusAccepted, result)
	}
}

func writeEmailChannelError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, emailchannel.ErrNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Mailbox not found.")
	case errors.Is(err, emailchannel.ErrInvalidAddress), errors.Is(err, emailchannel.ErrInvalidMode), errors.Is(err, emailchannel.ErrInvalidInbox):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	default:
		writeEmailChannelInternal(w, r)
	}
}

func writeEmailChannelInboundError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, emailchannel.ErrNotFound), errors.Is(err, emailchannel.ErrSignature), errors.Is(err, emailchannel.ErrSecretUnavailable):
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeUnauthorized, "Inbound email authentication failed.")
	case errors.Is(err, emailchannel.ErrSenderBlocked):
		httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeForbidden, "This sender is not allowed.")
	case errors.Is(err, emailchannel.ErrDuplicateMessage):
		httpserver.WriteJSON(w, http.StatusAccepted, map[string]any{"status": "already_received"})
	case errors.Is(err, emailchannel.ErrInvalidMessage):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	default:
		writeEmailChannelInternal(w, r)
	}
}

func writeEmailChannelValidation(w http.ResponseWriter, r *http.Request, err error) {
	httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
}

func writeEmailChannelInternal(w http.ResponseWriter, r *http.Request) {
	httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load email channel configuration.")
}
