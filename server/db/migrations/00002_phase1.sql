-- +goose Up
-- Phase-1 application tables. The goose up section of
-- db/migrations/00002_phase1.sql must stay identical to this file.
-- Phase-2 columns and tables are not created here.

CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE admin_users (
    id uuid PRIMARY KEY,
    username text NOT NULL UNIQUE CHECK (btrim(username) <> ''),
    password_hash text NOT NULL CHECK (btrim(password_hash) <> ''),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    last_login_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE admin_sessions (
    id uuid PRIMARY KEY,
    admin_id uuid NOT NULL REFERENCES admin_users (id),
    token_hash text NOT NULL UNIQUE CHECK (btrim(token_hash) <> ''),
    csrf_secret_hash text NOT NULL CHECK (btrim(csrf_secret_hash) <> ''),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX admin_sessions_admin_id_idx ON admin_sessions (admin_id);
CREATE INDEX admin_sessions_expires_at_idx ON admin_sessions (expires_at);

CREATE TABLE audit_logs (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_admin_id uuid REFERENCES admin_users (id),
    actor_type text NOT NULL CHECK (actor_type IN ('admin', 'system', 'ingest')),
    action text NOT NULL CHECK (btrim(action) <> ''),
    target_type text NOT NULL CHECK (btrim(target_type) <> ''),
    target_id text NOT NULL,
    changes jsonb NOT NULL CHECK (jsonb_typeof(changes) = 'object'),
    request_id text,
    job_id text,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_logs_target_idx ON audit_logs (target_type, target_id, created_at DESC);
CREATE INDEX audit_logs_actor_idx ON audit_logs (actor_admin_id, created_at DESC);

CREATE TABLE idempotency_requests (
    id uuid PRIMARY KEY,
    principal_key text NOT NULL CHECK (char_length(principal_key) BETWEEN 1 AND 200),
    scope text NOT NULL CHECK (char_length(scope) BETWEEN 1 AND 300),
    idempotency_key text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 128),
    request_hash text NOT NULL CHECK (btrim(request_hash) <> ''),
    status text NOT NULL DEFAULT 'processing' CHECK (status IN ('processing', 'completed')),
    response_status smallint,
    response_body jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    UNIQUE (principal_key, scope, idempotency_key),
    CHECK (
        (status = 'processing' AND response_status IS NULL AND response_body IS NULL)
        OR status = 'completed'
    ),
    CHECK (response_body IS NULL OR jsonb_typeof(response_body) IN ('object', 'array'))
);

CREATE INDEX idempotency_requests_expires_at_idx ON idempotency_requests (expires_at);

CREATE TABLE resources (
    id uuid PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('tool', 'tutorial', 'repo')),
    identity_key text,
    slug text NOT NULL CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'hidden', 'archived')),
    draft_revision_id uuid,
    edit_version bigint NOT NULL DEFAULT 1 CHECK (edit_version > 0),
    field_locks text[] NOT NULL DEFAULT '{}',
    is_demo boolean NOT NULL DEFAULT false,
    freshness_eligible boolean NOT NULL DEFAULT true,
    first_published_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    last_checked_at timestamptz,
    UNIQUE (id, kind)
);

CREATE UNIQUE INDEX resources_identity_key_idx ON resources (kind, identity_key) WHERE identity_key IS NOT NULL;
CREATE INDEX resources_public_latest_idx ON resources (kind, first_published_at DESC, id DESC) WHERE status = 'published' AND NOT is_demo;
CREATE INDEX resources_admin_idx ON resources (status, updated_at DESC, id);

CREATE TABLE resource_revisions (
    id uuid PRIMARY KEY,
    resource_id uuid NOT NULL REFERENCES resources (id),
    revision_no bigint NOT NULL CHECK (revision_no > 0),
    schema_version integer NOT NULL DEFAULT 1,
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    origin text NOT NULL CHECK (origin IN ('manual', 'import', 'pipeline')),
    created_by uuid REFERENCES admin_users (id),
    change_reason text NOT NULL CHECK (btrim(change_reason) <> ''),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (resource_id, revision_no),
    UNIQUE (resource_id, id)
);

