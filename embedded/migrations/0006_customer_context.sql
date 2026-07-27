-- 0006_customer_context.sql
--
-- Contact sessions, application events, the metadata schema, and identity
-- merge history.
--
-- §6.10 calls advanced customer metadata a central differentiator, and §26.4
-- names event volume as a top technical risk. Both shape this file: the
-- attribute schema is strict (every accepted key is declared, typed, and has
-- an allowlist of sources), and the event table is designed to be pruned —
-- narrow rows, a retention index, and no foreign keys that would make a bulk
-- delete cascade somewhere expensive.

BEGIN;

-- ------------------------------------------------- additional identities

-- A customer may reach support from several addresses (§6.9 linked
-- identities). `customers.email` stays the primary; these are the others.
CREATE TABLE customer_emails (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    customer_id  text        NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    email        citext      NOT NULL,
    verified_at  timestamptz,
    is_primary   boolean     NOT NULL DEFAULT false,
    created_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (workspace_id, customer_id, email)
);

-- "Which customer owns this verified address?" — the email-channel routing
-- lookup. Only verified rows participate, for the same reason 0002 restricted
-- its own unique index: an unverified address proves nothing.
CREATE UNIQUE INDEX customer_emails_verified
    ON customer_emails (workspace_id, email)
    WHERE verified_at IS NOT NULL;

CREATE INDEX customer_emails_by_customer ON customer_emails (customer_id);

CREATE TABLE customer_phones (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    customer_id  text        NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    -- Stored E.164-normalised; the display form is derived, not stored.
    phone        text        NOT NULL,
    verified_at  timestamptz,
    is_primary   boolean     NOT NULL DEFAULT false,
    created_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (workspace_id, customer_id, phone)
);

CREATE INDEX customer_phones_by_customer ON customer_phones (customer_id);

-- -------------------------------------------------------- contact sessions

-- One visit or application session (§4 contact session). Presence is
-- ephemeral and lives in memory; this table is the durable part §9 permits us
-- to keep.
CREATE TABLE contact_sessions (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    customer_id  text        REFERENCES customers (id) ON DELETE CASCADE,
    visitor_id   text        REFERENCES visitors (id) ON DELETE CASCADE,
    widget_id    text,
    -- Country only, never a full address. §12 makes IP storage a workspace
    -- choice, and the country is the part that is actually useful for support.
    ip_country   text,
    browser      text,
    os           text,
    device       text,
    referrer     text,
    landing_url  text,
    current_url  text,
    page_views   integer     NOT NULL DEFAULT 0,
    -- The consent state in force when the session started, captured at the
    -- time rather than looked up later, because the policy may change (§12).
    consent      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    started_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    ended_at     timestamptz
);

CREATE INDEX contact_sessions_by_customer
    ON contact_sessions (workspace_id, customer_id, started_at DESC)
    WHERE customer_id IS NOT NULL;

CREATE INDEX contact_sessions_by_visitor
    ON contact_sessions (visitor_id, started_at DESC)
    WHERE visitor_id IS NOT NULL;

-- Retention sweep.
CREATE INDEX contact_sessions_by_started_at ON contact_sessions (started_at);

-- --------------------------------------------------------- customer events

-- Application and page events (§6.10 event tracking).
--
-- Deliberately without a foreign key to `contact_sessions`: this is the
-- highest-volume table in the product, retention deletes from it in bulk, and
-- a referential check per row is a cost paid on every insert to protect
-- against a dangling id that nothing reads transactionally anyway. The
-- customer reference is kept because the timeline query joins on it.
CREATE TABLE customer_events (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    customer_id  text        REFERENCES customers (id) ON DELETE CASCADE,
    visitor_id   text,
    session_id   text,
    -- Dotted name: 'page.viewed', 'checkout.started', or a workspace's own.
    type         text        NOT NULL,
    source       text        NOT NULL DEFAULT 'js_sdk',
    url          text,
    -- Validated against the workspace's event schema and size-capped by
    -- cfg.Limits.MaxEventBytes before it ever reaches here (§26.4).
    payload      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    occurred_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT customer_events_source CHECK (
        source IN ('widget_init', 'js_sdk', 'rest_api', 'identity_token',
                   'portal_profile', 'form', 'url_params', 'local_storage',
                   'cookie', 'event')
    )
);

