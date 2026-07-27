-- 0007_widgets_portals.sql
--
-- Website widgets, support portals, and customer-side authentication.
--
-- Two security boundaries meet in this file, and both are enforced here rather
-- than only in application code:
--
--   · the widget's public key is embedded in every customer's page source and
--     is therefore not a secret. What makes it safe is `widget_domains` — an
--     allowlist checked on every config request, so a stolen key handed to a
--     different origin gets nothing (§11.4).
--   · portal sessions are a completely separate credential from agent
--     sessions. A customer signing in to a portal must never end up holding
--     anything that `user_sessions` would recognise.

BEGIN;

-- ---------------------------------------------------------------- widgets

CREATE TABLE widgets (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         text        NOT NULL,
    -- Public by design: it ships in the install snippet. Unique globally
    -- because the loader presents it before any workspace is known.
    public_key   text        NOT NULL UNIQUE,
    inbox_id     text        REFERENCES inboxes (id) ON DELETE SET NULL,
    modes        text[]      NOT NULL DEFAULT '{chat}',
    -- The three config groups the browser already types. Stored as jsonb
    -- rather than fifty columns because the server never filters on them — it
    -- hands the whole projection to the loader — and because §6.4 keeps adding
    -- appearance knobs.
    appearance   jsonb       NOT NULL DEFAULT '{}'::jsonb,
    content      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    behavior     jsonb       NOT NULL DEFAULT '{}'::jsonb,
    environment  text        NOT NULL DEFAULT 'production',
    -- Percentage of visitors who get the widget at all (§6.4 rollout).
    rollout_percent smallint NOT NULL DEFAULT 100,
    -- The currently published config version. Incremented on every save so the
    -- loader can cache against it.
    version      integer     NOT NULL DEFAULT 1,
    enabled      boolean     NOT NULL DEFAULT true,
    -- Set by the installation health check when a config request is first seen
    -- from an allowed origin (§6.4 installation health check).
    installed_at timestamptz,
    last_seen_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT widgets_environment CHECK (
        environment IN ('production', 'test')
    ),
    CONSTRAINT widgets_rollout CHECK (rollout_percent BETWEEN 0 AND 100)
);

CREATE INDEX widgets_by_workspace ON widgets (workspace_id, created_at);

-- §6.4 requires configuration history and rollback. Each save snapshots the
-- whole config rather than a diff: reconstructing state from a chain of diffs
-- is how a rollback ends up applying half a change.
CREATE TABLE widget_config_versions (
    id          text        PRIMARY KEY,
    widget_id   text        NOT NULL REFERENCES widgets (id) ON DELETE CASCADE,
    version     integer     NOT NULL,
    modes       text[]      NOT NULL DEFAULT '{}',
    appearance  jsonb       NOT NULL DEFAULT '{}'::jsonb,
    content     jsonb       NOT NULL DEFAULT '{}'::jsonb,
    behavior    jsonb       NOT NULL DEFAULT '{}'::jsonb,
    changed_by  text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    note        text,
    created_at  timestamptz NOT NULL DEFAULT now(),

    UNIQUE (widget_id, version)
);

-- The allowlist that makes a public key safe (§11.4 widget origin control).
CREATE TABLE widget_domains (
    id         text        PRIMARY KEY,
    widget_id  text        NOT NULL REFERENCES widgets (id) ON DELETE CASCADE,
    -- Hostname only, no scheme or path. A leading "*." permits subdomains;
    -- a bare "*" is rejected by the service, because a widget allowlisted to
    -- everything is a widget with no allowlist.
    domain     citext      NOT NULL,
    verified_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),

    UNIQUE (widget_id, domain)
);

-- The origin check runs on every single config request, so it gets its own
-- index rather than relying on the composite unique above.
CREATE INDEX widget_domains_by_domain ON widget_domains (domain);

-- ---------------------------------------------------------------- portals

CREATE TABLE portals (
    id            text        PRIMARY KEY,
    workspace_id  text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name          text        NOT NULL,
    -- The <subdomain>.<public-host> form, always available.
    subdomain     citext      NOT NULL UNIQUE,
    theme         jsonb       NOT NULL DEFAULT '{}'::jsonb,
    -- Which sections the portal exposes: tickets, knowledge base, feedback,
    -- changelog.
    features      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    -- Ordered subset of password | magic_link | ticket_link | sso_token |
    -- anonymous. Empty means the portal is read-only public.
    auth_methods  text[]      NOT NULL DEFAULT '{magic_link}',
    -- What a signed-in customer may do (§6.5 portal permissions).
    permissions   jsonb       NOT NULL DEFAULT '{}'::jsonb,
    -- Verifies signed SSO tokens from the customer's own application. Stored
    -- encrypted; never returned by any API (§11.5).
    sso_secret    bytea,
    sso_issuer    text,
    default_inbox_id text     REFERENCES inboxes (id) ON DELETE SET NULL,
    default_language text     NOT NULL DEFAULT 'en',
    enabled       boolean     NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT portals_subdomain_format CHECK (
        subdomain ~ '^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$'
    )
);

