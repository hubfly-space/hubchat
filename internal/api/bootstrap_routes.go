package api

import (
	"net/http"
	"time"

	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/workspace"
)

// registerBootstrapRoutes mounts the dashboard's opening request.
func registerBootstrapRoutes(mux *http.ServeMux, deps Deps) {
	// No capability requirement beyond membership: this is the payload that
	// tells the client what it may do, so gating it on a capability would be
	// circular.
	mux.HandleFunc("GET /v1/bootstrap", requireActor(deps, handleBootstrap(deps)))
}

// bootstrapResponse mirrors the browser's contract types exactly (Workspace,
// Member, Team, Tag, Inbox in @hubchat/shared). Field names are snake_case
// here for the same reason they are there: one shape, defined once, no mapping
// layer in between.
type bootstrapResponse struct {
	Workspace  workspaceJSON        `json:"workspace"`
	Workspaces []workspaceEntryJSON `json:"workspaces"`
	Viewer     viewerJSON           `json:"viewer"`
	Members    []memberJSON         `json:"members"`
	Teams      []teamJSON           `json:"teams"`
	Tags       []tagJSON            `json:"tags"`
	Inboxes    []inboxJSON          `json:"inboxes"`
}

type workspaceJSON struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Slug            string `json:"slug"`
	DefaultLanguage string `json:"default_language"`
	Timezone        string `json:"timezone"`
	TicketPrefix    string `json:"ticket_prefix"`
	CreatedAt       string `json:"created_at"`
}

type workspaceEntryJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	Role string `json:"role"`
}

type viewerJSON struct {
	ID           string   `json:"id"`
	UserID       string   `json:"user_id"`
	Name         string   `json:"name"`
	Email        string   `json:"email"`
	AvatarURL    *string  `json:"avatar_url"`
	Role         string   `json:"role"`
	Capabilities []string `json:"capabilities"`
}

type memberJSON struct {
	ID           string   `json:"id"`
	WorkspaceID  string   `json:"workspace_id"`
	UserID       string   `json:"user_id"`
	Name         string   `json:"name"`
	Email        string   `json:"email"`
	AvatarURL    *string  `json:"avatar_url"`
	Role         string   `json:"role"`
	Capabilities []string `json:"capabilities"`
	Teams        []string `json:"teams"`
	Presence     string   `json:"presence"`
	Accepting    bool     `json:"accepting_conversations"`
	LastSeenAt   *string  `json:"last_seen_at"`
	CreatedAt    string   `json:"created_at"`
}

type teamJSON struct {
	ID              string   `json:"id"`
	WorkspaceID     string   `json:"workspace_id"`
	Name            string   `json:"name"`
	Description     *string  `json:"description"`
	LeadID          *string  `json:"lead_id"`
	MemberIDs       []string `json:"member_ids"`
	InboxIDs        []string `json:"inbox_ids"`
	RoutingStrategy string   `json:"routing_strategy"`
	CreatedAt       string   `json:"created_at"`
}

type tagJSON struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color int    `json:"color"`
}

// inboxJSON mirrors the shared `Inbox` type exactly — same fields, same
// snake_case names — so the browser's contract stays "one shape, defined
// once" whether inboxes arrive here (bootstrap) or from /v1/inboxes.
type inboxJSON struct {
	ID          string   `json:"id"`
	WorkspaceID string   `json:"workspace_id"`
	Name        string   `json:"name"`
	Slug        string   `json:"slug"`
	Description *string  `json:"description"`
	Channels    []string `json:"channels"`
	TeamIDs     []string `json:"team_ids"`
	IsDefault   bool     `json:"is_default"`
	SLAPolicyID *string  `json:"sla_policy_id"`
	OpenCount   int      `json:"open_count"`
	CreatedAt   string   `json:"created_at"`
}

func handleBootstrap(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		data, err := deps.Workspace.LoadBootstrap(r.Context(), actor.WorkspaceID, actor.UserID)
		if err != nil {
			deps.Logger.Error("bootstrap failed",
				"error", err, "request_id", httpserver.RequestIDFrom(r.Context()))
			httpserver.WriteError(w, r, http.StatusInternalServerError,
				httpserver.CodeInternalError, "Could not load this workspace.")
			return
		}

		httpserver.WriteJSON(w, http.StatusOK, buildBootstrapResponse(data))
	}
}

