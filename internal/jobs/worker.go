package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/config"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/ids"
)

// Handler runs one job. Returning nil marks it succeeded.
//
// Handlers must be idempotent. A job whose lease expires mid-run is reclaimed
// and run again, so "did this already happen?" is a question the handler has
// to be able to answer for itself — the queue guarantees at-least-once, not
// exactly-once, and pretending otherwise is how duplicate emails get sent.
type Handler func(ctx context.Context, job *Job) error

// ErrPermanent wraps an error that must not be retried.
//
// Some failures are not transient: a malformed payload, a webhook endpoint
// that has been deleted, a file that no longer exists. Retrying those five
// times accomplishes nothing except delaying the dead-letter by an hour.
type ErrPermanent struct{ Err error }

func (e *ErrPermanent) Error() string { return e.Err.Error() }
func (e *ErrPermanent) Unwrap() error { return e.Err }

// Permanent marks err as not worth retrying.
func Permanent(err error) error { return &ErrPermanent{Err: err} }

// Worker claims and runs jobs until its context is cancelled.
type Worker struct {
	pool     *database.Pool
	logger   *slog.Logger
	cfg      config.Jobs
	handlers map[string]Handler

	// id identifies this worker in `jobs.leased_by`, so an expired lease can
	// be attributed to the process that dropped it.
	id string

	mu sync.RWMutex
}

// NewWorker returns a Worker. Register handlers before calling Run.
func NewWorker(pool *database.Pool, logger *slog.Logger, cfg config.Jobs) *Worker {
	return &Worker{
		pool:     pool,
		logger:   logger,
		cfg:      cfg,
		handlers: make(map[string]Handler),
		id:       ids.New("wkr"),
	}
}

// Register binds a handler to a job type. Panics on a duplicate registration,
// which is a wiring mistake that should never reach a running system.
func (w *Worker) Register(jobType string, handler Handler) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.handlers[jobType]; exists {
		panic(fmt.Sprintf("jobs: handler for %q registered twice", jobType))
	}
	w.handlers[jobType] = handler
}

func (w *Worker) handlerFor(jobType string) (Handler, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	handler, ok := w.handlers[jobType]
	return handler, ok
}

// Run starts cfg.Concurrency goroutines and blocks until ctx is cancelled.
//
// The pool is bounded rather than one goroutine per job (§17 bounded
// goroutines): an export backlog must not be able to spawn ten thousand
// goroutines and take the realtime hub down with it. Contention for realtime
// is the risk §26.5 names, and a fixed ceiling is the mitigation.
func (w *Worker) Run(ctx context.Context) {
	var wg sync.WaitGroup

	for range w.cfg.Concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.loop(ctx)
		}()
	}

	// One goroutine reclaims leases dropped by processes that died. Doing it
	// here rather than in a separate reaper keeps the failure mode simple:
	// if no worker is running, nothing needed reclaiming anyway.
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.reclaimLoop(ctx)
	}()

	w.logger.Info("job worker started",
		"concurrency", w.cfg.Concurrency, "worker_id", w.id)

	wg.Wait()
	w.logger.Info("job worker stopped", "worker_id", w.id)
}

// loop is one worker goroutine: claim, run, repeat.
func (w *Worker) loop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}

		job, err := w.claim(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			w.logger.Error("job claim failed", "error", err)
			if !sleep(ctx, w.cfg.PollInterval) {
				return
			}
			continue
		}

		if job == nil {
			// Nothing to do. Polling rather than LISTEN/NOTIFY here is a
			// deliberate simplification: the queue is not latency-critical
			// (the realtime path never touches it), and a poll that finds
			// nothing is one indexed lookup.
			if !sleep(ctx, w.cfg.PollInterval) {
				return
			}
			continue
		}

		w.run(ctx, job)
	}
}

