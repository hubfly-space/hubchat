package api

import (
	"encoding/base64"
	"encoding/json"
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
	mux.HandleFunc("POST /v1/roles",
		requireCapability(deps, authorization.MemberManage, idempotent(handleCreateRole(deps))))
	mux.HandleFunc("PATCH /v1/roles/{id}",
		requireCapability(deps, authorization.MemberManage, idempotent(handleUpdateRole(deps))))
	mux.HandleFunc("DELETE /v1/roles/{id}",
		requireCapability(deps, authorization.MemberManage, idempotent(handleDeleteRole(deps))))

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
				Role: member.Role, Active: member.Active, Capabilities: []string{}, Teams: orEmpty(member.TeamIDs),
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
	ID           string   `json:"id"`
	WorkspaceID  string   `json:"workspace_id,omitempty"`
	Key          string   `json:"key"`
	Name         string   `json:"name"`
	Description  *string  `json:"description"`
	IsBuiltin    bool     `json:"is_builtin"`
	Capabilities []string `json:"capabilities"`
}

type createRoleRequest struct {
	Key          string   `json:"key"`
	Name         string   `json:"name"`
	Description  *string  `json:"description"`
	Capabilities []string `json:"capabilities"`
}

type updateRoleRequest struct {
	Name         string   `json:"name"`
	Description  *string  `json:"description"`
	Capabilities []string `json:"capabilities"`
}

func roleJSONValue(role *workspace.RoleDefinition) roleJSON {
	return roleJSON{
		ID: role.ID, WorkspaceID: role.WorkspaceID, Key: role.Key, Name: role.Name,
		Description: role.Description, IsBuiltin: role.IsBuiltin,
		Capabilities: orEmpty(role.Capabilities),
	}
}

func handleCreateRole(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req createRoleRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed role request.")
			return
		}
		capabilities := make([]authorization.Capability, len(req.Capabilities))
		for i, capability := range req.Capabilities {
			capabilities[i] = authorization.Capability(capability)
		}
		role, err := deps.Workspace.CreateRole(r.Context(), actor.WorkspaceID, actor.MemberID, workspace.RoleInput{
			Key: req.Key, Name: req.Name, Description: req.Description, Capabilities: capabilities,
		})
		if err != nil {
			writeRoleError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, roleJSONValue(role))
	}
}

func handleUpdateRole(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req updateRoleRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed role request.")
			return
		}
		var capabilities []authorization.Capability
		if req.Capabilities != nil {
			capabilities = make([]authorization.Capability, len(req.Capabilities))
			for i, capability := range req.Capabilities {
				capabilities[i] = authorization.Capability(capability)
			}
		}
		role, err := deps.Workspace.UpdateRole(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), workspace.RoleUpdateInput{
			Name: req.Name, Description: req.Description, Capabilities: capabilities,
		})
		if err != nil {
			writeRoleError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, roleJSONValue(role))
	}
}

func handleDeleteRole(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if err := deps.Workspace.DeleteRole(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id")); err != nil {
			writeRoleError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleListRoles(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed role cursor.")
			return
		}
		roleCursor, err := decodeRoleCursor(cursor.Value)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed role cursor.")
			return
		}
		roles, err := deps.Workspace.ListRolesPage(r.Context(), actor.WorkspaceID, roleCursor, limit+1)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load roles.")
			return
		}

		page := NewPage(roles, limit, func(role workspace.RoleDefinition) Cursor {
			return Cursor{Value: encodeRoleCursor(workspace.RoleCursorFor(role)), ID: role.ID}
		})
		out := make([]roleJSON, 0, len(page.Data))
		for _, role := range page.Data {
			out = append(out, roleJSONValue(&role))
		}
		httpserver.WriteJSON(w, http.StatusOK, Page[roleJSON]{Data: out, NextCursor: page.NextCursor, HasMore: page.HasMore})
	}
}

func encodeRoleCursor(cursor workspace.RoleListCursor) string {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeRoleCursor(value string) (workspace.RoleListCursor, error) {
	if value == "" {
		return workspace.RoleListCursor{}, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return workspace.RoleListCursor{}, ErrBadCursor
	}
	var cursor workspace.RoleListCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.Key == "" || cursor.Name == "" || cursor.BuiltinRank < 0 || cursor.BuiltinRank > 1 || cursor.OwnerRank < 0 || cursor.OwnerRank > 1 {
		return workspace.RoleListCursor{}, ErrBadCursor
	}
	return cursor, nil
}

func writeRoleError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, workspace.ErrRoleNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "No such role.")
	case errors.Is(err, workspace.ErrRoleInUse), errors.Is(err, workspace.ErrRoleKeyTaken):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, err.Error())
	case errors.Is(err, workspace.ErrRoleBuiltin), errors.Is(err, workspace.ErrRoleKeyInvalid),
		errors.Is(err, workspace.ErrRoleKeyReserved), errors.Is(err, workspace.ErrRoleNameRequired),
		errors.Is(err, workspace.ErrInvalidCapability), errors.Is(err, workspace.ErrInvalidRole):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	default:
		httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not update roles.")
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
