package api

import (
	"log/slog"
	"net/http"

	"github.com/coder/websocket"

	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/realtime"
)

// NewWebSocketHandler builds the /ws/conversations endpoint.
//
// Authorization happens before the protocol upgrade, using the same session
// cookie and workspace resolution as every other endpoint (§11.3 — realtime
// subscriptions are authorized like anything else, not treated as a special
// case). Once upgraded, the connection is handed to the Hub, which knows
// nothing about sessions or cookies at all.
func NewWebSocketHandler(deps Deps, hub *realtime.Hub) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /conversations", func(w http.ResponseWriter, r *http.Request) {
		token := httpserver.SessionToken(r)
		if token == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		user, err := deps.Auth.UserForSession(r.Context(), token)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		workspaceID := r.URL.Query().Get("workspace_id")
		if workspaceID == "" {
			workspaceID, err = deps.Workspace.DefaultWorkspaceID(r.Context(), user.ID)
			if err != nil {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}

		if _, err := deps.Workspace.ActorForUser(r.Context(), workspaceID, user.ID); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			// The dashboard and widget are served from this same origin in
			// production; OriginPatterns stays default (same-origin only)
			// rather than opting into cross-origin upgrades, which nothing here
			// needs.
			CompressionMode: websocket.CompressionDisabled,
		})
		if err != nil {
			deps.Logger.Warn("websocket upgrade failed", slog.Any("error", err))
			return
		}

		// An authenticated member gets the workspace firehose: the inbox list
		// has to react to conversations they are not currently reading. The
		// grant is decided here, at the boundary that verified membership, and
		// the hub only ever narrows it (§11.3).
		hub.Serve(r.Context(), conn, workspaceID, realtime.AgentGrant())
	})

	return mux
}
