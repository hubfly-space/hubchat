package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/httpserver"
)

// registerCompanyRoutes mounts company (account) CRUD, tags, roster
// management, and attributes (§6.9).
func registerCompanyRoutes(mux *http.ServeMux, deps Deps) {
	idempotent := Idempotency(deps)
	mux.HandleFunc("GET /v1/companies",
		requireCapability(deps, authorization.CustomerRead, handleListCompanies(deps)))
	mux.HandleFunc("POST /v1/companies",
		requireCapability(deps, authorization.CompanyManage, idempotent(handleCreateCompany(deps))))
	mux.HandleFunc("GET /v1/companies/{id}",
		requireCapability(deps, authorization.CustomerRead, handleGetCompany(deps)))
	mux.HandleFunc("PATCH /v1/companies/{id}",
		requireCapability(deps, authorization.CompanyManage, handleUpdateCompany(deps)))

	mux.HandleFunc("POST /v1/companies/{id}/tags",
		requireCapability(deps, authorization.CompanyManage, handleAddCompanyTag(deps)))
	mux.HandleFunc("DELETE /v1/companies/{id}/tags/{tagID}",
		requireCapability(deps, authorization.CompanyManage, handleRemoveCompanyTag(deps)))

	mux.HandleFunc("PATCH /v1/companies/{id}/attributes",
		requireCapability(deps, authorization.CompanyManage, handleSetCompanyAttributes(deps)))

	mux.HandleFunc("GET /v1/companies/{id}/customers",
		requireCapability(deps, authorization.CustomerRead, handleListCompanyCustomers(deps)))
	mux.HandleFunc("PUT /v1/companies/{id}/customers/{customerID}",
		requireCapability(deps, authorization.CompanyManage, idempotent(handleLinkCompanyCustomer(deps))))
	mux.HandleFunc("DELETE /v1/companies/{id}/customers/{customerID}",
		requireCapability(deps, authorization.CompanyManage, handleUnlinkCompanyCustomer(deps)))
}

func companyJSON(c customer.Company, tagIDs []string) map[string]any {
	return map[string]any{
		"id": c.ID, "workspace_id": c.WorkspaceID, "name": c.Name, "domain": c.Domain,
		"external_id": c.ExternalID, "tier": c.Tier, "owner_id": c.OwnerID, "attributes": c.Attributes,
		"tag_ids": orEmpty(tagIDs), "sla_policy_id": c.SLAPolicyID,
		"customer_count": c.CustomerCount, "open_ticket_count": c.OpenTicketCount, "created_at": c.CreatedAt,
	}
}

func loadCompanyJSON(r *http.Request, deps Deps, workspaceID string, c customer.Company) map[string]any {
	tagIDs, err := deps.Customer.CompanyTags(r.Context(), workspaceID, c.ID)
	if err != nil {
		tagIDs = []string{}
	}
	return companyJSON(c, tagIDs)
}

func handleListCompanies(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		query := r.URL.Query()

		if idsParam := query.Get("ids"); idsParam != "" {
			companies, err := deps.Customer.Companies(r.Context(), actor.WorkspaceID, strings.Split(idsParam, ","))
			if err != nil {
				httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load companies.")
				return
			}
			out := make([]map[string]any, len(companies))
			for i, c := range companies {
				out[i] = loadCompanyJSON(r, deps, actor.WorkspaceID, c)
			}
			httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
			return
		}

		limit := 50
		if raw := query.Get("limit"); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil {
				limit = parsed
			}
		}
		companies, err := deps.Customer.ListCompanies(r.Context(), actor.WorkspaceID, query.Get("q"), limit)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load companies.")
			return
		}
		out := make([]map[string]any, len(companies))
		for i, c := range companies {
			out[i] = loadCompanyJSON(r, deps, actor.WorkspaceID, c)
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
	}
}

type companyRequest struct {
	Name       string  `json:"name"`
	Domain     *string `json:"domain"`
	ExternalID *string `json:"external_id"`
	Tier       *string `json:"tier"`
	OwnerID    *string `json:"owner_id"`
}

func handleCreateCompany(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req companyRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		c, err := deps.Customer.CreateCompany(r.Context(), actor.WorkspaceID, actor.MemberID, req.Name, req.Domain, req.ExternalID, req.Tier)
		if err != nil {
			writeCustomerError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, loadCompanyJSON(r, deps, actor.WorkspaceID, *c))
	}
}

func handleGetCompany(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		c, err := deps.Customer.Company(r.Context(), actor.WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeCustomerError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, loadCompanyJSON(r, deps, actor.WorkspaceID, *c))
	}
}

func handleUpdateCompany(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req companyRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		c, err := deps.Customer.UpdateCompany(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.Name, req.Domain, req.ExternalID, req.Tier, req.OwnerID)
		if err != nil {
			writeCustomerError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, loadCompanyJSON(r, deps, actor.WorkspaceID, *c))
	}
}

type addCompanyTagRequest struct {
	TagID string `json:"tag_id"`
}

func handleAddCompanyTag(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req addCompanyTagRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		if err := deps.Customer.AddCompanyTag(r.Context(), actor.WorkspaceID, r.PathValue("id"), req.TagID); err != nil {
			writeCustomerError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleRemoveCompanyTag(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if err := deps.Customer.RemoveCompanyTag(r.Context(), actor.WorkspaceID, r.PathValue("id"), r.PathValue("tagID")); err != nil {
			writeCustomerError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleSetCompanyAttributes(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req setAttributesRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		c, err := deps.Customer.SetCompanyAttributes(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), "rest_api", req.Attributes)
		if err != nil {
			writeCustomerError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, loadCompanyJSON(r, deps, actor.WorkspaceID, *c))
	}
}

func handleListCompanyCustomers(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		customers, err := deps.Customer.CompanyCustomers(r.Context(), actor.WorkspaceID, r.PathValue("id"), 50)
		if err != nil {
			writeCustomerError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": customerJSONList(r, deps, actor.WorkspaceID, customers)})
	}
}

func handleLinkCompanyCustomer(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if err := deps.Customer.LinkCustomer(r.Context(), actor.WorkspaceID, r.PathValue("id"), r.PathValue("customerID")); err != nil {
			writeCustomerError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleUnlinkCompanyCustomer(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if err := deps.Customer.UnlinkCustomer(r.Context(), actor.WorkspaceID, r.PathValue("id"), r.PathValue("customerID")); err != nil {
			writeCustomerError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
