//go:build integration

package conversation_test

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/workspace"
)

// TestConversationListAtConfiguredScale is deliberately separate from the
// regular integration suite. It is a destructive, data-heavy acceptance gate
// for a deployment's expected PostgreSQL shape, not a unit-style regression
// test. The default dataset is large enough to exercise the production index
// path without pretending that one developer laptop proves every deployment's
// capacity.
//
// Run it through `make test-capacity`, or set HUBCHAT_RUN_SCALE_TESTS=1 and
// provide the other HUBCHAT_SCALE_* variables directly.
func TestConversationListAtConfiguredScale(t *testing.T) {
	if os.Getenv("HUBCHAT_RUN_SCALE_TESTS") != "1" {
		t.Skip("set HUBCHAT_RUN_SCALE_TESTS=1 to run the destructive PostgreSQL capacity gate")
	}

	pool := dbtest.Pool(t)
	ctx := dbtest.Context(t)
	resetCapacityDatabase(t, ctx, pool)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		resetCapacityDatabase(t, cleanupCtx, pool)
	})

	conversationCount := envPositiveInt(t, "HUBCHAT_SCALE_CONVERSATIONS", 25000)
	workers := envPositiveInt(t, "HUBCHAT_SCALE_WORKERS", 16)
	requestsPerWorker := envPositiveInt(t, "HUBCHAT_SCALE_REQUESTS", 40)
	maxP95 := time.Duration(envPositiveInt(t, "HUBCHAT_SCALE_MAX_P95_MS", 250)) * time.Millisecond

	seeded := seedCapacityDataset(t, ctx, pool, conversationCount)
	service := conversation.New(pool, events.New(pool), audit.New(pool))

	// Warm the connection and query plan before measuring. The acceptance
	// metric is steady-state list latency, not first-connection startup.
	warmup, err := service.List(ctx, seeded.primary.WorkspaceID, conversation.ListFilter{
		InboxID: seeded.primary.InboxID,
		Limit:   100,
	})
	if err != nil {
		t.Fatalf("warm conversation list: %v", err)
	}
	if len(warmup) != 100 {
		t.Fatalf("warm conversation list returned %d rows, want 100", len(warmup))
	}

	latencies := make([]time.Duration, 0, workers*requestsPerWorker)
	var latenciesMu sync.Mutex
	var failures atomic.Int64
	errorsCh := make(chan error, workers*requestsPerWorker)
	var group sync.WaitGroup
	group.Add(workers)
	started := time.Now()
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer group.Done()
			for request := 0; request < requestsPerWorker; request++ {
				began := time.Now()
				items, listErr := service.List(ctx, seeded.primary.WorkspaceID, conversation.ListFilter{
					InboxID: seeded.primary.InboxID,
					Limit:   100,
				})
				latency := time.Since(began)
				latenciesMu.Lock()
				latencies = append(latencies, latency)
				latenciesMu.Unlock()
				if listErr != nil {
					failures.Add(1)
					errorsCh <- listErr
					continue
				}
				if len(items) != 100 {
					failures.Add(1)
					errorsCh <- fmt.Errorf("scale list returned %d rows, want 100", len(items))
					continue
				}
				for _, item := range items {
					if item.WorkspaceID != seeded.primary.WorkspaceID || item.InboxID != seeded.primary.InboxID {
						failures.Add(1)
						errorsCh <- fmt.Errorf("scale list leaked conversation %s from workspace %s/inbox %s", item.ID, item.WorkspaceID, item.InboxID)
						break
					}
				}
			}
		}()
	}
	group.Wait()
	close(errorsCh)
	if failures.Load() != 0 {
		for err := range errorsCh {
			t.Error(err)
		}
		t.Fatalf("scale list had %d failures", failures.Load())
	}

	if foreign, foreignErr := service.List(ctx, seeded.foreign.WorkspaceID, conversation.ListFilter{
		InboxID: seeded.foreign.InboxID,
		Limit:   100,
	}); foreignErr != nil {
		t.Fatalf("list foreign workspace: %v", foreignErr)
	} else {
		for _, item := range foreign {
			if item.WorkspaceID != seeded.foreign.WorkspaceID || item.InboxID != seeded.foreign.InboxID {
				t.Fatalf("foreign list leaked conversation %s from workspace %s/inbox %s", item.ID, item.WorkspaceID, item.InboxID)
			}
		}
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p50 := percentile(latencies, 0.50)
	p95 := percentile(latencies, 0.95)
	p99 := percentile(latencies, 0.99)
	t.Logf("conversation list capacity: rows=%d workers=%d requests=%d elapsed=%s p50=%s p95=%s p99=%s", conversationCount, workers, len(latencies), time.Since(started).Round(time.Millisecond), p50.Round(time.Microsecond), p95.Round(time.Microsecond), p99.Round(time.Microsecond))
	if p95 > maxP95 {
		t.Fatalf("conversation list p95=%s exceeds configured limit %s; tune deployment/database before capacity sign-off", p95.Round(time.Microsecond), maxP95)
	}
}

