-- 0011_integrations.sql
--
-- API keys, webhooks, and the email channel.
--
-- Everything in this file handles a credential or talks to a system Hubchat
-- does not control, so two rules run throughout:
--
--   · secrets are hashed when we only ever need to verify them (API keys) and
--     encrypted when we have to reproduce them (webhook signing secrets, IMAP
--     passwords). Nothing here stores a credential in plain text (§11.5).
--   · outbound delivery is never attempted inline. A webhook endpoint that
--     hangs for thirty seconds must not hold a request open, so deliveries are
--     rows that the job queue drains.

BEGIN;

-- --------------------------------------------------------------- api keys

CREATE TABLE api_keys (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         text        NOT NULL,
    -- The first few characters, kept in clear so the interface can show
    -- "hc_live_a1b2…" next to a key the operator cannot otherwise identify.
    prefix       text        NOT NULL,
    -- SHA-256 of the full key. Verification only needs a comparison, so there
    -- is no reason to be able to reproduce the original (§11.5 hash API keys).
    key_hash     bytea       NOT NULL UNIQUE,
    -- Capability names, a subset of what the creating member holds. A key can
    -- never grant more than its creator had.
    scopes       text[]      NOT NULL DEFAULT '{}',
    last_used_at timestamptz,
    expires_at   timestamptz,
    created_by   text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    revoked_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- The key list, live keys first.
CREATE INDEX api_keys_by_workspace
    ON api_keys (workspace_id, created_at DESC)
    WHERE revoked_at IS NULL;

-- --------------------------------------------------------------- webhooks

CREATE TABLE webhook_endpoints (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    url          text        NOT NULL,
    description  text,
    -- Event types this endpoint wants. Empty means every type, which is
    -- allowed but discouraged in the interface.
    events       text[]      NOT NULL DEFAULT '{}',
    -- Encrypted, not hashed: the HMAC signature has to be recomputed on every
    -- delivery, so the value must be recoverable (§11.5 encrypt recoverable
    -- integration secrets).
    secret       bytea       NOT NULL,
    -- Last four characters, shown so an operator can confirm which secret an
    -- endpoint holds without revealing it.
    secret_hint  text        NOT NULL DEFAULT '',
    enabled      boolean     NOT NULL DEFAULT true,
    -- Set when consecutive failures crossed the threshold (§6.16 endpoint
    -- disable after repeated failures). Cleared by a successful test delivery.
    auto_disabled_at timestamptz,
    consecutive_failures integer NOT NULL DEFAULT 0,
    created_by   text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- The fan-out query, run once per event: "which live endpoints want this?"
CREATE INDEX webhook_endpoints_live
    ON webhook_endpoints (workspace_id)
    WHERE enabled AND auto_disabled_at IS NULL;

-- One row per (event, endpoint) attempt sequence.
--
-- The payload is stored rather than re-derived from `workspace_events` at
-- retry time. §6.16 requires replay to send what was originally promised, and
-- an event whose entity has since changed would otherwise replay differently
-- than it first delivered.
CREATE TABLE webhook_deliveries (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    endpoint_id  text        NOT NULL REFERENCES webhook_endpoints (id) ON DELETE CASCADE,
    event_id     text        REFERENCES workspace_events (id) ON DELETE SET NULL,
    event_type   text        NOT NULL,
    payload      jsonb       NOT NULL,
    status       text        NOT NULL DEFAULT 'pending',
    attempt      integer     NOT NULL DEFAULT 0,
    max_attempts integer     NOT NULL DEFAULT 6,
    response_status integer,
    response_body   text,
    duration_ms  integer,
    error        text,
    next_attempt_at timestamptz,
    delivered_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT webhook_deliveries_status CHECK (
        status IN ('pending', 'delivered', 'failed', 'exhausted', 'cancelled')
    )
);

-- The retry sweep.
CREATE INDEX webhook_deliveries_pending
    ON webhook_deliveries (next_attempt_at)
    WHERE status = 'pending';

-- The delivery log screen, newest first.
CREATE INDEX webhook_deliveries_by_endpoint
    ON webhook_deliveries (endpoint_id, created_at DESC);

-- "Was this event already delivered to this endpoint?" — the guard against
-- double-sending when a replay races the original.
CREATE UNIQUE INDEX webhook_deliveries_once
    ON webhook_deliveries (endpoint_id, event_id)
    WHERE event_id IS NOT NULL;

-- Third-party connections that are not webhooks: OAuth apps, chat bridges.
-- Present now so the credential-handling pattern is established before the
-- integration catalogue exists.
CREATE TABLE integration_connections (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    provider     text        NOT NULL,
    external_id  text,
    display_name text        NOT NULL DEFAULT '',
    -- Encrypted blob; shape is provider-specific and never logged.
    credentials  bytea,
    settings     jsonb       NOT NULL DEFAULT '{}'::jsonb,
    status       text        NOT NULL DEFAULT 'active',
    last_error   text,
    last_synced_at timestamptz,
    created_by   text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (workspace_id, provider, external_id),
    CONSTRAINT integration_connections_status CHECK (
        status IN ('active', 'error', 'disconnected')
    )
);

-- ---------------------------------------------------------- email channel

CREATE TABLE email_mailboxes (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    inbox_id     text        NOT NULL REFERENCES inboxes (id) ON DELETE CASCADE,
    -- The address customers write to.
    address      citext      NOT NULL,
    display_name text        NOT NULL DEFAULT '',
    -- 'webhook' for a provider posting to us, 'imap' for self-hosted polling,
    -- 'off' for send-only (§6.15).
    inbound_mode text        NOT NULL DEFAULT 'off',
    imap_host    text,
    imap_port    integer,
    imap_username text,
    -- Encrypted; recoverable because IMAP login needs the original.
    imap_password bytea,
    -- Shared secret for verifying the provider's inbound webhook.
    inbound_secret bytea,
    -- Addresses and domains that may open a conversation here. Empty means
    -- anyone, which is normal for a support address.
    allowed_senders text[]   NOT NULL DEFAULT '{}',
    blocked_senders text[]   NOT NULL DEFAULT '{}',
    enabled      boolean     NOT NULL DEFAULT true,
    last_polled_at timestamptz,
    last_error   text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (workspace_id, address),
    CONSTRAINT email_mailboxes_inbound_mode CHECK (
        inbound_mode IN ('webhook', 'imap', 'off')
    )
);

CREATE INDEX email_mailboxes_pollable
    ON email_mailboxes (last_polled_at)
    WHERE enabled AND inbound_mode = 'imap';

-- Every message in or out.
--
-- `message_id_header`, `in_reply_to`, and `references` are preserved verbatim
-- because they are the only reliable way to thread a reply (§26.6). Deriving
-- threading from the subject line is how two unrelated "Re: Order" emails end
-- up in one conversation.
CREATE TABLE email_messages (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    mailbox_id   text        REFERENCES email_mailboxes (id) ON DELETE SET NULL,
    direction    text        NOT NULL,
    -- The internal message this email carries, once matched or created.
    message_id   text        REFERENCES messages (id) ON DELETE SET NULL,
    conversation_id text     REFERENCES conversations (id) ON DELETE SET NULL,
    message_id_header text,
    in_reply_to  text,
    references_headers text[],
    from_address citext,
    to_addresses text[]      NOT NULL DEFAULT '{}',
    cc_addresses text[]      NOT NULL DEFAULT '{}',
    subject      text,
    -- Delivery state for outbound; ingestion state for inbound.
    status       text        NOT NULL DEFAULT 'pending',
    error        text,
    -- Recorded when the provider reports one (§6.15 bounce and delivery-event
    -- recording).
    bounce_type  text,
    sent_at      timestamptz,
    received_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT email_messages_direction CHECK (
        direction IN ('inbound', 'outbound')
    ),
    CONSTRAINT email_messages_status CHECK (
        status IN ('pending', 'sent', 'delivered', 'bounced', 'failed', 'received', 'rejected')
    )
);

-- Threading: "have we seen this Message-ID?" and "what does this reply
-- reference?" Both run on every inbound email.
CREATE UNIQUE INDEX email_messages_by_message_id
    ON email_messages (workspace_id, message_id_header)
    WHERE message_id_header IS NOT NULL;

CREATE INDEX email_messages_by_in_reply_to
    ON email_messages (workspace_id, in_reply_to)
    WHERE in_reply_to IS NOT NULL;

CREATE INDEX email_messages_by_conversation
    ON email_messages (conversation_id, created_at)
    WHERE conversation_id IS NOT NULL;

-- The outbound send queue's view of its own work.
CREATE INDEX email_messages_pending
    ON email_messages (created_at)
    WHERE status = 'pending' AND direction = 'outbound';

-- --------------------------------------------------- notification prefs

-- Per-member delivery preferences (§6.15). One row per member per channel
-- pair, rather than a jsonb blob, so the notification fan-out can filter in
-- SQL instead of loading every member's preferences into memory.
CREATE TABLE notification_preferences (
    workspace_id text    NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    member_id    text    NOT NULL REFERENCES workspace_members (id) ON DELETE CASCADE,
    -- 'assignment' | 'mention' | 'sla_warning' | 'new_conversation' | …
    type         text    NOT NULL,
    in_app       boolean NOT NULL DEFAULT true,
    email        boolean NOT NULL DEFAULT false,
    browser      boolean NOT NULL DEFAULT false,
    sound        boolean NOT NULL DEFAULT false,

    PRIMARY KEY (member_id, type)
);

CREATE INDEX notification_preferences_by_workspace
    ON notification_preferences (workspace_id, type);

COMMIT;
