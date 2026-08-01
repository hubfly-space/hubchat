//go:build integration

package api

import (
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/ticket"
)

func TestTicketExportWalksAllBoundedPagesAndStaysWorkspaceScoped(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id, name, slug) VALUES
			('wrk_ticket_export_a', 'Ticket Export A', 'ticket-export-a'),
			('wrk_ticket_export_b', 'Ticket Export B', 'ticket-export-b');
		INSERT INTO inboxes (id, workspace_id, name, slug) VALUES
			('inb_ticket_export_a', 'wrk_ticket_export_a', 'Support A', 'support-a'),
			('inb_ticket_export_b', 'wrk_ticket_export_b', 'Support B', 'support-b');
		INSERT INTO tickets (id, workspace_id, number, prefix, title, status, priority, inbox_id, channel, created_at, updated_at)
		SELECT 'tkt_export_a_' || lpad(i::text, 4, '0'), 'wrk_ticket_export_a', i, 'SUP',
		       'Export ticket ' || i, 'open', 'normal', 'inb_ticket_export_a', 'manual',
		       now() - (i || ' seconds')::interval, now() - (i || ' seconds')::interval
		FROM generate_series(1, 205) AS values(i);
		INSERT INTO tickets (id, workspace_id, number, prefix, title, status, priority, inbox_id, channel)
		VALUES ('tkt_export_b_0001', 'wrk_ticket_export_b', 1, 'OTH', 'Other workspace ticket', 'open', 'normal', 'inb_ticket_export_b', 'manual')
	`); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Ticket: ticket.New(pool, nil, nil, nil)}
	actor := &authorization.Actor{WorkspaceID: "wrk_ticket_export_a", Role: "owner"}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/tickets/export", nil)
	request = request.WithContext(authorization.WithActor(request.Context(), actor))
	response := httptest.NewRecorder()
	handleExportTickets(deps)(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("ticket export response = %d %q", response.Code, response.Body.String())
	}

	rows, err := csv.NewReader(strings.NewReader(response.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("parse ticket export: %v", err)
	}
	if len(rows) != 206 {
		t.Fatalf("ticket export rows = %d, want header plus 205 tickets", len(rows))
	}
	if !strings.Contains(response.Body.String(), "Export ticket 1") || !strings.Contains(response.Body.String(), "Export ticket 205") {
		t.Fatalf("ticket export omitted a page boundary: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "Other workspace ticket") {
		t.Fatal("ticket export crossed the workspace boundary")
	}
}
