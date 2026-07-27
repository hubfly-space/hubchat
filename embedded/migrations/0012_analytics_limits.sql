-- 0012_analytics_limits.sql
--
-- Report rollups, saved reports, usage counters, and workspace limits.
--
-- §6.18 requires deterministic aggregation from stored events — no sampling,
-- no estimation, and no AI. That makes reporting a scheduled fold over
-- `workspace_events` and the operational tables into `report_rollups`, from
-- which every chart is a range scan.
--
-- Computing metrics on demand was the alternative and is rejected: "average
-- first response time this quarter, by team" over a year of messages is a
-- query no support team should wait for, and it would run again on every
-- dashboard refresh.

BEGIN;

-- --------------------------------------------------------- report rollups

-- One row per (metric, bucket, dimension) per workspace.
--
-- Dimensions live in a jsonb column rather than as columns because the set
-- differs per metric — conversation volume splits by channel, satisfaction by
-- agent — and adding a dimension should not be a migration. The unique index
-- below is what makes a rollup re-runnable: recomputing a day upserts over it
-- rather than doubling it.
CREATE TABLE report_rollups (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    -- e.g. 'conversations.created', 'sla.first_response_seconds',
    -- 'survey.csat'. The definition shown next to each chart (§6.18) is keyed
    -- off this.
    metric       text        NOT NULL,
    -- 'hour' | 'day' | 'week' | 'month'. Several grains are stored rather than
    -- derived so a year-long chart does not fold 8760 hourly rows per series.
    grain        text        NOT NULL,
    -- Bucket start, always UTC. Timezone-aware reporting shifts the query
    -- window, never the stored bucket.
    bucket       timestamptz NOT NULL,
    dimensions   jsonb       NOT NULL DEFAULT '{}'::jsonb,
    -- Both are kept so an average can be recomputed when buckets are merged
    -- into a coarser grain. Storing only a mean would make that impossible.
    value        double precision NOT NULL DEFAULT 0,
    count        integer     NOT NULL DEFAULT 0,
    computed_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT report_rollups_grain CHECK (
        grain IN ('hour', 'day', 'week', 'month')
    )
);

-- The upsert target, and the chart query: one metric, one grain, over a range.
CREATE UNIQUE INDEX report_rollups_unique
    ON report_rollups (workspace_id, metric, grain, bucket, dimensions);

CREATE INDEX report_rollups_by_metric
    ON report_rollups (workspace_id, metric, grain, bucket DESC);

-- How far each metric has been folded. The aggregation job reads this to know
-- where to resume, so a restart recomputes one partial bucket rather than the
-- whole history.
CREATE TABLE report_rollup_state (
    workspace_id     text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    metric           text        NOT NULL,
    grain            text        NOT NULL,
    -- The last event sequence folded in. Ties the rollup back to the event log
    -- so "is this chart current?" has an exact answer.
    last_sequence    bigint      NOT NULL DEFAULT 0,
    last_bucket      timestamptz,
    updated_at       timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (workspace_id, metric, grain)
);

-- --------------------------------------------------------- saved reports

CREATE TABLE saved_reports (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         text        NOT NULL,
    description  text,
    -- Which metrics, over what range, with which filters and grouping.
    definition   jsonb       NOT NULL DEFAULT '{}'::jsonb,
    -- Relative ranges ('last_30_days') survive being saved; absolute ones are
    -- stored as dates in `definition`.
    date_range   text        NOT NULL DEFAULT 'last_30_days',
    timezone     text,
    -- Which roles may open it (§6.18 role-based visibility). Empty means
    -- anyone who can read reports at all.
    visible_to_roles text[]  NOT NULL DEFAULT '{}',
    owner_id     text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (workspace_id, name)
);

CREATE INDEX saved_reports_by_workspace ON saved_reports (workspace_id, name);

-- A saved report mailed on a cron-like cadence (§6.18 scheduled email report).
CREATE TABLE report_schedules (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    report_id    text        NOT NULL REFERENCES saved_reports (id) ON DELETE CASCADE,
    -- 'daily' | 'weekly' | 'monthly', with the hour and weekday in `options`.
    cadence      text        NOT NULL,
    options      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    -- Free-form addresses rather than member ids: recipients are often people
    -- who never sign in, which is the point of mailing a report.
    recipients   text[]      NOT NULL DEFAULT '{}',
    format       text        NOT NULL DEFAULT 'csv',
    enabled      boolean     NOT NULL DEFAULT true,
    last_sent_at timestamptz,
    next_run_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT report_schedules_cadence CHECK (
        cadence IN ('daily', 'weekly', 'monthly')
    ),
    CONSTRAINT report_schedules_format CHECK (format IN ('csv', 'pdf'))
);

