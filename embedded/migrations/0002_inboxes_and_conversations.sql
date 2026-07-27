-- 0002_inboxes_and_conversations.sql
--
-- The support core: inboxes, customers, conversations, and messages.
--
-- The index choices here are the ones that decide whether the inbox stays
-- responsive at a million conversations (§13 important indexes). Each is
-- annotated with the query it exists for; an index without a named query is an
-- index nobody will dare delete later.

BEGIN;

-- --------------------------------------------------------------- inboxes

CREATE TABLE inboxes (
    id             text PRIMARY KEY,
    workspace_id   text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name           text        NOT NULL,
    slug           citext      NOT NULL,
    description    text,
    channels       text[]      NOT NULL DEFAULT '{}',
    is_default     boolean     NOT NULL DEFAULT false,
    sla_policy_id  text,
    settings       jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at     timestamptz NOT NULL DEFAULT now(),

    UNIQUE (workspace_id, slug)
);

CREATE UNIQUE INDEX inboxes_single_default
    ON inboxes (workspace_id)
    WHERE is_default;

CREATE TABLE inbox_teams (
    inbox_id text NOT NULL REFERENCES inboxes (id) ON DELETE CASCADE,
    team_id  text NOT NULL REFERENCES teams (id) ON DELETE CASCADE,

    PRIMARY KEY (inbox_id, team_id)
);

-- ------------------------------------------------------------- companies

CREATE TABLE companies (
    id            text PRIMARY KEY,
    workspace_id  text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name          text        NOT NULL,
    domain        citext,
    external_id   text,
    tier          text,
    owner_id      text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    sla_policy_id text,
    attributes    jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    UNIQUE (workspace_id, external_id)
);

CREATE INDEX companies_by_domain ON companies (workspace_id, domain)
    WHERE domain IS NOT NULL;

-- ------------------------------------------------------------- customers

CREATE TABLE customers (
    id            text PRIMARY KEY,
    workspace_id  text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name          text,
    -- Normalised at write time. Two customers in one workspace cannot share a
    -- verified email; unverified duplicates are permitted because refusing
    -- them would let an attacker probe which addresses exist.
    email         citext,
    phone         text,
    avatar_url    text,
    external_id   text,
    verification  text        NOT NULL DEFAULT 'anonymous',
    language      text,
    timezone      text,
    owner_id      text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    attributes    jsonb       NOT NULL DEFAULT '{}'::jsonb,
    first_seen_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at  timestamptz,
    last_contacted_at timestamptz,
    -- Optimistic concurrency: two agents editing the same profile must not
    -- silently overwrite one another (§13 conventions).
    version       integer     NOT NULL DEFAULT 1,
    created_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT customers_verification CHECK (
        verification IN ('anonymous', 'unverified', 'verified')
    )
);

-- "Find the customer this signed token refers to" — the hottest lookup on the
-- identify path.
CREATE UNIQUE INDEX customers_by_external_id
    ON customers (workspace_id, external_id)
    WHERE external_id IS NOT NULL;

-- "Is this verified email already taken in this workspace?"
CREATE UNIQUE INDEX customers_by_verified_email
    ON customers (workspace_id, email)
    WHERE email IS NOT NULL AND verification = 'verified';

CREATE INDEX customers_by_email ON customers (workspace_id, email)
    WHERE email IS NOT NULL;

-- Directory search on name and email.
CREATE INDEX customers_search ON customers
    USING gin ((coalesce(name, '') || ' ' || coalesce(email::text, '')) gin_trgm_ops);

CREATE TABLE company_customers (
    company_id  text NOT NULL REFERENCES companies (id) ON DELETE CASCADE,
    customer_id text NOT NULL REFERENCES customers (id) ON DELETE CASCADE,

    PRIMARY KEY (company_id, customer_id)
);

CREATE INDEX company_customers_by_customer ON company_customers (customer_id);

-- Anonymous browser identities, linked to a customer once identity is proven.
CREATE TABLE visitors (
    id           text PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    customer_id  text        REFERENCES customers (id) ON DELETE SET NULL,
    token_hash   bytea       NOT NULL UNIQUE,
    first_seen_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now()
);

-- §6.9 requires merges to be auditable and reversible for a limited period, so
-- the link is a record with provenance rather than a nullable column update.
CREATE TABLE visitor_customer_links (
    id          text PRIMARY KEY,
    visitor_id  text        NOT NULL REFERENCES visitors (id) ON DELETE CASCADE,
    customer_id text        NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    -- How the link was established. Never 'inferred'; §6.9 forbids merging on
    -- weak signals.
    method      text        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT visitor_links_method CHECK (
        method IN ('identity_token', 'verified_email', 'external_id', 'manual')
    )
);

-- --------------------------------------------------------- conversations

CREATE TABLE conversations (
    id             text PRIMARY KEY,
    workspace_id   text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    inbox_id       text        NOT NULL REFERENCES inboxes (id) ON DELETE RESTRICT,
    channel        text        NOT NULL,
    subject        text,
    state          text        NOT NULL DEFAULT 'new',
    priority       text        NOT NULL DEFAULT 'normal',
    customer_id    text        REFERENCES customers (id) ON DELETE SET NULL,
    visitor_id     text        REFERENCES visitors (id) ON DELETE SET NULL,
    assignee_id    text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    team_id        text        REFERENCES teams (id) ON DELETE SET NULL,
    ticket_id      text,
    -- Denormalised for the list view. Recomputing these per row would make the
    -- inbox query a join fan-out over the entire message table.
    message_count      integer NOT NULL DEFAULT 0,
    last_message_preview text  NOT NULL DEFAULT '',
    last_message_at    timestamptz NOT NULL DEFAULT now(),
    last_customer_at   timestamptz,
    snoozed_until      timestamptz,
    -- Monotonic per conversation; the resume cursor for WebSocket clients (§9).
    sequence       bigint      NOT NULL DEFAULT 0,
    version        integer     NOT NULL DEFAULT 1,
    created_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT conversations_state CHECK (
        state IN ('new', 'open', 'pending', 'waiting_for_customer',
                  'waiting_for_support', 'snoozed', 'resolved', 'closed', 'spam')
    ),
    CONSTRAINT conversations_priority CHECK (
        priority IN ('low', 'normal', 'high', 'urgent')
    ),
    CONSTRAINT conversations_channel CHECK (
        channel IN ('widget', 'portal', 'email', 'form', 'api', 'manual')
    )
);

