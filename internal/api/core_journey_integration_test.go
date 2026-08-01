//go:build integration

package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/config"
	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	filemodule "github.com/hubchat/hubchat/internal/file"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/portal"
	"github.com/hubchat/hubchat/internal/ticket"
	"github.com/hubchat/hubchat/internal/workspace"
)

// TestCoreSupportPortalJourney exercises the cross-module support path as one
// customer would experience it. Module tests prove local invariants; this
// journey proves that their workspace, identity, conversation, ticket, portal
// session, and retry contracts still compose on a real PostgreSQL schema.
func TestCoreSupportPortalJourney(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, name, email, password_hash, email_verified_at)
		VALUES ('usr_core_journey', 'Journey Owner', 'journey-owner@example.com', 'x', now())
	`); err != nil {
		t.Fatal(err)
	}

	eventLog := events.New(pool)
	auditLog := audit.New(pool)
	workspaceService := workspace.New(pool, eventLog, auditLog)
	createdWorkspace, err := workspaceService.Bootstrap(ctx, "usr_core_journey", "Journey workspace", "core-journey")
	if err != nil {
		t.Fatalf("bootstrap workspace: %v", err)
	}

	var inboxID string
	if err := pool.QueryRow(ctx, `SELECT id FROM inboxes WHERE workspace_id=$1`, createdWorkspace.ID).Scan(&inboxID); err != nil {
		t.Fatalf("load default inbox: %v", err)
	}

	customerService := customer.New(pool, eventLog, auditLog, config.Default().Limits)
	customerName := "Journey Customer"
	customerEmail := "journey-customer@example.com"
	createdCustomer, err := customerService.Identify(ctx, createdWorkspace.ID, nil, &customerName, &customerEmail, nil, true)
	if err != nil {
		t.Fatalf("identify customer: %v", err)
	}

	portalService := portal.New(pool, portal.Options{SessionLifetime: time.Hour})
	createdPortal, err := portalService.Create(ctx, createdWorkspace.ID, portal.CreateRequest{
		Name: "Customer help", Subdomain: "core-journey", DefaultInboxID: &inboxID,
	})
	if err != nil {
		t.Fatalf("create portal: %v", err)
	}
	magicLink, err := portalService.IssueMagicLink(ctx, createdPortal.ID, customerEmail)
	if err != nil {
		t.Fatalf("issue portal magic link: %v", err)
	}
	portalSession, err := portalService.RedeemMagicLink(ctx, magicLink.Token, "journey-test", "127.0.0.1")
	if err != nil {
		t.Fatalf("redeem portal magic link: %v", err)
	}
	if portalSession.CustomerID != createdCustomer.ID || portalSession.WorkspaceID != createdWorkspace.ID {
		t.Fatalf("portal session = customer %q workspace %q, want %q and %q", portalSession.CustomerID, portalSession.WorkspaceID, createdCustomer.ID, createdWorkspace.ID)
	}

	conversationService := conversation.New(pool, eventLog, auditLog)
	ticketService := ticket.New(pool, workspaceService, eventLog, auditLog)
	localStore, err := filemodule.NewLocalStore(t.TempDir(), 1<<20, []string{"text/plain", "application/octet-stream"})
	if err != nil {
		t.Fatalf("create journey file store: %v", err)
	}
	fileService := filemodule.New(pool, localStore)
	deps := Deps{
		Pool:         pool,
		Conversation: conversationService,
		File:         fileService,
		Customer:     customerService,
		Portal:       portalService,
		Ticket:       ticketService,
	}

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/portal/tickets?portal="+createdPortal.ID,
		strings.NewReader(`{"title":"Cannot sign in","description":"The sign-in button does not respond.","priority":"high"}`))
	createRequest.AddCookie(&http.Cookie{Name: httpserver.PortalSessionCookieName, Value: portalSession.Token})
	createResponse := httptest.NewRecorder()
	handlePortalCreateTicket(deps)(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("portal ticket creation status = %d: %s", createResponse.Code, createResponse.Body.String())
	}

	var created map[string]json.RawMessage
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil {
		t.Fatalf("decode portal ticket creation: %v", err)
	}
	var createdTicket portalTicketResponse
	if err := json.Unmarshal(created["ticket"], &createdTicket); err != nil {
		t.Fatalf("decode created ticket: %v", err)
	}
	if createdTicket.ID == "" || createdTicket.ConversationID == "" {
		t.Fatalf("created ticket did not preserve conversation link: %+v", createdTicket)
	}

	agentName := "Journey Agent"
	if _, err := conversationService.PostMessage(ctx, createdWorkspace.ID, createdTicket.ConversationID,
		nil, "reply", "agent", nil, agentName, "I am checking the sign-in flow now."); err != nil {
		t.Fatalf("agent reply: %v", err)
	}

	var upload bytes.Buffer
	multipartWriter := multipart.NewWriter(&upload)
	part, err := multipartWriter.CreateFormFile("file", "sign-in-error.txt")
	if err != nil {
		t.Fatalf("create journey attachment: %v", err)
	}
	if _, err := io.WriteString(part, "screenshot details"); err != nil {
		t.Fatalf("write journey attachment: %v", err)
	}
	if err := multipartWriter.Close(); err != nil {
		t.Fatalf("close journey attachment: %v", err)
	}
	uploadRequest := httptest.NewRequest(http.MethodPost, "/api/v1/portal/tickets/"+createdTicket.ID+"/files?portal="+createdPortal.ID, &upload)
	uploadRequest.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	uploadRequest.AddCookie(&http.Cookie{Name: httpserver.PortalSessionCookieName, Value: portalSession.Token})
	uploadRequest.SetPathValue("id", createdTicket.ID)
	uploadResponse := httptest.NewRecorder()
	handlePortalTicketFileUpload(deps)(uploadResponse, uploadRequest)
	if uploadResponse.Code != http.StatusCreated {
		t.Fatalf("portal attachment upload status = %d: %s", uploadResponse.Code, uploadResponse.Body.String())
	}
	var uploadedFile struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(uploadResponse.Body).Decode(&uploadedFile); err != nil {
		t.Fatalf("decode portal attachment: %v", err)
	}
	if uploadedFile.ID == "" {
		t.Fatal("portal attachment response did not include an id")
	}

	replyPayload, err := json.Marshal(map[string]any{
		"body":      "The issue is still happening on my side.",
		"client_id": "journey-portal-reply-1",
		"file_ids":  []string{uploadedFile.ID},
	})
	if err != nil {
		t.Fatalf("encode portal reply: %v", err)
	}
	replyBody := string(replyPayload)
	portalReply := func() *httptest.ResponseRecorder {
		replyRequest := httptest.NewRequest(http.MethodPost, "/api/v1/portal/tickets/"+createdTicket.ID+"/replies?portal="+createdPortal.ID, strings.NewReader(replyBody))
		replyRequest.AddCookie(&http.Cookie{Name: httpserver.PortalSessionCookieName, Value: portalSession.Token})
		replyRequest.SetPathValue("id", createdTicket.ID)
		replyResponse := httptest.NewRecorder()
		handlePortalTicketReply(deps)(replyResponse, replyRequest)
		return replyResponse
	}
	firstReply := portalReply()
	secondReply := portalReply()
	if firstReply.Code != http.StatusCreated || secondReply.Code != http.StatusCreated {
		t.Fatalf("portal reply statuses = %d and %d", firstReply.Code, secondReply.Code)
	}
	var firstMessage, secondMessage map[string]any
	if err := json.NewDecoder(firstReply.Body).Decode(&firstMessage); err != nil {
		t.Fatalf("decode first portal reply: %v", err)
	}
	if err := json.NewDecoder(secondReply.Body).Decode(&secondMessage); err != nil {
		t.Fatalf("decode replayed portal reply: %v", err)
	}
	if firstMessage["id"] != secondMessage["id"] {
		t.Fatalf("portal retry created a second message: first=%v second=%v", firstMessage["id"], secondMessage["id"])
	}
	attachments, ok := firstMessage["attachments"].([]any)
	if !ok || len(attachments) != 1 {
		t.Fatalf("portal reply attachments = %v, want one", firstMessage["attachments"])
	}

	downloadRequest := httptest.NewRequest(http.MethodGet, "/api/v1/portal/files/"+uploadedFile.ID+"?portal="+createdPortal.ID, nil)
	downloadRequest.AddCookie(&http.Cookie{Name: httpserver.PortalSessionCookieName, Value: portalSession.Token})
	downloadRequest.SetPathValue("id", uploadedFile.ID)
	downloadResponse := httptest.NewRecorder()
	handlePortalFileDownload(deps)(downloadResponse, downloadRequest)
	if downloadResponse.Code != http.StatusOK || downloadResponse.Body.String() != "screenshot details" {
		t.Fatalf("portal attachment download = status %d body %q", downloadResponse.Code, downloadResponse.Body.String())
	}

	detailRequest := httptest.NewRequest(http.MethodGet, "/api/v1/portal/tickets/"+createdTicket.ID+"?portal="+createdPortal.ID, nil)
	detailRequest.AddCookie(&http.Cookie{Name: httpserver.PortalSessionCookieName, Value: portalSession.Token})
	detailRequest.SetPathValue("id", createdTicket.ID)
	detailResponse := httptest.NewRecorder()
	handlePortalTicket(deps)(detailResponse, detailRequest)
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("portal ticket detail status = %d: %s", detailResponse.Code, detailResponse.Body.String())
	}
	var detail struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.NewDecoder(detailResponse.Body).Decode(&detail); err != nil {
		t.Fatalf("decode portal ticket detail: %v", err)
	}
	if len(detail.Messages) != 3 {
		t.Fatalf("portal ticket message count = %d, want opening message plus agent reply plus one retried customer reply", len(detail.Messages))
	}
	attachmentMessage := false
	for _, message := range detail.Messages {
		if values, ok := message["attachments"].([]any); ok && len(values) == 1 {
			attachmentMessage = true
		}
	}
	if !attachmentMessage {
		t.Fatalf("portal ticket detail did not retain the uploaded attachment: %+v", detail.Messages)
	}
}

type portalTicketResponse struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
}
