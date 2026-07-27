-- 0004_events_jobs_ops.sql
--
-- The operational spine: the workspace event log, the durable job queue, audit
-- records, notifications, idempotency keys, and file metadata.
--
-- The event log is the load-bearing piece. Five subsystems need to know "what
-- happened, in order": WebSocket resume (§9), webhook delivery (§6.16),
-- automation triggers (§6.13), notifications (§6.15), and analytics (§6.18).
-- Giving each its own mechanism would mean five different answers to the same
-- question and five different ways to be subtly wrong. There is one log, and
-- everything reads from it.

BEGIN;

-- -------------------------------------------------------- event sequences

-- The per-workspace sequence counter, in its own table rather than a column on
-- `workspaces`.
--
-- Every event append takes a row lock here and holds it until the transaction
-- commits. That is deliberate, and it is the whole reason resume works:
-- because the lock is held to commit, sequence order and commit order are the
-- same. Without it a reader could observe sequence 10 while 9 was still in
-- flight, resume from 10, and never see 9 — a silently dropped message, which
-- is the worst failure this system can have.
--
-- The cost is that event appends within one workspace serialise. That is an
-- acceptable trade for a support product (one workspace's writes are human-
-- paced) and it is why this counter lives here instead of on `workspaces`,
-- where the lock would collide with every unrelated workspace read.
CREATE TABLE workspace_event_sequences (
    workspace_id  text   PRIMARY KEY REFERENCES workspaces (id) ON DELETE CASCADE,
    next_sequence bigint NOT NULL DEFAULT 1
);

-- ---------------------------------------------------------- workspace events

-- Append-only. Nothing updates a row here; retention deletes whole ranges.
CREATE TABLE workspace_events (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    -- Monotonic within the workspace. The resume cursor every realtime client
    -- and webhook subscription tracks.
    sequence     bigint      NOT NULL,
    -- Dotted event name from the closed set the API documents, e.g.
    -- 'message.created'. §16 forbids changing what an existing type means;
    -- a new meaning gets a new type.
    type         text        NOT NULL,
    entity_type  text,
    entity_id    text,
    actor_type   text        NOT NULL DEFAULT 'system',
    actor_id     text,
    -- The event payload as delivered to webhooks and realtime clients.
    data         jsonb       NOT NULL DEFAULT '{}'::jsonb,
    -- Set when this event was produced by an automation acting on another
    -- event. The rule engine walks the chain to enforce its depth cap and
    -- detect loops (§26.7).
    causation_id text        REFERENCES workspace_events (id) ON DELETE SET NULL,
    request_id   text,
    occurred_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT workspace_events_actor_type CHECK (
        actor_type IN ('user', 'customer', 'visitor', 'system', 'automation', 'api_key')
    )
);

-- "Everything after sequence N for this workspace" — the resume query, run on
-- every WebSocket reconnect and every webhook retry.
CREATE UNIQUE INDEX workspace_events_by_sequence
    ON workspace_events (workspace_id, sequence);

-- "What happened to this conversation / ticket / customer" — the activity
-- timeline, and how automation finds the subject of a trigger.
CREATE INDEX workspace_events_by_entity
    ON workspace_events (workspace_id, entity_type, entity_id, sequence DESC)
    WHERE entity_type IS NOT NULL;

-- The retention job's scan (§12 retention by data category).
CREATE INDEX workspace_events_by_occurred_at
    ON workspace_events (occurred_at);

-- ------------------------------------------------------------------- jobs

