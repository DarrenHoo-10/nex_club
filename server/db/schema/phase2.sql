-- Phase-2 application tables. The goose up section of
-- db/migrations/00004_phase2.sql must stay identical to this file.
-- Phase-1 tables already exist. Do not edit the phase-1 up script.

ALTER TABLE idempotency_requests
    ADD COLUMN parent_id uuid,
    ADD COLUMN item_index integer,
    ADD COLUMN request_meta jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE idempotency_requests
    ADD CONSTRAINT idempotency_requests_id_principal_key UNIQUE (id, principal_key),
    ADD CONSTRAINT idempotency_requests_parent_fk
        FOREIGN KEY (parent_id, principal_key)
        REFERENCES idempotency_requests (id, principal_key),
    ADD CONSTRAINT idempotency_requests_parent_item_key UNIQUE (parent_id, item_index),
    ADD CONSTRAINT idempotency_requests_parent_pair CHECK (
        (parent_id IS NULL AND item_index IS NULL)
        OR (parent_id IS NOT NULL AND item_index IS NOT NULL)
    ),
    ADD CONSTRAINT idempotency_requests_item_index_range CHECK (
        item_index IS NULL OR item_index BETWEEN 0 AND 49
    ),
    ADD CONSTRAINT idempotency_requests_parent_not_self CHECK (
        parent_id IS NULL OR parent_id <> id
    ),
    ADD CONSTRAINT idempotency_requests_meta_object CHECK (jsonb_typeof(request_meta) = 'object');

CREATE TABLE sources (
    id uuid PRIMARY KEY,
    source_key text NOT NULL UNIQUE CHECK (btrim(source_key) <> ''),
    name text NOT NULL CHECK (btrim(name) <> ''),
    kind text NOT NULL CHECK (kind IN ('github', 'rss', 'json', 'web', 'x', 'wechat', 'external')),
    participation_mode text NOT NULL DEFAULT 'internal' CHECK (participation_mode IN ('content', 'signal', 'internal')),
    trust_tier text NOT NULL DEFAULT 'community' CHECK (trust_tier IN ('official', 'verified', 'community', 'excluded')),
    config jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(config) = 'object'),
    credential_ref text,
    enabled boolean NOT NULL DEFAULT false,
    interval_seconds integer NOT NULL CHECK (interval_seconds > 0),
    checkpoint jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(checkpoint) = 'object'),
    next_fetch_at timestamptz,
    last_success_at timestamptz,
    failure_count integer NOT NULL DEFAULT 0 CHECK (failure_count >= 0),
    auto_update_fields text[] NOT NULL DEFAULT '{}',
    allow_fulltext boolean NOT NULL DEFAULT false,
    edit_version bigint NOT NULL DEFAULT 1 CHECK (edit_version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sources_due_idx ON sources (next_fetch_at, id) WHERE enabled;

CREATE TABLE source_runs (
    id uuid PRIMARY KEY,
    source_id uuid NOT NULL REFERENCES sources (id),
    source_edit_version bigint NOT NULL CHECK (source_edit_version > 0),
    scheduled_for timestamptz NOT NULL,
    run_key text NOT NULL UNIQUE CHECK (btrim(run_key) <> ''),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'cancelled')),
    checkpoint_before jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(checkpoint_before) = 'object'),
    checkpoint_after jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(checkpoint_after) = 'object'),
    stats jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(stats) = 'object'),
    error_code text,
    error_message text,
    river_job_id bigint,
    started_at timestamptz,
    finished_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX source_runs_source_idx ON source_runs (source_id, created_at DESC);
CREATE INDEX source_runs_status_idx ON source_runs (status, created_at);

CREATE TABLE ingest_credentials (
    id uuid PRIMARY KEY,
    source_id uuid NOT NULL REFERENCES sources (id),
    name text NOT NULL CHECK (btrim(name) <> ''),
    token_hash text NOT NULL UNIQUE CHECK (btrim(token_hash) <> ''),
    scopes text[] NOT NULL CHECK (
        cardinality(scopes) > 0
        AND scopes <@ ARRAY['items:write']::text[]
    ),
    expires_at timestamptz,
    revoked_at timestamptz,
    created_by uuid NOT NULL REFERENCES admin_users (id),
    last_used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ingest_credentials_source_idx ON ingest_credentials (source_id);
CREATE INDEX ingest_credentials_created_by_idx ON ingest_credentials (created_by);
CREATE INDEX ingest_credentials_expires_at_idx ON ingest_credentials (expires_at);

CREATE TABLE raw_items (
    id uuid PRIMARY KEY,
    owner_source_id uuid NOT NULL REFERENCES sources (id),
    identity_key text NOT NULL UNIQUE CHECK (btrim(identity_key) <> ''),
    canonical_url text,
    current_revision_id uuid,
    is_backfill boolean NOT NULL DEFAULT false,
    first_discovered_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL
);

CREATE INDEX raw_items_owner_idx ON raw_items (owner_source_id);
CREATE INDEX raw_items_last_seen_idx ON raw_items (last_seen_at);

CREATE TABLE raw_item_revisions (
    id uuid PRIMARY KEY,
    raw_item_id uuid NOT NULL REFERENCES raw_items (id),
    revision_no bigint NOT NULL CHECK (revision_no > 0),
    content_hash text NOT NULL CHECK (btrim(content_hash) <> ''),
    normalization_version text NOT NULL CHECK (btrim(normalization_version) <> ''),
    title text NOT NULL,
    excerpt text,
    body_text text,
    body_html text,
    author text,
    language text,
    source_published_at timestamptz,
    source_updated_at timestamptz,
    raw_payload jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(raw_payload) = 'object'),
    fetched_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (raw_item_id, revision_no),
    UNIQUE (raw_item_id, id)
);

