package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/mailer"
	"github.com/hubchat/hubchat/internal/workspace"
)

func registerInviteRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/invites",
		requireCapability(deps, authorization.MemberManage, handleListInvites(deps)))
	mux.HandleFunc("POST /v1/invites",
		requireCapability(deps, authorization.MemberManage, handleCreateInvite(deps)))
	mux.HandleFunc("DELETE /v1/invites/{id}",
		requireCapability(deps, authorization.MemberManage, handleRevokeInvite(deps)))

	// Public: reachable by anyone holding the link, signed in or not — that
	// is the entire mechanism an invite is.
	mux.HandleFunc("GET /v1/invites/lookup/{token}", handleLookupInvite(deps))
	mux.HandleFunc("POST /v1/invites/redeem", handleRedeemInvite(deps))
}

type inviteJSON struct {
	ID         string  `json:"id"`
	Email      string  `json:"email"`
	Role       string  `json:"role"`
	ExpiresAt  string  `json:"expires_at"`
	AcceptedAt *string `json:"accepted_at"`
	CreatedAt  string  `json:"created_at"`
}

func handleListInvites(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		invites, err := deps.Workspace.ListInvites(r.Context(), actor.WorkspaceID)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load invites.")
			return
		}

		out := make([]inviteJSON, 0, len(invites))
		for _, invite := range invites {
			out = append(out, inviteJSON{
				ID: invite.ID, Email: invite.Email, Role: invite.Role,
				ExpiresAt:  invite.ExpiresAt.UTC().Format(time.RFC3339),
				AcceptedAt: formatOptionalTime(invite.AcceptedAt),
				CreatedAt:  invite.CreatedAt.UTC().Format(time.RFC3339),
			})
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
	}
}

type createInviteRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

func handleCreateInvite(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req createInviteRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		token, invite, err := deps.Workspace.IssueInvite(r.Context(), actor.WorkspaceID, actor.MemberID, req.Email, req.Role)
		if err != nil {
			writeInviteError(w, r, err)
			return
		}

		ws, err := deps.Workspace.Get(r.Context(), actor.WorkspaceID)
		if err == nil {
			deps.sendMail(r, invite.Email, "You've been invited to "+ws.Name, "workspace_invite", mailer.Data{
				WorkspaceName: ws.Name,
				RoleLabel:     invite.Role,
				Link:          deps.pathLink("/app/invite/" + token),
				ExpiresIn:     "7 days",
			})
		}

		deps.recordUserAudit(r, audit.MemberInvited, actor.UserID)

		httpserver.WriteJSON(w, http.StatusCreated, inviteJSON{
			ID: invite.ID, Email: invite.Email, Role: invite.Role,
			ExpiresAt: invite.ExpiresAt.UTC().Format(time.RFC3339),
			CreatedAt: invite.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
}

func handleRevokeInvite(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		if err := deps.Workspace.RevokeInvite(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id")); err != nil {
			writeInviteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleLookupInvite(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		details, err := deps.Workspace.LookupInvite(r.Context(), r.PathValue("token"))
		if err != nil {
			writeInviteError(w, r, err)
			return
		}

		httpserver.WriteJSON(w, http.StatusOK, map[string]any{
			"workspace_name": details.WorkspaceName,
			"email":          details.Email,
			"role":           details.Role,
			"expires_at":     details.ExpiresAt.UTC().Format(time.RFC3339),
		})
	}
}

type redeemInviteRequest struct {
	Token string `json:"token"`
	// Name and Password are only used when the invited address has no
	// account yet — RedeemInvite itself only ever attaches an *existing*
	// user, so this handler creates the account first when there is none to
	// attach.
	Name     string `json:"name"`
	Password string `json:"password"`
}

// handleRedeemInvite accepts an invite for either a signed-in user or a brand
// new one.
//
// Account creation is orchestrated here rather than inside
// workspace.RedeemInvite, deliberately: creating a user is auth's job, and
// attaching a membership is workspace's, and the docs/backend.md boundary
// rule is that cross-module actions are explicit calls at this layer, not one
// module reaching into another's tables.
func handleRedeemInvite(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req redeemInviteRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		details, err := deps.Workspace.LookupInvite(r.Context(), req.Token)
		if err != nil {
			writeInviteError(w, r, err)
			return
		}

		var userID, userEmail string

		if token := httpserver.SessionToken(r); token != "" {
			user, err := deps.Auth.UserForSession(r.Context(), token)
			if err != nil {
				httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeUnauthorized, "Your session has expired.")
				return
			}
			userID, userEmail = user.ID, user.Email
		} else {
			// No session: this must be a new account for the invited address.
			// SignUp normalises and validates it the same as any other signup.
			user, err := deps.Auth.SignUp(r.Context(), req.Name, details.Email, req.Password)
			if err != nil {
				writeAuthError(w, r, err)
				return
			}
			userID, userEmail = user.ID, user.Email
			issueSession(w, r, deps, userID)
		}

		ws, err := deps.Workspace.RedeemInvite(r.Context(), req.Token, userID, userEmail)
		if err != nil {
			writeInviteError(w, r, err)
			return
		}

		httpserver.WriteJSON(w, http.StatusOK, map[string]any{
			"workspace_id": ws.ID, "workspace_name": ws.Name, "workspace_slug": ws.Slug,
		})
	}
}

func formatOptionalTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	formatted := t.UTC().Format(time.RFC3339)
	return &formatted
}

func writeInviteError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, workspace.ErrInviteExists):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, err.Error())
	case errors.Is(err, workspace.ErrAlreadyMember):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, err.Error())
	case errors.Is(err, workspace.ErrInvalidRole), errors.Is(err, workspace.ErrCannotDemoteOwner):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	case errors.Is(err, workspace.ErrInviteNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "This invite does not exist or was already used.")
	case errors.Is(err, workspace.ErrInviteExpired):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, "invite_expired", err.Error())
	case errors.Is(err, workspace.ErrInviteEmailMismatch):
		httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeForbidden, err.Error())
	default:
		httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Something went wrong.")
	}
}
