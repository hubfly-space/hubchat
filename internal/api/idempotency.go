package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/ids"
)

// idempotencyWindow is how long a key is remembered.
//
// Long enough to cover any realistic retry — a phone that reconnects after a
// tunnel, a job queue backing off, a user reloading a stuck page — and short
// enough that the table stays small. §16 requires a documented retention
// window rather than an unbounded one.
const idempotencyWindow = 24 * time.Hour

// Idempotency replays the stored response for a repeated Idempotency-Key
// instead of performing the write twice (§16).
//
// The mechanism is a row claimed before the handler runs and completed after
// it. Three outcomes matter:
//
//   - No row: this is the first attempt. Claim it, run the handler, store the
//     response.
//   - A row with a stored response: this is a retry of a completed request.
//     Return what was returned the first time, without touching the database.
//   - A row with no response yet: the original is still in flight. Return 409
//     rather than running the handler, because two concurrent attempts both
//     proceeding is exactly the duplicate this header exists to prevent.
//
// A key reused with a *different* body is rejected outright. That is a client
// bug, and replaying the first response for it would hide the bug behind a
// plausible-looking success — the caller would believe it created the second
// thing.
func Idempotency(deps Deps) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("Idempotency-Key")
			if key == "" || len(key) > 255 {
				next(w, r)
				return
			}

			actor := authorization.FromContext(r.Context())
			if actor == nil && deps.Portal != nil {
				if token := httpserver.PortalSessionToken(r); token != "" {
					if session, sessionErr := deps.Portal.Session(r.Context(), token, portalIdentifier(r)); sessionErr == nil {
						// Idempotency is workspace-scoped bookkeeping. A portal
						// customer is not an agent actor, so use a minimal synthetic
						// actor only for the key's tenant lookup.
						actor = &authorization.Actor{WorkspaceID: session.WorkspaceID, Role: "portal"}
						r = r.WithContext(authorization.WithActor(r.Context(), actor))
					}
				}
			}
			if actor == nil {
				// Public workspace-scoped routes (forms, feedback, surveys, and
				// knowledge-base feedback) have no agent session, but they still
				// have a tenant in the URL. Use that tenant only for the
				// idempotency ledger; the owning service remains responsible for
				// validating that the resource belongs to it.
				if workspaceID := strings.TrimSpace(r.PathValue("workspaceID")); workspaceID != "" {
					actor = &authorization.Actor{WorkspaceID: workspaceID, Role: "public"}
					r = r.WithContext(authorization.WithActor(r.Context(), actor))
				}
			}
			if actor == nil {
				next(w, r)
				return
			}

			body, err := io.ReadAll(r.Body)
			if err != nil {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest,
					"The request body could not be read.")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))

			endpoint := r.Method + " " + r.URL.Path
			fingerprint := sha256.Sum256(body)

			stored, err := claimIdempotencyKey(
				r.Context(), deps.Pool, actor.WorkspaceID, key, endpoint, fingerprint[:])

			switch {
			case errors.Is(err, errIdempotencyMismatch):
				httpserver.WriteError(w, r, http.StatusUnprocessableEntity, "idempotency_key_reused",
					"This Idempotency-Key was already used with a different request body.")
				return

			case errors.Is(err, errIdempotencyInFlight):
				w.Header().Set("Retry-After", "1")
				httpserver.WriteError(w, r, http.StatusConflict, "idempotency_in_flight",
					"An earlier request with this Idempotency-Key is still being processed.")
				return

			case err != nil:
				deps.Logger.Error("idempotency claim failed",
					"error", err, "request_id", httpserver.RequestIDFrom(r.Context()))
				// Failing open is deliberate. The alternative — refusing the
				// write because bookkeeping is unavailable — turns a
				// housekeeping problem into an outage for a request that would
				// otherwise succeed.
				next(w, r)
				return
			}

			if stored != nil {
				w.Header().Set("Idempotent-Replay", "true")
				httpserver.WriteJSON(w, stored.status, json.RawMessage(stored.body))
				return
			}

			recorder := &bodyRecorder{ResponseWriter: w, status: http.StatusOK}
			next(recorder, r)

			// Only successful writes are worth replaying. A failed request
			// should be retryable in the ordinary sense: the caller fixes what
			// was wrong and sends it again, with the same key, and gets a real
			// attempt rather than its own earlier error played back forever.
			if recorder.status >= 200 && recorder.status < 300 {
				completeIdempotencyKey(r.Context(), deps, actor.WorkspaceID, key, endpoint,
					recorder.status, recorder.buf.Bytes())
				return
			}
			releaseIdempotencyKey(r.Context(), deps, actor.WorkspaceID, key, endpoint)
		}
	}
}

