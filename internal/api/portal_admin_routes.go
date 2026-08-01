package api

import (
	"errors"
	"net/http"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/portal"
)

func registerPortalAdminRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/portals", requireCapability(deps, authorization.PortalManage, handleListPortals(deps)))
	mux.HandleFunc("POST /v1/portals", requireCapability(deps, authorization.PortalManage, Idempotency(deps)(handleCreatePortal(deps))))
	mux.HandleFunc("GET /v1/portals/{id}", requireCapability(deps, authorization.PortalManage, handleGetPortal(deps)))
	mux.HandleFunc("PATCH /v1/portals/{id}", requireCapability(deps, authorization.PortalManage, Idempotency(deps)(handleUpdatePortal(deps))))
	mux.HandleFunc("GET /v1/portals/{id}/domains", requireCapability(deps, authorization.PortalManage, handleListPortalDomains(deps)))
	mux.HandleFunc("POST /v1/portals/{id}/domains", requireCapability(deps, authorization.PortalManage, Idempotency(deps)(handleAddPortalDomain(deps))))
	mux.HandleFunc("POST /v1/portals/{id}/domains/{domainID}/verify", requireCapability(deps, authorization.PortalManage, handleVerifyPortalDomain(deps)))
	mux.HandleFunc("DELETE /v1/portals/{id}/domains/{domainID}", requireCapability(deps, authorization.PortalManage, Idempotency(deps)(handleDeletePortalDomain(deps))))
}

func handleListPortalDomains(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := deps.Portal.ListDomains(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"))
		if err != nil {
			writePortalAdminError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
	}
}

func handleAddPortalDomain(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Domain string `json:"domain"`
		}
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		item, err := deps.Portal.AddDomain(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"), input.Domain)
		if err != nil {
			writePortalAdminError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, item)
	}
}

func handleVerifyPortalDomain(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := deps.Portal.VerifyDomain(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"), r.PathValue("domainID"))
		if err != nil {
			writePortalAdminError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}

func handleDeletePortalDomain(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := deps.Portal.DeleteDomain(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"), r.PathValue("domainID")); err != nil {
			writePortalAdminError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"deleted": true})
	}
}

func writePortalAdminError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, portal.ErrNotFound) {
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Portal or domain not found.")
		return
	}
	httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
}

func handleListPortals(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		portals, err := deps.Portal.List(r.Context(), actor.WorkspaceID)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load portals.")
			return
		}
		out := make([]map[string]any, len(portals))
		for i, p := range portals {
			out[i] = portalJSON(p)
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
	}
}

type createPortalRequest struct {
	Name           string  `json:"name"`
	Subdomain      string  `json:"subdomain"`
	DefaultInboxID *string `json:"default_inbox_id"`
}

func handleCreatePortal(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req createPortalRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		p, err := deps.Portal.Create(r.Context(), actor.WorkspaceID, portal.CreateRequest{Name: req.Name, Subdomain: req.Subdomain, DefaultInboxID: req.DefaultInboxID})
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeValidationError, "Could not create this portal.")
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, portalJSON(*p))
	}
}

func handleGetPortal(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		p, err := deps.Portal.Get(r.Context(), actor.WorkspaceID, r.PathValue("id"))
		if errors.Is(err, portal.ErrNotFound) {
			writePortalNotFound(w, r)
			return
		}
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load this portal.")
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, portalJSON(*p))
	}
}

type updatePortalRequest struct {
	Name            *string                  `json:"name"`
	Subdomain       *string                  `json:"subdomain"`
	Theme           map[string]any           `json:"theme"`
	Features        map[string]any           `json:"features"`
	AuthMethods     []string                 `json:"auth_methods"`
	Permissions     map[string]any           `json:"permissions"`
	Navigation      *[]portal.NavigationItem `json:"navigation"`
	DefaultInboxID  *string                  `json:"default_inbox_id"`
	DefaultLanguage *string                  `json:"default_language"`
	Enabled         *bool                    `json:"enabled"`
}

func handleUpdatePortal(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req updatePortalRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		p, err := deps.Portal.Update(r.Context(), actor.WorkspaceID, r.PathValue("id"), portal.UpdateRequest{
			Name: req.Name, Subdomain: req.Subdomain, Theme: req.Theme, Features: req.Features,
			AuthMethods: req.AuthMethods, Permissions: req.Permissions, Navigation: req.Navigation, DefaultInboxID: req.DefaultInboxID,
			DefaultLanguage: req.DefaultLanguage, Enabled: req.Enabled,
		})
		if errors.Is(err, portal.ErrNotFound) {
			writePortalNotFound(w, r)
			return
		}
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeValidationError, "Could not update this portal.")
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, portalJSON(*p))
	}
}
