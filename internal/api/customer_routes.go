package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/httpserver"
)

// registerCustomerRoutes mounts the minimal customer surface the inbox needs
// today: read a profile, edit its basic fields, tag it, and block it. Full
// identity merging, attribute definitions, and event ingestion are Stage 4.
func registerCustomerRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/customers",
		requireCapability(deps, authorization.CustomerRead, handleSearchCustomers(deps)))
	mux.HandleFunc("GET /v1/customers/{id}",
		requireCapability(deps, authorization.CustomerRead, handleGetCustomer(deps)))
	mux.HandleFunc("PATCH /v1/customers/{id}",
		requireCapability(deps, authorization.CustomerRead, handleUpdateCustomer(deps)))

	mux.HandleFunc("POST /v1/customers/{id}/tags",
		requireCapability(deps, authorization.CustomerRead, handleAddCustomerTag(deps)))
	mux.HandleFunc("DELETE /v1/customers/{id}/tags/{tagID}",
		requireCapability(deps, authorization.CustomerRead, handleRemoveCustomerTag(deps)))

	mux.HandleFunc("POST /v1/blocked-contacts",
		requireCapability(deps, authorization.ConversationDelete, handleBlockContact(deps)))
}

func customerJSON(c customer.Customer, tagIDs, companyIDs []string) map[string]any {
	return map[string]any{
		"id":           c.ID,
		"workspace_id": c.WorkspaceID,
		"name":         c.Name,
		"email":        c.Email,
		"phone":        c.Phone,
		"avatar_url":   c.AvatarURL,
		"external_id":  c.ExternalID,
		"verification": c.Verification,
		"company_ids":  orEmpty(companyIDs),
		"language":     c.Language,
		"timezone":     c.Timezone,
		"attributes":   c.Attributes,
		"tag_ids":      orEmpty(tagIDs),
		"owner_id":     c.OwnerID,
		// Presence and current_url describe a live widget/portal session
		// (Stage 5/6). Until that exists there is no session to report, so
		// these are always their "nobody is here" values rather than a guess.
		"presence":          "offline",
		"current_url":       nil,
		"first_seen_at":     c.FirstSeenAt,
		"last_seen_at":      c.LastSeenAt,
		"last_contacted_at": c.LastContactedAt,
		"version":           c.Version,
	}
}

func loadCustomerJSON(r *http.Request, deps Deps, workspaceID string, c customer.Customer) map[string]any {
	tagIDs, err := deps.Customer.Tags(r.Context(), workspaceID, c.ID)
	if err != nil {
		tagIDs = []string{}
	}
	companyIDs, err := deps.Customer.CompanyIDs(r.Context(), workspaceID, c.ID)
	if err != nil {
		companyIDs = []string{}
	}
	return customerJSON(c, tagIDs, companyIDs)
}

func handleGetCustomer(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		c, err := deps.Customer.Get(r.Context(), actor.WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeCustomerError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, loadCustomerJSON(r, deps, actor.WorkspaceID, *c))
	}
}

func handleSearchCustomers(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		query := r.URL.Query()

		// ids= is a batch lookup for a page of rows that each reference a
		// customer (the inbox list), not a search — distinct enough from q=
		// that branching here beats a second endpoint for one query shape.
		var customers []customer.Customer
		var err error
		if idsParam := query.Get("ids"); idsParam != "" {
			ids := strings.Split(idsParam, ",")
			customers, err = deps.Customer.GetMany(r.Context(), actor.WorkspaceID, ids)
		} else {
			limit := 20
			if raw := query.Get("limit"); raw != "" {
				if parsed, parseErr := strconv.Atoi(raw); parseErr == nil {
					limit = parsed
				}
			}
			customers, err = deps.Customer.Search(r.Context(), actor.WorkspaceID, query.Get("q"), limit)
		}
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load customers.")
			return
		}

		out := make([]map[string]any, len(customers))
		for i, c := range customers {
			out[i] = loadCustomerJSON(r, deps, actor.WorkspaceID, c)
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
	}
}

type updateCustomerRequest struct {
	Version  int     `json:"version"`
	Name     *string `json:"name"`
	Email    *string `json:"email"`
	Phone    *string `json:"phone"`
	Language *string `json:"language"`
	Timezone *string `json:"timezone"`
}

func handleUpdateCustomer(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req updateCustomerRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		c, err := deps.Customer.Update(
			r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.Version,
			req.Name, req.Email, req.Phone, req.Language, req.Timezone,
		)
		if err != nil {
			writeCustomerError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, loadCustomerJSON(r, deps, actor.WorkspaceID, *c))
	}
}

type addCustomerTagRequest struct {
	TagID string `json:"tag_id"`
}

func handleAddCustomerTag(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req addCustomerTagRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		if err := deps.Customer.AddTag(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.TagID); err != nil {
			writeCustomerError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleRemoveCustomerTag(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		if err := deps.Customer.RemoveTag(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), r.PathValue("tagID")); err != nil {
			writeCustomerError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type blockContactRequest struct {
	Kind   string  `json:"kind"`
	Value  string  `json:"value"`
	Reason *string `json:"reason"`
}

func handleBlockContact(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req blockContactRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		if err := deps.Customer.Block(r.Context(), actor.WorkspaceID, actor.MemberID, req.Kind, req.Value, req.Reason); err != nil {
			writeCustomerError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func writeCustomerError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, customer.ErrNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Customer not found.")
	case errors.Is(err, customer.ErrVersionConflict):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, err.Error())
	case errors.Is(err, customer.ErrTagNotFound):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	case errors.Is(err, customer.ErrInvalidBlockKind):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	default:
		httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Something went wrong.")
	}
}
