//go:build integration

package events_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
)

func TestAppendAssignsGaplessSequencePerWorkspace(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	log := events.New(pool)
	workspaceA := seedWorkspace(t, ctx, pool, "alpha")
	workspaceB := seedWorkspace(t, ctx, pool, "beta")

	for i := 1; i <= 3; i++ {
		record := appendOne(t, ctx, pool, log, workspaceA, events.MessageCreated)
		if record.Sequence != int64(i) {
			t.Fatalf("workspace A event %d: got sequence %d, want %d", i, record.Sequence, i)
		}
	}

	// A second workspace starts its own count. Sequences are per-tenant, so a
	// busy workspace must not advance a quiet one's resume cursor.
	first := appendOne(t, ctx, pool, log, workspaceB, events.TicketCreated)
	if first.Sequence != 1 {
		t.Fatalf("workspace B first event: got sequence %d, want 1", first.Sequence)
	}
}

// The property the whole resume design rests on: under concurrency, no two
// events in a workspace share a sequence, and none are skipped. If this fails,
// a reconnecting client silently misses messages.
func TestAppendSerialisesConcurrentWriters(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	log := events.New(pool)
	workspaceID := seedWorkspace(t, ctx, pool, "concurrent")

	const writers = 16
	var wg sync.WaitGroup
	sequences := make([]int64, writers)

	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			record := appendOne(t, ctx, pool, log, workspaceID, events.MessageCreated)
			sequences[i] = record.Sequence
		}()
	}
	wg.Wait()

	seen := make(map[int64]bool, writers)
	for _, sequence := range sequences {
		if seen[sequence] {
			t.Fatalf("sequence %d was assigned twice", sequence)
		}
		seen[sequence] = true
	}

	for want := int64(1); want <= writers; want++ {
		if !seen[want] {
			t.Fatalf("sequence %d was never assigned; the log has a gap", want)
		}
	}
}

