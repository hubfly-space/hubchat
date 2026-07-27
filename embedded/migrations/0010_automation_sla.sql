-- 0010_automation_sla.sql
--
-- The deterministic rules engine, business-hours calendars, and SLA policies.
--
-- §26.7 names rule loops as a top technical risk: a rule that assigns a ticket
-- fires "ticket updated", which triggers a rule that tags it, which fires
-- "ticket updated" again. The defence is structural and lives in the schema —
-- every execution records its `depth` and `causation_id`, so the engine can
-- refuse to go deeper rather than discovering the loop by running out of
-- database connections.
--
-- SLA timers are the other thing that has to be right here. They are stored as
-- explicit instances with their own paused/running state rather than being
-- recomputed from ticket history on each read: §6.14 requires the interface to
-- explain plainly why a timer is paused, and a derived value cannot say.

BEGIN;

-- ------------------------------------------------- business hours calendars

CREATE TABLE business_hour_calendars (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         text        NOT NULL,
    -- IANA zone. SLA maths happens in this zone, not the workspace's, because
    -- a follow-the-sun team has one calendar per region.
    timezone     text        NOT NULL DEFAULT 'UTC',
    -- Seven entries, Monday first, each an array of { start, end } in local
    -- "HH:MM". An empty array is a non-working day. Stored as jsonb because
    -- the database never reasons about it — the SLA service does, and it needs
    -- the whole week in memory anyway to walk forward over a weekend.
    weekly       jsonb       NOT NULL DEFAULT '[]'::jsonb,
    is_default   boolean     NOT NULL DEFAULT false,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (workspace_id, name)
);

CREATE UNIQUE INDEX business_hour_calendars_single_default
    ON business_hour_calendars (workspace_id)
    WHERE is_default;

CREATE TABLE calendar_holidays (
    id          text        PRIMARY KEY,
    calendar_id text        NOT NULL REFERENCES business_hour_calendars (id) ON DELETE CASCADE,
    name        text        NOT NULL,
    -- A single date. Recurring holidays are expanded on save rather than
    -- stored as a rule, so "why was the clock stopped that day" is answerable
    -- by looking at one row.
    date        date        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),

    UNIQUE (calendar_id, date)
);

CREATE INDEX calendar_holidays_by_date ON calendar_holidays (calendar_id, date);

-- ------------------------------------------------------------ sla policies

CREATE TABLE sla_policies (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         text        NOT NULL,
    description  text,
    calendar_id  text        REFERENCES business_hour_calendars (id) ON DELETE SET NULL,
    -- States in which the clock stops, e.g. waiting_for_customer. §6.14
    -- requires this to be explicit rather than inferred, because "are we
    -- waiting on them or on us" is the whole question an SLA answers.
    pause_states text[]      NOT NULL DEFAULT '{waiting_for_customer}',
    -- Percentage of the target at which the conversation starts showing as
    -- "approaching" rather than "active".
    warning_threshold_percent smallint NOT NULL DEFAULT 80,
    -- What happens on breach: notify, reassign, escalate priority.
    escalation_actions jsonb  NOT NULL DEFAULT '[]'::jsonb,
    -- Which work this policy governs: inbox ids, customer tiers, ticket types.
    applies_to   jsonb       NOT NULL DEFAULT '{}'::jsonb,
    enabled      boolean     NOT NULL DEFAULT true,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (workspace_id, name),
    CONSTRAINT sla_policies_warning_threshold CHECK (
        warning_threshold_percent BETWEEN 1 AND 100
    )
);

CREATE INDEX sla_policies_by_workspace
    ON sla_policies (workspace_id)
    WHERE enabled;

-- Targets vary by priority, so they are rows rather than columns — adding a
-- fifth priority should not be a migration on `sla_policies`.
CREATE TABLE sla_policy_targets (
    id         text     PRIMARY KEY,
    policy_id  text     NOT NULL REFERENCES sla_policies (id) ON DELETE CASCADE,
    priority   text     NOT NULL,
    -- Null means "no target of this kind at this priority", which is different
    -- from zero.
    first_response_minutes integer,
    next_response_minutes  integer,
    resolution_minutes     integer,

    UNIQUE (policy_id, priority),
    CONSTRAINT sla_policy_targets_priority CHECK (
        priority IN ('low', 'normal', 'high', 'urgent')
    )
);

