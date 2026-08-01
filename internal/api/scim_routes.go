package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/workspace"
)

const (
	scimUserSchema  = "urn:ietf:params:scim:schemas:core:2.0:User"
	scimListSchema  = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	scimErrorSchema = "urn:ietf:params:scim:api:messages:2.0:Error"
)

func registerSCIMRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/scim/v2.0/{workspaceID}/Users", requireSCIM(deps, handleSCIMListUsers(deps)))
	mux.HandleFunc("POST /v1/scim/v2.0/{workspaceID}/Users", requireSCIM(deps, handleSCIMCreateUser(deps)))
	mux.HandleFunc("GET /v1/scim/v2.0/{workspaceID}/Users/{id}", requireSCIM(deps, handleSCIMGetUser(deps)))
	mux.HandleFunc("PUT /v1/scim/v2.0/{workspaceID}/Users/{id}", requireSCIM(deps, handleSCIMReplaceUser(deps)))
	mux.HandleFunc("PATCH /v1/scim/v2.0/{workspaceID}/Users/{id}", requireSCIM(deps, handleSCIMPatchUser(deps)))
	mux.HandleFunc("DELETE /v1/scim/v2.0/{workspaceID}/Users/{id}", requireSCIM(deps, handleSCIMDeleteUser(deps)))
}

func requireSCIM(deps Deps, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		value := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(value, "Bearer ") || deps.APIKeys == nil {
			scimError(w, http.StatusUnauthorized, "unauthorized", "A SCIM bearer token is required.")
			return
		}
		principal, err := deps.APIKeys.Authenticate(r.Context(), strings.TrimSpace(strings.TrimPrefix(value, "Bearer ")))
		if err != nil {
			scimError(w, http.StatusUnauthorized, "unauthorized", "The SCIM bearer token is invalid or expired.")
			return
		}
		if principal.WorkspaceID != r.PathValue("workspaceID") {
			scimError(w, http.StatusForbidden, "forbidden", "The SCIM token does not belong to this workspace.")
			return
		}
		allowed := false
		for _, scope := range principal.Scopes {
			if scope == string(authorization.MemberManage) {
				allowed = true
				break
			}
		}
		if !allowed {
			scimError(w, http.StatusForbidden, "forbidden", "The SCIM token must include the member.manage scope.")
			return
		}
		actor := &authorization.Actor{MemberID: principal.KeyID, WorkspaceID: principal.WorkspaceID, Role: "api_key", Capabilities: map[authorization.Capability]bool{authorization.MemberManage: true}}
		next(w, r.WithContext(authorization.WithActor(r.Context(), actor)))
	}
}

type scimUserRequest struct {
	Schemas     []string `json:"schemas"`
	ExternalID  string   `json:"externalId"`
	UserName    string   `json:"userName"`
	DisplayName string   `json:"displayName"`
	Active      *bool    `json:"active"`
	Roles       []struct {
		Value string `json:"value"`
	} `json:"roles"`
}

type scimPatchRequest struct {
	Schemas    []string `json:"schemas"`
	Operations []struct {
		Op    string `json:"op"`
		Path  string `json:"path"`
		Value any    `json:"value"`
	} `json:"operations"`
}

func scimInput(req scimUserRequest) workspace.SCIMProvisionInput {
	role := ""
	if len(req.Roles) > 0 {
		role = strings.TrimSpace(req.Roles[0].Value)
	}
	return workspace.SCIMProvisionInput{ExternalID: req.ExternalID, UserName: req.UserName, DisplayName: req.DisplayName, Active: req.Active, Role: role}
}

func handleSCIMListUsers(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userName, externalID, err := parseSCIMFilter(r.URL.Query().Get("filter"))
		if err != nil {
			scimError(w, http.StatusBadRequest, "invalidFilter", err.Error())
			return
		}
		start, count, err := scimPageParams(r)
		if err != nil {
			scimError(w, http.StatusBadRequest, "invalidValue", err.Error())
			return
		}
		items, total, err := deps.Workspace.ListSCIMUsers(r.Context(), r.PathValue("workspaceID"), userName, externalID, start, count)
		if err != nil {
			scimError(w, http.StatusInternalServerError, "serverError", "Could not list SCIM users.")
			return
		}
		resources := make([]scimUserResource, 0, len(items))
		for _, item := range items {
			resources = append(resources, scimResource(item))
		}
		writeSCIM(w, http.StatusOK, map[string]any{"schemas": []string{scimListSchema}, "totalResults": total, "startIndex": start, "itemsPerPage": len(resources), "Resources": resources})
	}
}