// A rolled-back transaction must not burn a sequence *visibly* — more
// precisely, it must not leave a committed event behind. The counter itself is
// allowed to advance, because a gap is harmless to a reader asking for
// "everything after N".
func TestAppendRollsBackWithItsTransaction(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	log := events.New(pool)
	workspaceID := seedWorkspace(t, ctx, pool, "rollback")

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := log.Append(ctx, tx, events.Event{
		WorkspaceID: workspaceID,
		Type:        events.MessageCreated,
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	records, err := log.Since(ctx, workspaceID, 0, 100)
	if err != nil {
		t.Fatalf("since: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("rolled-back append left %d event(s) behind", len(records))
	}
}

func TestSinceReplaysInOrderFromCursor(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	log := events.New(pool)
	workspaceID := seedWorkspace(t, ctx, pool, "replay")

	for range 5 {
		appendOne(t, ctx, pool, log, workspaceID, events.MessageCreated)
	}

	records, err := log.Since(ctx, workspaceID, 2, 100)
	if err != nil {
		t.Fatalf("since: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("resuming after sequence 2: got %d events, want 3", len(records))
	}
	for i, record := range records {
		want := int64(i + 3)
		if record.Sequence != want {
			t.Fatalf("replay position %d: got sequence %d, want %d", i, record.Sequence, want)
		}
	}
}

// §11.3 and §11.6: a workspace predicate is required on every read. Replaying
// with another tenant's id must return nothing, not that tenant's events.
func TestSinceIsScopedToOneWorkspace(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	log := events.New(pool)
	mine := seedWorkspace(t, ctx, pool, "mine")
	theirs := seedWorkspace(t, ctx, pool, "theirs")

	appendOne(t, ctx, pool, log, theirs, events.MessageCreated)

	records, err := log.Since(ctx, mine, 0, 100)
	if err != nil {
		t.Fatalf("since: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("cross-tenant leak: reading workspace %q returned %d event(s) from %q",
			mine, len(records), theirs)
	}
}

func TestGetRefusesCrossTenantIds(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	log := events.New(pool)
	mine := seedWorkspace(t, ctx, pool, "mine")
	theirs := seedWorkspace(t, ctx, pool, "theirs")

	foreign := appendOne(t, ctx, pool, log, theirs, events.TicketCreated)

	// Holding the exact id must still not be enough without membership.
	if _, err := log.Get(ctx, mine, foreign.ID); err == nil {
		t.Fatal("cross-tenant leak: Get returned another workspace's event by id")
	}
}

func TestAppendStoresDataAsJSONObject(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	log := events.New(pool)
	workspaceID := seedWorkspace(t, ctx, pool, "payload")

	type payload struct {
		ConversationID string `json:"conversation_id"`
	}

	var stored *events.Record
	inTx(t, ctx, pool, func(tx pgx.Tx) {
		record, err := log.Append(ctx, tx, events.Event{
			WorkspaceID: workspaceID,
			Type:        events.MessageCreated,
			EntityType:  "conversation",
			EntityID:    "cnv_test",
			Data:        payload{ConversationID: "cnv_test"},
		})
		if err != nil {
			t.Fatalf("append: %v", err)
		}
		stored = record
	})

	records, err := log.Since(ctx, workspaceID, 0, 10)
	if err != nil {
		t.Fatalf("since: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d events, want 1", len(records))
	}

	var decoded payload
	if err := json.Unmarshal(records[0].Data, &decoded); err != nil {
		t.Fatalf("stored data is not the object that was appended: %v", err)
	}
	if decoded.ConversationID != "cnv_test" {
		t.Fatalf("got conversation_id %q, want %q", decoded.ConversationID, "cnv_test")
	}
	if records[0].ID != stored.ID {
		t.Fatalf("replayed id %q does not match appended id %q", records[0].ID, stored.ID)
	}
}

// An event with no payload must still round-trip as `{}` so consumers can
// index into `data` unconditionally.
func TestAppendDefaultsMissingDataToEmptyObject(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	log := events.New(pool)
	workspaceID := seedWorkspace(t, ctx, pool, "nodata")

	appendOne(t, ctx, pool, log, workspaceID, events.PresenceUpdate)

	records, err := log.Since(ctx, workspaceID, 0, 10)
	if err != nil {
		t.Fatalf("since: %v", err)
	}
	if got := string(records[0].Data); got != "{}" {
		t.Fatalf("got data %q, want %q", got, "{}")
	}
}

func TestLatestSequenceReportsTheResumeStartingPoint(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	log := events.New(pool)
	workspaceID := seedWorkspace(t, ctx, pool, "latest")

	// A workspace where nothing has happened starts at 0, so a fresh client
	// subscribes without replaying anything.
	sequence, err := log.LatestSequence(ctx, workspaceID)
	if err != nil {
		t.Fatalf("latest sequence: %v", err)
	}
	if sequence != 0 {
		t.Fatalf("empty workspace: got sequence %d, want 0", sequence)
	}

	for range 4 {
		appendOne(t, ctx, pool, log, workspaceID, events.MessageCreated)
	}

	sequence, err = log.LatestSequence(ctx, workspaceID)
	if err != nil {
		t.Fatalf("latest sequence: %v", err)
	}
	if sequence != 4 {
		t.Fatalf("got sequence %d, want 4", sequence)
	}
}

// ------------------------------------------------------------------ helpers

func appendOne(
	t *testing.T,
	ctx context.Context,
	pool *database.Pool,
	log *events.Log,
	workspaceID string,
	eventType events.Type,
) *events.Record {
	t.Helper()

	var record *events.Record
	inTx(t, ctx, pool, func(tx pgx.Tx) {
		var err error
		record, err = log.Append(ctx, tx, events.Event{
			WorkspaceID: workspaceID,
			Type:        eventType,
		})
		if err != nil {
			t.Fatalf("append: %v", err)
		}
	})
	return record
}

func inTx(t *testing.T, ctx context.Context, pool *database.Pool, fn func(pgx.Tx)) {
	t.Helper()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	fn(tx)

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func seedWorkspace(t *testing.T, ctx context.Context, pool *database.Pool, slug string) string {
	t.Helper()

	id := ids.New(ids.PrefixWorkspace)
	_, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id, name, slug, ticket_prefix)
		VALUES ($1, $2, $3, 'SUP')
	`, id, slug, slug)
	if err != nil {
		t.Fatalf("seed workspace %q: %v", slug, err)
	}
	return id
}
