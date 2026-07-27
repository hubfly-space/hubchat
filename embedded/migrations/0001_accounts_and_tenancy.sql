-- 0001_accounts_and_tenancy.sql
--
-- Users, workspaces, membership, and the capability model.
--
-- Conventions established here and followed by every later migration:
--
--   · identifiers are ULID-shaped text with a type prefix (`wrk_01hq7x…`).
--     Prefixed ids are self-describing in logs and make a mis-joined query
--     obvious rather than silently wrong. They sort by creation time, which
--     gives cursor pagination a natural key.
--   · every tenant-owned table carries workspace_id, and it is part of the
--     primary lookup index. §11.3 requires a workspace predicate on every
--     query; making it the leading index column means a query that forgets it
--     is slow enough to notice in review.
--   · timestamps are timestamptz, always UTC.
--   · soft deletion is deliberately absent. Deletion is either real or it is
--     anonymisation, and §12 requires the interface to say which.

BEGIN;

CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS citext;

-- ---------------------------------------------------------------- users

CREATE TABLE users (
    id              text PRIMARY KEY,
    name            text        NOT NULL,
    -- citext so a login attempt is case-insensitive without a functional index
    -- on every lookup.
    email           citext      NOT NULL UNIQUE,
    email_verified_at timestamptz,
    password_hash   text,
    avatar_url      text,
    locale          text        NOT NULL DEFAULT 'en',
    totp_secret     text,
    totp_enabled_at timestamptz,
    recovery_codes  text[],
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()

    -- No constraint requiring a credential: a user may legitimately have
    -- neither a password nor an OAuth account while a magic-link invitation is
    -- outstanding. "Can this account authenticate?" is a question for the auth
    -- module, which has the full picture; a CHECK here could only see one
    -- column and would have to be wrong.
);

CREATE TABLE oauth_accounts (
    id            text PRIMARY KEY,
    user_id       text        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider      text        NOT NULL,
    provider_uid  text        NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),

    UNIQUE (provider, provider_uid)
);

