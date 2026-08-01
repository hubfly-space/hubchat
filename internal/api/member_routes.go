package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/workspace"
)

// registerMemberRoutes mounts member directory and lifecycle management.
func registerMemberRoutes(mux *http.ServeMux, deps Deps) {
	idempotent := Idempotency(deps)
	mux.HandleFunc("GET /v1/members",
		requireActor(deps, handleListMembers(deps)))
	mux.HandleFunc("GET /v1/roles",
		requireActor(deps, handleListRoles(deps)))

	mux.HandleFunc("PATCH /v1/members/{id}/role",
		requireCapability(deps, authorization.MemberManage, idempotent(handleSetMemberRole(deps))))
	mux.HandleFunc("PATCH /v1/members/{id}/capabilities",
		requireCapability(deps, authorization.MemberManage, idempotent(handleSetMemberCapabilities(deps))))
	mux.HandleFunc("DELETE /v1/members/{id}",
		requireCapability(deps, authorization.MemberManage, idempotent(handleRemoveMember(deps))))

	// Self-service: a member changes their own status without member.manage.
	mux.HandleFunc("PATCH /v1/members/me/presence",
		requireActor(deps, idempotent(handleSetOwnPresence(deps))))
	mux.HandleFunc("PATCH /v1/members/me/accepting-conversations",
		requireActor(deps, idempotent(handleSetOwnAccepting(deps))))
}

func handleListMembers(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed member cursor.")
			return
		}
		members, err := deps.Workspace.ListMembersPage(r.Context(), actor.WorkspaceID, r.URL.Query().Get("q"), cursor.Value, cursor.ID, limit+1)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load members.")
			return
		}

		page := NewPage(members, limit, func(member workspace.MemberProfile) Cursor { return Cursor{Value: member.Name, ID: member.ID} })
		out := make([]memberJSON, 0, len(page.Data))
		for _, member := range page.Data {
			out = append(out, memberJSON{
				ID: member.ID, WorkspaceID: actor.WorkspaceID, UserID: member.UserID,
				Name: member.Name, Email: member.Email, AvatarURL: member.AvatarURL,
				Role: member.Role, Capabilities: []string{}, Teams: orEmpty(member.TeamIDs),
				Presence: member.Presence, Accepting: member.Accepting,
				LastSeenAt: formatOptionalTime(member.LastSeenAt),
				CreatedAt:  member.CreatedAt.UTC().Format(time.RFC3339),
			})
		}
		httpserver.WriteJSON(w, http.StatusOK, Page[memberJSON]{Data: out, NextCursor: page.NextCursor, HasMore: page.HasMore})
	}
}

type setRoleRequest struct {
	Role string `json:"role"`
}

func handleSetMemberRole(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req setRoleRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		err := deps.Workspace.SetMemberRole(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.Role)
		if err != nil {
			writeMemberError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type setCapabilitiesRequest struct {
	Capabilities []string `json:"capabilities"`
}

func handleSetMemberCapabilities(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req setCapabilitiesRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		capabilities := make([]authorization.Capability, len(req.Capabilities))
		for i, c := range req.Capabilities {
			capabilities[i] = authorization.Capability(c)
		}

		err := deps.Workspace.SetExtraCapabilities(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), capabilities)
		if err != nil {
			writeMemberError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleRemoveMember(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		if err := deps.Workspace.RemoveMember(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id")); err != nil {
			writeMemberError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type setPresenceRequest struct {
	Presence string `json:"presence"`
}

func handleSetOwnPresence(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req setPresenceRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		if err := deps.Workspace.SetOwnPresence(r.Context(), actor.WorkspaceID, actor.UserID, req.Presence); err != nil {
			writeMemberError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type setAcceptingRequest struct {
	Accepting bool `json:"accepting"`
}

func handleSetOwnAccepting(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req setAcceptingRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		if err := deps.Workspace.SetOwnAcceptingConversations(r.Context(), actor.WorkspaceID, actor.UserID, req.Accepting); err != nil {
			writeMemberError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type roleJSON struct {
	Key          string   `json:"key"`
	Name         string   `json:"name"`
	Description  *string  `json:"description"`
	Capabilities []string `json:"capabilities"`
}

func handleListRoles(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		roles, err := deps.Workspace.ListRoles(r.Context())
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load roles.")
			return
		}

		out := make([]roleJSON, 0, len(roles))
		for _, role := range roles {
			out = append(out, roleJSON{
				Key: role.Key, Name: role.Name, Description: role.Description,
				Capabilities: orEmpty(role.Capabilities),
			})
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
	}
}

func writeMemberError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, workspace.ErrInvalidRole),
		errors.Is(err, workspace.ErrCannotDemoteOwner),
		errors.Is(err, workspace.ErrInvalidPresence):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	case errors.Is(err, workspace.ErrLastOwner):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, err.Error())
	case errors.Is(err, workspace.ErrSelfRemoval):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	case errors.Is(err, workspace.ErrMemberNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "No such member.")
	default:
		httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Something went wrong.")
	}
}