CREATE INDEX portals_by_workspace ON portals (workspace_id, created_at);

-- Custom domains are separate rows because verification is per-domain and a
-- portal may be mid-migration between two of them.
CREATE TABLE portal_domains (
    id           text        PRIMARY KEY,
    portal_id    text        NOT NULL REFERENCES portals (id) ON DELETE CASCADE,
    domain       citext      NOT NULL UNIQUE,
    -- pending → verified → failed. The DNS check writes this.
    status       text        NOT NULL DEFAULT 'pending',
    verification_token text  NOT NULL,
    verified_at  timestamptz,
    last_checked_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT portal_domains_status CHECK (
        status IN ('pending', 'verified', 'failed')
    )
);

CREATE INDEX portal_domains_by_portal ON portal_domains (portal_id);

CREATE TABLE portal_navigation_items (
    id         text        PRIMARY KEY,
    portal_id  text        NOT NULL REFERENCES portals (id) ON DELETE CASCADE,
    label      text        NOT NULL,
    href       text        NOT NULL,
    external   boolean     NOT NULL DEFAULT false,
    position   smallint    NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX portal_navigation_by_portal
    ON portal_navigation_items (portal_id, position);

-- ------------------------------------------------- customer credentials

-- A customer's ability to sign in to a portal.
--
-- Separate from `users` entirely. A customer is not a user: they have no
-- workspace membership, no capabilities, and no access to anything but their
-- own records. Sharing the table would mean one bug away from a customer
-- holding an agent session (§11.3).
CREATE TABLE portal_identities (
    id            text        PRIMARY KEY,
    workspace_id  text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    customer_id   text        NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    -- Null for customers who only ever use magic links or SSO.
    password_hash text,
    -- The external subject from a verified SSO token, when that is how this
    -- identity was established.
    sso_subject   text,
    last_sign_in_at timestamptz,
    -- Brute-force protection, mirroring what agent sign-in gets (§11.4).
    failed_attempts integer   NOT NULL DEFAULT 0,
    locked_until  timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),

    UNIQUE (workspace_id, customer_id)
);

CREATE UNIQUE INDEX portal_identities_by_sso_subject
    ON portal_identities (workspace_id, sso_subject)
    WHERE sso_subject IS NOT NULL;

CREATE TABLE portal_sessions (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    portal_id    text        NOT NULL REFERENCES portals (id) ON DELETE CASCADE,
    customer_id  text        NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    -- SHA-256 of the cookie value, never the value itself (§11.5), matching
    -- how `user_sessions` stores agent sessions.
    token_hash   bytea       NOT NULL UNIQUE,
    -- How this session was established, so a session created by a one-time
    -- ticket link can be granted narrower access than a password sign-in.
    auth_method  text        NOT NULL,
    user_agent   text,
    ip           inet,
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    revoked_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT portal_sessions_auth_method CHECK (
        auth_method IN ('password', 'magic_link', 'ticket_link', 'sso_token', 'anonymous')
    )
);

CREATE INDEX portal_sessions_by_customer
    ON portal_sessions (customer_id, last_seen_at DESC)
    WHERE revoked_at IS NULL;

CREATE INDEX portal_sessions_expiry ON portal_sessions (expires_at);

-- Single-use tokens: magic-link sign-in and one-time ticket access.
--
-- `scope_id` narrows a ticket_link token to the one ticket it was issued for,
-- so a leaked link cannot be used to read the customer's whole history.
CREATE TABLE portal_access_tokens (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    portal_id    text        NOT NULL REFERENCES portals (id) ON DELETE CASCADE,
    customer_id  text        NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    token_hash   bytea       NOT NULL UNIQUE,
    purpose      text        NOT NULL,
    scope_id     text,
    expires_at   timestamptz NOT NULL,
    used_at      timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT portal_access_tokens_purpose CHECK (
        purpose IN ('magic_link', 'ticket_link', 'email_verification')
    )
);

CREATE INDEX portal_access_tokens_expiry ON portal_access_tokens (expires_at);

-- --------------------------------------------------------- announcements

-- Shown in the widget's announcement centre and on the portal (§6.8).
CREATE TABLE announcements (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    title        text        NOT NULL,
    body         text        NOT NULL DEFAULT '',
    -- 'info' | 'warning' | 'incident' | 'release'
    kind         text        NOT NULL DEFAULT 'info',
    -- Empty means every surface; otherwise the widget and portal ids that
    -- should show it.
    surface_ids  text[]      NOT NULL DEFAULT '{}',
    published_at timestamptz,
    expires_at   timestamptz,
    created_by   text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT announcements_kind CHECK (
        kind IN ('info', 'warning', 'incident', 'release')
    )
);

-- "What should this visitor see right now" — published, not expired.
CREATE INDEX announcements_live
    ON announcements (workspace_id, published_at DESC)
    WHERE published_at IS NOT NULL;

COMMIT;
