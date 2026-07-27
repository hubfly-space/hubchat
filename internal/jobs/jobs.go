// Package jobs is the durable background job queue.
//
// # Responsibilities
//
// Enqueueing, claiming, leasing, retrying, and dead-lettering work that must
// survive a restart: webhook delivery, outbound email, scheduled messages, SLA
// checks, retention sweeps, export generation, and analytics rollups.
//
// # Boundary
//
// PostgreSQL is the queue (ADR-0002). There is no Redis and no broker to
// operate, which is what keeps the deployment story "one binary and a
// database". Modules enqueue through Client and register handlers with Worker;
// nothing outside this package writes to the `jobs` table.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/ids"
)

// State mirrors the CHECK constraint on jobs.state in migration 0004.
type State string

const (
	StatePending   State = "pending"
	StateRunning   State = "running"
	StateSucceeded State = "succeeded"
	StateFailed    State = "failed"
	// StateDead is terminal: attempts are exhausted and a human must look.
	StateDead      State = "dead"
	StateCancelled State = "cancelled"
)

// Job is one unit of durable work.
type Job struct {
	ID          string
	WorkspaceID string
	Queue       string
	Type        string
	Payload     json.RawMessage
	State       State
	Priority    int16
	Attempt     int
	MaxAttempts int
	ScheduledAt time.Time
	LastError   string
	CreatedAt   time.Time
}

// Decode unmarshals the job payload into v.
func (j *Job) Decode(v any) error {
	if len(j.Payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(j.Payload, v); err != nil {
		return fmt.Errorf("jobs: decode %s payload: %w", j.Type, err)
	}
	return nil
}

// Spec describes work to enqueue.
type Spec struct {
	// WorkspaceID is empty for deployment-wide work such as retention sweeps.
	// When set, it enables the per-workspace fairness the worker applies.
	WorkspaceID string
	Queue       string
	Type        string
	Payload     any

	// Priority orders the claim query; higher runs first.
	Priority int16
	// RunAt defers the job. Zero means "as soon as a worker is free".
	RunAt time.Time
	// MaxAttempts overrides the default retry budget.
	MaxAttempts int

	// DedupeKey makes enqueueing idempotent while an identical job is still
	// pending or running. Backed by a unique index rather than a check-then-
	// insert, because the race it prevents is exactly the one two workers
	// reacting to the same event would lose.
	DedupeKey string
}

// ErrDuplicate is returned by Enqueue when DedupeKey matches live work.
//
// It is a normal outcome, not a failure: "this is already queued" is the
// answer the caller wanted. Callers reacting to an at-least-once signal should
// treat it as success.
var ErrDuplicate = errors.New("jobs: an identical job is already queued")

// Client enqueues work. Safe for concurrent use.
type Client struct {
	pool *database.Pool
}

// NewClient returns a Client backed by pool.
func NewClient(pool *database.Pool) *Client {
	return &Client{pool: pool}
}

const (
	defaultQueue       = "default"
	defaultMaxAttempts = 5
)

// Enqueue adds a job on its own transaction.
//
// Prefer EnqueueTx when the job accompanies a state change: a webhook delivery
// enqueued outside the transaction that created the event will fire for a row
// that may yet roll back.
func (c *Client) Enqueue(ctx context.Context, spec Spec) (string, error) {
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("jobs: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	id, err := EnqueueTx(ctx, tx, spec)
	if err != nil {
		return "", err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("jobs: commit: %w", err)
	}
	return id, nil
}

// EnqueueTx adds a job inside the caller's transaction, so the work and the
// state change that justifies it commit together.
func EnqueueTx(ctx context.Context, tx pgx.Tx, spec Spec) (string, error) {
	if spec.Type == "" {
		return "", errors.New("jobs: type is required")
	}
	if spec.Queue == "" {
		spec.Queue = defaultQueue
	}
	if spec.MaxAttempts <= 0 {
		spec.MaxAttempts = defaultMaxAttempts
	}
	if spec.RunAt.IsZero() {
		spec.RunAt = time.Now()
	}

	payload, err := json.Marshal(orEmptyObject(spec.Payload))
	if err != nil {
		return "", fmt.Errorf("jobs: marshal payload: %w", err)
	}

	id := ids.New(ids.PrefixJob)
	var inserted string
	err = tx.QueryRow(ctx, `
		INSERT INTO jobs (
			id, workspace_id, queue, type, payload,
			priority, scheduled_at, max_attempts, dedupe_key
		)
		VALUES ($1, NULLIF($2, ''), $3, $4, $5, $6, $7, $8, NULLIF($9, ''))
		ON CONFLICT DO NOTHING
		RETURNING id
	`,
		id, spec.WorkspaceID, spec.Queue, spec.Type, payload,
		spec.Priority, spec.RunAt, spec.MaxAttempts, spec.DedupeKey,
	).Scan(&inserted)

	if errors.Is(err, pgx.ErrNoRows) {
		// The partial unique index on (queue, dedupe_key) rejected it, which
		// means equivalent work is already pending or running.
		return "", ErrDuplicate
	}
	if err != nil {
		return "", fmt.Errorf("jobs: enqueue %s: %w", spec.Type, err)
	}

	return inserted, nil
}

// Cancel marks a pending job cancelled. Running jobs are left alone: stopping
// one mid-flight would need cooperation the handler has not been asked for.
func (c *Client) Cancel(ctx context.Context, workspaceID, id string) error {
	tag, err := c.pool.Exec(ctx, `
		UPDATE jobs
		SET state = 'cancelled', finished_at = now()
		WHERE id = $1 AND workspace_id = $2 AND state = 'pending'
	`, id, workspaceID)
	if err != nil {
		return fmt.Errorf("jobs: cancel: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Retry moves a failed or dead job back to pending with a fresh budget, for
// the admin "retry" action (§8.7).
func (c *Client) Retry(ctx context.Context, workspaceID, id string) error {
	tag, err := c.pool.Exec(ctx, `
		UPDATE jobs
		SET state         = 'pending',
		    attempt       = 0,
		    scheduled_at  = now(),
		    leased_until  = NULL,
		    leased_by     = NULL,
		    last_error    = NULL,
		    finished_at   = NULL
		WHERE id = $1 AND workspace_id = $2 AND state IN ('failed', 'dead', 'cancelled')
	`, id, workspaceID)
	if err != nil {
		return fmt.Errorf("jobs: retry: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ErrNotFound is returned when a job id does not resolve within the workspace.
var ErrNotFound = errors.New("jobs: not found")

// QueueDepth reports how many jobs are waiting, for /readyz and the ops screen.
func (c *Client) QueueDepth(ctx context.Context) (map[string]int, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT queue, count(*)
		FROM jobs
		WHERE state = 'pending' AND scheduled_at <= now()
		GROUP BY queue
	`)
	if err != nil {
		return nil, fmt.Errorf("jobs: queue depth: %w", err)
	}
	defer rows.Close()

	depths := make(map[string]int)
	for rows.Next() {
		var queue string
		var count int
		if err := rows.Scan(&queue, &count); err != nil {
			return nil, fmt.Errorf("jobs: scan queue depth: %w", err)
		}
		depths[queue] = count
	}
	return depths, rows.Err()
}

func orEmptyObject(payload any) any {
	if payload == nil {
		return struct{}{}
	}
	return payload
}