-- "The open queue for this inbox, newest activity first" — the single most
-- frequent query in the product. Partial, because closed and spam threads are
-- the majority of rows after a year and are never in this view.
CREATE INDEX conversations_active_queue
    ON conversations (workspace_id, inbox_id, last_message_at DESC)
    WHERE state NOT IN ('closed', 'spam');

-- "Assigned to me" and "assigned to my team".
CREATE INDEX conversations_by_assignee
    ON conversations (workspace_id, assignee_id, last_message_at DESC)
    WHERE state NOT IN ('closed', 'spam');

CREATE INDEX conversations_by_team
    ON conversations (workspace_id, team_id, last_message_at DESC)
    WHERE state NOT IN ('closed', 'spam');

-- "Unassigned" — the view that decides whether anyone is being ignored.
CREATE INDEX conversations_unassigned
    ON conversations (workspace_id, inbox_id, last_message_at DESC)
    WHERE assignee_id IS NULL AND state NOT IN ('closed', 'spam');

-- The scheduler's wake-up query.
CREATE INDEX conversations_snoozed
    ON conversations (snoozed_until)
    WHERE snoozed_until IS NOT NULL AND state = 'snoozed';

CREATE INDEX conversations_by_customer
    ON conversations (workspace_id, customer_id, last_message_at DESC);

CREATE TABLE conversation_participants (
    conversation_id text NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    customer_id     text NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    added_at        timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (conversation_id, customer_id)
);

CREATE TABLE conversation_followers (
    conversation_id text NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    member_id       text NOT NULL REFERENCES workspace_members (id) ON DELETE CASCADE,

    PRIMARY KEY (conversation_id, member_id)
);

-- Append-only. Reporting derives time-in-state from this, so an UPDATE here
-- would rewrite history.
CREATE TABLE conversation_status_history (
    id              text PRIMARY KEY,
    conversation_id text        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    from_state      text,
    to_state        text        NOT NULL,
    actor_type      text        NOT NULL,
    actor_id        text,
    occurred_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX conversation_status_history_by_conversation
    ON conversation_status_history (conversation_id, occurred_at);

-- -------------------------------------------------------------- messages

CREATE TABLE messages (
    id              text PRIMARY KEY,
    workspace_id    text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    conversation_id text        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    -- Supplied by the client so a retried send is a no-op rather than a
    -- duplicate (§9 idempotent message submission).
    client_id       text,
    kind            text        NOT NULL DEFAULT 'reply',
    author_type     text        NOT NULL,
    author_id       text,
    author_name     text        NOT NULL,
    body            text        NOT NULL DEFAULT '',
    event_type      text,
    event_data      jsonb,
    quoted_message_id text      REFERENCES messages (id) ON DELETE SET NULL,
    delivery        text        NOT NULL DEFAULT 'sent',
    -- Position within the conversation. Ordering never relies on created_at:
    -- two messages can share a microsecond, and clock skew is real.
    sequence        bigint      NOT NULL,
    edited_at       timestamptz,
    redacted_at     timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT messages_kind CHECK (kind IN ('reply', 'note', 'event')),
    CONSTRAINT messages_author_type CHECK (
        author_type IN ('customer', 'agent', 'system', 'automation')
    )
);

-- The timeline read, and the WebSocket resume query ("everything after N").
CREATE UNIQUE INDEX messages_by_conversation
    ON messages (conversation_id, sequence);

-- Enforces idempotent submission at the database level rather than trusting a
-- check-then-insert race in application code.
CREATE UNIQUE INDEX messages_client_id
    ON messages (conversation_id, client_id)
    WHERE client_id IS NOT NULL;

-- Full-text search across message bodies, workspace-scoped (§6.17). Public
-- replies and notes only — event rows carry no prose.
CREATE INDEX messages_search
    ON messages USING gin (to_tsvector('english', body))
    WHERE kind <> 'event' AND redacted_at IS NULL;

CREATE TABLE message_revisions (
    id         text PRIMARY KEY,
    message_id text        NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
    body       text        NOT NULL,
    edited_by  text,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE message_reads (
    message_id text NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
    reader_type text NOT NULL,
    reader_id  text NOT NULL,
    read_at    timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (message_id, reader_type, reader_id)
);

-- ---------------------------------------------------------------- tags

CREATE TABLE tags (
    id           text PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         citext      NOT NULL,
    color        smallint    NOT NULL DEFAULT 3,
    created_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (workspace_id, name),
    CONSTRAINT tags_color CHECK (color BETWEEN 1 AND 6)
);

CREATE TABLE conversation_tags (
    conversation_id text NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    tag_id          text NOT NULL REFERENCES tags (id) ON DELETE CASCADE,

    PRIMARY KEY (conversation_id, tag_id)
);

CREATE INDEX conversation_tags_by_tag ON conversation_tags (tag_id);

COMMIT;
