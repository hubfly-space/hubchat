package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Signal reports that a workspace has events up to Sequence.
//
// It carries no payload beyond the position on purpose: the subscriber reads
// the rows it actually needs from the log. That keeps the notification under
// PostgreSQL's 8000-byte cap regardless of event size, and means a dropped
// signal costs nothing — the next one carries a higher sequence and the
// subscriber catches up from wherever it was.
type Signal struct {
	WorkspaceID string
	Sequence    int64
}

// Listener turns PostgreSQL NOTIFY traffic into Signals.
//
// This is what makes multi-node work without Redis (ADR-0002): a message
// written by the process handling an HTTP request has to reach a WebSocket
// held open by a different process, and LISTEN/NOTIFY is the delivery
// mechanism PostgreSQL already provides.
//
// It holds one dedicated connection outside the pool. A connection running
// LISTEN is not usable for anything else, so borrowing one from the pool for
// the process's lifetime would permanently shrink it.
type Listener struct {
	pool    *pgxpool.Pool
	logger  *slog.Logger
	signals chan Signal
}

// NewListener returns a Listener. Call Run to start it.
func NewListener(pool *pgxpool.Pool, logger *slog.Logger) *Listener {
	return &Listener{
		pool:   pool,
		logger: logger,
		// Buffered so a brief consumer stall does not block the connection
		// that is reading notifications. Overflow is dropped rather than
		// queued without bound — see the comment in dispatch.
		signals: make(chan Signal, 1024),
	}
}

// Signals returns the channel Run publishes to. Closed when Run returns.
func (l *Listener) Signals() <-chan Signal {
	return l.signals
}

// Run listens until ctx is cancelled, reconnecting on failure.
//
// A dropped connection is expected rather than exceptional — PostgreSQL
// restarts, failovers, and idle-connection reapers all cause it. What matters
// is that reconnecting is cheap and that subscribers do not need to know it
// happened: they track their own sequence, so a gap during the outage is
// closed by the next signal (§18 database reconnect strategy).
func (l *Listener) Run(ctx context.Context) {
	defer close(l.signals)

	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for ctx.Err() == nil {
		err := l.listen(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			l.logger.Warn("event listener disconnected, retrying",
				"error", err, "retry_in", backoff)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// listen holds one connection and pumps notifications until it fails.
func (l *Listener) listen(ctx context.Context) error {
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("events: acquire listener connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "LISTEN "+notifyChannel); err != nil {
		return fmt.Errorf("events: listen: %w", err)
	}

	l.logger.Info("event listener connected", "channel", notifyChannel)

	for {
		notification, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			// The connection is no longer usable for LISTEN. Hint that to the
			// pool so it is not handed back out still subscribed.
			conn.Conn().Close(ctx)
			return fmt.Errorf("events: wait for notification: %w", err)
		}

		signal, ok := parseSignal(notification.Payload)
		if !ok {
			l.logger.Warn("event listener got malformed payload",
				"payload", notification.Payload)
			continue
		}

		l.dispatch(signal)
	}
}

// dispatch hands a signal to the consumer, dropping it if the buffer is full.
//
// Dropping is safe here in a way it would not be for the events themselves:
// a signal only says "look, there is something new", and the next one says it
// again with a higher sequence. A subscriber that misses one and catches the
// next reads the same rows it would have read anyway. Blocking instead would
// stall the shared listener connection for every workspace at once.
func (l *Listener) dispatch(signal Signal) {
	select {
	case l.signals <- signal:
	default:
		l.logger.Warn("event signal buffer full, dropping signal",
			"workspace_id", signal.WorkspaceID, "sequence", signal.Sequence)
	}
}

// parseSignal reads the "<workspace_id>:<sequence>" payload Append sends.
func parseSignal(payload string) (Signal, bool) {
	workspaceID, sequenceText, found := strings.Cut(payload, ":")
	if !found || workspaceID == "" {
		return Signal{}, false
	}

	sequence, err := strconv.ParseInt(sequenceText, 10, 64)
	if err != nil {
		return Signal{}, false
	}

	return Signal{WorkspaceID: workspaceID, Sequence: sequence}, true
}
