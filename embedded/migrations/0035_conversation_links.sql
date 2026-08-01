-- 0035_conversation_links.sql
-- Durable relationships between support conversations.  The service stores
-- endpoints in lexical order so a related link cannot be created twice in
-- opposite directions.

BEGIN;

CREATE TABLE conversation_links (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    source_id    text        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    target_id    text        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    relation     text        NOT NULL DEFAULT 'related',
    created_by   text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT conversation_links_relation CHECK (
        relation IN ('related', 'duplicate_of', 'follow_up')
    ),
    CONSTRAINT conversation_links_not_self CHECK (source_id <> target_id)
);

CREATE UNIQUE INDEX conversation_links_unique
    ON conversation_links (workspace_id, source_id, target_id, relation);

CREATE INDEX conversation_links_by_target
    ON conversation_links (workspace_id, target_id, created_at DESC);

COMMIT;