CREATE TABLE user_sessions (
    id            text PRIMARY KEY,
    user_id       text        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- Only the hash is stored. A database dump must not be a set of live
    -- sessions (§11.5).
    token_hash    bytea       NOT NULL UNIQUE,
    user_agent    text,
    ip            inet,
    last_seen_at  timestamptz NOT NULL DEFAULT now(),
    expires_at    timestamptz NOT NULL,
    revoked_at    timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX user_sessions_by_user ON user_sessions (user_id, last_seen_at DESC)
    WHERE revoked_at IS NULL;

CREATE TABLE email_verification_tokens (
    id         text PRIMARY KEY,
    user_id    text        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    email      citext      NOT NULL,
    token_hash bytea       NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE password_reset_tokens (
    id         text PRIMARY KEY,
    user_id    text        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash bytea       NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- ------------------------------------------------------------ workspaces

CREATE TABLE workspaces (
    id               text PRIMARY KEY,
    name             text        NOT NULL,
    slug             citext      NOT NULL UNIQUE,
    logo_url         text,
    icon_url         text,
    default_language text        NOT NULL DEFAULT 'en',
    timezone         text        NOT NULL DEFAULT 'UTC',
    ticket_prefix    text        NOT NULL DEFAULT 'SUP',
    -- Monotonic per workspace. Allocated with UPDATE … RETURNING inside the
    -- ticket-creating transaction so numbers are gapless and unique without a
    -- table-wide lock.
    ticket_sequence  bigint      NOT NULL DEFAULT 0,
    settings         jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT workspaces_slug_format CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$'),
    CONSTRAINT workspaces_prefix_format CHECK (ticket_prefix ~ '^[A-Z]{2,8}$')
);

CREATE TABLE workspace_members (
    id           text PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    user_id      text        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role         text        NOT NULL,
    -- Capabilities granted beyond the role's defaults. The effective set is
    -- role_permissions ∪ extra_capabilities, computed in the authorization
    -- module rather than denormalised here.
    extra_capabilities text[] NOT NULL DEFAULT '{}',
    accepting_conversations boolean NOT NULL DEFAULT true,
    presence     text        NOT NULL DEFAULT 'offline',
    last_seen_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (workspace_id, user_id),
    CONSTRAINT workspace_members_role CHECK (
        role IN ('owner', 'admin', 'manager', 'agent', 'developer', 'analyst')
    )
);

CREATE INDEX workspace_members_by_user ON workspace_members (user_id);

-- Exactly one owner per workspace, enforced by the database rather than by
-- application discipline.
CREATE UNIQUE INDEX workspace_members_single_owner
    ON workspace_members (workspace_id)
    WHERE role = 'owner';

CREATE TABLE workspace_invites (
    id           text PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    email        citext      NOT NULL,
    role         text        NOT NULL,
    token_hash   bytea       NOT NULL UNIQUE,
    invited_by   text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    expires_at   timestamptz NOT NULL,
    accepted_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (workspace_id, email)
);

-- ----------------------------------------------------------------- teams

CREATE TABLE teams (
    id               text PRIMARY KEY,
    workspace_id     text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name             text        NOT NULL,
    description      text,
    lead_id          text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    routing_strategy text        NOT NULL DEFAULT 'manual',
    created_at       timestamptz NOT NULL DEFAULT now(),

    UNIQUE (workspace_id, name),
    CONSTRAINT teams_routing CHECK (
        routing_strategy IN ('manual', 'round_robin', 'least_active', 'team_queue', 'customer_owner', 'weighted')
    )
);

CREATE TABLE team_members (
    team_id   text NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    member_id text NOT NULL REFERENCES workspace_members (id) ON DELETE CASCADE,
    -- Only meaningful under the weighted strategy; ignored otherwise.
    weight    integer NOT NULL DEFAULT 1,

    PRIMARY KEY (team_id, member_id)
);

-- ----------------------------------------------------------- permissions

-- Roles are seeded rows rather than an enum so custom roles (§5.9) can be
-- added later without a migration that rewrites every membership.
CREATE TABLE roles (
    id           text PRIMARY KEY,
    -- NULL means a built-in role shared by every workspace.
    workspace_id text        REFERENCES workspaces (id) ON DELETE CASCADE,
    key          text        NOT NULL,
    name         text        NOT NULL,
    description  text,
    is_builtin   boolean     NOT NULL DEFAULT false,
    created_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (workspace_id, key)
);

CREATE TABLE role_permissions (
    role_id    text NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    capability text NOT NULL,

    PRIMARY KEY (role_id, capability)
);

INSERT INTO roles (id, workspace_id, key, name, description, is_builtin) VALUES
    ('rol_owner',     NULL, 'owner',     'Owner',     'Full control, including destructive workspace operations.', true),
    ('rol_admin',     NULL, 'admin',     'Admin',     'Manages people, surfaces, and integrations.',               true),
    ('rol_manager',   NULL, 'manager',   'Manager',   'Runs queues, assignments, SLAs, and reporting.',            true),
    ('rol_agent',     NULL, 'agent',     'Agent',     'Reads and replies to conversations in permitted inboxes.',  true),
    ('rol_developer', NULL, 'developer', 'Developer', 'Integrations and technical configuration.',                 true),
    ('rol_analyst',   NULL, 'analyst',   'Analyst',   'Read-only access to reports and records.',                  true);

INSERT INTO role_permissions (role_id, capability) VALUES
    ('rol_admin', 'conversation.read'), ('rol_admin', 'conversation.reply'),
    ('rol_admin', 'conversation.assign'), ('rol_admin', 'customer.read'),
    ('rol_admin', 'ticket.manage'), ('rol_admin', 'widget.manage'),
    ('rol_admin', 'portal.manage'), ('rol_admin', 'knowledgebase.manage'),
    ('rol_admin', 'feedback.moderate'), ('rol_admin', 'automation.manage'),
    ('rol_admin', 'sla.manage'), ('rol_admin', 'integration.manage'),
    ('rol_admin', 'report.read'), ('rol_admin', 'audit.read'),
    ('rol_admin', 'member.manage'), ('rol_admin', 'workspace.manage'),

    ('rol_manager', 'conversation.read'), ('rol_manager', 'conversation.reply'),
    ('rol_manager', 'conversation.assign'), ('rol_manager', 'customer.read'),
    ('rol_manager', 'ticket.manage'), ('rol_manager', 'sla.manage'),
    ('rol_manager', 'automation.manage'), ('rol_manager', 'report.read'),

    ('rol_agent', 'conversation.read'), ('rol_agent', 'conversation.reply'),
    ('rol_agent', 'customer.read'),

    ('rol_developer', 'integration.manage'), ('rol_developer', 'report.read'),

    ('rol_analyst', 'report.read'), ('rol_analyst', 'customer.read');

-- Owner is not listed: the authorization module short-circuits on the owner
-- role rather than enumerating capabilities, so adding a new capability can
-- never accidentally leave the owner without it.

COMMIT;