-- The customer timeline: "this person's activity, most recent first".
CREATE INDEX customer_events_by_customer
    ON customer_events (workspace_id, customer_id, occurred_at DESC)
    WHERE customer_id IS NOT NULL;

-- The developer event stream, and automation's `event.received` trigger.
CREATE INDEX customer_events_by_type
    ON customer_events (workspace_id, type, occurred_at DESC);

-- Retention. Kept separate from the composite indexes above so the sweep is a
-- range scan rather than a full table scan (§26.4 apply retention).
CREATE INDEX customer_events_by_occurred_at ON customer_events (occurred_at);

CREATE INDEX customer_events_by_session
    ON customer_events (session_id, occurred_at)
    WHERE session_id IS NOT NULL;

-- ------------------------------------------------------- customer notes

CREATE TABLE customer_notes (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    customer_id  text        REFERENCES customers (id) ON DELETE CASCADE,
    company_id   text        REFERENCES companies (id) ON DELETE CASCADE,
    author_id    text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    -- Denormalised so the note still attributes correctly after the member
    -- leaves the workspace.
    author_name  text        NOT NULL DEFAULT '',
    body         text        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    -- A note belongs to exactly one of a customer or a company.
    CONSTRAINT customer_notes_one_subject CHECK (
        (customer_id IS NOT NULL) <> (company_id IS NOT NULL)
    )
);

CREATE INDEX customer_notes_by_customer
    ON customer_notes (customer_id, created_at DESC)
    WHERE customer_id IS NOT NULL;

CREATE INDEX customer_notes_by_company
    ON customer_notes (company_id, created_at DESC)
    WHERE company_id IS NOT NULL;

-- --------------------------------------------------- attribute definitions

-- The metadata allowlist (§6.10 metadata schema, §12 privacy controls).
--
-- Separate from `field_definitions` even though the shapes rhyme. Custom
-- fields are filled in by agents through the interface; attributes arrive
-- unattended from the SDK, the widget, and URL parameters — so this table
-- carries the things that only matter for untrusted input: which sources may
-- write a key, what patterns are blocked outright, and how long values live.
CREATE TABLE attribute_definitions (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    entity_type  text        NOT NULL DEFAULT 'customer',
    key          text        NOT NULL,
    label        text        NOT NULL,
    type         text        NOT NULL,
    description  text,
    options      jsonb       NOT NULL DEFAULT '[]'::jsonb,
    -- Empty means no source may write it — the safe default. A key writable
    -- from `url_params` is a key the end user controls, so opting in is
    -- explicit.
    allowed_sources jsonb    NOT NULL DEFAULT '[]'::jsonb,
    required_capability text,
    sensitive    boolean     NOT NULL DEFAULT false,
    searchable   boolean     NOT NULL DEFAULT false,
    validation   jsonb       NOT NULL DEFAULT '{}'::jsonb,
    -- Null means the workspace default; set to expire this key's values sooner
    -- than everything else (§12 sensitive metadata: shorter retention).
    retention_days integer,
    archived_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (workspace_id, entity_type, key),
    CONSTRAINT attribute_definitions_entity_type CHECK (
        entity_type IN ('customer', 'company', 'session')
    ),
    CONSTRAINT attribute_definitions_type CHECK (
        type IN ('string', 'integer', 'decimal', 'boolean', 'timestamp', 'date',
                 'enum', 'string_list', 'url', 'json')
    ),
    CONSTRAINT attribute_definitions_retention CHECK (
        retention_days IS NULL OR retention_days > 0
    )
);

