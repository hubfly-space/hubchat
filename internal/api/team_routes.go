package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/workspace"
)

func registerTeamRoutes(mux *http.ServeMux, deps Deps) {
	idempotent := Idempotency(deps)
	mux.HandleFunc("GET /v1/teams",
		requireActor(deps, handleListTeams(deps)))
	mux.HandleFunc("POST /v1/teams",
		requireCapability(deps, authorization.MemberManage, idempotent(handleCreateTeam(deps))))
	mux.HandleFunc("PATCH /v1/teams/{id}",
		requireCapability(deps, authorization.MemberManage, idempotent(handleUpdateTeam(deps))))
	mux.HandleFunc("DELETE /v1/teams/{id}",
		requireCapability(deps, authorization.MemberManage, idempotent(handleDeleteTeam(deps))))
	mux.HandleFunc("PUT /v1/teams/{id}/members/{memberId}",
		requireCapability(deps, authorization.MemberManage, idempotent(handleAddTeamMember(deps))))
	mux.HandleFunc("DELETE /v1/teams/{id}/members/{memberId}",
		requireCapability(deps, authorization.MemberManage, idempotent(handleRemoveTeamMember(deps))))
}

type teamPayload struct {
	Name            string         `json:"name"`
	Description     *string        `json:"description"`
	LeadID          *string        `json:"lead_id"`
	RoutingStrategy string         `json:"routing_strategy"`
	MemberIDs       []string       `json:"member_ids"`
	RoutingConfig   map[string]any `json:"routing_config"`
}

func handleListTeams(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed team cursor.")
			return
		}
		teams, err := deps.Workspace.ListTeamsPage(r.Context(), actor.WorkspaceID, cursor.Value, cursor.ID, limit+1)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load teams.")
			return
		}
		page := NewPage(teams, limit, func(team workspace.Team) Cursor { return Cursor{Value: team.Name, ID: team.ID} })
		httpserver.WriteJSON(w, http.StatusOK, Page[teamJSON]{Data: teamsJSON(actor.WorkspaceID, page.Data), NextCursor: page.NextCursor, HasMore: page.HasMore})
	}
}

func handleCreateTeam(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req teamPayload
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		team, err := deps.Workspace.CreateTeamWithRouting(
			r.Context(), actor.WorkspaceID, actor.MemberID, req.Name,
			req.Description, req.LeadID, req.RoutingStrategy, req.MemberIDs, req.RoutingConfig,
		)
		if err != nil {
			writeTeamError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, teamJSON{
			ID: team.ID, WorkspaceID: actor.WorkspaceID, Name: team.Name,
			Description: team.Description, LeadID: team.LeadID,
			MemberIDs: orEmpty(team.MemberIDs), InboxIDs: []string{},
			RoutingStrategy: team.RoutingStrategy,
			RoutingConfig:   team.RoutingConfig,
			CreatedAt:       team.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
}

func handleUpdateTeam(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req teamPayload
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		team, err := deps.Workspace.UpdateTeamWithRouting(
			r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"),
			req.Name, req.Description, req.LeadID, req.RoutingStrategy, req.RoutingConfig,
		)
		if err != nil {
			writeTeamError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, teamJSON{
			ID: team.ID, WorkspaceID: actor.WorkspaceID, Name: team.Name,
			Description: team.Description, LeadID: team.LeadID,
			MemberIDs: orEmpty(team.MemberIDs), InboxIDs: []string{},
			RoutingStrategy: team.RoutingStrategy,
			RoutingConfig:   team.RoutingConfig,
			CreatedAt:       team.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
}

func handleDeleteTeam(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		if err := deps.Workspace.DeleteTeam(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id")); err != nil {
			writeTeamError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleAddTeamMember(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		err := deps.Workspace.AddTeamMember(r.Context(), actor.WorkspaceID, r.PathValue("id"), r.PathValue("memberId"))
		if err != nil {
			writeTeamError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleRemoveTeamMember(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		err := deps.Workspace.RemoveTeamMember(r.Context(), actor.WorkspaceID, r.PathValue("id"), r.PathValue("memberId"))
		if err != nil {
			writeTeamError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func teamsJSON(workspaceID string, teams []workspace.Team) []teamJSON {
	out := make([]teamJSON, 0, len(teams))
	for _, team := range teams {
		out = append(out, teamJSON{
			ID: team.ID, WorkspaceID: workspaceID, Name: team.Name,
			Description: team.Description, LeadID: team.LeadID,
			MemberIDs: orEmpty(team.MemberIDs), InboxIDs: []string{},
			RoutingStrategy: team.RoutingStrategy,
			RoutingConfig:   team.RoutingConfig,
			CreatedAt:       team.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}

func writeTeamError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, workspace.ErrInvalidTeamName), errors.Is(err, workspace.ErrInvalidRoutingStrategy):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	case errors.Is(err, workspace.ErrTeamNameTaken):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, err.Error())
	case errors.Is(err, workspace.ErrTeamNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "No such team.")
	default:
		httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Something went wrong.")
	}
}
