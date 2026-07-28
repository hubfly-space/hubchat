package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/workspace"
)

// registerSettingsRoutes mounts the workspace settings screens: general,
// branding, security, and privacy.
func registerSettingsRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/workspace/settings",
		requireActor(deps, handleGetSettings(deps)))

	mux.HandleFunc("PATCH /v1/workspace/general",
		requireCapability(deps, authorization.WorkspaceManage, handleUpdateGeneral(deps)))
	mux.HandleFunc("PATCH /v1/workspace/branding",
		requireCapability(deps, authorization.WorkspaceManage, handleUpdateBranding(deps)))

	// Security and privacy are gated on workspace.manage_security rather than
	// workspace.manage — §5.1/§5.2 reserve session policy, sign-in methods,
	// and retention specifically for the owner and security-privileged
	// admins, not general administration.
	mux.HandleFunc("PATCH /v1/workspace/security",
		requireCapability(deps, authorization.WorkspaceManageSecurity, handleUpdateSecurity(deps)))
	mux.HandleFunc("PATCH /v1/workspace/privacy",
		requireCapability(deps, authorization.WorkspaceManageSecurity, handleUpdatePrivacy(deps)))
}

func handleGetSettings(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		settings, err := deps.Workspace.GetSettings(r.Context(), actor.WorkspaceID)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load settings.")
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, settings)
	}
}

type updateGeneralRequest struct {
	Name            string `json:"name"`
	TicketPrefix    string `json:"ticket_prefix"`
	Timezone        string `json:"timezone"`
	DefaultLanguage string `json:"default_language"`
}

func handleUpdateGeneral(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req updateGeneralRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		ws, err := deps.Workspace.UpdateGeneral(
			r.Context(), actor.WorkspaceID, actor.MemberID,
			req.Name, req.TicketPrefix, req.Timezone, req.DefaultLanguage,
		)
		if err != nil {
			writeSettingsError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, workspaceJSON{
			ID: ws.ID, Name: ws.Name, Slug: ws.Slug, DefaultLanguage: ws.DefaultLanguage,
			Timezone: ws.Timezone, TicketPrefix: ws.TicketPrefix,
			CreatedAt: ws.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
}

type updateBrandingRequest struct {
	LogoURL      *string `json:"logo_url"`
	IconURL      *string `json:"icon_url"`
	AccentColor  string  `json:"accent_color"`
	EmailFooter  string  `json:"email_footer"`
	HideBranding bool    `json:"hide_branding"`
}

func handleUpdateBranding(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req updateBrandingRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		_, err := deps.Workspace.UpdateBranding(r.Context(), actor.WorkspaceID, actor.MemberID, req.LogoURL, req.IconURL, workspace.BrandingSettings{
			AccentColor: req.AccentColor, EmailFooter: req.EmailFooter, HideBranding: req.HideBranding,
		})
		if err != nil {
			writeSettingsError(w, r, err)
			return
		}

		settings, err := deps.Workspace.GetSettings(r.Context(), actor.WorkspaceID)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Saved, but could not reload settings.")
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, settings)
	}
}

func handleUpdateSecurity(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var security workspace.SecuritySettings
		if err := httpserver.DecodeJSON(r, &security); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		settings, err := deps.Workspace.UpdateSecuritySettings(r.Context(), actor.WorkspaceID, actor.MemberID, security)
		if err != nil {
			writeSettingsError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, settings)
	}
}

func handleUpdatePrivacy(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var privacy workspace.PrivacySettings
		if err := httpserver.DecodeJSON(r, &privacy); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		settings, err := deps.Workspace.UpdatePrivacySettings(r.Context(), actor.WorkspaceID, actor.MemberID, privacy)
		if err != nil {
			writeSettingsError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, settings)
	}
}

func writeSettingsError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, workspace.ErrInvalidName), errors.Is(err, workspace.ErrInvalidSettings):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	case errors.Is(err, workspace.ErrNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Workspace not found.")
	default:
		httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Something went wrong.")
	}
}
