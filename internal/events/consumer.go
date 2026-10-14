package events

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hubchat/hubchat/internal/database"
)

// ConsumerSource reads durable events. Notifications are only wake-up hints.
type ConsumerSource interface {
	Since(context.Context, string, int64, int) ([]Record, error)
}

// Consume resumes persisted workspace checkpoints, polls for lost notifications,
// and serializes each consumer/workspace across processes. Handle must commit
// durable, replay-safe effects before returning: delivery is at least once.
func Consume(ctx context.Context, pool *database.Pool, name string, signals <-chan Signal, source ConsumerSource, handle func(context.Context, Record) error) {
	reconcile := func() {
		rows, err := pool.Query(ctx, `SELECT s.workspace_id
   FROM workspace_event_sequences s
   LEFT JOIN event_consumer_cursors c ON c.workspace_id=s.workspace_id AND c.consumer=$1
   WHERE s.next_sequence-1 > coalesce(c.last_sequence,0)
   ORDER BY coalesce(c.updated_at,'epoch'::timestamptz), s.workspace_id LIMIT 100`, name)
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("events: reconcile failed", "consumer", name, "error", err)
			}
			return
		}
		var workspaces []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				break
			}
			workspaces = append(workspaces, id)
		}
		rows.Close()
		for _, id := range workspaces {
			if ctx.Err() != nil {
				return
			}
			consumeWorkspace(ctx, pool, name, id, source, handle)
		}
	}
	reconcile()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case signal, ok := <-signals:
			if !ok {
				signals = nil
				continue
			}
			consumeWorkspace(ctx, pool, name, signal.WorkspaceID, source, handle)
		case <-ticker.C:
			reconcile()
		}
	}
}

func consumeWorkspace(ctx context.Context, pool *database.Pool, name, workspaceID string, source ConsumerSource, handle func(context.Context, Record) error) {
	err := drainConsumer(ctx, pool, name, workspaceID, source, handle)
	if err != nil && ctx.Err() == nil {
		slog.Error("events: consumer failed; checkpoint retained for retry", "consumer", name, "workspace_id", workspaceID, "error", err)
	}
}

func drainConsumer(ctx context.Context, pool *database.Pool, name, workspaceID string, source ConsumerSource, handle func(context.Context, Record) error) error {
	var importing bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM import_requests WHERE workspace_id=$1 AND kind='workspace' AND state='running')`, workspaceID).Scan(&importing); err != nil {
		return err
	}
	if importing {
		return nil
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var locked bool
	lockKey := "hubchat:consumer:" + name + ":" + workspaceID
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, lockKey).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return nil
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, lockKey); err != nil {
			_ = conn.Conn().Close(unlockCtx)
		}
	}()
	if _, err := conn.Exec(ctx, `INSERT INTO event_consumer_cursors(consumer,workspace_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, name, workspaceID); err != nil {
		return err
	}
	var after int64
	if err := conn.QueryRow(ctx, `SELECT last_sequence FROM event_consumer_cursors WHERE consumer=$1 AND workspace_id=$2`, name, workspaceID).Scan(&after); err != nil {
		return err
	}
	// Bound one turn so a busy workspace cannot starve others or shutdown.
	for range 5 {
		records, err := source.Since(ctx, workspaceID, after, 200)
		if err != nil {
			return err
		}
		for _, record := range records {
			if err := handle(ctx, record); err != nil {
				return fmt.Errorf("event %s: %w", record.ID, err)
			}
			if _, err := conn.Exec(ctx, `UPDATE event_consumer_cursors SET last_sequence=$3,updated_at=now() WHERE consumer=$1 AND workspace_id=$2`, name, workspaceID, record.Sequence); err != nil {
				return err
			}
			after = record.Sequence
		}
		if len(records) < 200 {
			return nil
		}
	}
	return nil
}