-- Durable background work. PostgreSQL is the queue (ADR-0002) — there is no
-- Redis and no broker to operate.
CREATE TABLE jobs (
    id            text        PRIMARY KEY,
    -- Nullable: some work (retention sweeps, orphaned-upload cleanup) is not
    -- owned by any one tenant.
    workspace_id  text        REFERENCES workspaces (id) ON DELETE CASCADE,
    queue         text        NOT NULL DEFAULT 'default',
    type          text        NOT NULL,
    payload       jsonb       NOT NULL DEFAULT '{}'::jsonb,
    state         text        NOT NULL DEFAULT 'pending',
    priority      smallint    NOT NULL DEFAULT 0,
    attempt       integer     NOT NULL DEFAULT 0,
    max_attempts  integer     NOT NULL DEFAULT 5,
    -- When this job becomes eligible. Backoff is expressed by pushing this
    -- forward rather than by sleeping in a worker.
    scheduled_at  timestamptz NOT NULL DEFAULT now(),
    -- Held by whichever worker claimed the job. A worker that dies without
    -- releasing its claim is reclaimed once this passes (§8.7 lease and
    -- heartbeat) — no separate reaper process required.
    leased_until  timestamptz,
    leased_by     text,
    -- Set for jobs that must not be enqueued twice (a webhook delivery for one
    -- event, a scheduled message). The unique index below makes that a
    -- database guarantee rather than a check-then-insert race.
    dedupe_key    text,
    last_error    text,
    started_at    timestamptz,
    finished_at   timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT jobs_state CHECK (
        state IN ('pending', 'running', 'succeeded', 'failed', 'dead', 'cancelled')
    )
);

-- The claim query: "the highest-priority eligible job, oldest first". Partial,
-- because finished jobs vastly outnumber pending ones and are never claimed.
-- Workers read this with FOR UPDATE SKIP LOCKED so N workers do not contend.
CREATE INDEX jobs_claimable
    ON jobs (queue, priority DESC, scheduled_at)
    WHERE state = 'pending';

-- Reclaiming jobs whose worker died mid-flight.
CREATE INDEX jobs_expired_leases
    ON jobs (leased_until)
    WHERE state = 'running';

-- Per-workspace fairness (§8.7): "how much of the queue is this tenant using?"
CREATE INDEX jobs_by_workspace
    ON jobs (workspace_id, state, scheduled_at)
    WHERE workspace_id IS NOT NULL;

CREATE UNIQUE INDEX jobs_dedupe
    ON jobs (queue, dedupe_key)
    WHERE dedupe_key IS NOT NULL AND state IN ('pending', 'running');

-- The admin "why did this fail" view (§8.7 admin inspection and retry). Kept
-- separate from `jobs` so a job row stays small and its history survives the
-- retry that overwrites `last_error`.
CREATE TABLE job_attempts (
    id          text        PRIMARY KEY,
    job_id      text        NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
    attempt     integer     NOT NULL,
    outcome     text        NOT NULL,
    error       text,
    duration_ms integer,
    started_at  timestamptz NOT NULL,
    finished_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT job_attempts_outcome CHECK (
        outcome IN ('succeeded', 'failed', 'timeout', 'lease_expired')
    )
);

CREATE INDEX job_attempts_by_job ON job_attempts (job_id, attempt);

-- ------------------------------------------------------------- audit logs

-- Append-only and deliberately denormalised: `actor_name` and `entity_type`
-- are copied in rather than joined, because an audit record must still read
-- correctly after the member is removed and the entity is deleted. That is the
-- entire point of keeping it (§6.19).
CREATE TABLE audit_logs (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    actor_type   text        NOT NULL,
    actor_id     text,
    actor_name   text        NOT NULL DEFAULT '',
    action       text        NOT NULL,
    entity_type  text,
    entity_id    text,
    request_id   text,
    -- Stored per the workspace's IP policy (§12). Null when the workspace has
    -- IP retention switched off.
    ip           inet,
    -- A description of what changed, never the changed values themselves when
    -- they are sensitive (§19 logging rules).
    metadata     jsonb       NOT NULL DEFAULT '{}'::jsonb,
    occurred_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT audit_logs_actor_type CHECK (
        actor_type IN ('user', 'customer', 'system', 'automation', 'api_key')
    )
);

-- The audit log screen: newest first, filtered by actor or entity.
CREATE INDEX audit_logs_by_workspace
    ON audit_logs (workspace_id, occurred_at DESC);

CREATE INDEX audit_logs_by_actor
    ON audit_logs (workspace_id, actor_id, occurred_at DESC)
    WHERE actor_id IS NOT NULL;

