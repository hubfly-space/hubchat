package portability

import (
	"strings"
	"testing"
)

func TestParseCSVImportNormalizesHeadersAndSkipsBlankRows(t *testing.T) {
	rows, err := parseCSVImport([]byte("External ID,Email Address,Name\nuser-1,alice@example.com,Alice\n,,\n"), KindCustomersCSV)
	if err != nil {
		t.Fatalf("parseCSVImport returned error: %v", err)
	}
	if len(rows) != 1 || rows[0].values["external_id"] != "user-1" || rows[0].values["email"] != "alice@example.com" {
		t.Fatalf("unexpected parsed rows: %+v", rows)
	}
}

func TestParseCSVImportRequiresKindSpecificColumns(t *testing.T) {
	if _, err := parseCSVImport([]byte("phone\n+250780000000\n"), KindCustomersCSV); err == nil {
		t.Fatal("customer CSV without an identity column was accepted")
	}
	if _, err := parseCSVImport([]byte("domain\nexample.com\n"), KindCompaniesCSV); err == nil {
		t.Fatal("company CSV without a name column was accepted")
	}
	if _, err := parseCSVImport([]byte("name\nAlice\n"), "unknown"); err == nil || !strings.Contains(err.Error(), "unsupported CSV import kind") {
		t.Fatalf("unexpected unsupported-kind error: %v", err)
	}
}

func TestParseTicketCSVNormalizesHeadersAndRequiresInbox(t *testing.T) {
	rows, err := parseCSVImport([]byte("External ID,Title,Inbox ID,Due At\nticket-1,Login issue,inbox-1,2026-07-31\n"), KindTicketsCSV)
	if err != nil {
		t.Fatalf("parse ticket CSV returned error: %v", err)
	}
	if len(rows) != 1 || rows[0].values["external_id"] != "ticket-1" || rows[0].values["inbox_id"] != "inbox-1" {
		t.Fatalf("unexpected ticket row: %+v", rows)
	}
	if _, err := parseCSVImport([]byte("title\nMissing inbox\n"), KindTicketsCSV); err == nil || !strings.Contains(err.Error(), "inbox_id") {
		t.Fatalf("expected missing inbox error, got %v", err)
	}
}

func TestParseFeedbackCSVRequiresBoardAndTitle(t *testing.T) {
	rows, err := parseCSVImport([]byte("External ID,Board ID,Title,Status\nfeedback-1,board-1,Export request,planned\n"), KindFeedbackCSV)
	if err != nil {
		t.Fatalf("parse feedback CSV returned error: %v", err)
	}
	if len(rows) != 1 || rows[0].values["board_id"] != "board-1" {
		t.Fatalf("unexpected feedback row: %+v", rows)
	}
	if _, err := parseCSVImport([]byte("title\nMissing board\n"), KindFeedbackCSV); err == nil || !strings.Contains(err.Error(), "board_id") {
		t.Fatalf("expected missing board error, got %v", err)
	}
}
