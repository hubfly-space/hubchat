-- 0009_feedback_kb_surveys.sql
--
-- Feedback boards, the knowledge base, the changelog, and surveys.
--
-- These three modules share a property that shapes the schema: they are the
-- parts of the product that untrusted people write to. Feedback items and
-- comments come from customers, article feedback comes from anonymous
-- readers, and survey responses come from anyone holding a link. So every
-- table here carries a moderation or rate-limiting affordance, and none of
-- them trust a submitter-supplied identifier.

BEGIN;

-- ------------------------------------------------------- feedback boards

CREATE TABLE feedback_boards (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         text        NOT NULL,
    slug         citext      NOT NULL,
    description  text,
    -- 'public' is world-readable, 'private' needs a portal session,
    -- 'invite_only' needs an explicit subscription.
    visibility   text        NOT NULL DEFAULT 'public',
    allow_comments boolean   NOT NULL DEFAULT true,
    allow_voting boolean     NOT NULL DEFAULT true,
    -- Null means unlimited. §6.6 requires per-customer vote limits because a
    -- board where one person can vote a thousand times is a board that tells
    -- you nothing.
    votes_per_customer integer,
    -- When true, new items land in the moderation queue instead of the board.
    moderation   boolean     NOT NULL DEFAULT false,
    item_count   integer     NOT NULL DEFAULT 0,
    position     smallint    NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (workspace_id, slug),
    CONSTRAINT feedback_boards_visibility CHECK (
        visibility IN ('public', 'private', 'invite_only')
    ),
    CONSTRAINT feedback_boards_vote_limit CHECK (
        votes_per_customer IS NULL OR votes_per_customer > 0
    )
);

CREATE INDEX feedback_boards_by_workspace
    ON feedback_boards (workspace_id, position);