CREATE INDEX audit_logs_by_entity
    ON audit_logs (workspace_id, entity_type, entity_id, occurred_at DESC)
    WHERE entity_type IS NOT NULL;

-- ---------------------------------------------------------- notifications

CREATE TABLE notifications (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    -- Who sees it. Notifications are per-member, not per-user: the same person
    -- in two workspaces has two independent inboxes.
    member_id    text        NOT NULL REFERENCES workspace_members (id) ON DELETE CASCADE,
    type         text        NOT NULL,
    title        text        NOT NULL,
    body         text        NOT NULL DEFAULT '',
    entity_type  text,
    entity_id    text,
    url          text,
    read_at      timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- The bell menu, and the unread badge count.
CREATE INDEX notifications_by_member
    ON notifications (member_id, created_at DESC);

CREATE INDEX notifications_unread
    ON notifications (member_id)
    WHERE read_at IS NULL;

-- ------------------------------------------------------- idempotency keys

-- Backs the Idempotency-Key header (§16). A retried create returns the stored
-- response instead of creating a second resource.
--
-- `request_fingerprint` catches the dangerous case: the same key reused with a
-- different body. That is a client bug, and returning the first response for
-- it would hide the bug behind a plausible-looking success.
CREATE TABLE idempotency_keys (
    id                  text        PRIMARY KEY,
    workspace_id        text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    key                 text        NOT NULL,
    endpoint            text        NOT NULL,
    request_fingerprint bytea       NOT NULL,
    -- Null while the first request is still in flight; a concurrent retry sees
    -- the row, finds no response, and is told to retry rather than being let
    -- through to duplicate the work.
    response_status     integer,
    response_body       jsonb,
    created_at          timestamptz NOT NULL DEFAULT now(),
    expires_at          timestamptz NOT NULL
);

CREATE UNIQUE INDEX idempotency_keys_lookup
    ON idempotency_keys (workspace_id, endpoint, key);

CREATE INDEX idempotency_keys_expiry ON idempotency_keys (expires_at);

-- ------------------------------------------------------------------ files

-- Metadata only. Bytes live on local disk or in S3 (§10); this table is the
-- authorisation boundary in front of them, which is why every read path joins
-- here before handing out a URL.
CREATE TABLE files (
    id            text        PRIMARY KEY,
    workspace_id  text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    -- Random, never derived from the uploaded name. A user-supplied name in a
    -- storage key is how path traversal happens (§10.3).
    storage_key   text        NOT NULL,
    backend       text        NOT NULL DEFAULT 'local',
    -- The original name, preserved for display and download only. Never used
    -- to build a path.
    name          text        NOT NULL,
    mime_type     text        NOT NULL,
    size_bytes    bigint      NOT NULL,
    checksum      bytea,
    -- What the file hangs off: a message, an article, a form submission, a
    -- workspace logo. Nullable while an upload is still in progress.
    owner_type    text,
    owner_id      text,
    uploaded_by_type text     NOT NULL DEFAULT 'user',
    uploaded_by_id   text,
    -- Set once the upload completes. Rows that never reach this state are
    -- swept by the abandoned-upload job (§18).
    committed_at  timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT files_backend CHECK (backend IN ('local', 's3')),
    CONSTRAINT files_size_positive CHECK (size_bytes >= 0)
);

CREATE UNIQUE INDEX files_storage_key ON files (backend, storage_key);

CREATE INDEX files_by_owner
    ON files (workspace_id, owner_type, owner_id)
    WHERE owner_type IS NOT NULL;

-- The abandoned-upload sweep.
CREATE INDEX files_uncommitted
    ON files (created_at)
    WHERE committed_at IS NULL;

-- Message attachments. A join table rather than a column on `files` because
-- §6.2 allows the same file to appear on a quoted reply and its original.
CREATE TABLE message_attachments (
    message_id text     NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
    file_id    text     NOT NULL REFERENCES files (id) ON DELETE CASCADE,
    position   smallint NOT NULL DEFAULT 0,

    PRIMARY KEY (message_id, file_id)
);

CREATE INDEX message_attachments_by_file ON message_attachments (file_id);

COMMIT;
