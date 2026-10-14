//go:build integration

package events

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/jackc/pgx/v5"
)

func TestConsumerRecoversWithoutSignalsAndResumesAfterFailure(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces(id,name,slug) VALUES('wrk_consumer','Consumer','consumer')`); err != nil {
		t.Fatal(err)
	}
	log := New(pool)
	for range 3 {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = log.Append(ctx, tx, Event{WorkspaceID: "wrk_consumer", Type: ConversationCreated}); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	failed := true
	var received []int64
	err := drainConsumer(ctx, pool, "test", "wrk_consumer", log, func(_ context.Context, r Record) error {
		if r.Sequence == 2 && failed {
			return errors.New("temporary failure")
		}
		received = append(received, r.Sequence)
		return nil
	})
	if err == nil {
		t.Fatal("expected failure")
	}
	var cursor int64
	if err := pool.QueryRow(ctx, `SELECT last_sequence FROM event_consumer_cursors WHERE consumer='test' AND workspace_id='wrk_consumer'`).Scan(&cursor); err != nil {
		t.Fatal(err)
	}
	if cursor != 1 {
		t.Fatalf("checkpoint advanced past failed event: %d", cursor)
	}
	failed = false
	if err := drainConsumer(ctx, pool, "test", "wrk_consumer", log, func(_ context.Context, r Record) error { received = append(received, r.Sequence); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(received) != 3 || received[0] != 1 || received[1] != 2 || received[2] != 3 {
		t.Fatalf("replay=%v", received)
	}
	// Startup catches committed events with no notification channel at all.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	processed := make(chan int64, 4)
	go func() {
		defer close(done)
		Consume(runCtx, pool, "startup-test", nil, log, func(_ context.Context, r Record) error { processed <- r.Sequence; return nil })
	}()
	for range 3 {
		select {
		case <-processed:
		case <-time.After(5 * time.Second):
			t.Fatal("startup catch-up timed out")
		}
	}
	cancel()
	<-done
}

func TestConsumerSerializesWorkers(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces(id,name,slug) VALUES('wrk_consumer_multi','Consumer multi','consumer-multi')`); err != nil {
		t.Fatal(err)
	}
	log := New(pool)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = log.Append(ctx, tx, Event{WorkspaceID: "wrk_consumer_multi", Type: ConversationCreated})
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	count := 0
	handle := func(_ context.Context, _ Record) error { mu.Lock(); count++; mu.Unlock(); return nil }
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := drainConsumer(ctx, pool, "multi", "wrk_consumer_multi", log, handle); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if count != 1 {
		t.Fatalf("processed %d times", count)
	}
}