CREATE INDEX raw_item_revisions_hash_idx ON raw_item_revisions (raw_item_id, content_hash);

ALTER TABLE raw_items
    ADD CONSTRAINT raw_items_current_revision_fk
    FOREIGN KEY (id, current_revision_id)
    REFERENCES raw_item_revisions (raw_item_id, id)
    ON DELETE RESTRICT;

CREATE TABLE raw_item_discoveries (
    id uuid PRIMARY KEY,
    raw_item_id uuid NOT NULL REFERENCES raw_items (id),
    source_id uuid NOT NULL REFERENCES sources (id),
    source_item_key text NOT NULL CHECK (btrim(source_item_key) <> ''),
    observed_url text,
    first_seen_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    seen_count bigint NOT NULL DEFAULT 1 CHECK (seen_count >= 0),
    UNIQUE (source_id, source_item_key)
);

CREATE INDEX raw_item_discoveries_item_idx ON raw_item_discoveries (raw_item_id);

CREATE TABLE processing_runs (
    id uuid PRIMARY KEY,
    raw_revision_id uuid NOT NULL REFERENCES raw_item_revisions (id),
    stage text NOT NULL CHECK (stage IN ('extract', 'prefilter', 'structure', 'score', 'write', 'propose')),
    pipeline_key text NOT NULL CHECK (btrim(pipeline_key) <> ''),
    pipeline_plan jsonb NOT NULL CHECK (jsonb_typeof(pipeline_plan) = 'object'),
    input_hash text NOT NULL CHECK (btrim(input_hash) <> ''),
    rule_version text NOT NULL CHECK (btrim(rule_version) <> ''),
    rerun_no integer NOT NULL DEFAULT 0 CHECK (rerun_no >= 0),
    run_key text NOT NULL UNIQUE CHECK (btrim(run_key) <> ''),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'blocked', 'stale')),
    output jsonb,
    attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    lease_until timestamptz,
    river_job_id bigint,
    error_code text,
    error_message text,
    started_at timestamptz,
    finished_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (raw_revision_id, pipeline_key, stage)
);

CREATE INDEX processing_runs_stage_idx ON processing_runs (raw_revision_id, stage);
CREATE INDEX processing_runs_lease_idx ON processing_runs (status, lease_until);

CREATE TABLE change_proposals (
    id uuid PRIMARY KEY,
    processing_run_id uuid NOT NULL REFERENCES processing_runs (id),
    proposal_no integer NOT NULL DEFAULT 1 CHECK (proposal_no > 0),
    resource_id uuid REFERENCES resources (id),
    base_edit_version bigint,
    proposed_kind text NOT NULL CHECK (proposed_kind IN ('tool', 'tutorial', 'repo')),
    proposed_payload jsonb NOT NULL CHECK (jsonb_typeof(proposed_payload) = 'object'),
    field_changes jsonb NOT NULL CHECK (jsonb_typeof(field_changes) = 'object'),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'partially_applied', 'applied', 'rejected', 'conflict')),
    reviewed_by uuid REFERENCES admin_users (id),
    review_decisions jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(review_decisions) = 'object'),
    reviewed_at timestamptz,
    applied_resource_id uuid,
    applied_revision_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (processing_run_id, proposal_no),
    CHECK (
        (resource_id IS NULL AND base_edit_version IS NULL)
        OR (resource_id IS NOT NULL AND base_edit_version IS NOT NULL)
    ),
    CHECK (
        (applied_resource_id IS NULL AND applied_revision_id IS NULL)
        OR (applied_resource_id IS NOT NULL AND applied_revision_id IS NOT NULL)
    ),
    FOREIGN KEY (applied_resource_id, applied_revision_id)
        REFERENCES resource_revisions (resource_id, id)
);

CREATE INDEX change_proposals_status_idx ON change_proposals (status, created_at);
CREATE INDEX change_proposals_resource_idx ON change_proposals (resource_id);
CREATE INDEX change_proposals_reviewer_idx ON change_proposals (reviewed_by);
CREATE INDEX change_proposals_applied_idx ON change_proposals (applied_revision_id);