// claim atomically takes the next eligible job.
//
// FOR UPDATE SKIP LOCKED is what lets N workers share one queue without
// coordinating: each skips rows another has locked rather than blocking behind
// them. Without SKIP LOCKED, concurrency would be exactly one.
func (w *Worker) claim(ctx context.Context) (*Job, error) {
	lease := time.Now().Add(w.cfg.LeaseDuration)

	row := w.pool.QueryRow(ctx, `
		WITH claimed AS (
			SELECT id
			FROM jobs
			WHERE state = 'pending' AND scheduled_at <= now()
			ORDER BY priority DESC, scheduled_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE jobs
		SET state        = 'running',
		    attempt      = jobs.attempt + 1,
		    leased_until = $1,
		    leased_by    = $2,
		    started_at   = now()
		FROM claimed
		WHERE jobs.id = claimed.id
		RETURNING jobs.id, coalesce(jobs.workspace_id, ''), jobs.queue, jobs.type,
		          jobs.payload, jobs.state, jobs.priority, jobs.attempt,
		          jobs.max_attempts, jobs.scheduled_at,
		          coalesce(jobs.last_error, ''), jobs.created_at
	`, lease, w.id)

	var job Job
	err := row.Scan(
		&job.ID, &job.WorkspaceID, &job.Queue, &job.Type,
		&job.Payload, &job.State, &job.Priority, &job.Attempt,
		&job.MaxAttempts, &job.ScheduledAt, &job.LastError, &job.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("jobs: claim: %w", err)
	}

	return &job, nil
}

// run executes a job and records the outcome.
func (w *Worker) run(ctx context.Context, job *Job) {
	handler, ok := w.handlerFor(job.Type)
	if !ok {
		// An unregistered type is a deployment mismatch, not a transient
		// failure — most often a job enqueued by a newer version than the one
		// draining the queue. Retrying will not teach this process the
		// handler, so it goes straight to dead-letter for a human.
		w.finish(ctx, job, Permanent(fmt.Errorf("no handler registered for job type %q", job.Type)), 0)
		return
	}

	// The job gets its own deadline from the lease. A handler that outlives it
	// would have its work reclaimed and run concurrently by another worker,
	// which is precisely the duplicate-execution case leases exist to bound.
	runCtx, cancel := context.WithTimeout(ctx, w.cfg.LeaseDuration)
	defer cancel()

	started := time.Now()
	err := safeRun(runCtx, handler, job)
	duration := time.Since(started)

	w.finish(ctx, job, err, duration)
}

// safeRun converts a panicking handler into an error.
//
// One bad handler must not take down a worker goroutine and, with it, every
// other queue this process drains.
func safeRun(ctx context.Context, handler Handler, job *Job) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("jobs: handler panicked: %v", recovered)
		}
	}()

	return handler(ctx, job)
}

