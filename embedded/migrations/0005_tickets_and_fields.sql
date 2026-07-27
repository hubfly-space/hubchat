-- 0005_tickets_and_fields.sql
--
-- Tickets, the custom field system, saved views, macros, and saved replies.
--
-- A ticket and a conversation are separate rows that reference each other
-- rather than one table with a discriminator. §4 allows a live conversation to
-- become a ticket and a ticket to contain a live conversation, so neither owns
-- the other; collapsing them would force every conversation to carry twenty
-- null ticket columns and every ticket to pretend it has a message timeline.

BEGIN;

-- ---------------------------------------------------------------- tickets

CREATE TABLE tickets (
    id            text        PRIMARY KEY,
    workspace_id  text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    -- The display number (§6.3). Immutable ids and human-facing numbers are
    -- kept separate on purpose: the number is per-workspace, reusable-looking,
    -- and safe to print; the id is global and opaque.
    number        integer     NOT NULL,
    prefix        text        NOT NULL,
    title         text        NOT NULL,
    description   text        NOT NULL DEFAULT '',
    status        text        NOT NULL DEFAULT 'new',
    priority      text        NOT NULL DEFAULT 'normal',
    type          text,
    customer_id   text        REFERENCES customers (id) ON DELETE SET NULL,
    company_id    text        REFERENCES companies (id) ON DELETE SET NULL,
    inbox_id      text        REFERENCES inboxes (id) ON DELETE SET NULL,
    channel       text        NOT NULL DEFAULT 'manual',
    assignee_id   text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    team_id       text        REFERENCES teams (id) ON DELETE SET NULL,
    conversation_id text      REFERENCES conversations (id) ON DELETE SET NULL,
    parent_id     text        REFERENCES tickets (id) ON DELETE SET NULL,
    sla_policy_id text,
    due_at        timestamptz,
    -- Set once and never cleared on reopen; reporting needs "was this ever
    -- resolved" and "how many times was it reopened" to be different questions.
    first_resolved_at timestamptz,
    resolved_at   timestamptz,
    closed_at     timestamptz,
    reopen_count  integer     NOT NULL DEFAULT 0,
    version       integer     NOT NULL DEFAULT 1,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT tickets_status CHECK (
        status IN ('new', 'open', 'pending', 'on_hold', 'resolved', 'closed')
    ),
    CONSTRAINT tickets_priority CHECK (
        priority IN ('low', 'normal', 'high', 'urgent')
    ),
    CONSTRAINT tickets_channel CHECK (
        channel IN ('widget', 'portal', 'email', 'form', 'api', 'manual')
    ),
    -- A ticket cannot be its own parent. Deeper cycles are prevented in the
    -- service layer, but the one-hop case is cheap to catch here.
    CONSTRAINT tickets_parent_not_self CHECK (parent_id IS DISTINCT FROM id)
);

-- "SUP-1042" resolves to exactly one ticket.
CREATE UNIQUE INDEX tickets_by_number ON tickets (workspace_id, number);

-- The ticket queue, newest activity first. Partial for the same reason as the
-- conversation queue: closed tickets dominate the table and are never in it.
CREATE INDEX tickets_active_queue
    ON tickets (workspace_id, status, updated_at DESC)
    WHERE status NOT IN ('closed');

CREATE INDEX tickets_by_assignee
    ON tickets (workspace_id, assignee_id, updated_at DESC)
    WHERE status NOT IN ('closed');

CREATE INDEX tickets_by_team
    ON tickets (workspace_id, team_id, updated_at DESC)
    WHERE status NOT IN ('closed');

CREATE INDEX tickets_by_customer
    ON tickets (workspace_id, customer_id, created_at DESC);

CREATE INDEX tickets_by_company
    ON tickets (workspace_id, company_id, created_at DESC)
    WHERE company_id IS NOT NULL;

-- The SLA scheduler's scan: "what is due soonest and not yet resolved".
CREATE INDEX tickets_by_due
    ON tickets (workspace_id, due_at)
    WHERE due_at IS NOT NULL AND status NOT IN ('resolved', 'closed');

CREATE INDEX tickets_by_parent
    ON tickets (parent_id)
    WHERE parent_id IS NOT NULL;

CREATE INDEX tickets_search
    ON tickets USING gin (to_tsvector('english', title || ' ' || description));

-- The reverse of `conversations.ticket_id`, which 0002 left unconstrained
-- because `tickets` did not exist yet. Adding it now closes the loop.
ALTER TABLE conversations
    ADD CONSTRAINT conversations_ticket_id_fkey
    FOREIGN KEY (ticket_id) REFERENCES tickets (id) ON DELETE SET NULL;

CREATE INDEX conversations_by_ticket
    ON conversations (ticket_id)
    WHERE ticket_id IS NOT NULL;