-- The 0002 and 0005 tables referenced `sla_policy_id` before this table
-- existed. Closing the loop now, the same way 0005 did for ticket ids.
ALTER TABLE inboxes
    ADD CONSTRAINT inboxes_sla_policy_id_fkey
    FOREIGN KEY (sla_policy_id) REFERENCES sla_policies (id) ON DELETE SET NULL;

ALTER TABLE companies
    ADD CONSTRAINT companies_sla_policy_id_fkey
    FOREIGN KEY (sla_policy_id) REFERENCES sla_policies (id) ON DELETE SET NULL;

ALTER TABLE tickets
    ADD CONSTRAINT tickets_sla_policy_id_fkey
    FOREIGN KEY (sla_policy_id) REFERENCES sla_policies (id) ON DELETE SET NULL;

-- A live timer attached to one conversation or ticket.
--
-- `elapsed_minutes` accumulates only while running, and `deadline_at` is
-- recomputed whenever the timer resumes. Storing both means the countdown in
-- the conversation list is a single indexed read rather than a walk over the
-- status history of every row on screen.
CREATE TABLE sla_instances (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    policy_id    text        NOT NULL REFERENCES sla_policies (id) ON DELETE CASCADE,
    conversation_id text     REFERENCES conversations (id) ON DELETE CASCADE,
    ticket_id    text        REFERENCES tickets (id) ON DELETE CASCADE,
    -- Which of the three targets this instance tracks. One conversation can
    -- have a first-response and a resolution timer running at once.
    kind         text        NOT NULL,
    state        text        NOT NULL DEFAULT 'active',
    target_minutes integer   NOT NULL,
    elapsed_minutes integer  NOT NULL DEFAULT 0,
    -- Wall-clock instant the target is missed, accounting for business hours
    -- and current pauses. Null while paused.
    deadline_at  timestamptz,
    paused_at    timestamptz,
    paused_reason text,
    started_at   timestamptz NOT NULL DEFAULT now(),
    satisfied_at timestamptz,
    breached_at  timestamptz,
    -- Set once the "approaching" notification has gone out, so it is not sent
    -- on every scheduler tick.
    warned_at    timestamptz,

    CONSTRAINT sla_instances_kind CHECK (
        kind IN ('first_response', 'next_response', 'resolution')
    ),
    CONSTRAINT sla_instances_state CHECK (
        state IN ('active', 'paused', 'met', 'breached', 'cancelled')
    ),
    CONSTRAINT sla_instances_one_subject CHECK (
        (conversation_id IS NOT NULL) <> (ticket_id IS NOT NULL)
    )
);

-- The scheduler's sweep: "which timers breach or warn next?" Partial, because
-- satisfied and breached timers never need looking at again.
CREATE INDEX sla_instances_pending
    ON sla_instances (deadline_at)
    WHERE state = 'active' AND deadline_at IS NOT NULL;

CREATE INDEX sla_instances_by_conversation
    ON sla_instances (conversation_id)
    WHERE conversation_id IS NOT NULL;

CREATE INDEX sla_instances_by_ticket
    ON sla_instances (ticket_id)
    WHERE ticket_id IS NOT NULL;

-- Breach and compliance reporting.
CREATE INDEX sla_instances_by_workspace
    ON sla_instances (workspace_id, kind, started_at DESC);

-- ------------------------------------------------------- automation rules

CREATE TABLE automation_rules (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         text        NOT NULL,
    description  text,
    -- One trigger per rule. A rule that fires on three different events is
    -- three rules; §6.13 favours rules that can be read and reasoned about
    -- individually over compact ones.
    trigger      text        NOT NULL,
    -- A FilterGroup, the same grammar saved views use.
    conditions   jsonb       NOT NULL DEFAULT '{}'::jsonb,
    actions      jsonb       NOT NULL DEFAULT '[]'::jsonb,
    -- Lower runs first. Ties break on created_at so ordering is total and
    -- stable across restarts.
    position     smallint    NOT NULL DEFAULT 0,
    enabled      boolean     NOT NULL DEFAULT false,
    -- Per-rule cap, checked against recent executions before firing (§6.13
    -- rule safety: per-rule rate limits).
    max_runs_per_hour integer,
    version      integer     NOT NULL DEFAULT 1,
    last_run_at  timestamptz,
    created_by   text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT automation_rules_trigger CHECK (
        trigger IN ('conversation.created', 'message.received', 'ticket.created',
                    'ticket.updated', 'customer.identified', 'customer.updated',
                    'event.received', 'form.submitted', 'feedback.submitted',
                    'sla.approaching', 'sla.breached', 'conversation.idle',
                    'business_hours.changed', 'schedule')
    ),
    CONSTRAINT automation_rules_rate_limit CHECK (
        max_runs_per_hour IS NULL OR max_runs_per_hour > 0
    )
);