// finish records the attempt and decides whether the job retries.
//
// The write always runs on a context detached from the caller's, never on ctx
// itself. By the time we get here the handler has already done its work, and
// abandoning the bookkeeping because shutdown started would leave the job
// stranded in 'running' until its lease expired — at which point it would be
// reclaimed and the work done a second time.
//
// Checking ctx.Err() first and only detaching when already cancelled is not
// good enough, and was a real bug here: cancellation can land between the
// check and the commit, which is exactly what happens when a worker is stopped
// the instant its last job returns.
func (w *Worker) finish(ctx context.Context, job *Job, runErr error, duration time.Duration) {
	writeCtx, cancelWrite := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancelWrite()

	outcome := "succeeded"
	if runErr != nil {
		outcome = "failed"
		if errors.Is(runErr, context.DeadlineExceeded) {
			outcome = "timeout"
		}
	}

	tx, err := w.pool.Begin(writeCtx)
	if err != nil {
		w.logger.Error("job finish: begin failed", "job_id", job.ID, "error", err)
		return
	}
	defer tx.Rollback(writeCtx)

	if _, err := tx.Exec(writeCtx, `
		INSERT INTO job_attempts (id, job_id, attempt, outcome, error, duration_ms, started_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7)
	`,
		ids.New(ids.PrefixJobAttempt), job.ID, job.Attempt, outcome,
		errText(runErr), duration.Milliseconds(), time.Now().Add(-duration),
	); err != nil {
		w.logger.Error("job finish: record attempt failed", "job_id", job.ID, "error", err)
		return
	}

	switch {
	case runErr == nil:
		_, err = tx.Exec(writeCtx, `
			UPDATE jobs
			SET state = 'succeeded', finished_at = now(), leased_until = NULL, leased_by = NULL
			WHERE id = $1
		`, job.ID)

	case isPermanent(runErr) || job.Attempt >= job.MaxAttempts:
		// Terminal. §8.7 wants a dead-letter state rather than silent
		// discard, so an operator can see it and retry deliberately.
		_, err = tx.Exec(writeCtx, `
			UPDATE jobs
			SET state = 'dead', finished_at = now(), last_error = $2,
			    leased_until = NULL, leased_by = NULL
			WHERE id = $1
		`, job.ID, errText(runErr))

	default:
		_, err = tx.Exec(writeCtx, `
			UPDATE jobs
			SET state = 'pending', scheduled_at = $2, last_error = $3,
			    leased_until = NULL, leased_by = NULL, started_at = NULL
			WHERE id = $1
		`, job.ID, time.Now().Add(backoff(job.Attempt)), errText(runErr))
	}

	if err != nil {
		w.logger.Error("job finish: update failed", "job_id", job.ID, "error", err)
		return
	}

	if err := tx.Commit(writeCtx); err != nil {
		w.logger.Error("job finish: commit failed", "job_id", job.ID, "error", err)
		return
	}

	if runErr != nil {
		w.logger.Warn("job failed",
			"job_id", job.ID, "type", job.Type,
			"attempt", job.Attempt, "max_attempts", job.MaxAttempts,
			"error", runErr)
	}
}

// reclaimLoop returns jobs whose worker died back to the queue.
func (w *Worker) reclaimLoop(ctx context.Context) {
	// Checking at a fraction of the lease means a dead worker's jobs are
	// picked up promptly without hammering the table.
	interval := w.cfg.LeaseDuration / 4
	if interval < time.Second {
		interval = time.Second
	}

	for {
		if !sleep(ctx, interval) {
			return
		}

		tag, err := w.pool.Exec(ctx, `
			UPDATE jobs
			SET state = 'pending', leased_until = NULL, leased_by = NULL,
			    started_at = NULL,
			    last_error = 'lease expired; worker did not report an outcome'
			WHERE state = 'running' AND leased_until < now()
		`)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			w.logger.Error("job lease reclaim failed", "error", err)
			continue
		}

		if reclaimed := tag.RowsAffected(); reclaimed > 0 {
			w.logger.Warn("reclaimed jobs from expired leases", "count", reclaimed)
		}
	}
}

// backoff returns the delay before retrying attempt n.
//
// Exponential with full jitter. The jitter is not decoration: without it, a
// provider outage that fails a hundred webhook deliveries at once would retry
// all hundred at the same instant, repeatedly, turning our retry policy into a
// self-inflicted thundering herd against a service that is already struggling.
func backoff(attempt int) time.Duration {
	const (
		base = 2 * time.Second
		max  = 30 * time.Minute
	)

	if attempt < 1 {
		attempt = 1
	}
	// Cap the exponent before shifting so a large attempt count cannot
	// overflow the multiplication into a negative duration.
	exponent := min(attempt-1, 20)
	delay := time.Duration(float64(base) * math.Pow(2, float64(exponent)))
	if delay > max || delay <= 0 {
		delay = max
	}

	return time.Duration(rand.Int64N(int64(delay)) + 1)
}

func isPermanent(err error) bool {
	var permanent *ErrPermanent
	return errors.As(err, &permanent)
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// sleep waits for d, reporting false if the context ended first.
func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