// UserIdempotency is the onboarding counterpart to Idempotency. Workspace
// creation is authenticated by a user session but has no workspace id yet, so
// it uses the separate user-scoped ledger rather than weakening the tenant FK
// on idempotency_keys.
func UserIdempotency(deps Deps) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("Idempotency-Key")
			if key == "" || len(key) > 255 {
				next(w, r)
				return
			}
			token := httpserver.SessionToken(r)
			if token == "" || deps.Auth == nil {
				next(w, r)
				return
			}
			user, err := deps.Auth.UserForSession(r.Context(), token)
			if err != nil || user == nil {
				next(w, r)
				return
			}

			body, err := io.ReadAll(r.Body)
			if err != nil {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "The request body could not be read.")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			endpoint := r.Method + " " + r.URL.Path
			fingerprint := sha256.Sum256(body)
			stored, err := claimUserIdempotencyKey(r.Context(), deps.Pool, user.ID, key, endpoint, fingerprint[:])
			switch {
			case errors.Is(err, errIdempotencyMismatch):
				httpserver.WriteError(w, r, http.StatusUnprocessableEntity, "idempotency_key_reused", "This Idempotency-Key was already used with a different request body.")
				return
			case errors.Is(err, errIdempotencyInFlight):
				w.Header().Set("Retry-After", "1")
				httpserver.WriteError(w, r, http.StatusConflict, "idempotency_in_flight", "An earlier request with this Idempotency-Key is still being processed.")
				return
			case err != nil:
				deps.Logger.Error("user idempotency claim failed", "error", err, "request_id", httpserver.RequestIDFrom(r.Context()))
				next(w, r)
				return
			}
			if stored != nil {
				w.Header().Set("Idempotent-Replay", "true")
				httpserver.WriteJSON(w, stored.status, json.RawMessage(stored.body))
				return
			}

			recorder := &bodyRecorder{ResponseWriter: w, status: http.StatusOK}
			next(recorder, r)
			if recorder.status >= 200 && recorder.status < 300 {
				completeUserIdempotencyKey(r.Context(), deps, user.ID, key, endpoint, recorder.status, recorder.buf.Bytes())
				return
			}
			releaseUserIdempotencyKey(r.Context(), deps, user.ID, key, endpoint)
		}
	}
}

var (
	errIdempotencyMismatch = errors.New("api: idempotency key reused with a different body")
	errIdempotencyInFlight = errors.New("api: idempotency key is still in flight")
)

type storedResponse struct {
	status int
	body   []byte
}

// claimIdempotencyKey inserts the key or reports what is already there.
//
// The INSERT ... ON CONFLICT DO NOTHING plus follow-up SELECT is one atomic
// claim: two concurrent first attempts cannot both insert, so exactly one
// proceeds and the other is told the request is in flight.
func claimIdempotencyKey(
	ctx context.Context,
	pool *database.Pool,
	workspaceID, key, endpoint string,
	fingerprint []byte,
) (*storedResponse, error) {
	var inserted string
	err := pool.QueryRow(ctx, `
		INSERT INTO idempotency_keys
			(id, workspace_id, key, endpoint, request_fingerprint, expires_at)
		VALUES ($1, $2, $3, $4, $5, now() + $6::interval)
		ON CONFLICT (workspace_id, endpoint, key) DO NOTHING
		RETURNING id
	`,
		ids.New(ids.PrefixIdempotency), workspaceID, key, endpoint, fingerprint,
		idempotencyWindow.String(),
	).Scan(&inserted)

	if err == nil {
		return nil, nil // claimed; this is the first attempt
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("api: claim idempotency key: %w", err)
	}

	var (
		existingFingerprint []byte
		status              *int
		body                []byte
	)
	err = pool.QueryRow(ctx, `
		SELECT request_fingerprint, response_status, response_body
		FROM idempotency_keys
		WHERE workspace_id = $1 AND endpoint = $2 AND key = $3
	`, workspaceID, endpoint, key).Scan(&existingFingerprint, &status, &body)
	if err != nil {
		return nil, fmt.Errorf("api: read idempotency key: %w", err)
	}

	if !bytes.Equal(existingFingerprint, fingerprint) {
		return nil, errIdempotencyMismatch
	}
	if status == nil {
		return nil, errIdempotencyInFlight
	}

	return &storedResponse{status: *status, body: body}, nil
}

