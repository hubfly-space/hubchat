package api

import (
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/httpserver"
	"net/http"
)

func registerContextCardRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/customer-context/cards", requireCapability(deps, authorization.CustomerRead, func(w http.ResponseWriter, r *http.Request) {
		cards, err := deps.Customer.ContextCards(r.Context(), actorFromRequest(r).WorkspaceID)
		if err != nil {
			writeCustomerError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, Page[customer.ContextCard]{Data: cards})
	}))
	save := func(w http.ResponseWriter, r *http.Request) {
		var input customer.ContextCard
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed card.")
			return
		}
		card, err := deps.Customer.SaveContextCard(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"), input)
		if err != nil {
			writeCustomerError(w, r, err)
			return
		}
		status := http.StatusOK
		if r.PathValue("id") == "" {
			status = http.StatusCreated
		}
		httpserver.WriteJSON(w, status, card)
	}
	mux.HandleFunc("POST /v1/customer-context/cards", requireCapability(deps, authorization.WorkspaceManage, Idempotency(deps)(save)))
	mux.HandleFunc("PATCH /v1/customer-context/cards/{id}", requireCapability(deps, authorization.WorkspaceManage, Idempotency(deps)(save)))
	mux.HandleFunc("DELETE /v1/customer-context/cards/{id}", requireCapability(deps, authorization.WorkspaceManage, func(w http.ResponseWriter, r *http.Request) {
		if err := deps.Customer.DeleteContextCard(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id")); err != nil {
			writeCustomerError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
}