CREATE INDEX resource_revisions_resource_created_idx ON resource_revisions (resource_id, created_at DESC);
CREATE INDEX resource_revisions_created_by_idx ON resource_revisions (created_by);

ALTER TABLE resources
    ADD CONSTRAINT resources_draft_revision_fk
    FOREIGN KEY (id, draft_revision_id) REFERENCES resource_revisions (resource_id, id);

CREATE TABLE tags (
    id uuid PRIMARY KEY,
    dimension text NOT NULL CHECK (dimension IN ('category', 'capability', 'audience', 'difficulty')),
    name text NOT NULL CHECK (btrim(name) <> ''),
    slug text NOT NULL CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled', 'merged')),
    merged_into_id uuid REFERENCES tags (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (dimension, slug),
    UNIQUE (id, dimension),
    CHECK (
        (status = 'merged' AND merged_into_id IS NOT NULL AND merged_into_id <> id)
        OR (status <> 'merged' AND merged_into_id IS NULL)
    )
);

CREATE INDEX tags_merged_into_idx ON tags (merged_into_id);

CREATE TABLE tag_aliases (
    id uuid PRIMARY KEY,
    tag_id uuid NOT NULL,
    dimension text NOT NULL,
    alias text NOT NULL CHECK (btrim(alias) <> ''),
    normalized_alias text NOT NULL CHECK (btrim(normalized_alias) <> ''),
    is_primary boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (dimension, normalized_alias),
    FOREIGN KEY (tag_id, dimension) REFERENCES tags (id, dimension)
);

CREATE UNIQUE INDEX tag_aliases_one_primary_idx ON tag_aliases (tag_id) WHERE is_primary;

CREATE TABLE resource_publications (
    resource_id uuid PRIMARY KEY REFERENCES resources (id),
    revision_id uuid NOT NULL,
    kind text NOT NULL,
    title text NOT NULL CHECK (btrim(title) <> ''),
    aliases text[] NOT NULL DEFAULT '{}',
    summary text NOT NULL CHECK (btrim(summary) <> ''),
    body_markdown text,
    cover_urls text[] NOT NULL DEFAULT '{}',
    primary_category_id uuid REFERENCES tags (id),
    quality_score smallint NOT NULL DEFAULT 0 CHECK (quality_score BETWEEN 0 AND 100),
    recommendation_reason text,
    details jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(details) = 'object'),
    search_text text NOT NULL,
    content_updated_at timestamptz NOT NULL,
    projected_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (resource_id, revision_id) REFERENCES resource_revisions (resource_id, id),
    FOREIGN KEY (resource_id, kind) REFERENCES resources (id, kind)
);

CREATE INDEX resource_publications_primary_category_idx ON resource_publications (primary_category_id);
CREATE INDEX resource_publications_revision_idx ON resource_publications (revision_id);
CREATE INDEX resource_publications_title_norm_idx ON resource_publications (lower(btrim(title)));
CREATE INDEX resource_publications_search_trgm_idx ON resource_publications USING gin (search_text gin_trgm_ops);

CREATE TABLE resource_tags (
    resource_id uuid NOT NULL REFERENCES resource_publications (resource_id),
    tag_id uuid NOT NULL REFERENCES tags (id),
    assigned_by text NOT NULL CHECK (assigned_by IN ('manual', 'import', 'accepted_pipeline')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (resource_id, tag_id)
);

CREATE INDEX resource_tags_tag_idx ON resource_tags (tag_id, resource_id);

CREATE TABLE featured_slots (
    id uuid PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('tool', 'tutorial', 'repo')),
    placement text NOT NULL DEFAULT 'hero' CHECK (placement = 'hero'),
    position integer NOT NULL CHECK (position > 0),
    resource_id uuid NOT NULL,
    starts_at timestamptz NOT NULL,
    ends_at timestamptz,
    enabled boolean NOT NULL DEFAULT true,
    created_by uuid NOT NULL REFERENCES admin_users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (ends_at IS NULL OR ends_at > starts_at),
    FOREIGN KEY (resource_id, kind) REFERENCES resources (id, kind),
    EXCLUDE USING gist (
        kind WITH =,
        placement WITH =,
        position WITH =,
        tstzrange(starts_at, COALESCE(ends_at, 'infinity'::timestamptz), '[)') WITH &&
    ) WHERE (enabled)
);