func completeIdempotencyKey(
	ctx context.Context,
	deps Deps,
	workspaceID, key, endpoint string,
	status int,
	body []byte,
) {
	// The response is recorded even if the caller has gone away, so their retry
	// finds it.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if !json.Valid(body) {
		body = []byte("null")
	}

	if _, err := deps.Pool.Exec(writeCtx, `
		UPDATE idempotency_keys
		SET response_status = $4, response_body = $5
		WHERE workspace_id = $1 AND endpoint = $2 AND key = $3
	`, workspaceID, endpoint, key, status, body); err != nil {
		deps.Logger.Error("recording idempotent response failed", "error", err)
	}
}

// releaseIdempotencyKey drops the claim after a failed attempt so the caller
// can retry the same key for real.
func releaseIdempotencyKey(ctx context.Context, deps Deps, workspaceID, key, endpoint string) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if _, err := deps.Pool.Exec(writeCtx, `
		DELETE FROM idempotency_keys
		WHERE workspace_id = $1 AND endpoint = $2 AND key = $3
		  AND response_status IS NULL
	`, workspaceID, endpoint, key); err != nil {
		deps.Logger.Error("releasing idempotency key failed", "error", err)
	}
}

func claimUserIdempotencyKey(ctx context.Context, pool *database.Pool, userID, key, endpoint string, fingerprint []byte) (*storedResponse, error) {
	var inserted string
	err := pool.QueryRow(ctx, `
		INSERT INTO user_idempotency_keys
			(id, user_id, key, endpoint, request_fingerprint, expires_at)
		VALUES ($1, $2, $3, $4, $5, now() + $6::interval)
		ON CONFLICT (user_id, endpoint, key) DO NOTHING
		RETURNING id
	`, ids.New(ids.PrefixIdempotency), userID, key, endpoint, fingerprint, idempotencyWindow.String()).Scan(&inserted)
	if err == nil {
		return nil, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("api: claim user idempotency key: %w", err)
	}
	var existingFingerprint []byte
	var status *int
	var body []byte
	err = pool.QueryRow(ctx, `SELECT request_fingerprint, response_status, response_body FROM user_idempotency_keys WHERE user_id=$1 AND endpoint=$2 AND key=$3`, userID, endpoint, key).Scan(&existingFingerprint, &status, &body)
	if err != nil {
		return nil, fmt.Errorf("api: read user idempotency key: %w", err)
	}
	if !bytes.Equal(existingFingerprint, fingerprint) {
		return nil, errIdempotencyMismatch
	}
	if status == nil {
		return nil, errIdempotencyInFlight
	}
	return &storedResponse{status: *status, body: body}, nil
}

func completeUserIdempotencyKey(ctx context.Context, deps Deps, userID, key, endpoint string, status int, body []byte) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if !json.Valid(body) {
		body = []byte("null")
	}
	if _, err := deps.Pool.Exec(writeCtx, `UPDATE user_idempotency_keys SET response_status=$4,response_body=$5 WHERE user_id=$1 AND endpoint=$2 AND key=$3`, userID, endpoint, key, status, body); err != nil {
		deps.Logger.Error("recording user idempotent response failed", "error", err)
	}
}

func releaseUserIdempotencyKey(ctx context.Context, deps Deps, userID, key, endpoint string) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if _, err := deps.Pool.Exec(writeCtx, `DELETE FROM user_idempotency_keys WHERE user_id=$1 AND endpoint=$2 AND key=$3 AND response_status IS NULL`, userID, endpoint, key); err != nil {
		deps.Logger.Error("releasing user idempotency key failed", "error", err)
	}
}

// bodyRecorder captures a handler's response so it can be replayed.
type bodyRecorder struct {
	http.ResponseWriter
	status int
	buf    bytes.Buffer
}

func (r *bodyRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *bodyRecorder) Write(b []byte) (int, error) {
	r.buf.Write(b)
	return r.ResponseWriter.Write(b)
}

func (r *bodyRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
