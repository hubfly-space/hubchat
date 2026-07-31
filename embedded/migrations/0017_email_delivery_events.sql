-- Provider delivery callbacks are append-only evidence. The unique provider
-- key makes retries safe while the message status and suppression projection
-- remain cheap operational reads.

CREATE TABLE email_delivery_events (
    id                 text        PRIMARY KEY,
    workspace_id       text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    mailbox_id         text        NOT NULL REFERENCES email_mailboxes (id) ON DELETE CASCADE,
    provider           text        NOT NULL,
    provider_event_id  text        NOT NULL,
    event_type         text        NOT NULL,
    email_message_id   text        REFERENCES email_messages (id) ON DELETE SET NULL,
    recipient          citext,
    bounce_type        text,
    reason             text,
    hard               boolean     NOT NULL DEFAULT false,
    payload            jsonb       NOT NULL DEFAULT '{}'::jsonb,
    occurred_at        timestamptz NOT NULL DEFAULT now(),
    created_at         timestamptz NOT NULL DEFAULT now(),

    UNIQUE (workspace_id, mailbox_id, provider, provider_event_id)
);

CREATE INDEX email_delivery_events_by_mailbox
    ON email_delivery_events (workspace_id, mailbox_id, occurred_at DESC, id DESC);

CREATE TABLE email_suppressions (
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    address      citext      NOT NULL,
    reason       text        NOT NULL,
    source       text        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (workspace_id, address)
);

CREATE INDEX email_suppressions_by_workspace
    ON email_suppressions (workspace_id, updated_at DESC);