func handleSCIMCreateUser(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req scimUserRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			scimError(w, http.StatusBadRequest, "invalidSyntax", "Malformed SCIM user resource.")
			return
		}
		item, created, err := deps.Workspace.ProvisionSCIMUser(r.Context(), r.PathValue("workspaceID"), actorFromRequest(r).MemberID, scimInput(req))
		if err != nil {
			handleSCIMError(w, err)
			return
		}
		if !created {
			writeSCIM(w, http.StatusOK, scimResource(*item))
			return
		}
		writeSCIM(w, http.StatusCreated, scimResource(*item))
	}
}

func handleSCIMGetUser(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := deps.Workspace.GetSCIMUser(r.Context(), r.PathValue("workspaceID"), r.PathValue("id"))
		if err != nil {
			handleSCIMError(w, err)
			return
		}
		writeSCIM(w, http.StatusOK, scimResource(*item))
	}
}

func handleSCIMReplaceUser(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req scimUserRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			scimError(w, http.StatusBadRequest, "invalidSyntax", "Malformed SCIM user resource.")
			return
		}
		current, err := deps.Workspace.GetSCIMUser(r.Context(), r.PathValue("workspaceID"), r.PathValue("id"))
		if err != nil {
			handleSCIMError(w, err)
			return
		}
		if req.ExternalID == "" {
			req.ExternalID = current.ExternalID
		}
		if req.UserName == "" {
			req.UserName = current.UserName
		}
		if req.DisplayName == "" {
			req.DisplayName = current.DisplayName
		}
		item, _, err := deps.Workspace.ProvisionSCIMUser(r.Context(), r.PathValue("workspaceID"), actorFromRequest(r).MemberID, scimInput(req))
		if err != nil {
			handleSCIMError(w, err)
			return
		}
		writeSCIM(w, http.StatusOK, scimResource(*item))
	}
}

func handleSCIMPatchUser(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		current, err := deps.Workspace.GetSCIMUser(r.Context(), r.PathValue("workspaceID"), r.PathValue("id"))
		if err != nil {
			handleSCIMError(w, err)
			return
		}
		var req scimPatchRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			scimError(w, http.StatusBadRequest, "invalidSyntax", "Malformed SCIM patch resource.")
			return
		}
		input := workspace.SCIMProvisionInput{ExternalID: current.ExternalID, UserName: current.UserName, DisplayName: current.DisplayName}
		active := current.Active
		input.Active = &active
		for _, operation := range req.Operations {
			path := strings.ToLower(strings.TrimSpace(operation.Path))
			switch path {
			case "active":
				value, ok := operation.Value.(bool)
				if !ok {
					scimError(w, http.StatusBadRequest, "invalidValue", "active must be boolean.")
					return
				}
				active = value
			case "displayname":
				value, ok := operation.Value.(string)
				if !ok {
					scimError(w, http.StatusBadRequest, "invalidValue", "displayName must be a string.")
					return
				}
				input.DisplayName = value
			case "username":
				value, ok := operation.Value.(string)
				if !ok {
					scimError(w, http.StatusBadRequest, "invalidValue", "userName must be a string.")
					return
				}
				input.UserName = value
			case "role":
				value, ok := operation.Value.(string)
				if !ok {
					scimError(w, http.StatusBadRequest, "invalidValue", "role must be a string.")
					return
				}
				input.Role = value
			default:
				scimError(w, http.StatusBadRequest, "noTarget", "Unsupported SCIM patch path.")
				return
			}
		}
		input.Active = &active
		item, _, err := deps.Workspace.ProvisionSCIMUser(r.Context(), r.PathValue("workspaceID"), actorFromRequest(r).MemberID, input)
		if err != nil {
			handleSCIMError(w, err)
			return
		}
		writeSCIM(w, http.StatusOK, scimResource(*item))
	}
}