CREATE TABLE feedback_items (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    board_id     text        NOT NULL REFERENCES feedback_boards (id) ON DELETE CASCADE,
    title        text        NOT NULL,
    description  text        NOT NULL DEFAULT '',
    type         text        NOT NULL DEFAULT 'feature_request',
    status       text        NOT NULL DEFAULT 'open',
    visibility   text        NOT NULL DEFAULT 'public',
    submitter_id text        REFERENCES customers (id) ON DELETE SET NULL,
    -- Set when an agent raised this from a conversation rather than a customer
    -- submitting it (§6.6 agent feedback workflow).
    created_by_member_id text REFERENCES workspace_members (id) ON DELETE SET NULL,
    company_id   text        REFERENCES companies (id) ON DELETE SET NULL,
    product_area text,
    priority     text,
    -- Denormalised counters. The board sorts by votes, and counting them per
    -- row would make the roadmap a fan-out over the whole vote table.
    vote_count   integer     NOT NULL DEFAULT 0,
    comment_count integer    NOT NULL DEFAULT 0,
    subscriber_count integer NOT NULL DEFAULT 0,
    -- Set when this item was merged into another; it then redirects there and
    -- its votes are counted on the winner (§6.6 duplicate merging).
    merged_into_id text      REFERENCES feedback_items (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT feedback_items_type CHECK (
        type IN ('feature_request', 'idea', 'usability_issue', 'bug',
                 'integration_request', 'suggestion', 'custom')
    ),
    CONSTRAINT feedback_items_status CHECK (
        status IN ('open', 'reviewing', 'planned', 'in_progress', 'completed',
                   'declined', 'held')
    ),
    CONSTRAINT feedback_items_visibility CHECK (
        visibility IN ('public', 'private')
    ),
    CONSTRAINT feedback_items_not_merged_into_self CHECK (
        merged_into_id IS DISTINCT FROM id
    )
);

-- The board view: most-voted first, merged items excluded.
CREATE INDEX feedback_items_by_board
    ON feedback_items (workspace_id, board_id, vote_count DESC)
    WHERE merged_into_id IS NULL;

-- The roadmap, grouped by status.
CREATE INDEX feedback_items_by_status
    ON feedback_items (workspace_id, status, updated_at DESC)
    WHERE merged_into_id IS NULL;

-- The moderation queue.
CREATE INDEX feedback_items_held
    ON feedback_items (workspace_id, created_at DESC)
    WHERE status = 'held';

CREATE INDEX feedback_items_by_submitter
    ON feedback_items (workspace_id, submitter_id, created_at DESC)
    WHERE submitter_id IS NOT NULL;

CREATE INDEX feedback_items_search
    ON feedback_items USING gin (to_tsvector('english', title || ' ' || description));

-- One row per customer per item. The primary key is what enforces the "one
-- vote each" rule; doing it in application code would be a check-then-insert
-- race that a double-clicking user wins.
CREATE TABLE feedback_votes (
    item_id     text        NOT NULL REFERENCES feedback_items (id) ON DELETE CASCADE,
    customer_id text        NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    workspace_id text       NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    weight      smallint    NOT NULL DEFAULT 1,
    created_at  timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (item_id, customer_id)
);

-- Enforcing the per-board vote limit: "how many has this customer used?"
CREATE INDEX feedback_votes_by_customer
    ON feedback_votes (workspace_id, customer_id);

CREATE TABLE feedback_comments (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    item_id      text        NOT NULL REFERENCES feedback_items (id) ON DELETE CASCADE,
    author_type  text        NOT NULL,
    author_id    text,
    -- Denormalised so the comment survives the author's deletion.
    author_name  text        NOT NULL DEFAULT '',
    body         text        NOT NULL,
    -- Rendered as a status update from the team rather than a normal comment
    -- (§6.6 status update messages).
    is_official_update boolean NOT NULL DEFAULT false,
    hidden_at    timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT feedback_comments_author_type CHECK (
        author_type IN ('customer', 'agent', 'system')
    )
);

CREATE INDEX feedback_comments_by_item
    ON feedback_comments (item_id, created_at)
    WHERE hidden_at IS NULL;

CREATE TABLE feedback_subscriptions (
    item_id     text        NOT NULL REFERENCES feedback_items (id) ON DELETE CASCADE,
    customer_id text        NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    created_at  timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (item_id, customer_id)
);

-- The notify-on-status-change fan-out.
CREATE INDEX feedback_subscriptions_by_customer
    ON feedback_subscriptions (customer_id);

-- Append-only; the roadmap's "time in each status" metric reads it.
CREATE TABLE feedback_status_history (
    id          text        PRIMARY KEY,
    item_id     text        NOT NULL REFERENCES feedback_items (id) ON DELETE CASCADE,
    from_status text,
    to_status   text        NOT NULL,
    note        text,
    actor_id    text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    occurred_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX feedback_status_history_by_item
    ON feedback_status_history (item_id, occurred_at);

-- Links between feedback and support work (§6.6 agent feedback workflow).
CREATE TABLE feedback_links (
    id          text        PRIMARY KEY,
    workspace_id text       NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    item_id     text        NOT NULL REFERENCES feedback_items (id) ON DELETE CASCADE,
    conversation_id text    REFERENCES conversations (id) ON DELETE CASCADE,
    ticket_id   text        REFERENCES tickets (id) ON DELETE CASCADE,
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT feedback_links_one_target CHECK (
        (conversation_id IS NOT NULL) <> (ticket_id IS NOT NULL)
    )
);

CREATE INDEX feedback_links_by_item ON feedback_links (item_id);
CREATE INDEX feedback_links_by_conversation
    ON feedback_links (conversation_id) WHERE conversation_id IS NOT NULL;
CREATE INDEX feedback_links_by_ticket
    ON feedback_links (ticket_id) WHERE ticket_id IS NOT NULL;

CREATE TABLE feedback_tags (
    item_id text NOT NULL REFERENCES feedback_items (id) ON DELETE CASCADE,
    tag_id  text NOT NULL REFERENCES tags (id) ON DELETE CASCADE,

    PRIMARY KEY (item_id, tag_id)
);

-- --------------------------------------------------------- knowledge base

CREATE TABLE knowledge_bases (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         text        NOT NULL,
    slug         citext      NOT NULL,
    default_language text    NOT NULL DEFAULT 'en',
    languages    text[]      NOT NULL DEFAULT '{en}',
    visibility   text        NOT NULL DEFAULT 'public',
    article_count integer    NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (workspace_id, slug),
    CONSTRAINT knowledge_bases_visibility CHECK (
        visibility IN ('public', 'private')
    )
);

CREATE TABLE article_collections (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    knowledge_base_id text   NOT NULL REFERENCES knowledge_bases (id) ON DELETE CASCADE,
    -- Self-referencing for the collection → category nesting §6.8 describes.
    parent_id    text        REFERENCES article_collections (id) ON DELETE CASCADE,
    name         text        NOT NULL,
    slug         citext      NOT NULL,
    description  text,
    icon         text,
    article_count integer    NOT NULL DEFAULT 0,
    position     smallint    NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (knowledge_base_id, slug),
    CONSTRAINT article_collections_parent_not_self CHECK (
        parent_id IS DISTINCT FROM id
    )
);

CREATE INDEX article_collections_by_kb
    ON article_collections (knowledge_base_id, position);

CREATE TABLE articles (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    knowledge_base_id text   NOT NULL REFERENCES knowledge_bases (id) ON DELETE CASCADE,
    collection_id text       REFERENCES article_collections (id) ON DELETE SET NULL,
    title        text        NOT NULL,
    slug         citext      NOT NULL,
    excerpt      text        NOT NULL DEFAULT '',
    body         text        NOT NULL DEFAULT '',
    state        text        NOT NULL DEFAULT 'draft',
    language     text        NOT NULL DEFAULT 'en',
    -- Groups the language variants of one article (§6.8 language variants).
    -- Null for an article with only one language.
    translation_group_id text,
    author_id    text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    seo          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    view_count   integer     NOT NULL DEFAULT 0,
    helpful_count integer    NOT NULL DEFAULT 0,
    unhelpful_count integer  NOT NULL DEFAULT 0,
    -- Manual ordering within a collection. Authors arrange a collection into a
    -- reading order that alphabetical or chronological sorting would destroy.
    position     smallint    NOT NULL DEFAULT 0,
    version      integer     NOT NULL DEFAULT 1,
    scheduled_at timestamptz,
    published_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (knowledge_base_id, language, slug),
    CONSTRAINT articles_state CHECK (
        state IN ('draft', 'in_review', 'scheduled', 'published', 'archived')
    )
);

-- The public article list: published only, newest first.
CREATE INDEX articles_published
    ON articles (workspace_id, knowledge_base_id, published_at DESC)
    WHERE state = 'published';

CREATE INDEX articles_by_collection
    ON articles (collection_id, position)
    WHERE collection_id IS NOT NULL;

-- The scheduler's publish sweep.
CREATE INDEX articles_scheduled
    ON articles (scheduled_at)
    WHERE state = 'scheduled';

CREATE INDEX articles_by_translation_group
    ON articles (translation_group_id)
    WHERE translation_group_id IS NOT NULL;

-- Search over published articles, title-weighted (§6.8 search: title
-- weighting). setweight makes a title match outrank a body match without a
-- second query or a hand-tuned score.
CREATE INDEX articles_search
    ON articles USING gin ((
        setweight(to_tsvector('english', title), 'A') ||
        setweight(to_tsvector('english', excerpt), 'B') ||
        setweight(to_tsvector('english', body), 'C')
    ))
    WHERE state = 'published';

CREATE TABLE article_revisions (
    id         text        PRIMARY KEY,
    article_id text        NOT NULL REFERENCES articles (id) ON DELETE CASCADE,
    version    integer     NOT NULL,
    title      text        NOT NULL,
    body       text        NOT NULL,
    excerpt    text        NOT NULL DEFAULT '',
    edited_by  text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    note       text,
    created_at timestamptz NOT NULL DEFAULT now(),

    UNIQUE (article_id, version)
);

-- Anonymous "was this helpful?" votes. `fingerprint` is a hash of coarse
-- request attributes, enough to stop a refresh loop inflating the count
-- without storing anything that identifies a reader (§12).
CREATE TABLE article_feedback (
    id          text        PRIMARY KEY,
    workspace_id text       NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    article_id  text        NOT NULL REFERENCES articles (id) ON DELETE CASCADE,
    helpful     boolean     NOT NULL,
    comment     text,
    customer_id text        REFERENCES customers (id) ON DELETE SET NULL,
    fingerprint bytea,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX article_feedback_by_article
    ON article_feedback (article_id, created_at DESC);

CREATE UNIQUE INDEX article_feedback_one_per_fingerprint
    ON article_feedback (article_id, fingerprint)
    WHERE fingerprint IS NOT NULL;

CREATE TABLE article_tags (
    article_id text NOT NULL REFERENCES articles (id) ON DELETE CASCADE,
    tag_id     text NOT NULL REFERENCES tags (id) ON DELETE CASCADE,

    PRIMARY KEY (article_id, tag_id)
);

CREATE TABLE article_relations (
    article_id text NOT NULL REFERENCES articles (id) ON DELETE CASCADE,
    related_id text NOT NULL REFERENCES articles (id) ON DELETE CASCADE,
    position   smallint NOT NULL DEFAULT 0,

    PRIMARY KEY (article_id, related_id),
    CONSTRAINT article_relations_not_self CHECK (article_id <> related_id)
);

-- Every search performed against the knowledge base, including the ones that
-- found nothing. §6.8 asks specifically for no-result analytics: the searches
-- that fail are the article backlog, and they are invisible unless recorded.
CREATE TABLE article_searches (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    knowledge_base_id text   REFERENCES knowledge_bases (id) ON DELETE CASCADE,
    query        text        NOT NULL,
    result_count integer     NOT NULL DEFAULT 0,
    language     text,
    -- 'widget' | 'portal' | 'dashboard'
    surface      text        NOT NULL DEFAULT 'portal',
    clicked_article_id text  REFERENCES articles (id) ON DELETE SET NULL,
    occurred_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX article_searches_no_results
    ON article_searches (workspace_id, occurred_at DESC)
    WHERE result_count = 0;

CREATE INDEX article_searches_by_occurred_at
    ON article_searches (occurred_at);

-- ------------------------------------------------------------- changelog

CREATE TABLE changelog_entries (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    title        text        NOT NULL,
    body         text        NOT NULL DEFAULT '',
    -- 'added' | 'improved' | 'fixed' | 'removed'
    kind         text        NOT NULL DEFAULT 'added',
    -- Set when this entry was published from a completed feedback item, so the
    -- item's subscribers can be notified (§7.6).
    feedback_item_id text    REFERENCES feedback_items (id) ON DELETE SET NULL,
    published_at timestamptz,
    created_by   text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT changelog_entries_kind CHECK (
        kind IN ('added', 'improved', 'fixed', 'removed')
    )
);

CREATE INDEX changelog_published
    ON changelog_entries (workspace_id, published_at DESC)
    WHERE published_at IS NOT NULL;

-- --------------------------------------------------------------- surveys

CREATE TABLE surveys (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         text        NOT NULL,
    type         text        NOT NULL DEFAULT 'csat',
    -- Where it is delivered: widget, portal, email, link.
    delivery     text[]      NOT NULL DEFAULT '{email}',
    -- What causes it to be sent, e.g. { on: 'conversation.resolved',
    -- delay_minutes: 30 }.
    trigger      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    -- Branding and copy for the completion page.
    completion   jsonb       NOT NULL DEFAULT '{}'::jsonb,
    anonymous    boolean     NOT NULL DEFAULT false,
    -- Null means unlimited.
    max_responses integer,
    response_count integer   NOT NULL DEFAULT 0,
    sent_count   integer     NOT NULL DEFAULT 0,
    enabled      boolean     NOT NULL DEFAULT true,
    expires_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT surveys_type CHECK (type IN ('csat', 'ces', 'nps', 'custom'))
);

CREATE INDEX surveys_by_workspace ON surveys (workspace_id, created_at);

CREATE TABLE survey_questions (
    id         text        PRIMARY KEY,
    survey_id  text        NOT NULL REFERENCES surveys (id) ON DELETE CASCADE,
    prompt     text        NOT NULL,
    type       text        NOT NULL,
    options    jsonb       NOT NULL DEFAULT '[]'::jsonb,
    required   boolean     NOT NULL DEFAULT false,
    condition  jsonb,
    position   smallint    NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT survey_questions_type CHECK (
        type IN ('star', 'number', 'emoji', 'choice', 'multi_choice', 'text', 'boolean')
    )
);

CREATE INDEX survey_questions_by_survey ON survey_questions (survey_id, position);

CREATE TABLE survey_responses (
    id           text        PRIMARY KEY,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    survey_id    text        NOT NULL REFERENCES surveys (id) ON DELETE CASCADE,
    customer_id  text        REFERENCES customers (id) ON DELETE SET NULL,
    conversation_id text     REFERENCES conversations (id) ON DELETE SET NULL,
    ticket_id    text        REFERENCES tickets (id) ON DELETE SET NULL,
    -- The agent whose work is being rated, captured at send time — reassigning
    -- the conversation afterwards must not move the score.
    agent_id     text        REFERENCES workspace_members (id) ON DELETE SET NULL,
    team_id      text        REFERENCES teams (id) ON DELETE SET NULL,
    -- The headline number, normalised across survey types so reporting does
    -- not branch on `surveys.type`.
    score        real,
    comment      text,
    token_hash   bytea,
    sent_at      timestamptz,
    submitted_at timestamptz
);

-- The response list and the aggregate queries, which only count submissions.
CREATE INDEX survey_responses_by_survey
    ON survey_responses (workspace_id, survey_id, submitted_at DESC)
    WHERE submitted_at IS NOT NULL;

CREATE INDEX survey_responses_by_agent
    ON survey_responses (workspace_id, agent_id, submitted_at DESC)
    WHERE agent_id IS NOT NULL AND submitted_at IS NOT NULL;

-- Resolving the one-time link in a survey email.
CREATE UNIQUE INDEX survey_responses_token
    ON survey_responses (token_hash)
    WHERE token_hash IS NOT NULL;

CREATE TABLE survey_answers (
    response_id text NOT NULL REFERENCES survey_responses (id) ON DELETE CASCADE,
    question_id text NOT NULL REFERENCES survey_questions (id) ON DELETE CASCADE,
    value       jsonb,

    PRIMARY KEY (response_id, question_id)
);

CREATE INDEX survey_answers_by_question ON survey_answers (question_id);

COMMIT;
