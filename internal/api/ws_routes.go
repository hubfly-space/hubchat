package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/hubchat/hubchat/internal/auth"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/events"
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
		// Reuse REST's session, membership, capability, and SSO boundary.
		if workspaceID := r.URL.Query().Get("workspace_id"); workspaceID != "" {
			r = r.Clone(r.Context())
			r.Header.Set("Hubchat-Workspace-Id", workspaceID)
		}
		requireCapability(deps, authorization.ConversationRead, func(w http.ResponseWriter, r *http.Request) {
			actor := actorFromRequest(r)
			workspaceID := actor.WorkspaceID
			token := httpserver.SessionToken(r)

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

			hub.Serve(r.Context(), conn, workspaceID, agentRealtimeGrant(r.Context(), deps, token, actor))
		}).ServeHTTP(w, r)
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

// Resolve the actor again at delivery time: an open socket is not a permanent
// authorization token. This applies equally to resume and live events.
func agentRealtimeGrant(ctx context.Context, deps Deps, token string, initial *authorization.Actor) realtime.Grant {
	resolve := func() *authorization.Actor {
		checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		user, err := deps.Auth.UserForSession(checkCtx, token)
		if err != nil {
			return nil
		}
		actor, err := deps.Workspace.ActorForUser(checkCtx, initial.WorkspaceID, user.ID)
		if err != nil || !actor.Can(authorization.ConversationRead) {
			return nil
		}
		settings, err := deps.Workspace.GetSettings(checkCtx, initial.WorkspaceID)
		if err != nil || (settings.Security.RequireSSO && user.AuthMethod != auth.AuthMethodOAuth) {
			return nil
		}
		return actor
	}
	grant := realtime.AgentGrant(initial.MemberID, "")
	if user, err := deps.Auth.UserForSession(ctx, token); err == nil {
		grant.MemberName = user.Name
	}
	grant.Valid = func() bool { return resolve() != nil }
	grant.AllowTopic = func(topic string) bool {
		actor := resolve()
		if actor == nil {
			return false
		}
		if topic == realtime.TopicWorkspace {
			return true
		}
		kind, id, ok := strings.Cut(topic, ":")
		if !ok || !realtimeEntityAllowed(actor, kind) {
			return false
		}
		return realtimeSubjectAllowed(ctx, deps, actor, kind, id)
	}
	grant.Filter = func(record events.Record) (events.Record, bool) {
		actor := resolve()
		if actor == nil {
			return record, false
		}
		policyChange := record.EntityType == "role" || record.EntityType == "inbox" || record.Type == events.MemberRoleSet || record.Type == events.MemberRemoved || record.Type == events.MemberDeactivated || record.EntityType == "team"
		if policyChange {
			record.ID = ""
			record.EntityType = ""
			record.EntityID = ""
			record.Type = "workspace.visibility_changed"
			record.Data = json.RawMessage(`{}`)
			return record, true
		}
		if !realtimeEntityAllowed(actor, record.EntityType) {
			return record, false
		}
		if !realtimeSubjectAllowed(ctx, deps, actor, record.EntityType, record.EntityID) {
			var changed map[string]any
			if record.Type == events.ConversationUpdated && json.Unmarshal(record.Data, &changed) == nil && changed["inbox_id"] != nil {
				record.ID = ""
				record.EntityType = ""
				record.EntityID = ""
				record.Type = "workspace.visibility_changed"
				record.Data = json.RawMessage(`{}`)
				return record, true
			}
			return record, false
		}
		// Basic readers receive an invalidation, not the raw customer payload.
		if (record.EntityType == "customer" || record.EntityType == "company") && !actor.Can(authorization.CustomerReadSensitive) {
			record.Data = json.RawMessage(`{}`)
		}
		return record, true
	}
	return grant
}

func realtimeEntityAllowed(actor *authorization.Actor, entity string) bool {
	switch entity {
	case "conversation", "saved_view", "inbox":
		return actor.Can(authorization.ConversationRead)
	case "ticket":
		return actor.Can(authorization.TicketManage)
	case "customer", "company":
		return actor.Can(authorization.CustomerRead)
	case "article", "knowledge_base", "collection":
		return actor.Can(authorization.KnowledgebaseManage)
	case "feedback", "feedback_board", "changelog":
		return actor.Can(authorization.FeedbackModerate)
	case "survey":
		return actor.Can(authorization.SurveyManage)
	case "task":
		return actor.Can(authorization.TaskManage)
	case "automation", "rule":
		return actor.Can(authorization.AutomationManage)
	case "webhook", "api_key", "integration":
		return actor.Can(authorization.IntegrationManage)
	case "member", "team", "role", "invite":
		return actor.Can(authorization.MemberManage)
	case "widget":
		return actor.Can(authorization.WidgetManage)
	case "portal":
		return actor.Can(authorization.PortalManage)
	default:
		return actor.Can(authorization.WorkspaceManage)
	}
}

func realtimeSubjectAllowed(ctx context.Context, deps Deps, actor *authorization.Actor, kind, id string) bool {
	if actor.AllowedInboxIDs == nil {
		return true
	}
	ctx = authorization.WithActor(ctx, actor)
	switch kind {
	case "conversation":
		if id == "" || deps.Conversation == nil {
			return false
		}
		_, err := deps.Conversation.Get(ctx, actor.WorkspaceID, id)
		return err == nil
	case "ticket":
		if id == "" || deps.Ticket == nil {
			return false
		}
		_, err := deps.Ticket.Get(ctx, actor.WorkspaceID, id)
		return err == nil
	case "inbox":
		return actor.CanSeeInbox(id)
	default:
		return true
	}
}
