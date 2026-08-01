//go:build integration

package emailchannel_test

import (
	"testing"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/emailchannel"
)

func TestListPageUsesAddressCursorAndWorkspaceScope(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES
			('wrk_email_page','Email Page','email-page'),
			('wrk_email_other','Email Other','email-other');
		INSERT INTO inboxes (id,workspace_id,name,slug,is_default) VALUES
			('inb_email_page','wrk_email_page','Support','support',true),
			('inb_email_other','wrk_email_other','Support','support',true);
		INSERT INTO email_mailboxes (id,workspace_id,inbox_id,address,created_at) VALUES
			('mbx_email_a','wrk_email_page','inb_email_page','a@example.com','2026-08-01T10:00:00Z'),
			('mbx_email_b','wrk_email_page','inb_email_page','b@example.com','2026-08-01T11:00:00Z'),
			('mbx_email_other','wrk_email_other','inb_email_other','other@example.com','2026-08-01T09:00:00Z')
	`); err != nil {
		t.Fatal(err)
	}
	service := emailchannel.New(pool, nil, nil, nil, nil)
	first, err := service.ListPage(ctx, "wrk_email_page", "", "", 1)
	if err != nil || len(first) != 1 || first[0].Address != "a@example.com" {
		t.Fatalf("first mailbox page = %#v, err=%v", first, err)
	}
	second, err := service.ListPage(ctx, "wrk_email_page", first[0].Address, first[0].ID, 1)
	if err != nil || len(second) != 1 || second[0].Address != "b@example.com" {
		t.Fatalf("second mailbox page = %#v, err=%v", second, err)
	}
}
