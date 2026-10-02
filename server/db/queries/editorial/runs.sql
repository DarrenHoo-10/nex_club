-- name: InsertRun :one
INSERT INTO processing_runs (
    id, raw_revision_id, stage, pipeline_key, pipeline_plan,
    input_hash, rule_version, rerun_no, run_key, status, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending', $10, $10
)
ON CONFLICT (raw_revision_id, pipeline_key, stage) DO NOTHING
RETURNING id;

-- name: GetRun :one
SELECT id, raw_revision_id, stage, pipeline_key, pipeline_plan, input_hash, rule_version,
       rerun_no, run_key, status, output, error_code, error_message, attempt_count
FROM processing_runs
WHERE id = $1;

-- name: LockRun :one
SELECT id, raw_revision_id, stage, pipeline_key, pipeline_plan, input_hash, rule_version,
       rerun_no, run_key, status, output, error_code, error_message, attempt_count
FROM processing_runs
WHERE id = $1
FOR UPDATE;

-- name: GetRunByStage :one
SELECT id, raw_revision_id, stage, pipeline_key, pipeline_plan, input_hash, rule_version,
       rerun_no, run_key, status, output, error_code, error_message, attempt_count
FROM processing_runs
WHERE raw_revision_id = $1 AND pipeline_key = $2 AND stage = $3;

-- name: ListRunsForItem :many
SELECT pr.id, pr.raw_revision_id, pr.stage, pr.pipeline_key, pr.status, pr.rule_version,
       pr.run_key, pr.input_hash, pr.error_code, pr.output
FROM processing_runs pr
JOIN raw_item_revisions rr ON rr.id = pr.raw_revision_id
WHERE rr.raw_item_id = $1
ORDER BY pr.created_at, pr.stage;

-- name: MarkRunRunning :execrows
UPDATE processing_runs
SET status = 'running',
    attempt_count = attempt_count + 1,
    started_at = COALESCE(started_at, $2),
    updated_at = $2
WHERE id = $1 AND status IN ('pending', 'running', 'failed');

-- name: SaveRunSucceeded :execrows
UPDATE processing_runs
SET status = 'succeeded',
    output = $2,
    error_code = NULL,
    error_message = NULL,
    finished_at = $3,
    updated_at = $3
WHERE id = $1 AND status IN ('pending', 'running', 'failed');

-- name: SaveRunBlocked :execrows
UPDATE processing_runs
SET status = 'blocked',
    output = $2,
    error_code = $3,
    error_message = $4,
    finished_at = $5,
    updated_at = $5
WHERE id = $1 AND status IN ('pending', 'running', 'failed');

-- name: SaveRunFailed :execrows
UPDATE processing_runs
SET status = 'failed',
    output = $2,
    error_code = $3,
    error_message = $4,
    finished_at = $5,
    updated_at = $5
WHERE id = $1 AND status IN ('pending', 'running', 'failed');

-- name: SaveRunStale :execrows
UPDATE processing_runs
SET status = 'stale',
    output = $2,
    finished_at = $3,
    updated_at = $3
WHERE id = $1 AND status IN ('pending', 'running', 'failed');

-- name: LockRawItem :one
SELECT id, owner_source_id, identity_key, canonical_url, current_revision_id
FROM raw_items
WHERE id = $1
FOR UPDATE;

-- name: GetRawRevision :one
SELECT id, raw_item_id, revision_no, content_hash, title, excerpt, body_text, body_html, language, raw_payload
FROM raw_item_revisions
WHERE id = $1;

-- name: GetSource :one
SELECT id, kind, auto_update_fields
FROM sources
WHERE id = $1;

-- name: FindActiveTag :one
SELECT t.id
FROM tags t
LEFT JOIN tag_aliases a ON a.tag_id = t.id AND a.dimension = t.dimension
WHERE t.status = 'active'
  AND (
    lower(btrim(t.name)) = lower(btrim($1))
    OR lower(btrim(a.alias)) = lower(btrim($1))
    OR a.normalized_alias = lower(btrim($1))
  )
ORDER BY t.id
LIMIT 1;

-- name: LockTaxonomyShared :exec
SELECT pg_advisory_xact_lock_shared(1313821761::int4, 1::int4);
