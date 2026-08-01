package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/apikey"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
)

func registerAPIKeyRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/api-keys", requireCapability(deps, authorization.IntegrationManage, handleListAPIKeys(deps)))
	mux.HandleFunc("POST /v1/api-keys", requireCapability(deps, authorization.IntegrationManage, Idempotency(deps)(handleCreateAPIKey(deps))))
	mux.HandleFunc("DELETE /v1/api-keys/{id}", requireCapability(deps, authorization.IntegrationManage, Idempotency(deps)(handleRevokeAPIKey(deps))))
	mux.HandleFunc("POST /v1/api-keys/{id}/rotate", requireCapability(deps, authorization.IntegrationManage, Idempotency(deps)(handleRotateAPIKey(deps))))
}

type apiKeyRequest struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresAt string   `json:"expires_at"`
}

func handleListAPIKeys(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}
		actor := actorFromRequest(r)
		keys, err := deps.APIKeys.ListPage(r.Context(), actor.WorkspaceID, cursor.At, cursor.ID, limit+1)
		if err != nil {
			writeAPIKeyInternalError(w, r)
			return
		}
		keyPage := NewPage(keys, limit, func(key apikey.Key) Cursor {
			return Cursor{At: key.CreatedAt, ID: key.ID}
		})
		out := make([]map[string]any, 0, len(keyPage.Data))
		for _, key := range keyPage.Data {
			out = append(out, apiKeyJSON(key))
		}
		httpserver.WriteJSON(w, http.StatusOK, Page[map[string]any]{Data: out, NextCursor: keyPage.NextCursor, HasMore: keyPage.HasMore})
	}
}

func handleCreateAPIKey(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req apiKeyRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		expiresAt, err := parseAPIKeyExpiry(req.ExpiresAt)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, "expires_at must be a future RFC3339 timestamp or date.")
			return
		}
		if err := validateAPIKeyScopes(actor, req.Scopes); err != nil {
			httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeForbidden, err.Error())
			return
		}
		created, err := deps.APIKeys.Create(r.Context(), actor.WorkspaceID, actor.MemberID, req.Name, req.Scopes, expiresAt)
		if err != nil {
			writeAPIKeyError(w, r, err)
			return
		}
		response := apiKeyJSON(created.Key)
		response["token"] = created.Token
		httpserver.WriteJSON(w, http.StatusCreated, response)
	}
}

func handleRevokeAPIKey(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if err := deps.APIKeys.Revoke(r.Context(), actor.WorkspaceID, r.PathValue("id")); err != nil {
			writeAPIKeyError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleRotateAPIKey(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		old, err := deps.APIKeys.Get(r.Context(), actor.WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeAPIKeyError(w, r, err)
			return
		}
		var req apiKeyRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		if strings.TrimSpace(req.Name) == "" {
			req.Name = old.Name
		}
		if len(req.Scopes) == 0 {
			req.Scopes = old.Scopes
		}
		expiresAt, expiryErr := parseAPIKeyExpiry(req.ExpiresAt)
		if expiryErr != nil && req.ExpiresAt != "" {
			httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, "expires_at must be a future RFC3339 timestamp or date.")
			return
		}
		if expiryErr != nil && old.ExpiresAt != nil {
			expiresAt = old.ExpiresAt
		}
		if err := validateAPIKeyScopes(actor, req.Scopes); err != nil {
			httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeForbidden, err.Error())
			return
		}
		created, err := deps.APIKeys.Rotate(r.Context(), actor.WorkspaceID, actor.MemberID, old.ID, req.Name, req.Scopes, expiresAt)
		if err != nil {
			writeAPIKeyError(w, r, err)
			return
		}
		response := apiKeyJSON(created.Key)
		response["token"] = created.Token
		httpserver.WriteJSON(w, http.StatusCreated, response)
	}
}

func apiKeyJSON(key apikey.Key) map[string]any {
	return map[string]any{
		"id": key.ID, "workspace_id": key.WorkspaceID, "name": key.Name, "prefix": key.Prefix,
		"scopes": key.Scopes, "last_used_at": key.LastUsedAt, "expires_at": key.ExpiresAt,
		"created_by": key.CreatedBy, "revoked_at": key.RevokedAt, "created_at": key.CreatedAt,
	}
}

func validateAPIKeyScopes(actor *authorization.Actor, scopes []string) error {
	known := make(map[string]struct{})
	for _, name := range authorization.AllCapabilityNames() {
		known[name] = struct{}{}
	}
	for _, scope := range scopes {
		if _, ok := known[scope]; !ok {
			return errors.New("unknown API key scope: " + scope)
		}
		if !actor.Can(authorization.Capability(scope)) {
			return errors.New("you cannot grant the " + scope + " scope")
		}
	}
	return nil
}

func parseAPIKeyExpiry(value string) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		parsed, err = time.Parse("2006-01-02", value)
	}
	if err != nil || !parsed.After(time.Now()) {
		return nil, errors.New("invalid expiry")
	}
	return &parsed, nil
}

func writeAPIKeyError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, apikey.ErrNotFound) {
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "API key not found.")
		return
	}
	if errors.Is(err, apikey.ErrInvalidName) {
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
		return
	}
	if errors.Is(err, apikey.ErrRevoked) {
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, err.Error())
		return
	}
	writeAPIKeyInternalError(w, r)
}

func writeAPIKeyInternalError(w http.ResponseWriter, r *http.Request) {
	httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not update API keys.")
}