CREATE INDEX report_schedules_due
    ON report_schedules (next_run_at)
    WHERE enabled AND next_run_at IS NOT NULL;

-- -------------------------------------------------------- usage counters

-- What a workspace is consuming, per period. §23 asks for usage and limits to
-- be modelled cleanly even though the open-source edition bills nobody — so
-- that entitlement checks live in one place rather than scattering plan logic
-- across every module later.
CREATE TABLE usage_counters (
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    -- 'conversations' | 'tickets' | 'events' | 'storage_bytes' |
    -- 'api_requests' | 'monthly_active_contacts' | …
    metric       text        NOT NULL,
    -- First day of the month for monthly counters; epoch for lifetime ones.
    period       date        NOT NULL,
    value        bigint      NOT NULL DEFAULT 0,
    updated_at   timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (workspace_id, metric, period)
);

CREATE INDEX usage_counters_by_period ON usage_counters (period, metric);

-- Per-workspace overrides. A null value means "no limit"; a missing row means
-- the deployment default applies. Both are needed: the open-source edition
-- ships with no limits at all, and an operator may still want to cap one
-- tenant.
CREATE TABLE workspace_limits (
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    key          text        NOT NULL,
    value        bigint,
    note         text,
    updated_at   timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (workspace_id, key)
);

-- --------------------------------------------------------- feature flags

-- Deployment-wide when workspace_id is null, per-tenant otherwise. Used for
-- staged rollout of new surfaces, not for A/B testing.
CREATE TABLE feature_flags (
    id           text        PRIMARY KEY,
    workspace_id text        REFERENCES workspaces (id) ON DELETE CASCADE,
    key          text        NOT NULL,
    enabled      boolean     NOT NULL DEFAULT false,
    note         text,
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- Two partial indexes rather than one over (workspace_id, key): a unique index
-- treats NULLs as distinct, so the global rows would not be deduplicated by a
-- plain composite. The same trap 0003 documents for `roles`.
CREATE UNIQUE INDEX feature_flags_global
    ON feature_flags (key)
    WHERE workspace_id IS NULL;

CREATE UNIQUE INDEX feature_flags_scoped
    ON feature_flags (workspace_id, key)
    WHERE workspace_id IS NOT NULL;

-- ------------------------------------------------------ export requests

-- §6.20 workspace export and §12 customer export. Generation is a background
-- job that can take minutes, so the request is a tracked row and the result is
-- a file with an expiry.
CREATE TABLE export_requests (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    kind         text        NOT NULL,
    -- Narrows the export: a customer id, a date range, selected entities.
    scope        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    format       text        NOT NULL DEFAULT 'csv',
    state        text        NOT NULL DEFAULT 'pending',
    file_id      text        REFERENCES files (id) ON DELETE SET NULL,
    row_count    bigint,
    error        text,
    requested_by text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    -- Exports contain everything the requester could read, so they do not live
    -- forever (§12).
    expires_at   timestamptz,
    completed_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT export_requests_state CHECK (
        state IN ('pending', 'running', 'completed', 'failed', 'expired')
    ),
    CONSTRAINT export_requests_format CHECK (
        format IN ('csv', 'json', 'zip', 'markdown')
    )
);

CREATE INDEX export_requests_by_workspace
    ON export_requests (workspace_id, created_at DESC);

CREATE INDEX export_requests_expiry
    ON export_requests (expires_at)
    WHERE state = 'completed' AND expires_at IS NOT NULL;

-- The mirror of export: a file being read in (§6.20 imports).
CREATE TABLE import_requests (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    kind         text        NOT NULL,
    file_id      text        REFERENCES files (id) ON DELETE SET NULL,
    -- Column-to-field mapping confirmed by the operator before the run starts.
    mapping      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    state        text        NOT NULL DEFAULT 'pending',
    total_rows   integer,
    processed_rows integer   NOT NULL DEFAULT 0,
    failed_rows  integer     NOT NULL DEFAULT 0,
    -- Per-row failures, capped, so a bad import is diagnosable without
    -- re-running it.
    errors       jsonb       NOT NULL DEFAULT '[]'::jsonb,
    requested_by text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    completed_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT import_requests_state CHECK (
        state IN ('pending', 'validating', 'running', 'completed', 'failed', 'cancelled')
    )
);

CREATE INDEX import_requests_by_workspace
    ON import_requests (workspace_id, created_at DESC);

COMMIT;
