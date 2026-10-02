-- name: InsertSourceRun :one
INSERT INTO source_runs (
    id, source_id, source_edit_version, scheduled_for, run_key, status, checkpoint_before
) VALUES (
    sqlc.arg('id'), sqlc.arg('source_id'), sqlc.arg('source_edit_version'),
    sqlc.arg('scheduled_for'), sqlc.arg('run_key'), 'pending', sqlc.arg('checkpoint_before')
)
ON CONFLICT (run_key) DO NOTHING
RETURNING id;

-- name: GetSourceRun :one
SELECT id, source_id, source_edit_version, scheduled_for, run_key, status,
       checkpoint_before, checkpoint_after, stats, error_code, error_message, river_job_id,
       started_at, finished_at
FROM source_runs
WHERE id = sqlc.arg('id');

-- name: MarkSourceRunRunning :exec
UPDATE source_runs
SET status = 'running',
    started_at = COALESCE(started_at, sqlc.arg('started_at'))
WHERE id = sqlc.arg('id')
  AND status IN ('pending', 'running');

-- name: SetSourceRunJob :exec
UPDATE source_runs
SET river_job_id = sqlc.arg('river_job_id')
WHERE id = sqlc.arg('id');

-- name: FinishSourceRun :execrows
UPDATE source_runs
SET status = sqlc.arg('status'),
    checkpoint_after = sqlc.arg('checkpoint_after'),
    stats = sqlc.arg('stats'),
    error_code = sqlc.narg('error_code'),
    error_message = sqlc.narg('error_message'),
    finished_at = sqlc.arg('finished_at')
WHERE id = sqlc.arg('id')
  AND status IN ('pending', 'running');
