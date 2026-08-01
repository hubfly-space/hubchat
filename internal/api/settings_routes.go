package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/hubchat/hubchat/internal/analytics"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/workspace"
)

// registerSettingsRoutes mounts the workspace settings screens: general,
// branding, security, and privacy.
func registerSettingsRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/workspace/settings",
		requireActor(deps, handleGetSettings(deps)))
	mux.HandleFunc("GET /v1/workspace/usage",
		requireCapability(deps, authorization.WorkspaceManage, handleGetUsage(deps)))

	mux.HandleFunc("PATCH /v1/workspace/general",
		requireCapability(deps, authorization.WorkspaceManage, Idempotency(deps)(handleUpdateGeneral(deps))))
	mux.HandleFunc("PATCH /v1/workspace/branding",
		requireCapability(deps, authorization.WorkspaceManage, Idempotency(deps)(handleUpdateBranding(deps))))

	// Security and privacy are gated on workspace.manage_security rather than
	// workspace.manage — §5.1/§5.2 reserve session policy, sign-in methods,
	// and retention specifically for the owner and security-privileged
	// admins, not general administration.
	mux.HandleFunc("PATCH /v1/workspace/security",
		requireCapability(deps, authorization.WorkspaceManageSecurity, Idempotency(deps)(handleUpdateSecurity(deps))))
	mux.HandleFunc("PATCH /v1/workspace/privacy",
		requireCapability(deps, authorization.WorkspaceManageSecurity, Idempotency(deps)(handleUpdatePrivacy(deps))))
	mux.HandleFunc("GET /v1/workspace/legal-holds",
		requireCapability(deps, authorization.WorkspaceManageSecurity, handleListLegalHolds(deps)))
	mux.HandleFunc("POST /v1/workspace/legal-holds",
		requireCapability(deps, authorization.WorkspaceManageSecurity, Idempotency(deps)(handleCreateLegalHold(deps))))
	mux.HandleFunc("POST /v1/workspace/legal-holds/{id}/release",
		requireCapability(deps, authorization.WorkspaceManageSecurity, Idempotency(deps)(handleReleaseLegalHold(deps))))
}

func handleGetUsage(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		usage, err := deps.Analytics.Usage(r.Context(), actor.WorkspaceID, time.Now().UTC())
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load workspace usage.")
			return
		}
		usage.RequestLimits = append(usage.RequestLimits,
			analytics.UsageLimit{Key: "max_file_bytes", Label: "Maximum file size", Value: deps.Config.Storage.MaxFileBytes, Unit: "bytes"},
			analytics.UsageLimit{Key: "max_event_bytes", Label: "Maximum event payload", Value: deps.Config.Limits.MaxEventBytes, Unit: "bytes"},
			analytics.UsageLimit{Key: "max_request_bytes", Label: "Maximum request body", Value: deps.Config.Server.MaxRequestBytes, Unit: "bytes"},
			analytics.UsageLimit{Key: "max_attributes_per_customer", Label: "Maximum custom attributes per customer", Value: int64(deps.Config.Limits.MaxAttributesPerCustomer), Unit: "count"},
			analytics.UsageLimit{Key: "max_actions_per_rule", Label: "Maximum actions per rule execution", Value: int64(deps.Config.Limits.MaxActionsPerRule), Unit: "count"},
		)
		httpserver.WriteJSON(w, http.StatusOK, usage)
	}
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
		if security.RequireSSO && deps.Auth.OAuthProvider() == nil {
			httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError,
				"Organization SSO is not configured for this deployment.")
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

type createLegalHoldRequest struct {
	Category string `json:"category"`
	Reason   string `json:"reason"`
}

func handleListLegalHolds(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed legal hold cursor.")
			return
		}
		holds, err := deps.Workspace.ListLegalHoldsPage(r.Context(), actor.WorkspaceID, r.URL.Query().Get("include_released") == "true", cursor.At, cursor.ID, limit+1)
		if err != nil {
			writeSettingsError(w, r, err)
			return
		}
		page := NewPage(holds, limit, func(item workspace.LegalHold) Cursor {
			return Cursor{At: item.CreatedAt, ID: item.ID}
		})
		httpserver.WriteJSON(w, http.StatusOK, page)
	}
}

func handleCreateLegalHold(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req createLegalHoldRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		hold, err := deps.Workspace.CreateLegalHold(r.Context(), actor.WorkspaceID, actor.MemberID, workspace.LegalHoldInput{Category: req.Category, Reason: req.Reason})
		if err != nil {
			writeSettingsError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, hold)
	}
}

func handleReleaseLegalHold(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		hold, err := deps.Workspace.ReleaseLegalHold(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"))
		if err != nil {
			writeSettingsError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, hold)
	}
}

func writeSettingsError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, workspace.ErrInvalidName), errors.Is(err, workspace.ErrInvalidSettings), errors.Is(err, workspace.ErrInvalidLegalHold):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	case errors.Is(err, workspace.ErrNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Workspace not found.")
	case errors.Is(err, workspace.ErrLegalHoldNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Legal hold not found in this workspace.")
	default:
		httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Something went wrong.")
	}
}