CREATE INDEX featured_slots_resource_idx ON featured_slots (resource_id);
CREATE INDEX featured_slots_created_by_idx ON featured_slots (created_by);

CREATE TABLE interaction_events (
    id uuid PRIMARY KEY,
    resource_id uuid NOT NULL REFERENCES resources (id),
    event_type text NOT NULL CHECK (event_type IN ('detail_view', 'outbound_click')),
    visitor_hash text NOT NULL CHECK (btrim(visitor_hash) <> ''),
    bucket_start timestamptz NOT NULL,
    accepted_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (resource_id, visitor_hash, event_type, bucket_start)
);

CREATE INDEX interaction_events_accepted_idx ON interaction_events (accepted_at, resource_id);
CREATE INDEX interaction_events_resource_idx ON interaction_events (resource_id, accepted_at);

CREATE TABLE resource_metrics_daily (
    resource_id uuid NOT NULL REFERENCES resources (id),
    metric_date date NOT NULL,
    detail_views bigint NOT NULL DEFAULT 0 CHECK (detail_views >= 0),
    outbound_clicks bigint NOT NULL DEFAULT 0 CHECK (outbound_clicks >= 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (resource_id, metric_date)
);

CREATE INDEX resource_metrics_daily_date_idx ON resource_metrics_daily (metric_date, resource_id);

CREATE TABLE ranking_runs (
    id uuid PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('tool', 'tutorial', 'repo')),
    rule_version text NOT NULL CHECK (btrim(rule_version) <> ''),
    parameters jsonb NOT NULL CHECK (jsonb_typeof(parameters) = 'object'),
    status text NOT NULL DEFAULT 'building' CHECK (status IN ('building', 'ready', 'failed', 'retired')),
    is_current boolean NOT NULL DEFAULT false,
    computed_at timestamptz,
    expires_at timestamptz NOT NULL,
    error_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, kind),
    CHECK (NOT is_current OR (status = 'ready' AND computed_at IS NOT NULL))
);

CREATE UNIQUE INDEX ranking_runs_one_current_idx ON ranking_runs (kind) WHERE is_current;

CREATE TABLE ranking_entries (
    run_id uuid NOT NULL,
    resource_id uuid NOT NULL,
    kind text NOT NULL,
    quality_score numeric(7, 4) NOT NULL CHECK (quality_score >= 0 AND quality_score <= 100),
    heat_score numeric(7, 4) NOT NULL CHECK (heat_score >= 0 AND heat_score <= 100),
    freshness_score numeric(7, 4) NOT NULL CHECK (freshness_score >= 0 AND freshness_score <= 100),
    recommendation_score numeric(7, 4) NOT NULL CHECK (recommendation_score >= 0 AND recommendation_score <= 100),
    heat_position integer NOT NULL CHECK (heat_position > 0),
    recommendation_position integer NOT NULL CHECK (recommendation_position > 0),
    reason_code text,
    PRIMARY KEY (run_id, resource_id),
    UNIQUE (run_id, heat_position),
    UNIQUE (run_id, recommendation_position),
    FOREIGN KEY (run_id, kind) REFERENCES ranking_runs (id, kind) ON DELETE CASCADE,
    FOREIGN KEY (resource_id, kind) REFERENCES resources (id, kind)
);

CREATE INDEX ranking_entries_resource_idx ON ranking_entries (resource_id);

-- +goose Down
DROP TABLE IF EXISTS ranking_entries;
DROP TABLE IF EXISTS ranking_runs;
DROP TABLE IF EXISTS resource_metrics_daily;
DROP TABLE IF EXISTS interaction_events;
DROP TABLE IF EXISTS featured_slots;
DROP TABLE IF EXISTS resource_tags;
DROP TABLE IF EXISTS resource_publications;
DROP TABLE IF EXISTS tag_aliases;
DROP TABLE IF EXISTS tags;
ALTER TABLE IF EXISTS resources DROP CONSTRAINT IF EXISTS resources_draft_revision_fk;
DROP TABLE IF EXISTS resource_revisions;
DROP TABLE IF EXISTS resources;
DROP TABLE IF EXISTS idempotency_requests;
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS admin_sessions;
DROP TABLE IF EXISTS admin_users;
