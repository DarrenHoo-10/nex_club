-- name: CurrentRun :one
SELECT id, kind, rule_version, status, is_current, computed_at, expires_at
FROM ranking_runs
WHERE kind = $1 AND is_current
LIMIT 1;

-- name: RunByID :one
SELECT id, kind, rule_version, status, is_current, computed_at, expires_at
FROM ranking_runs
WHERE id = $1;