func buildBootstrapResponse(data *workspace.Bootstrap) bootstrapResponse {
	response := bootstrapResponse{
		Workspace: workspaceJSON{
			ID:              data.Workspace.ID,
			Name:            data.Workspace.Name,
			Slug:            data.Workspace.Slug,
			DefaultLanguage: data.Workspace.DefaultLanguage,
			Timezone:        data.Workspace.Timezone,
			TicketPrefix:    data.Workspace.TicketPrefix,
			CreatedAt:       data.Workspace.CreatedAt.UTC().Format(time.RFC3339),
		},
		Viewer: viewerJSON{
			ID:           data.Viewer.MemberID,
			UserID:       data.Viewer.UserID,
			Name:         data.Viewer.Name,
			Email:        data.Viewer.Email,
			AvatarURL:    data.Viewer.AvatarURL,
			Role:         data.Viewer.Role,
			Capabilities: data.Viewer.Capabilities,
		},
		// Slices are initialised so an empty workspace serialises as `[]`
		// rather than `null`, and the browser can map over them unguarded.
		Workspaces: make([]workspaceEntryJSON, 0, len(data.Workspaces)),
		Members:    make([]memberJSON, 0, len(data.Members)),
		Teams:      make([]teamJSON, 0, len(data.Teams)),
		Tags:       make([]tagJSON, 0, len(data.Tags)),
		Inboxes:    make([]inboxJSON, 0, len(data.Inboxes)),
	}

	for _, entry := range data.Workspaces {
		response.Workspaces = append(response.Workspaces, workspaceEntryJSON(entry))
	}

	for _, member := range data.Members {
		response.Members = append(response.Members, memberJSON{
			ID:          member.ID,
			WorkspaceID: data.Workspace.ID,
			UserID:      member.UserID,
			Name:        member.Name,
			Email:       member.Email,
			AvatarURL:   member.AvatarURL,
			Role:        member.Role,
			// Other members' effective capabilities are not the viewer's
			// business: the UI never renders a control on someone else's
			// behalf, and shipping the full permission map of every colleague
			// is more than the interface needs to know.
			Capabilities: []string{},
			Teams:        orEmpty(member.TeamIDs),
			Presence:     member.Presence,
			Accepting:    member.Accepting,
			LastSeenAt:   formatOptionalTime(member.LastSeenAt),
			CreatedAt:    member.CreatedAt.UTC().Format(time.RFC3339),
		})
	}

	for _, team := range data.Teams {
		response.Teams = append(response.Teams, teamJSON{
			ID:              team.ID,
			WorkspaceID:     data.Workspace.ID,
			Name:            team.Name,
			Description:     team.Description,
			LeadID:          team.LeadID,
			MemberIDs:       orEmpty(team.MemberIDs),
			InboxIDs:        []string{},
			RoutingStrategy: team.RoutingStrategy,
			CreatedAt:       team.CreatedAt.UTC().Format(time.RFC3339),
		})
	}

	for _, tag := range data.Tags {
		response.Tags = append(response.Tags, tagJSON{ID: tag.ID, Name: tag.Name, Color: tag.Color})
	}

	for _, inbox := range data.Inboxes {
		response.Inboxes = append(response.Inboxes, inboxJSON{
			ID:          inbox.ID,
			WorkspaceID: inbox.WorkspaceID,
			Name:        inbox.Name,
			Slug:        inbox.Slug,
			Description: inbox.Description,
			Channels:    orEmpty(inbox.Channels),
			TeamIDs:     orEmpty(inbox.TeamIDs),
			IsDefault:   inbox.IsDefault,
			SLAPolicyID: inbox.SLAPolicyID,
			OpenCount:   inbox.OpenCount,
			CreatedAt:   inbox.CreatedAt.UTC().Format(time.RFC3339),
		})
	}

	return response
}

func orEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
