// Package authorization answers whether an actor may perform an action.
//
// # Responsibilities
//
// Resolves a member's effective capability set from their role plus grants,
// and enforces inbox and team scoping.
//
// # Boundary
//
// Every service method that touches tenant data calls into here. §11.3 makes
// this the security boundary; the browser's `can()` is a courtesy, not a
// check.
package authorization

import "context"

// Actor is the authenticated identity behind a request: a signed-in user in
// the scope of one workspace, or nil for anonymous/visitor-token requests
// (handled separately by the widget's own scoped-token verification).
//
// It is deliberately small. Anything more (teams, presence, preferences) is
// looked up from the owning module when needed rather than carried on every
// request — a fat context object is how modules end up coupled to each
// other's shapes.
type Actor struct {
	UserID       string
	MemberID     string
	WorkspaceID  string
	Role         string
	Capabilities map[Capability]bool
}

// Capability is a single, checkable permission (§5.9). The set is closed and
// small on purpose — see the constants below — so "what can an agent do" is
// answerable by reading one file.
type Capability string

const (
	ConversationRead   Capability = "conversation.read"
	ConversationReply  Capability = "conversation.reply"
	ConversationAssign Capability = "conversation.assign"
	ConversationDelete Capability = "conversation.delete"

	CustomerRead          Capability = "customer.read"
	CustomerReadSensitive Capability = "customer.read_sensitive"
	CustomerMerge         Capability = "customer.merge"

	TicketManage        Capability = "ticket.manage"
	WidgetManage        Capability = "widget.manage"
	PortalManage        Capability = "portal.manage"
	KnowledgebaseManage Capability = "knowledgebase.manage"
	FeedbackModerate    Capability = "feedback.moderate"
	AutomationManage    Capability = "automation.manage"
	SLAManage           Capability = "sla.manage"
	IntegrationManage   Capability = "integration.manage"

	ReportRead              Capability = "report.read"
	AuditRead               Capability = "audit.read"
	MemberManage            Capability = "member.manage"
	WorkspaceManage         Capability = "workspace.manage"
	WorkspaceManageSecurity Capability = "workspace.manage_security"
)

// Can reports whether the actor holds capability. The owner role is
// short-circuited rather than enumerated in the database, so adding a new
// capability can never accidentally leave the owner without it (mirrors the
// comment in 0001_accounts_and_tenancy.sql).
func (a *Actor) Can(capability Capability) bool {
	if a == nil {
		return false
	}
	if a.Role == "owner" {
		return true
	}
	return a.Capabilities[capability]
}

type contextKey int

const actorContextKey contextKey = 0

// WithActor attaches the actor to ctx.
func WithActor(ctx context.Context, actor *Actor) context.Context {
	return context.WithValue(ctx, actorContextKey, actor)
}

// FromContext returns the actor attached to ctx, or nil if none.
func FromContext(ctx context.Context) *Actor {
	actor, _ := ctx.Value(actorContextKey).(*Actor)
	return actor
}