func handleSCIMDeleteUser(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		current, err := deps.Workspace.GetSCIMUser(r.Context(), r.PathValue("workspaceID"), r.PathValue("id"))
		if err != nil {
			handleSCIMError(w, err)
			return
		}
		active := false
		item, _, err := deps.Workspace.ProvisionSCIMUser(r.Context(), r.PathValue("workspaceID"), actorFromRequest(r).MemberID, workspace.SCIMProvisionInput{ExternalID: current.ExternalID, UserName: current.UserName, DisplayName: current.DisplayName, Active: &active})
		if err != nil {
			handleSCIMError(w, err)
			return
		}
		if err := deps.Auth.RevokeAllCredentials(r.Context(), current.UserID); err != nil {
			if deps.Logger != nil {
				deps.Logger.Error("SCIM member credentials could not be revoked", "workspace_id", r.PathValue("workspaceID"), "member_id", current.ID, "error", err)
			}
			scimError(w, http.StatusInternalServerError, "serverError", "The member was deactivated but credentials could not be revoked; retry the request.")
			return
		}
		if err := deps.APIKeys.RevokeAllForMember(r.Context(), r.PathValue("workspaceID"), current.ID); err != nil {
			if deps.Logger != nil {
				deps.Logger.Error("SCIM member API keys could not be revoked", "workspace_id", r.PathValue("workspaceID"), "member_id", current.ID, "error", err)
			}
			scimError(w, http.StatusInternalServerError, "serverError", "The member was deactivated but API keys could not be revoked; retry the request.")
			return
		}
		_ = item
		w.WriteHeader(http.StatusNoContent)
	}
}

type scimUserResource struct {
	Schemas     []string `json:"schemas"`
	ID          string   `json:"id"`
	ExternalID  string   `json:"externalId,omitempty"`
	UserName    string   `json:"userName"`
	DisplayName string   `json:"displayName"`
	Active      bool     `json:"active"`
	Roles       []struct {
		Value string `json:"value"`
	} `json:"roles,omitempty"`
}

func scimResource(item workspace.SCIMUser) scimUserResource {
	return scimUserResource{Schemas: []string{scimUserSchema}, ID: item.ID, ExternalID: item.ExternalID, UserName: item.UserName, DisplayName: item.DisplayName, Active: item.Active, Roles: []struct {
		Value string `json:"value"`
	}{{Value: item.Role}}}
}

func parseSCIMFilter(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", nil
	}
	parts := strings.SplitN(value, " ", 3)
	if len(parts) != 3 || !strings.EqualFold(parts[1], "eq") {
		return "", "", errors.New("only userName eq or externalId eq filters are supported")
	}
	needle := strings.TrimSpace(parts[2])
	if len(needle) < 2 || needle[0] != '"' || needle[len(needle)-1] != '"' {
		return "", "", errors.New("SCIM filter value must be quoted")
	}
	needle = needle[1 : len(needle)-1]
	switch strings.ToLower(parts[0]) {
	case "username":
		return strings.ToLower(needle), "", nil
	case "externalid":
		return "", needle, nil
	default:
		return "", "", errors.New("only userName and externalId filters are supported")
	}
}

func scimPageParams(r *http.Request) (int, int, error) {
	start, count := 1, 100
	var err error
	if raw := r.URL.Query().Get("startIndex"); raw != "" {
		start, err = strconv.Atoi(raw)
		if err != nil || start < 1 {
			return 0, 0, errors.New("startIndex must be a positive integer")
		}
	}
	if raw := r.URL.Query().Get("count"); raw != "" {
		count, err = strconv.Atoi(raw)
		if err != nil || count < 1 || count > 200 {
			return 0, 0, errors.New("count must be between 1 and 200")
		}
	}
	return start, count, nil
}

func handleSCIMError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workspace.ErrSCIMUserNotFound):
		scimError(w, http.StatusNotFound, "", "SCIM user not found.")
	case errors.Is(err, workspace.ErrSCIMExternalIDConflict):
		scimError(w, http.StatusConflict, "uniqueness", err.Error())
	case errors.Is(err, workspace.ErrSCIMInvalidUserName), errors.Is(err, workspace.ErrInvalidRole):
		scimError(w, http.StatusBadRequest, "invalidValue", err.Error())
	case errors.Is(err, workspace.ErrSCIMOwnerDeactivation):
		scimError(w, http.StatusConflict, "mutability", err.Error())
	default:
		scimError(w, http.StatusInternalServerError, "serverError", "Could not update the SCIM user.")
	}
}

func writeSCIM(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func scimError(w http.ResponseWriter, status int, scimType, detail string) {
	value := map[string]any{"schemas": []string{scimErrorSchema}, "status": strconv.Itoa(status), "detail": detail}
	if scimType != "" {
		value["scimType"] = scimType
	}
	writeSCIM(w, status, value)
}
