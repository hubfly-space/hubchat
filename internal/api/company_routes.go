package api

import (
	"encoding/csv"
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
	mux.HandleFunc("GET /v1/companies/export",
		requireCapability(deps, authorization.CustomerRead, handleExportCompanies(deps)))
	mux.HandleFunc("POST /v1/companies",
		requireCapability(deps, authorization.CompanyManage, idempotent(handleCreateCompany(deps))))
	mux.HandleFunc("GET /v1/companies/{id}",
		requireCapability(deps, authorization.CustomerRead, handleGetCompany(deps)))
	mux.HandleFunc("PATCH /v1/companies/{id}",
		requireCapability(deps, authorization.CompanyManage, idempotent(handleUpdateCompany(deps))))

	mux.HandleFunc("POST /v1/companies/{id}/tags",
		requireCapability(deps, authorization.CompanyManage, idempotent(handleAddCompanyTag(deps))))
	mux.HandleFunc("DELETE /v1/companies/{id}/tags/{tagID}",
		requireCapability(deps, authorization.CompanyManage, idempotent(handleRemoveCompanyTag(deps))))

	mux.HandleFunc("PATCH /v1/companies/{id}/attributes",
		requireCapability(deps, authorization.CompanyManage, idempotent(handleSetCompanyAttributes(deps))))

	mux.HandleFunc("GET /v1/companies/{id}/customers",
		requireCapability(deps, authorization.CustomerRead, handleListCompanyCustomers(deps)))
	mux.HandleFunc("PUT /v1/companies/{id}/customers/{customerID}",
		requireCapability(deps, authorization.CompanyManage, idempotent(handleLinkCompanyCustomer(deps))))
	mux.HandleFunc("DELETE /v1/companies/{id}/customers/{customerID}",
		requireCapability(deps, authorization.CompanyManage, idempotent(handleUnlinkCompanyCustomer(deps))))
}

func handleExportCompanies(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		companies, err := deps.Customer.ListCompanies(r.Context(), actor.WorkspaceID, r.URL.Query().Get("q"), 10000)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not export companies.")
			return
		}

		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="companies-export.csv"`)
		writer := csv.NewWriter(w)
		_ = writer.Write([]string{"ID", "Name", "Domain", "External ID", "Tier", "Owner ID", "Customer Count", "Open Ticket Count", "Created"})
		for _, item := range companies {
			_ = writer.Write([]string{
				item.ID, item.Name, derefOrEmpty(item.Domain), derefOrEmpty(item.ExternalID),
				derefOrEmpty(item.Tier), derefOrEmpty(item.OwnerID),
				strconv.Itoa(item.CustomerCount), strconv.Itoa(item.OpenTicketCount),
				item.CreatedAt.Format(timeFormat),
			})
		}
		writer.Flush()
	}
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

		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}
		companies, err := deps.Customer.ListCompaniesPage(r.Context(), actor.WorkspaceID, query.Get("q"), cursor.Value, cursor.ID, limit+1)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load companies.")
			return
		}
		page := NewPage(companies, limit, func(c customer.Company) Cursor {
			return Cursor{Value: c.Name, ID: c.ID}
		})
		out := make([]map[string]any, len(page.Data))
		for i, c := range page.Data {
			out[i] = loadCompanyJSON(r, deps, actor.WorkspaceID, c)
		}
		httpserver.WriteJSON(w, http.StatusOK, Page[map[string]any]{Data: out, NextCursor: page.NextCursor, HasMore: page.HasMore})
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
