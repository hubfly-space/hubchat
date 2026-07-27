-- 0013_auth_flows.sql
--
-- Brute-force lockout state and magic-link tokens.
--
-- 0001 created `email_verification_tokens` and `password_reset_tokens` but no
-- equivalent for passwordless sign-in, and gave `users` no place to record
-- failed attempts. §11.4 requires both: brute-force protection, and magic-link
-- as a first-class authentication method rather than a reset flow in disguise.

BEGIN;

-- ------------------------------------------------------------- lockout

-- Counted per user rather than per IP.
--
-- Per-IP throttling already exists at the HTTP layer, and it answers a
-- different question: "is this client hammering us?" This answers "is someone
-- working through a password list against *this account*?" — which a botnet
-- spreading attempts across a thousand addresses would otherwise sail past.
ALTER TABLE users
    ADD COLUMN failed_attempts integer NOT NULL DEFAULT 0,
    ADD COLUMN locked_until    timestamptz,
    ADD COLUMN last_sign_in_at timestamptz;

-- The unlock sweep, and the "is this account currently locked" check.
CREATE INDEX users_locked ON users (locked_until) WHERE locked_until IS NOT NULL;

-- --------------------------------------------------------- magic links

-- Single-use, short-lived sign-in tokens.
--
-- Separate from `password_reset_tokens` even though the mechanics rhyme,
-- because the two grant different things: a reset token authorises changing a
-- credential, a magic link authorises becoming the user. Sharing one table
-- would make a reset link usable as a sign-in, which is a privilege escalation
-- hiding behind a shared schema.
CREATE TABLE magic_link_tokens (
    id         text        PRIMARY KEY,
    user_id    text        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash bytea       NOT NULL UNIQUE,
    -- Recorded at issue time so a token emailed to one address cannot be
    -- redeemed after the account's address changes.
    email      citext      NOT NULL,
    -- Where to land after redeeming, so a link from an expired session returns
    -- the agent to the conversation they were reading.
    redirect_to text,
    ip         inet,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX magic_link_tokens_by_user ON magic_link_tokens (user_id, created_at DESC);
CREATE INDEX magic_link_tokens_expiry  ON magic_link_tokens (expires_at);

-- ------------------------------------------------------ pending 2FA

-- The half-authenticated state between "password accepted" and "second factor
-- provided".
--
-- It is a row rather than a session with a flag because a session that exists
-- before the second factor is a session that one missing check turns into a
-- full login. Nothing here can be presented as a session cookie: the challenge
-- is exchanged for one only after the code verifies.
CREATE TABLE totp_challenges (
    id         text        PRIMARY KEY,
    user_id    text        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash bytea       NOT NULL UNIQUE,
    user_agent text,
    ip         inet,
    attempts   integer     NOT NULL DEFAULT 0,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX totp_challenges_expiry ON totp_challenges (expires_at);

-- Recovery codes are stored hashed, one row each, rather than as the plaintext
-- array `users.recovery_codes` that 0001 declared.
--
-- That column is left in place (immutable migrations) but is no longer written
-- to: a recovery code is a credential, and §11.5 does not make an exception for
-- credentials that are inconvenient to hash. One row each also makes "which
-- codes are still unused" answerable without rewriting the whole set.
CREATE TABLE recovery_codes (
    id         text        PRIMARY KEY,
    user_id    text        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash  bytea       NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),

    UNIQUE (user_id, code_hash)
);

CREATE INDEX recovery_codes_unused
    ON recovery_codes (user_id)
    WHERE used_at IS NULL;

COMMIT;