-- Non-hierarchical relationships: duplicates, blockers, "see also".
CREATE TABLE ticket_links (
    id          text        PRIMARY KEY,
    workspace_id text       NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    source_id   text        NOT NULL REFERENCES tickets (id) ON DELETE CASCADE,
    target_id   text        NOT NULL REFERENCES tickets (id) ON DELETE CASCADE,
    relation    text        NOT NULL DEFAULT 'related',
    created_by  text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ticket_links_relation CHECK (
        relation IN ('related', 'duplicate_of', 'blocks', 'blocked_by')
    ),
    CONSTRAINT ticket_links_not_self CHECK (source_id <> target_id)
);

CREATE UNIQUE INDEX ticket_links_unique
    ON ticket_links (source_id, target_id, relation);

CREATE INDEX ticket_links_by_target ON ticket_links (target_id);

-- Append-only, like conversation_status_history: time-in-status reporting
-- reads this, so an UPDATE would rewrite the past.
CREATE TABLE ticket_status_history (
    id          text        PRIMARY KEY,
    ticket_id   text        NOT NULL REFERENCES tickets (id) ON DELETE CASCADE,
    from_status text,
    to_status   text        NOT NULL,
    actor_type  text        NOT NULL,
    actor_id    text,
    occurred_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ticket_status_history_by_ticket
    ON ticket_status_history (ticket_id, occurred_at);

CREATE TABLE ticket_tags (
    ticket_id text NOT NULL REFERENCES tickets (id) ON DELETE CASCADE,
    tag_id    text NOT NULL REFERENCES tags (id) ON DELETE CASCADE,

    PRIMARY KEY (ticket_id, tag_id)
);

CREATE INDEX ticket_tags_by_tag ON ticket_tags (tag_id);

-- Watchers who are not the assignee (§6.3 watchers and followers).
CREATE TABLE ticket_followers (
    ticket_id text NOT NULL REFERENCES tickets (id) ON DELETE CASCADE,
    member_id text NOT NULL REFERENCES workspace_members (id) ON DELETE CASCADE,

    PRIMARY KEY (ticket_id, member_id)
);

-- --------------------------------------------------------- custom fields

-- One definition table for every entity that accepts custom fields, rather
-- than per-entity tables. §6.10 gives customers, companies, tickets, and
-- conversations the same type system, validation rules, and sensitivity flags;
-- duplicating that four times would guarantee they drift.
CREATE TABLE field_definitions (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    -- What this field attaches to.
    entity_type  text        NOT NULL,
    key          text        NOT NULL,
    label        text        NOT NULL,
    type         text        NOT NULL,
    description  text,
    -- Choices for enum and multi_enum; empty for every other type.
    options      jsonb       NOT NULL DEFAULT '[]'::jsonb,
    required     boolean     NOT NULL DEFAULT false,
    -- 'public' fields are visible to the customer in the portal; 'internal'
    -- ones never leave the agent interface.
    visibility   text        NOT NULL DEFAULT 'internal',
    -- Masked in the UI, excluded from search and export, and audited on reveal
    -- (§12 sensitive metadata).
    sensitive    boolean     NOT NULL DEFAULT false,
    searchable   boolean     NOT NULL DEFAULT false,
    -- Which of §6.10's metadata sources may write this field. An empty array
    -- means "API and agent interface only" — the safe default, because a field
    -- writable from `url_params` is a field an end user controls.
    allowed_sources jsonb    NOT NULL DEFAULT '[]'::jsonb,
    required_capability text,
    -- min / max / min_length / max_length / pattern, per the FieldValidation
    -- contract the browser already types.
    validation   jsonb       NOT NULL DEFAULT '{}'::jsonb,
    position     smallint    NOT NULL DEFAULT 0,
    -- Shown only when another field has a given value (§6.11 conditional
    -- fields).
    condition    jsonb,
    archived_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (workspace_id, entity_type, key),
    CONSTRAINT field_definitions_entity_type CHECK (
        entity_type IN ('ticket', 'conversation', 'customer', 'company')
    ),
    CONSTRAINT field_definitions_type CHECK (
        type IN ('string', 'text', 'integer', 'decimal', 'boolean', 'timestamp',
                 'date', 'enum', 'multi_enum', 'string_list', 'url', 'email',
                 'phone', 'json')
    ),
    CONSTRAINT field_definitions_visibility CHECK (
        visibility IN ('public', 'internal')
    )
);

CREATE INDEX field_definitions_by_entity
    ON field_definitions (workspace_id, entity_type, position)
    WHERE archived_at IS NULL;

-- Values are stored one row per field rather than as a jsonb blob on the
-- parent, so a field can be indexed, filtered, and reported on. `value` is
-- jsonb because the type system spans numbers, arrays, and dates; the
-- definition says how to read it.
CREATE TABLE field_values (
    workspace_id  text  NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    definition_id text  NOT NULL REFERENCES field_definitions (id) ON DELETE CASCADE,
    entity_type   text  NOT NULL,
    entity_id     text  NOT NULL,
    value         jsonb,
    updated_at    timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (definition_id, entity_id)
);

-- "Every custom field for this ticket" — one lookup per detail view.
CREATE INDEX field_values_by_entity
    ON field_values (workspace_id, entity_type, entity_id);

-- Filtering a queue by a custom field value.
CREATE INDEX field_values_by_value
    ON field_values USING gin (value jsonb_path_ops);

-- ----------------------------------------------------------- saved views

CREATE TABLE saved_views (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         text        NOT NULL,
    icon         text,
    -- What the view lists. A view over tickets and a view over conversations
    -- share the filter grammar but not the columns.
    entity_type  text        NOT NULL DEFAULT 'conversation',
    scope        text        NOT NULL DEFAULT 'personal',
    owner_id     text        REFERENCES workspace_members (id) ON DELETE CASCADE,
    team_id      text        REFERENCES teams (id) ON DELETE CASCADE,
    -- A FilterGroup: { match: 'all'|'any', conditions: [...], groups: [...] }.
    -- Stored as jsonb because it is a tree the database never has to
    -- understand — the service compiles it to SQL with a workspace predicate
    -- always bolted on (§11.3).
    filters      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    sort         jsonb       NOT NULL DEFAULT '{}'::jsonb,
    position     smallint    NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT saved_views_scope CHECK (
        scope IN ('personal', 'team', 'workspace')
    ),
    -- A personal view without an owner, or a team view without a team, would
    -- be invisible to everyone.
    CONSTRAINT saved_views_scope_target CHECK (
        (scope = 'personal' AND owner_id IS NOT NULL)
        OR (scope = 'team' AND team_id IS NOT NULL)
        OR scope = 'workspace'
    )
);

CREATE INDEX saved_views_visible
    ON saved_views (workspace_id, entity_type, position);

-- ---------------------------------------------------- macros and replies

-- A macro is a named list of actions (§6.13). It shares its action vocabulary
-- with the automation engine so "what a macro can do" and "what a rule can do"
-- never diverge into two half-implemented sets.
CREATE TABLE macros (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         text        NOT NULL,
    folder       text,
    scope        text        NOT NULL DEFAULT 'workspace',
    owner_id     text        REFERENCES workspace_members (id) ON DELETE CASCADE,
    team_id      text        REFERENCES teams (id) ON DELETE CASCADE,
    -- Reply text inserted before the actions run. Empty for macros that only
    -- change state.
    body         text        NOT NULL DEFAULT '',
    actions      jsonb       NOT NULL DEFAULT '[]'::jsonb,
    usage_count  integer     NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT macros_scope CHECK (scope IN ('personal', 'team', 'workspace'))
);

CREATE INDEX macros_by_workspace ON macros (workspace_id, name);

CREATE TABLE saved_replies (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         text        NOT NULL,
    -- Typed in the composer to insert the reply, e.g. ";refund".
    shortcut     citext,
    folder       text,
    scope        text        NOT NULL DEFAULT 'workspace',
    owner_id     text        REFERENCES workspace_members (id) ON DELETE CASCADE,
    team_id      text        REFERENCES teams (id) ON DELETE CASCADE,
    body         text        NOT NULL,
    usage_count  integer     NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT saved_replies_scope CHECK (
        scope IN ('personal', 'team', 'workspace')
    )
);

CREATE INDEX saved_replies_by_workspace ON saved_replies (workspace_id, name);

-- Shortcuts must be unambiguous within a workspace, or the composer cannot
-- decide which reply ";refund" means.
CREATE UNIQUE INDEX saved_replies_shortcut
    ON saved_replies (workspace_id, shortcut)
    WHERE shortcut IS NOT NULL;

-- ------------------------------------------------------ composer drafts

-- Per-member, per-conversation autosave (§6.2 draft autosave). One row per
-- pair; a draft is overwritten, never versioned.
CREATE TABLE composer_drafts (
    workspace_id    text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    member_id       text        NOT NULL REFERENCES workspace_members (id) ON DELETE CASCADE,
    conversation_id text        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    kind            text        NOT NULL DEFAULT 'reply',
    body            text        NOT NULL DEFAULT '',
    updated_at      timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (member_id, conversation_id),
    CONSTRAINT composer_drafts_kind CHECK (kind IN ('reply', 'note'))
);

CREATE INDEX composer_drafts_by_conversation
    ON composer_drafts (conversation_id);

COMMIT;
