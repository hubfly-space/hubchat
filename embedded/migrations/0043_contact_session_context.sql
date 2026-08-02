-- Preserve the bounded browser context that accompanies widget events on the
-- rolling contact session. This keeps the inbox useful between page events
-- and after an identify-only flow without storing raw URLs or unbounded data.

ALTER TABLE contact_sessions
    ADD COLUMN language text,
    ADD COLUMN timezone text,
    ADD COLUMN platform text,
    ADD COLUMN user_agent text,
    ADD COLUMN viewport jsonb,
    ADD COLUMN current_title text;