-- The engine's hot path: "which enabled rules listen for this event type?",
-- run once per event in the log.
CREATE INDEX automation_rules_by_trigger
    ON automation_rules (workspace_id, trigger, position)
    WHERE enabled;

-- §6.13 requires version history and rollback. Whole snapshots, not diffs, for
-- the same reason widget config versions are.
CREATE TABLE automation_rule_versions (
    id         text        PRIMARY KEY,
    rule_id    text        NOT NULL REFERENCES automation_rules (id) ON DELETE CASCADE,
    version    integer     NOT NULL,
    name       text        NOT NULL,
    trigger    text        NOT NULL,
    conditions jsonb       NOT NULL DEFAULT '{}'::jsonb,
    actions    jsonb       NOT NULL DEFAULT '[]'::jsonb,
    changed_by text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    note       text,
    created_at timestamptz NOT NULL DEFAULT now(),

    UNIQUE (rule_id, version)
);

-- Every evaluation, including the ones that matched nothing and the dry runs.
--
-- `depth` and `causation_id` are the loop defence (§26.7). A rule fired by an
-- event that a rule produced inherits depth + 1, and the engine refuses past
-- cfg.Limits.MaxRuleDepth. Recording it rather than tracking it in memory means
-- the limit still holds when the chain spans two processes.
CREATE TABLE automation_executions (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    rule_id      text        NOT NULL REFERENCES automation_rules (id) ON DELETE CASCADE,
    rule_version integer     NOT NULL,
    -- The event that triggered this evaluation.
    event_id     text        REFERENCES workspace_events (id) ON DELETE SET NULL,
    subject_type text,
    subject_id   text,
    outcome      text        NOT NULL,
    depth        smallint    NOT NULL DEFAULT 0,
    causation_id text,
    actions_applied jsonb    NOT NULL DEFAULT '[]'::jsonb,
    error        text,
    duration_ms  integer,
    -- True for a test run from the rule builder: conditions are evaluated and
    -- actions are reported, but nothing is written (§6.13 dry-run testing).
    dry_run      boolean     NOT NULL DEFAULT false,
    occurred_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT automation_executions_outcome CHECK (
        outcome IN ('matched', 'skipped', 'failed', 'rate_limited', 'depth_exceeded')
    )
);

-- The execution log screen.
CREATE INDEX automation_executions_by_rule
    ON automation_executions (rule_id, occurred_at DESC);

CREATE INDEX automation_executions_by_workspace
    ON automation_executions (workspace_id, occurred_at DESC);

-- The rate-limit check: "how often has this rule fired recently?"
CREATE INDEX automation_executions_recent
    ON automation_executions (rule_id, occurred_at DESC)
    WHERE outcome = 'matched' AND NOT dry_run;

-- Following a causation chain when investigating a loop.
CREATE INDEX automation_executions_by_causation
    ON automation_executions (causation_id)
    WHERE causation_id IS NOT NULL;

-- --------------------------------------------------------- scheduled work

-- Deferred actions: a snooze waking up, a scheduled follow-up, a rule's
-- "close after inactivity", a survey sent 30 minutes after resolution.
--
-- Distinct from `jobs`: a job is work the system owes itself and retries until
-- it succeeds, while a scheduled action is a domain decision that a user can
-- see and cancel. Merging them would put "send this follow-up on Tuesday" in
-- the same list as "retry this webhook".
CREATE TABLE scheduled_actions (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    kind         text        NOT NULL,
    subject_type text        NOT NULL,
    subject_id   text        NOT NULL,
    payload      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    scheduled_for timestamptz NOT NULL,
    state        text        NOT NULL DEFAULT 'pending',
    created_by   text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    executed_at  timestamptz,
    cancelled_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT scheduled_actions_state CHECK (
        state IN ('pending', 'executed', 'cancelled', 'failed')
    )
);

-- The scheduler's tick.
CREATE INDEX scheduled_actions_due
    ON scheduled_actions (scheduled_for)
    WHERE state = 'pending';

-- "What is scheduled against this conversation?" — so the UI can show and
-- cancel it.
CREATE INDEX scheduled_actions_by_subject
    ON scheduled_actions (workspace_id, subject_type, subject_id)
    WHERE state = 'pending';

COMMIT;