CREATE INDEX attribute_definitions_by_entity
    ON attribute_definitions (workspace_id, entity_type)
    WHERE archived_at IS NULL;

-- Key patterns refused regardless of any definition (§12 blocked key
-- patterns). Checked before the allowlist, so a workspace cannot accidentally
-- declare a key that collects card numbers.
CREATE TABLE attribute_blocklist (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    pattern      text        NOT NULL,
    reason       text,
    created_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (workspace_id, pattern)
);

-- ------------------------------------------------------- identity merges

-- §6.9 requires merges to be auditable, previewable, and reversible for a
-- limited period. That rules out doing the merge as a destructive UPDATE and
-- calling the audit log good enough — reversal needs to know what the losing
-- record actually contained, so a full snapshot is stored.
CREATE TABLE identity_merge_history (
    id            text        PRIMARY KEY,
    workspace_id  text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    -- The record that survived.
    winner_id     text        NOT NULL,
    -- The record that was absorbed. Not a foreign key: the whole point is that
    -- this row outlives it.
    loser_id      text        NOT NULL,
    entity_type   text        NOT NULL DEFAULT 'customer',
    -- Everything the loser held, sufficient to reconstruct it.
    loser_snapshot jsonb      NOT NULL,
    -- Counts of what moved, for the "undo this merge" confirmation to state
    -- the outcome plainly rather than asking "are you sure?".
    moved_counts  jsonb       NOT NULL DEFAULT '{}'::jsonb,
    merged_by     text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    -- After this, the snapshot is pruned and the merge is permanent.
    reversible_until timestamptz,
    reversed_at   timestamptz,
    reversed_by   text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT identity_merge_entity_type CHECK (
        entity_type IN ('customer', 'company')
    ),
    CONSTRAINT identity_merge_not_self CHECK (winner_id <> loser_id)
);

CREATE INDEX identity_merge_by_winner
    ON identity_merge_history (workspace_id, winner_id, created_at DESC);

-- "Was this id merged away, and into what?" — how a stale link or an old API
-- reference still resolves to the surviving record.
CREATE INDEX identity_merge_by_loser
    ON identity_merge_history (workspace_id, loser_id);

-- The sweep that expires reversibility and drops the snapshots.
CREATE INDEX identity_merge_reversible
    ON identity_merge_history (reversible_until)
    WHERE reversed_at IS NULL AND reversible_until IS NOT NULL;

-- ------------------------------------------------------- customer tags

CREATE TABLE customer_tags (
    customer_id text NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    tag_id      text NOT NULL REFERENCES tags (id) ON DELETE CASCADE,

    PRIMARY KEY (customer_id, tag_id)
);

CREATE INDEX customer_tags_by_tag ON customer_tags (tag_id);

CREATE TABLE company_tags (
    company_id text NOT NULL REFERENCES companies (id) ON DELETE CASCADE,
    tag_id     text NOT NULL REFERENCES tags (id) ON DELETE CASCADE,

    PRIMARY KEY (company_id, tag_id)
);

CREATE INDEX company_tags_by_tag ON company_tags (tag_id);

-- ------------------------------------------------------- blocked visitors

-- §6.2 allows blocking an abusive visitor. Blocking by customer id would be
-- trivially defeated by clearing cookies, so the block records whatever
-- identifier is actually available and the widget checks all of them.
CREATE TABLE blocked_contacts (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    kind         text        NOT NULL,
    value        text        NOT NULL,
    reason       text,
    blocked_by   text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    expires_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (workspace_id, kind, value),
    CONSTRAINT blocked_contacts_kind CHECK (
        kind IN ('email', 'domain', 'ip', 'visitor', 'customer')
    )
);

CREATE INDEX blocked_contacts_active
    ON blocked_contacts (workspace_id, kind, value)
    WHERE expires_at IS NULL;

COMMIT;
