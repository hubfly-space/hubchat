-- 0032_workspace_sso.sql
-- Workspace policy and authentication provenance for enforcing member SSO.

BEGIN;

ALTER TABLE user_sessions
    ADD COLUMN auth_method text NOT NULL DEFAULT 'password';

ALTER TABLE user_sessions
    ADD CONSTRAINT user_sessions_auth_method CHECK (
        auth_method IN ('password', 'magic_link', 'oauth')
    );

ALTER TABLE totp_challenges
    ADD COLUMN auth_method text NOT NULL DEFAULT 'password';

ALTER TABLE totp_challenges
    ADD CONSTRAINT totp_challenges_auth_method CHECK (
        auth_method IN ('password', 'magic_link', 'oauth')
    );

COMMIT;
