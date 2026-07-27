-- 0008_forms.sql
--
-- The intake builder: reusable forms, their fields, and their submissions.
--
-- §6.11 lets one form create a ticket, a conversation, a customer record, a
-- feedback item, or a survey response depending on its configured purpose.
-- That is why submissions record what they produced (`result_type`,
-- `result_id`) instead of each product owning its own submission table: the
-- moderation queue, the spam review, and the "where did this come from" link
-- on a ticket all need one place to look.

BEGIN;

-- ------------------------------------------------------------------ forms

CREATE TABLE forms (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         text        NOT NULL,
    slug         citext      NOT NULL,
    description  text,
    -- What a submission becomes.
    purpose      text        NOT NULL DEFAULT 'ticket',
    -- Where the resulting ticket or conversation lands, plus the tags and
    -- priority to apply (§6.11 routing).
    routing      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    -- Headline, body, and optional redirect for the post-submit page.
    confirmation jsonb       NOT NULL DEFAULT '{}'::jsonb,
    -- 'public' forms accept anonymous submissions; 'authenticated' ones
    -- require a portal session.
    access       text        NOT NULL DEFAULT 'public',
    -- Honeypot field name, minimum fill time, and rate caps. A public form
    -- with none of these is a public form that will be used to send spam.
    spam_protection jsonb    NOT NULL DEFAULT '{}'::jsonb,
    -- Null means unlimited; otherwise submissions are refused past this count
    -- (§6.11 submission limits).
    max_submissions integer,
    submission_count integer NOT NULL DEFAULT 0,
    enabled      boolean     NOT NULL DEFAULT true,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (workspace_id, slug),
    CONSTRAINT forms_purpose CHECK (
        purpose IN ('ticket', 'conversation', 'feedback', 'survey', 'customer')
    ),
    CONSTRAINT forms_access CHECK (
        access IN ('public', 'authenticated')
    ),
    CONSTRAINT forms_max_submissions CHECK (
        max_submissions IS NULL OR max_submissions > 0
    )
);

CREATE INDEX forms_by_workspace ON forms (workspace_id, created_at);

CREATE TABLE form_fields (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    form_id      text        NOT NULL REFERENCES forms (id) ON DELETE CASCADE,
    key          text        NOT NULL,
    label        text        NOT NULL,
    -- A superset of the custom-field types: forms additionally accept file
    -- uploads, star ratings, and hidden fields carrying metadata.
    type         text        NOT NULL,
    placeholder  text,
    description  text,
    options      jsonb       NOT NULL DEFAULT '[]'::jsonb,
    required     boolean     NOT NULL DEFAULT false,
    default_value jsonb,
    -- Show this field only when another field holds a given value (§6.11).
    condition    jsonb,
    validation   jsonb       NOT NULL DEFAULT '{}'::jsonb,
    -- When set, the answer is copied into this custom field on the created
    -- ticket rather than living only in the submission.
    maps_to_definition_id text REFERENCES field_definitions (id) ON DELETE SET NULL,
    position     smallint    NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (form_id, key),
    CONSTRAINT form_fields_type CHECK (
        type IN ('string', 'text', 'integer', 'decimal', 'boolean', 'timestamp',
                 'date', 'enum', 'multi_enum', 'string_list', 'url', 'email',
                 'phone', 'json', 'file', 'rating', 'hidden')
    )
);

CREATE INDEX form_fields_by_form ON form_fields (form_id, position);

-- ------------------------------------------------------- submissions

CREATE TABLE form_submissions (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    form_id      text        NOT NULL REFERENCES forms (id) ON DELETE CASCADE,
    customer_id  text        REFERENCES customers (id) ON DELETE SET NULL,
    visitor_id   text        REFERENCES visitors (id) ON DELETE SET NULL,
    -- What the submission produced, per the form's purpose. Not a foreign key
    -- because the target table varies; the service resolves it.
    result_type  text,
    result_id    text,
    -- 'accepted' | 'held' (awaiting moderation) | 'rejected' (failed spam
    -- checks). Held and rejected rows are kept so a false positive is
    -- recoverable rather than silently discarded.
    status       text        NOT NULL DEFAULT 'accepted',
    spam_score   real,
    source_url   text,
    ip           inet,
    user_agent   text,
    submitted_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT form_submissions_status CHECK (
        status IN ('accepted', 'held', 'rejected')
    )
);

CREATE INDEX form_submissions_by_form
    ON form_submissions (workspace_id, form_id, submitted_at DESC);

-- The moderation queue.
CREATE INDEX form_submissions_held
    ON form_submissions (workspace_id, submitted_at DESC)
    WHERE status = 'held';

-- "Which submission created this ticket?"
CREATE INDEX form_submissions_by_result
    ON form_submissions (workspace_id, result_type, result_id)
    WHERE result_id IS NOT NULL;

-- One row per answer rather than a jsonb blob on the submission, so an answer
-- can be reported on and exported without unpacking every submission first.
CREATE TABLE form_submission_values (
    submission_id text  NOT NULL REFERENCES form_submissions (id) ON DELETE CASCADE,
    field_id      text  NOT NULL REFERENCES form_fields (id) ON DELETE CASCADE,
    value         jsonb,
    -- File-typed answers point here; `value` stays null for them.
    file_id       text  REFERENCES files (id) ON DELETE SET NULL,

    PRIMARY KEY (submission_id, field_id)
);

CREATE INDEX form_submission_values_by_field ON form_submission_values (field_id);

COMMIT;
