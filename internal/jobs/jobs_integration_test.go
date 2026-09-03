//go:build integration

package jobs_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/config"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/jobs"
)

func TestEnqueueAndRunRoundTrip(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	client := jobs.NewClient(pool)
	workspaceID := seedWorkspace(t, ctx, pool, "run")

	type payload struct {
		Message string `json:"message"`
	}

	if _, err := client.Enqueue(ctx, jobs.Spec{
		WorkspaceID: workspaceID,
		Type:        "test.echo",
		Payload:     payload{Message: "hello"},
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	var got atomic.Value
	done := make(chan struct{})
	var once sync.Once

	worker := newWorker(t, pool)
	worker.Register("test.echo", func(_ context.Context, job *jobs.Job) error {
		var decoded payload
		if err := job.Decode(&decoded); err != nil {
			return err
		}
		got.Store(decoded.Message)
		once.Do(func() { close(done) })
		return nil
	})

	runWorker(t, ctx, worker, done)

	if message, _ := got.Load().(string); message != "hello" {
		t.Fatalf("handler received %q, want %q", message, "hello")
	}
	waitForState(t, ctx, pool, "test.echo", "succeeded")
}

// The dedupe index is what stops an at-least-once signal from enqueueing the
// same webhook delivery twice. Checking in application code would lose the
// race this test runs.
func TestEnqueueDeduplicatesLiveWork(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	client := jobs.NewClient(pool)
	workspaceID := seedWorkspace(t, ctx, pool, "dedupe")

	spec := jobs.Spec{
		WorkspaceID: workspaceID,
		Type:        "test.deliver",
		DedupeKey:   "delivery-42",
	}

	if _, err := client.Enqueue(ctx, spec); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}

	_, err := client.Enqueue(ctx, spec)
	if !errors.Is(err, jobs.ErrDuplicate) {
		t.Fatalf("second enqueue: got %v, want ErrDuplicate", err)
	}

	if count := countJobs(t, ctx, pool, "test.deliver"); count != 1 {
		t.Fatalf("got %d jobs queued, want 1", count)
	}
}

func TestEnsureScheduledDoesNotStartASecondRecurringChain(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	client := jobs.NewClient(pool)

	if _, err := client.Enqueue(ctx, jobs.Spec{
		Type: "test.schedule", DedupeKey: "test.schedule:previous-job",
		RunAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("enqueue existing schedule: %v", err)
	}

	_, err := client.EnsureScheduled(ctx, jobs.Spec{
		Type: "test.schedule", DedupeKey: "test.schedule",
	})
	if !errors.Is(err, jobs.ErrDuplicate) {
		t.Fatalf("ensure existing schedule: got %v, want ErrDuplicate", err)
	}
	if count := countJobs(t, ctx, pool, "test.schedule"); count != 1 {
		t.Fatalf("got %d scheduled jobs, want 1", count)
	}
}

func TestPruneTerminalBeforeKeepsRecentAndLiveJobs(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	client := jobs.NewClient(pool)

	workspaceID := seedWorkspace(t, ctx, pool, "prune")
	oldID, err := client.Enqueue(ctx, jobs.Spec{WorkspaceID: workspaceID, Type: "test.old"})
	if err != nil {
		t.Fatalf("enqueue old job: %v", err)
	}
	recentID, err := client.Enqueue(ctx, jobs.Spec{WorkspaceID: workspaceID, Type: "test.recent"})
	if err != nil {
		t.Fatalf("enqueue recent job: %v", err)
	}
	if _, err := client.Enqueue(ctx, jobs.Spec{WorkspaceID: workspaceID, Type: "test.pending"}); err != nil {
		t.Fatalf("enqueue pending job: %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE jobs SET state='succeeded', finished_at=now()-interval '2 days' WHERE id=$1`, oldID); err != nil {
		t.Fatalf("finish old job: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE jobs SET state='succeeded', finished_at=now() WHERE id=$1`, recentID); err != nil {
		t.Fatalf("finish recent job: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO job_attempts (id, job_id, attempt, outcome, started_at)
		VALUES ($1, $2, 1, 'succeeded', now()-interval '2 days')
	`, ids.New(ids.PrefixJobAttempt), oldID); err != nil {
		t.Fatalf("seed old job attempt: %v", err)
	}

	deleted, err := client.PruneTerminalBefore(ctx, time.Now().Add(-24*time.Hour), 100)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted %d jobs, want 1", deleted)
	}
	if count := countJobs(t, ctx, pool, "test.old"); count != 0 {
		t.Fatalf("old jobs remaining = %d, want 0", count)
	}
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM job_attempts WHERE job_id=$1`, oldID).Scan(&attempts); err != nil {
		t.Fatalf("count attempts: %v", err)
	}
	if attempts != 0 {
		t.Fatalf("old job attempts remaining = %d, want 0", attempts)
	}
	if count := countJobs(t, ctx, pool, "test.recent"); count != 1 {
		t.Fatalf("recent jobs remaining = %d, want 1", count)
	}
	if count := countJobs(t, ctx, pool, "test.pending"); count != 1 {
		t.Fatalf("pending jobs remaining = %d, want 1", count)
	}
}

func TestCancelPendingJobIsWorkspaceScoped(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	client := jobs.NewClient(pool)
	workspaceID := seedWorkspace(t, ctx, pool, "cancel")
	jobID, err := client.Enqueue(ctx, jobs.Spec{WorkspaceID: workspaceID, Type: "test.cancel"})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	if err := client.Cancel(ctx, workspaceID, jobID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	var state string
	if err := pool.QueryRow(ctx, `SELECT state FROM jobs WHERE id = $1`, jobID).Scan(&state); err != nil {
		t.Fatalf("read cancelled job: %v", err)
	}
	if state != string(jobs.StateCancelled) {
		t.Fatalf("cancelled job state = %q, want %q", state, jobs.StateCancelled)
	}
	if err := client.Cancel(ctx, workspaceID, jobID); !errors.Is(err, jobs.ErrNotFound) {
		t.Fatalf("second cancel error = %v, want ErrNotFound", err)
	}
}

// Concurrent enqueues of the same dedupe key: exactly one must win. This is
// the case a check-then-insert gets wrong.
func TestEnqueueDeduplicatesUnderConcurrency(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	client := jobs.NewClient(pool)
	workspaceID := seedWorkspace(t, ctx, pool, "dedupe-race")

	const writers = 12
	var accepted atomic.Int32
	var wg sync.WaitGroup

	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := client.Enqueue(ctx, jobs.Spec{
				WorkspaceID: workspaceID,
				Type:        "test.race",
				DedupeKey:   "only-once",
			})
			switch {
			case err == nil:
				accepted.Add(1)
			case errors.Is(err, jobs.ErrDuplicate):
			default:
				t.Errorf("enqueue: %v", err)
			}
		}()
	}
	wg.Wait()

	if n := accepted.Load(); n != 1 {
		t.Fatalf("%d concurrent enqueues were accepted, want exactly 1", n)
	}
	if count := countJobs(t, ctx, pool, "test.race"); count != 1 {
		t.Fatalf("got %d jobs queued, want 1", count)
	}
}

// SKIP LOCKED is what lets several workers share one queue. Without it every
// job would be handed to whichever worker asked first while the rest blocked.
func TestClaimDoesNotHandOneJobToTwoWorkers(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	client := jobs.NewClient(pool)
	workspaceID := seedWorkspace(t, ctx, pool, "claim")

	const total = 24
	for range total {
		if _, err := client.Enqueue(ctx, jobs.Spec{
			WorkspaceID: workspaceID,
			Type:        "test.count",
		}); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}

	var runs atomic.Int32
	done := make(chan struct{})
	var once sync.Once

	worker := newWorker(t, pool)
	worker.Register("test.count", func(context.Context, *jobs.Job) error {
		if runs.Add(1) == total {
			once.Do(func() { close(done) })
		}
		return nil
	})

	runWorker(t, ctx, worker, done)

	if n := runs.Load(); n != total {
		t.Fatalf("handler ran %d times, want %d", n, total)
	}
	if remaining := countJobsInState(t, ctx, pool, "test.count", "succeeded"); remaining != total {
		t.Fatalf("%d jobs succeeded, want %d", remaining, total)
	}
}

// A failing job retries until its budget is spent, then dead-letters rather
// than disappearing (§8.7).
func TestFailingJobRetriesThenDeadLetters(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	client := jobs.NewClient(pool)
	workspaceID := seedWorkspace(t, ctx, pool, "retry")

	if _, err := client.Enqueue(ctx, jobs.Spec{
		WorkspaceID: workspaceID,
		Type:        "test.flaky",
		MaxAttempts: 2,
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	var attempts atomic.Int32
	done := make(chan struct{})
	var once sync.Once

	worker := newWorker(t, pool)
	worker.Register("test.flaky", func(context.Context, *jobs.Job) error {
		if attempts.Add(1) == 2 {
			once.Do(func() { close(done) })
		}
		return errors.New("still broken")
	})

	runWorker(t, ctx, worker, done)
	waitForState(t, ctx, pool, "test.flaky", "dead")

	if n := attempts.Load(); n != 2 {
		t.Fatalf("handler ran %d times, want 2 (max_attempts)", n)
	}

	// Every attempt is recorded, so "why did this die" is answerable without
	// re-running it.
	if recorded := countAttempts(t, ctx, pool, "test.flaky"); recorded != 2 {
		t.Fatalf("recorded %d attempts, want 2", recorded)
	}
}

// A permanent error skips the retry budget entirely — retrying a malformed
// payload five times accomplishes nothing.
func TestPermanentErrorSkipsRetries(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	client := jobs.NewClient(pool)
	workspaceID := seedWorkspace(t, ctx, pool, "permanent")

	if _, err := client.Enqueue(ctx, jobs.Spec{
		WorkspaceID: workspaceID,
		Type:        "test.permanent",
		MaxAttempts: 5,
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	var attempts atomic.Int32
	done := make(chan struct{})
	var once sync.Once

	worker := newWorker(t, pool)
	worker.Register("test.permanent", func(context.Context, *jobs.Job) error {
		attempts.Add(1)
		once.Do(func() { close(done) })
		return jobs.Permanent(errors.New("payload will never parse"))
	})

	runWorker(t, ctx, worker, done)
	waitForState(t, ctx, pool, "test.permanent", "dead")

	if n := attempts.Load(); n != 1 {
		t.Fatalf("handler ran %d times, want 1 despite max_attempts=5", n)
	}
}

// An unregistered type is a deployment mismatch. It must dead-letter for a
// human rather than spin through its retry budget.
func TestUnknownJobTypeDeadLettersImmediately(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	client := jobs.NewClient(pool)
	workspaceID := seedWorkspace(t, ctx, pool, "unknown")

	if _, err := client.Enqueue(ctx, jobs.Spec{
		WorkspaceID: workspaceID,
		Type:        "test.never_registered",
		MaxAttempts: 5,
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	worker := newWorker(t, pool)
	// Deliberately registers nothing.

	runCtx, cancel := context.WithCancel(ctx)
	go worker.Run(runCtx)
	waitForState(t, ctx, pool, "test.never_registered", "dead")
	cancel()
}

// A panicking handler must not take the worker goroutine down with it.
func TestPanickingHandlerIsContained(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	client := jobs.NewClient(pool)
	workspaceID := seedWorkspace(t, ctx, pool, "panic")

	if _, err := client.Enqueue(ctx, jobs.Spec{
		WorkspaceID: workspaceID,
		Type:        "test.panic",
		MaxAttempts: 1,
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, err := client.Enqueue(ctx, jobs.Spec{
		WorkspaceID: workspaceID,
		Type:        "test.after_panic",
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	done := make(chan struct{})
	var once sync.Once

	// Keep this lifecycle assertion sequential: with a pool of workers the
	// follow-up job may signal completion while the panicking job is still
	// committing its terminal state, making shutdown race the assertion.
	worker := newWorkerWithConcurrency(t, pool, 1)
	worker.Register("test.panic", func(context.Context, *jobs.Job) error {
		panic("handler exploded")
	})
	// If the panic killed the pool, this job never runs and the test times out.
	worker.Register("test.after_panic", func(context.Context, *jobs.Job) error {
		once.Do(func() { close(done) })
		return nil
	})

	runWorker(t, ctx, worker, done)
	waitForState(t, ctx, pool, "test.panic", "dead")
}

// Deferred work must not be claimed before its time.
func TestScheduledJobIsNotClaimedEarly(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	client := jobs.NewClient(pool)
	workspaceID := seedWorkspace(t, ctx, pool, "scheduled")

	if _, err := client.Enqueue(ctx, jobs.Spec{
		WorkspaceID: workspaceID,
		Type:        "test.later",
		RunAt:       time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	var ran atomic.Bool
	worker := newWorker(t, pool)
	worker.Register("test.later", func(context.Context, *jobs.Job) error {
		ran.Store(true)
		return nil
	})

	runCtx, cancel := context.WithCancel(ctx)
	go worker.Run(runCtx)
	time.Sleep(500 * time.Millisecond)
	cancel()

	if ran.Load() {
		t.Fatal("a job scheduled an hour out was claimed immediately")
	}
	if state := jobState(t, ctx, pool, "test.later"); state != "pending" {
		t.Fatalf("got state %q, want pending", state)
	}
}

// ------------------------------------------------------------------ helpers

func newWorker(t *testing.T, pool *database.Pool) *jobs.Worker {
	t.Helper()
	return newWorkerWithConcurrency(t, pool, 4)
}

func newWorkerWithConcurrency(t *testing.T, pool *database.Pool, concurrency int) *jobs.Worker {
	t.Helper()

	cfg := config.Default().Jobs
	cfg.Concurrency = concurrency
	// Tight so the tests do not spend their time asleep.
	cfg.PollInterval = 20 * time.Millisecond
	cfg.LeaseDuration = 5 * time.Second

	return jobs.NewWorker(pool, slog.New(slog.NewTextHandler(io.Discard, nil)), cfg)
}

// runWorker starts the worker and stops it once done closes.
func runWorker(t *testing.T, ctx context.Context, worker *jobs.Worker, done <-chan struct{}) {
	t.Helper()

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	stopped := make(chan struct{})
	go func() {
		worker.Run(runCtx)
		close(stopped)
	}()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("timed out waiting for the worker to finish its jobs")
	}

	cancel()
	<-stopped
}

func waitForState(t *testing.T, ctx context.Context, pool *database.Pool, jobType, want string) {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		last = jobState(t, ctx, pool, jobType)
		if last == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("job %q settled in state %q, want %q", jobType, last, want)
}

func jobState(t *testing.T, ctx context.Context, pool *database.Pool, jobType string) string {
	t.Helper()

	var state string
	err := pool.QueryRow(ctx,
		`SELECT state FROM jobs WHERE type = $1 LIMIT 1`, jobType).Scan(&state)
	if err != nil {
		t.Fatalf("read job state for %q: %v", jobType, err)
	}
	return state
}

func countJobs(t *testing.T, ctx context.Context, pool *database.Pool, jobType string) int {
	t.Helper()

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM jobs WHERE type = $1`, jobType).Scan(&count); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	return count
}

func countJobsInState(t *testing.T, ctx context.Context, pool *database.Pool, jobType, state string) int {
	t.Helper()

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM jobs WHERE type = $1 AND state = $2`,
		jobType, state).Scan(&count); err != nil {
		t.Fatalf("count jobs in state: %v", err)
	}
	return count
}

func countAttempts(t *testing.T, ctx context.Context, pool *database.Pool, jobType string) int {
	t.Helper()

	var count int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM job_attempts a
		JOIN jobs j ON j.id = a.job_id
		WHERE j.type = $1
	`, jobType).Scan(&count); err != nil {
		t.Fatalf("count attempts: %v", err)
	}
	return count
}

func seedWorkspace(t *testing.T, ctx context.Context, pool *database.Pool, slug string) string {
	t.Helper()

	id := ids.New(ids.PrefixWorkspace)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id, name, slug, ticket_prefix)
		VALUES ($1, $2, $3, 'SUP')
	`, id, slug, slug); err != nil {
		t.Fatalf("seed workspace %q: %v", slug, err)
	}
	return id
}

var _ = io.Discard