CREATE TABLE resource_evidence (
    id uuid PRIMARY KEY,
    resource_id uuid NOT NULL REFERENCES resources (id),
    resource_revision_id uuid,
    proposal_id uuid REFERENCES change_proposals (id),
    raw_revision_id uuid NOT NULL REFERENCES raw_item_revisions (id),
    field_path text NOT NULL CHECK (btrim(field_path) <> ''),
    evidence_excerpt text,
    locator jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(locator) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (resource_id, resource_revision_id)
        REFERENCES resource_revisions (resource_id, id)
);

CREATE INDEX resource_evidence_resource_idx ON resource_evidence (resource_id);
CREATE INDEX resource_evidence_revision_idx ON resource_evidence (resource_revision_id);
CREATE INDEX resource_evidence_proposal_idx ON resource_evidence (proposal_id);
CREATE INDEX resource_evidence_raw_idx ON resource_evidence (raw_revision_id);

CREATE TABLE provider_calls (
    id uuid PRIMARY KEY,
    processing_run_id uuid REFERENCES processing_runs (id),
    source_run_id uuid REFERENCES source_runs (id),
    provider_key text NOT NULL CHECK (btrim(provider_key) <> ''),
    model text,
    profile_version text,
    request_key text NOT NULL CHECK (btrim(request_key) <> ''),
    attempt_no integer NOT NULL CHECK (attempt_no > 0),
    status text NOT NULL DEFAULT 'prepared' CHECK (status IN ('prepared', 'sent', 'succeeded', 'failed', 'unknown')),
    provider_request_id text,
    response_payload jsonb,
    input_tokens bigint CHECK (input_tokens IS NULL OR input_tokens >= 0),
    output_tokens bigint CHECK (output_tokens IS NULL OR output_tokens >= 0),
    currency text NOT NULL CHECK (btrim(currency) <> ''),
    reserved_cost numeric(18, 8) NOT NULL CHECK (reserved_cost >= 0),
    actual_cost numeric(18, 8) CHECK (actual_cost IS NULL OR actual_cost >= 0),
    error_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    sent_at timestamptz,
    completed_at timestamptz,
    UNIQUE (request_key, attempt_no),
    CHECK ((processing_run_id IS NULL) <> (source_run_id IS NULL)),
    CHECK (processing_run_id IS NULL OR (profile_version IS NOT NULL AND btrim(profile_version) <> ''))
);

CREATE UNIQUE INDEX provider_calls_open_request_idx
    ON provider_calls (request_key)
    WHERE status IN ('prepared', 'sent', 'unknown', 'succeeded');

CREATE INDEX provider_calls_processing_idx ON provider_calls (processing_run_id);
CREATE INDEX provider_calls_source_run_idx ON provider_calls (source_run_id);
CREATE INDEX provider_calls_provider_idx ON provider_calls (provider_key, created_at);

CREATE TABLE external_metric_snapshots (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    resource_id uuid NOT NULL REFERENCES resources (id),
    source_id uuid NOT NULL REFERENCES sources (id),
    observed_at timestamptz NOT NULL,
    stars bigint CHECK (stars IS NULL OR stars >= 0),
    forks bigint CHECK (forks IS NULL OR forks >= 0),
    open_issues bigint CHECK (open_issues IS NULL OR open_issues >= 0),
    extra jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(extra) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (resource_id, source_id, observed_at)
);

CREATE INDEX external_metric_snapshots_resource_idx ON external_metric_snapshots (resource_id, observed_at DESC);
CREATE INDEX external_metric_snapshots_source_idx ON external_metric_snapshots (source_id);

CREATE TABLE provider_budget_windows (
    id uuid PRIMARY KEY,
    scope_key text NOT NULL CHECK (btrim(scope_key) <> ''),
    currency text NOT NULL CHECK (btrim(currency) <> ''),
    window_start timestamptz NOT NULL,
    window_end timestamptz NOT NULL,
    limit_amount numeric(18, 8) NOT NULL CHECK (limit_amount >= 0),
    reserved_amount numeric(18, 8) NOT NULL DEFAULT 0 CHECK (reserved_amount >= 0),
    spent_amount numeric(18, 8) NOT NULL DEFAULT 0 CHECK (spent_amount >= 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (scope_key, currency, window_start, window_end),
    CHECK (window_end > window_start)
);

CREATE TABLE provider_budget_reservations (
    provider_call_id uuid NOT NULL REFERENCES provider_calls (id),
    budget_window_id uuid NOT NULL REFERENCES provider_budget_windows (id),
    reserved_amount numeric(18, 8) NOT NULL CHECK (reserved_amount >= 0),
    settled_amount numeric(18, 8) CHECK (settled_amount IS NULL OR settled_amount >= 0),
    status text NOT NULL DEFAULT 'held' CHECK (status IN ('held', 'settled', 'released')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider_call_id, budget_window_id)
);

CREATE INDEX provider_budget_reservations_window_idx ON provider_budget_reservations (budget_window_id);
