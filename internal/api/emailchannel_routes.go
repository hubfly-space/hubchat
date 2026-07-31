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
	mux.HandleFunc("GET /v1/email/status", requireCapability(deps, authorization.IntegrationManage, handleEmailStatus(deps)))
	mux.HandleFunc("GET /v1/email/mailboxes", requireCapability(deps, authorization.IntegrationManage, handleListMailboxes(deps)))
	mux.HandleFunc("POST /v1/email/mailboxes", requireCapability(deps, authorization.IntegrationManage, Idempotency(deps)(handleCreateMailbox(deps))))
	mux.HandleFunc("PATCH /v1/email/mailboxes/{id}", requireCapability(deps, authorization.IntegrationManage, handleUpdateMailbox(deps)))
	mux.HandleFunc("DELETE /v1/email/mailboxes/{id}", requireCapability(deps, authorization.IntegrationManage, handleDeleteMailbox(deps)))
	mux.HandleFunc("GET /v1/email/mailboxes/{id}/delivery-events", requireCapability(deps, authorization.IntegrationManage, handleListEmailDeliveryEvents(deps)))
	mux.HandleFunc("GET /v1/email/mailboxes/{id}/suppressions", requireCapability(deps, authorization.IntegrationManage, handleListEmailSuppressions(deps)))
	mux.HandleFunc("DELETE /v1/email/mailboxes/{id}/suppressions/{address}", requireCapability(deps, authorization.IntegrationManage, handleRemoveEmailSuppression(deps)))
	mux.HandleFunc("GET /v1/email/suppressions", requireCapability(deps, authorization.IntegrationManage, handleListEmailSuppressions(deps)))

	// Providers post their normalized JSON payload here. Authentication is the
	// mailbox's HMAC secret, not a dashboard session.
	mux.HandleFunc("POST /v1/email/inbound/{provider}", handleInboundEmail(deps))
	// Delivery providers use a mailbox-specific callback URL so status and
	// suppression updates cannot be routed to another workspace.
	mux.HandleFunc("POST /v1/email/delivery/{provider}/{mailboxID}", handleEmailDelivery(deps))
}

func handleEmailStatus(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email := deps.Config.Email
		configured := email.Enabled && email.SMTPHost != "" && email.FromAddress != ""
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{
			"configured":   configured,
			"host":         email.SMTPHost,
			"port":         email.SMTPPort,
			"from_address": email.FromAddress,
			"encryption":   email.Encryption,
		})
	}
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

func handleListEmailDeliveryEvents(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if _, err := deps.EmailChannel.Get(r.Context(), actor.WorkspaceID, r.PathValue("id")); err != nil {
			writeEmailChannelError(w, r, err)
			return
		}
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}
		items, err := deps.EmailChannel.ListDeliveryEvents(r.Context(), actor.WorkspaceID, r.PathValue("id"), cursor.At, cursor.ID, limit+1)
		if err != nil {
			writeEmailChannelInternal(w, r)
			return
		}
		page := NewPage(items, limit, func(item emailchannel.DeliveryEventView) Cursor {
			return Cursor{At: item.OccurredAt, ID: item.ID}
		})
		httpserver.WriteJSON(w, http.StatusOK, page)
	}
}

func handleListEmailSuppressions(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if mailboxID := r.PathValue("id"); mailboxID != "" {
			if _, err := deps.EmailChannel.Get(r.Context(), actor.WorkspaceID, mailboxID); err != nil {
				writeEmailChannelError(w, r, err)
				return
			}
		}
		items, err := deps.EmailChannel.ListSuppressions(r.Context(), actor.WorkspaceID, 200)
		if err != nil {
			writeEmailChannelInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
	}
}

func handleRemoveEmailSuppression(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if err := deps.EmailChannel.RemoveSuppression(r.Context(), actor.WorkspaceID, r.PathValue("address")); err != nil {
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
		input, err := emailchannel.UnmarshalProviderPayloadFor(r.PathValue("provider"), r.Header.Get("Content-Type"), body)
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

func handleEmailDelivery(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Delivery payload is too large or malformed.")
			return
		}
		event, err := emailchannel.UnmarshalDeliveryPayload(r.PathValue("provider"), r.Header.Get("Content-Type"), body)
		if err != nil {
			writeEmailChannelValidation(w, r, err)
			return
		}
		if err := deps.EmailChannel.IngestDelivery(r.Context(), r.PathValue("mailboxID"), r.PathValue("provider"), body, r.Header.Get("X-Hubchat-Signature"), event); err != nil {
			writeEmailChannelDeliveryError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
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

func writeEmailChannelDeliveryError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, emailchannel.ErrNotFound), errors.Is(err, emailchannel.ErrSignature), errors.Is(err, emailchannel.ErrSecretUnavailable):
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeUnauthorized, "Email delivery authentication failed.")
	case errors.Is(err, emailchannel.ErrInvalidDelivery):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, "The delivery event is invalid.")
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