type capacityWorkspace struct {
	WorkspaceID string
	InboxID     string
}

type capacityDataset struct {
	primary capacityWorkspace
	foreign capacityWorkspace
}

func seedCapacityDataset(t *testing.T, ctx context.Context, pool *database.Pool, count int) capacityDataset {
	t.Helper()
	workspaceService := workspace.New(pool, events.New(pool), audit.New(pool))
	primary := seedCapacityWorkspace(t, ctx, pool, workspaceService, "capacity-primary")
	foreign := seedCapacityWorkspace(t, ctx, pool, workspaceService, "capacity-foreign")

	base := time.Now().UTC().Truncate(time.Microsecond)
	conversationRows := make([][]any, 0, count)
	messageRows := make([][]any, 0, count)
	for index := 0; index < count; index++ {
		conversationID := fmt.Sprintf("cnv_capacity_%06d", index)
		messageID := fmt.Sprintf("msg_capacity_%06d", index)
		at := base.Add(-time.Duration(index) * time.Second)
		state := "open"
		if index%10 == 0 {
			state = "closed"
		}
		conversationRows = append(conversationRows, []any{
			conversationID, primary.WorkspaceID, primary.InboxID, "widget", state,
			"normal", 1, fmt.Sprintf("Capacity message %d", index), at, at,
		})
		messageRows = append(messageRows, []any{
			messageID, primary.WorkspaceID, conversationID, "reply", "customer",
			"Capacity visitor", fmt.Sprintf("Capacity message %d", index), 1, at,
		})
	}

	if copied, err := pool.CopyFrom(ctx, pgx.Identifier{"conversations"}, []string{
		"id", "workspace_id", "inbox_id", "channel", "state", "priority",
		"message_count", "last_message_preview", "last_message_at", "created_at",
	}, pgx.CopyFromRows(conversationRows)); err != nil || copied != int64(len(conversationRows)) {
		t.Fatalf("seed %d conversations: copied=%d err=%v", len(conversationRows), copied, err)
	}
	if copied, err := pool.CopyFrom(ctx, pgx.Identifier{"messages"}, []string{
		"id", "workspace_id", "conversation_id", "kind", "author_type", "author_name", "body", "sequence", "created_at",
	}, pgx.CopyFromRows(messageRows)); err != nil || copied != int64(len(messageRows)) {
		t.Fatalf("seed %d messages: copied=%d err=%v", len(messageRows), copied, err)
	}

	return capacityDataset{primary: primary, foreign: foreign}
}

func resetCapacityDatabase(t *testing.T, ctx context.Context, pool *database.Pool) {
	t.Helper()
	// A scale dataset intentionally contains tens of thousands of messages.
	// Truncating the conversation root lets PostgreSQL clear its dependent
	// tables in one metadata operation; the normal integration reset remains a
	// row-wise workspace delete because it must preserve global role seeds.
	if _, err := pool.Exec(ctx, `TRUNCATE TABLE conversations CASCADE`); err != nil {
		t.Fatalf("truncate scale conversation data: %v", err)
	}
	for _, stmt := range []string{`DELETE FROM workspaces`, `DELETE FROM users`} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("reset scale database (%s): %v", stmt, err)
		}
	}
}

func seedCapacityWorkspace(t *testing.T, ctx context.Context, pool *database.Pool, service *workspace.Service, slug string) capacityWorkspace {
	t.Helper()
	userID := "usr_" + slug
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, name, email, password_hash, email_verified_at)
		VALUES ($1, $2, $3, 'x', now())
	`, userID, slug, userID+"@example.com"); err != nil {
		t.Fatalf("seed %s user: %v", slug, err)
	}
	created, err := service.Bootstrap(ctx, userID, slug, slug)
	if err != nil {
		t.Fatalf("bootstrap %s workspace: %v", slug, err)
	}
	var inboxID string
	if err := pool.QueryRow(ctx, `SELECT id FROM inboxes WHERE workspace_id=$1 LIMIT 1`, created.ID).Scan(&inboxID); err != nil {
		t.Fatalf("load %s inbox: %v", slug, err)
	}
	return capacityWorkspace{WorkspaceID: created.ID, InboxID: inboxID}
}

func envPositiveInt(t *testing.T, name string, fallback int) int {
	t.Helper()
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		t.Fatalf("%s must be a positive integer, got %q", name, raw)
	}
	return value
}

func percentile(values []time.Duration, fraction float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	index := int(float64(len(values)-1) * fraction)
	return values[index]
}
