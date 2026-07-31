//go:build integration

package widget_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/config"
	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/inbox"
	"github.com/hubchat/hubchat/internal/widget"
	"github.com/hubchat/hubchat/internal/workspace"
)

var testSecretKey = []byte("integration-test-secret-key-needs-32-bytes!!")

type harness struct {
	Widget       *widget.Service
	Conversation *conversation.Service
	Customer     *customer.Service
}

func newHarness(pool *database.Pool) harness {
	eventLog := events.New(pool)
	auditLog := audit.New(pool)
	inboxSvc := inbox.New(pool, eventLog, auditLog)
	conversationSvc := conversation.New(pool, eventLog, auditLog)
	customerSvc := customer.New(pool, eventLog, auditLog, config.Limits{MaxEventBytes: 32 << 10, MaxAttributesPerCustomer: 100})
	widgetSvc := widget.New(pool, eventLog, auditLog, inboxSvc, conversationSvc, customerSvc, testSecretKey)
	return harness{Widget: widgetSvc, Conversation: conversationSvc, Customer: customerSvc}
}

type seededWorkspace struct {
	WorkspaceID string
	InboxID     string
	MemberID    string
}

func seedWorkspace(t *testing.T, ctx context.Context, pool *database.Pool) seededWorkspace {
	t.Helper()

	wsSvc := workspace.New(pool, events.New(pool), audit.New(pool))

	userID := ids.New(ids.PrefixUser)
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, name, email, password_hash, email_verified_at)
		VALUES ($1, 'Test Owner', $2, 'x', now())
	`, userID, userID+"@example.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	token := ids.New("t")
	ws, err := wsSvc.Bootstrap(ctx, userID, "Acme", "acme-"+token[len(token)-10:])
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	actor, err := wsSvc.ActorForUser(ctx, ws.ID, userID)
	if err != nil {
		t.Fatalf("resolve owner actor: %v", err)
	}

	var inboxID string
	if err := pool.QueryRow(ctx, `SELECT id FROM inboxes WHERE workspace_id = $1 LIMIT 1`, ws.ID).Scan(&inboxID); err != nil {
		t.Fatalf("find default inbox: %v", err)
	}
	return seededWorkspace{WorkspaceID: ws.ID, InboxID: inboxID, MemberID: actor.MemberID}
}

func signIdentityToken(t *testing.T, workspaceID, subject, email, name string, expiresIn time.Duration) string {
	t.Helper()

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, err := json.Marshal(map[string]any{
		"sub": subject, "iss": "test-integration", "email": email, "name": name,
		"exp": time.Now().Add(expiresIn).Unix(), "nonce": ids.New("n"),
	})
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	payloadEncoded := base64.RawURLEncoding.EncodeToString(payload)

	// Mirrors identityKeyForWorkspace's private derivation exactly — this is
	// what a workspace's own backend would compute from the secret Hubchat
	// hands it via IdentitySecret, proving the two sides actually agree.
	mac := hmac.New(sha256.New, testSecretKey)
	mac.Write([]byte("identity-token:" + workspaceID))
	key := mac.Sum(nil)

	signingInput := header + "." + payloadEncoded
	sigMac := hmac.New(sha256.New, key)
	sigMac.Write([]byte(signingInput))
	sig := base64.RawURLEncoding.EncodeToString(sigMac.Sum(nil))

	return signingInput + "." + sig
}

func TestWidgetCreateHasDefaults(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	h := newHarness(pool)
	ws := seedWorkspace(t, ctx, pool)

	w, err := h.Widget.Create(ctx, ws.WorkspaceID, ws.MemberID, "Support widget", &ws.InboxID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if w.Version != 1 || !w.Enabled || w.RolloutPercent != 100 || len(w.Modes) == 0 {
		t.Fatalf("unexpected defaults: %+v", w)
	}
	if w.PublicKey == "" {
		t.Fatalf("expected a public key to be generated")
	}
}

func TestWidgetUpdateVersionsAndRollback(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	h := newHarness(pool)
	ws := seedWorkspace(t, ctx, pool)

	w, err := h.Widget.Create(ctx, ws.WorkspaceID, ws.MemberID, "Support widget", &ws.InboxID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	appearance := map[string]any{"accent": "#ff0000"}
	updated, err := h.Widget.Update(ctx, ws.WorkspaceID, ws.MemberID, w.ID, widget.UpdateInput{
		Name: "Renamed", InboxID: &ws.InboxID, Modes: []string{"chat", "knowledge_base"},
		Appearance: appearance, Content: map[string]any{}, Behavior: map[string]any{},
		Environment: "production", RolloutPercent: 100, Enabled: true,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Version != 2 || updated.Appearance["accent"] != "#ff0000" {
		t.Fatalf("expected v2 with new accent, got %+v", updated)
	}

	versions, err := h.Widget.Versions(ctx, ws.WorkspaceID, w.ID)
	if err != nil {
		t.Fatalf("versions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions))
	}

	rolledBack, err := h.Widget.Rollback(ctx, ws.WorkspaceID, ws.MemberID, w.ID, 1)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if rolledBack.Version != 3 {
		t.Fatalf("rollback should create a new version, got v%d", rolledBack.Version)
	}
	if rolledBack.Appearance["accent"] != "#3B6EF6" {
		t.Fatalf("expected v1's default accent restored, got %+v", rolledBack.Appearance["accent"])
	}
}

func TestWidgetDomainAllowlistRejectsBareWildcard(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	h := newHarness(pool)
	ws := seedWorkspace(t, ctx, pool)
	w, _ := h.Widget.Create(ctx, ws.WorkspaceID, ws.MemberID, "Support widget", &ws.InboxID)

	if _, err := h.Widget.AddDomain(ctx, ws.WorkspaceID, ws.MemberID, w.ID, "*"); !errors.Is(err, widget.ErrWildcardDomain) {
		t.Fatalf("expected ErrWildcardDomain, got %v", err)
	}
}

func TestResolveConfigEnforcesOriginAllowlist(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	h := newHarness(pool)
	ws := seedWorkspace(t, ctx, pool)
	w, _ := h.Widget.Create(ctx, ws.WorkspaceID, ws.MemberID, "Support widget", &ws.InboxID)

	// No domains allowlisted yet — every origin is denied by default (fail
	// closed), including one that would later be allowed.
	if _, err := h.Widget.ResolveConfig(ctx, w.PublicKey, "https://shop.example.com/cart", ""); !errors.Is(err, widget.ErrOriginNotAllowed) {
		t.Fatalf("expected denial with no domains allowlisted, got %v", err)
	}

	if _, err := h.Widget.AddDomain(ctx, ws.WorkspaceID, ws.MemberID, w.ID, "*.example.com"); err != nil {
		t.Fatalf("add domain: %v", err)
	}

	config, err := h.Widget.ResolveConfig(ctx, w.PublicKey, "https://shop.example.com/cart", "")
	if err != nil {
		t.Fatalf("expected the subdomain wildcard to allow this origin: %v", err)
	}
	if !config.Enabled {
		t.Fatalf("expected an enabled config, got %+v", config)
	}
	if !config.Online {
		t.Fatalf("expected the bootstrap owner (presence=online, accepting_conversations=true) to read as online")
	}

	// The apex itself needs its own entry — a subdomain wildcard does not
	// implicitly cover it.
	if _, err := h.Widget.ResolveConfig(ctx, w.PublicKey, "https://example.com/", ""); !errors.Is(err, widget.ErrOriginNotAllowed) {
		t.Fatalf("expected the apex to still be denied, got %v", err)
	}

	// A different, unrelated origin is denied outright.
	if _, err := h.Widget.ResolveConfig(ctx, w.PublicKey, "https://evil.test/", ""); !errors.Is(err, widget.ErrOriginNotAllowed) {
		t.Fatalf("expected an unrelated origin to be denied, got %v", err)
	}
}

func TestVisitorConversationRoundTrip(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	h := newHarness(pool)
	ws := seedWorkspace(t, ctx, pool)
	w, _ := h.Widget.Create(ctx, ws.WorkspaceID, ws.MemberID, "Support widget", &ws.InboxID)
	if _, err := h.Widget.AddDomain(ctx, ws.WorkspaceID, ws.MemberID, w.ID, "example.com"); err != nil {
		t.Fatalf("add domain: %v", err)
	}

	token, visitor, err := h.Widget.IssueVisitor(ctx, ws.WorkspaceID)
	if err != nil {
		t.Fatalf("issue visitor: %v", err)
	}

	conv, msg, err := h.Widget.StartConversation(ctx, ws.WorkspaceID, w, visitor, "Hi, I need help")
	if err != nil {
		t.Fatalf("start conversation: %v", err)
	}
	if msg.AuthorType != "customer" {
		t.Fatalf("expected an anonymous visitor's message to still be authored as 'customer', got %q", msg.AuthorType)
	}
	if conv.VisitorID == nil || *conv.VisitorID != visitor.ID {
		t.Fatalf("expected the conversation to carry the visitor id, got %+v", conv.VisitorID)
	}
	if conv.CustomerID != nil {
		t.Fatalf("expected no customer id yet for an unidentified visitor, got %v", *conv.CustomerID)
	}

	if _, err := h.Widget.PostMessage(ctx, ws.WorkspaceID, conv.ID, visitor, "Any update?"); err != nil {
		t.Fatalf("post message: %v", err)
	}

	// A different visitor must not be able to read or post into this
	// conversation — the token, not the conversation id, is the credential.
	_, otherVisitor, err := h.Widget.IssueVisitor(ctx, ws.WorkspaceID)
	if err != nil {
		t.Fatalf("issue other visitor: %v", err)
	}
	if _, err := h.Widget.PostMessage(ctx, ws.WorkspaceID, conv.ID, otherVisitor, "sneaky"); !errors.Is(err, widget.ErrConversationOwner) {
		t.Fatalf("expected ErrConversationOwner for a different visitor, got %v", err)
	}

	msgs, err := h.Widget.Messages(ctx, ws.WorkspaceID, conv.ID, visitor, 0)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	// Resolving the same token again returns the same visitor identity.
	resolved, err := h.Widget.ResolveVisitor(ctx, ws.WorkspaceID, token)
	if err != nil {
		t.Fatalf("resolve visitor: %v", err)
	}
	if resolved.ID != visitor.ID {
		t.Fatalf("expected the same visitor id on re-resolve, got %s vs %s", resolved.ID, visitor.ID)
	}
}

func TestIdentifyUnsignedCreatesUnverifiedCustomer(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	h := newHarness(pool)
	ws := seedWorkspace(t, ctx, pool)

	_, visitor, err := h.Widget.IssueVisitor(ctx, ws.WorkspaceID)
	if err != nil {
		t.Fatalf("issue visitor: %v", err)
	}

	name, email := "Ada Lovelace", "ada@example.com"
	cust, err := h.Widget.Identify(ctx, ws.WorkspaceID, visitor, widget.IdentifyInput{Name: &name, Email: &email})
	if err != nil {
		t.Fatalf("identify: %v", err)
	}
	if cust.Verification != "unverified" {
		t.Fatalf("expected an unsigned identify() to leave verification as 'unverified', got %q", cust.Verification)
	}
	if visitor.CustomerID == nil || *visitor.CustomerID != cust.ID {
		t.Fatalf("expected the visitor to now be linked to the created customer")
	}
}

func TestIdentifyAppliesOnlySchemaAllowedSDKAttributes(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	h := newHarness(pool)
	ws := seedWorkspace(t, ctx, pool)
	if _, err := h.Customer.CreateAttributeDefinition(ctx, ws.WorkspaceID, "customer", "plan", "string", customer.AttributeDefinitionInput{
		Label: "Plan", AllowedSources: []string{"js_sdk"},
	}); err != nil {
		t.Fatalf("create SDK attribute definition: %v", err)
	}

	_, visitor, err := h.Widget.IssueVisitor(ctx, ws.WorkspaceID)
	if err != nil {
		t.Fatalf("issue visitor: %v", err)
	}
	cust, err := h.Widget.Identify(ctx, ws.WorkspaceID, visitor, widget.IdentifyInput{
		Attributes: map[string]any{"plan": "pro"},
	})
	if err != nil {
		t.Fatalf("identify with allowed SDK attribute: %v", err)
	}
	reloaded, err := h.Customer.Get(ctx, ws.WorkspaceID, cust.ID)
	if err != nil {
		t.Fatalf("reload customer: %v", err)
	}
	if reloaded.Attributes["plan"] != "pro" {
		t.Fatalf("expected plan attribute to persist, got %v", reloaded.Attributes)
	}

	if _, err := h.Customer.CreateAttributeDefinition(ctx, ws.WorkspaceID, "customer", "internal_tier", "string", customer.AttributeDefinitionInput{
		Label: "Internal tier", AllowedSources: []string{"rest_api"},
	}); err != nil {
		t.Fatalf("create restricted attribute definition: %v", err)
	}
	if _, err := h.Widget.Identify(ctx, ws.WorkspaceID, visitor, widget.IdentifyInput{
		Attributes: map[string]any{"internal_tier": "gold"},
	}); !errors.Is(err, customer.ErrAttrSourceNotAllowed) {
		t.Fatalf("expected js_sdk source rejection, got %v", err)
	}
}

func TestIdentifySignedTokenVerifiesAndMatchesByExternalID(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	h := newHarness(pool)
	ws := seedWorkspace(t, ctx, pool)

	// Establish a verified customer via a signed token from one visitor
	// session…
	token1 := signIdentityToken(t, ws.WorkspaceID, "ext-42", "grace@example.com", "Grace Hopper", time.Hour)
	_, visitor1, err := h.Widget.IssueVisitor(ctx, ws.WorkspaceID)
	if err != nil {
		t.Fatalf("issue visitor: %v", err)
	}
	first, err := h.Widget.Identify(ctx, ws.WorkspaceID, visitor1, widget.IdentifyInput{SignedToken: &token1})
	if err != nil {
		t.Fatalf("identify with signed token: %v", err)
	}
	if first.Verification != "verified" {
		t.Fatalf("expected a signed token to verify the customer, got %q", first.Verification)
	}

	// …and confirm a second, independent visitor presenting a token for the
	// same external id resolves to the *same* customer record rather than
	// creating a duplicate.
	token2 := signIdentityToken(t, ws.WorkspaceID, "ext-42", "grace@example.com", "Grace Hopper", time.Hour)
	_, visitor2, err := h.Widget.IssueVisitor(ctx, ws.WorkspaceID)
	if err != nil {
		t.Fatalf("issue second visitor: %v", err)
	}
	second, err := h.Widget.Identify(ctx, ws.WorkspaceID, visitor2, widget.IdentifyInput{SignedToken: &token2})
	if err != nil {
		t.Fatalf("identify second visitor: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected the same customer for the same external id, got %s vs %s", second.ID, first.ID)
	}

	// A tampered signature must be rejected outright.
	tampered := token1[:len(token1)-4] + "AAAA"
	if _, err := h.Widget.Identify(ctx, ws.WorkspaceID, visitor1, widget.IdentifyInput{SignedToken: &tampered}); !errors.Is(err, widget.ErrIdentityTokenInvalid) {
		t.Fatalf("expected a tampered token to be rejected, got %v", err)
	}

	// An expired token is rejected even with a valid signature.
	expired := signIdentityToken(t, ws.WorkspaceID, "ext-42", "grace@example.com", "Grace Hopper", -time.Hour)
	if _, err := h.Widget.Identify(ctx, ws.WorkspaceID, visitor1, widget.IdentifyInput{SignedToken: &expired}); !errors.Is(err, widget.ErrIdentityTokenExpired) {
		t.Fatalf("expected an expired token to be rejected, got %v", err)
	}
}

func TestVisitorMessagesNeverIncludeInternalNotes(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	h := newHarness(pool)
	ws := seedWorkspace(t, ctx, pool)
	w, _ := h.Widget.Create(ctx, ws.WorkspaceID, ws.MemberID, "Support widget", &ws.InboxID)
	if _, err := h.Widget.AddDomain(ctx, ws.WorkspaceID, ws.MemberID, w.ID, "example.com"); err != nil {
		t.Fatalf("add domain: %v", err)
	}

	_, visitor, err := h.Widget.IssueVisitor(ctx, ws.WorkspaceID)
	if err != nil {
		t.Fatalf("issue visitor: %v", err)
	}
	conv, _, err := h.Widget.StartConversation(ctx, ws.WorkspaceID, w, visitor, "Hello")
	if err != nil {
		t.Fatalf("start conversation: %v", err)
	}

	// An agent writes an internal note assuming it is invisible to the
	// customer — the whole point of this test.
	if _, err := h.Conversation.PostMessage(ctx, ws.WorkspaceID, conv.ID, nil, "note", "agent", &ws.MemberID, "Agent", "This customer seems upset, escalate carefully"); err != nil {
		t.Fatalf("post note: %v", err)
	}
	if _, err := h.Conversation.PostMessage(ctx, ws.WorkspaceID, conv.ID, nil, "reply", "agent", &ws.MemberID, "Agent", "Thanks for reaching out!"); err != nil {
		t.Fatalf("post reply: %v", err)
	}

	msgs, err := h.Widget.Messages(ctx, ws.WorkspaceID, conv.ID, visitor, 0)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	for _, m := range msgs {
		if m.Kind == "note" {
			t.Fatalf("internal note leaked to the visitor-facing message list: %+v", m)
		}
	}
	// The opening message plus the visible reply — the note is excluded, not
	// silently swallowing everything.
	if len(msgs) != 2 {
		t.Fatalf("expected 2 visible messages (opening + reply), got %d: %+v", len(msgs), msgs)
	}
}

func TestWidgetTenantIsolation(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	h := newHarness(pool)
	wsA := seedWorkspace(t, ctx, pool)
	wsB := seedWorkspace(t, ctx, pool)

	w, err := h.Widget.Create(ctx, wsA.WorkspaceID, wsA.MemberID, "A's widget", &wsA.InboxID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := h.Widget.Get(ctx, wsB.WorkspaceID, w.ID); !errors.Is(err, widget.ErrNotFound) {
		t.Fatalf("expected ErrNotFound reading workspace A's widget as workspace B, got %v", err)
	}
	if err := h.Widget.Delete(ctx, wsB.WorkspaceID, wsB.MemberID, w.ID); !errors.Is(err, widget.ErrNotFound) {
		t.Fatalf("expected ErrNotFound deleting workspace A's widget as workspace B, got %v", err)
	}
}
