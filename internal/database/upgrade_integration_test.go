//go:build integration

package database_test

import (
	"context"
	"io/fs"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hubchat/hubchat/embedded"
	"github.com/hubchat/hubchat/internal/config"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/jackc/pgx/v5"
)

func TestUpgradePreservesExistingWorkspaceAndSeedsConsumerBoundary(t *testing.T) {
	admin := dbtest.Pool(t)
	ctx := dbtest.Context(t)
	name := "hubchat_upgrade_" + strings.TrimPrefix(ids.New("tmp"), "tmp_")
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Skipf("upgrade fixture needs CREATEDB: %v", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = admin.Exec(cleanup, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	}()
	sourceURL, err := url.Parse(admin.Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	// ConnString can be a keyword DSN; use the public test URL for portability.
	_ = sourceURL
	cfg := config.Default().Database
	cfg.URL = admin.Config().ConnConfig.ConnString()
	cfgURL, err := url.Parse(cfg.URL)
	if err != nil || cfgURL.Scheme == "" {
		t.Skip("upgrade fixture requires a URL DSN")
	}
	cfgURL.Path = "/" + name
	cfg.URL = cfgURL.String()
	pool, err := database.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	migrations, err := embedded.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	old := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() < "0049" {
			body, err := fs.ReadFile(migrations, entry.Name())
			if err != nil {
				t.Fatal(err)
			}
			old[entry.Name()] = &fstest.MapFile{Data: body}
		}
	}
	if _, err := database.Migrate(ctx, pool, old); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces(id,name,slug) VALUES('wrk_upgrade','Upgrade','upgrade');INSERT INTO inboxes(id,workspace_id,name,slug) VALUES('inb_upgrade','wrk_upgrade','Existing inbox','existing');INSERT INTO workspace_event_sequences(workspace_id,next_sequence) VALUES('wrk_upgrade',8)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Migrate(ctx, pool, migrations); err != nil {
		t.Fatal(err)
	}
	var visibility string
	if err := pool.QueryRow(ctx, `SELECT visibility FROM inboxes WHERE id='inb_upgrade'`).Scan(&visibility); err != nil {
		t.Fatal(err)
	}
	if visibility != "workspace" {
		t.Fatal("upgrade silently restricted an existing inbox")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM event_consumer_cursors WHERE workspace_id='wrk_upgrade' AND last_sequence=7`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("consumer baseline count=%d", count)
	}
}
