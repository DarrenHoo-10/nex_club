-- name: LockSource :one
SELECT id, source_key, name, kind, participation_mode, trust_tier, config, credential_ref,
       enabled, interval_seconds, checkpoint, next_fetch_at, last_success_at, failure_count,
       auto_update_fields, allow_fulltext, edit_version
FROM sources
WHERE id = sqlc.arg('id')
FOR UPDATE;

-- name: LockSourceByKey :one
SELECT id, source_key, name, kind, participation_mode, trust_tier, config, credential_ref,
       enabled, interval_seconds, checkpoint, next_fetch_at, last_success_at, failure_count,
       auto_update_fields, allow_fulltext, edit_version
FROM sources
WHERE source_key = sqlc.arg('source_key')
FOR UPDATE;

-- name: GetSourceByKey :one
SELECT id, source_key, name, kind, participation_mode, trust_tier, config, credential_ref,
       enabled, interval_seconds, checkpoint, next_fetch_at, last_success_at, failure_count,
       auto_update_fields, allow_fulltext, edit_version
FROM sources
WHERE source_key = sqlc.arg('source_key');

-- name: LockDueSource :one
SELECT s.id, s.source_key, s.name, s.kind, s.participation_mode, s.trust_tier, s.config, s.credential_ref,
       s.enabled, s.interval_seconds, s.checkpoint, s.next_fetch_at, s.last_success_at, s.failure_count,
       s.auto_update_fields, s.allow_fulltext, s.edit_version
FROM sources s
WHERE s.enabled
  AND s.trust_tier <> 'excluded'
  AND s.next_fetch_at IS NOT NULL
  AND s.next_fetch_at <= sqlc.arg('now')
  AND NOT EXISTS (
      SELECT 1 FROM source_runs r
      WHERE r.source_id = s.id AND r.status IN ('pending', 'running')
  )
ORDER BY s.next_fetch_at, s.id
FOR UPDATE SKIP LOCKED
LIMIT 1;

-- name: SetSourceNextFetch :exec
UPDATE sources
SET next_fetch_at = sqlc.arg('next_fetch_at'),
    updated_at = sqlc.arg('updated_at')
WHERE id = sqlc.arg('id');

-- name: SaveSourceCheckpoint :execrows
UPDATE sources
SET checkpoint = sqlc.arg('checkpoint'),
    last_success_at = sqlc.arg('last_success_at'),
    next_fetch_at = sqlc.arg('next_fetch_at'),
    failure_count = 0,
    updated_at = sqlc.arg('updated_at')
WHERE id = sqlc.arg('id')
  AND edit_version = sqlc.arg('edit_version');

-- name: BumpSourceFailure :exec
UPDATE sources
SET failure_count = failure_count + 1,
    updated_at = sqlc.arg('updated_at')
WHERE id = sqlc.arg('id');
