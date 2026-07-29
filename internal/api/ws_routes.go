package api

import (
	"log/slog"
	"net/http"

	"github.com/coder/websocket"

	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/realtime"
)

// widgetOriginPatterns turns a widget's domain allowlist into the host glob
// patterns coder/websocket's Accept checks the connecting page's Origin
// against (see internal/widget's identical, independently-enforced check in
// WidgetForOrigin — this is defense in depth at the protocol-upgrade layer,
// not the only place it happens).
//
// Each domain gets both a bare form and a "domain:*" form: a browser's
// Origin header always carries an explicit port when the page is served
// from one that is not the scheme's default (http on :5173 in local
// development, for instance), while the REST domain check normalises this
// away by comparing hostnames alone. Accepting either keeps the two checks
// agreeing about what is actually allowed.
func widgetOriginPatterns(domains []string) []string {
	patterns := make([]string, 0, len(domains)*2)
	for _, d := range domains {
		patterns = append(patterns, d, d+":*")
	}
	return patterns
}

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

		actor, err := deps.Workspace.ActorForUser(r.Context(), workspaceID, user.ID)
		if err != nil {
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
		hub.Serve(r.Context(), conn, workspaceID, realtime.AgentGrant(actor.MemberID, user.Name))
	})

	// The visitor path is authorized entirely differently from /conversations
	// above: no session cookie, no workspace membership — a public widget key
	// plus a scoped visitor token, exactly like every other widget-facing
	// endpoint in internal/api/widget_routes.go. It is also the one WebSocket
	// route that must accept a cross-origin upgrade at all, since the caller
	// is JavaScript running on a customer's own site rather than this
	// deployment's own dashboard.
	mux.HandleFunc("GET /visitor", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		widgetRecord, err := deps.Widget.WidgetForOrigin(r.Context(), query.Get("key"), query.Get("url"), r.Header.Get("Origin"))
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		visitor, err := deps.Widget.ResolveVisitor(r.Context(), widgetRecord.WorkspaceID, query.Get("token"))
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		domains, err := deps.Widget.Domains(r.Context(), widgetRecord.WorkspaceID, widgetRecord.ID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		names := make([]string, len(domains))
		for i, d := range domains {
			names[i] = d.Domain
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			CompressionMode: websocket.CompressionDisabled,
			OriginPatterns:  widgetOriginPatterns(names),
		})
		if err != nil {
			deps.Logger.Warn("visitor websocket upgrade failed", slog.Any("error", err))
			return
		}

		conversationIDs, err := deps.Conversation.ConversationIDsForVisitor(r.Context(), widgetRecord.WorkspaceID, visitor.ID)
		if err != nil {
			conversationIDs = nil
		}
		hub.Serve(r.Context(), conn, widgetRecord.WorkspaceID, realtime.VisitorGrant(conversationIDs...))
	})

	return mux
}
