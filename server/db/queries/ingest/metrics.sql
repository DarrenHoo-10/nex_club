-- name: InsertMetricSnapshot :exec
INSERT INTO external_metric_snapshots (
    resource_id, source_id, observed_at, stars, forks, open_issues, extra
) VALUES (
    sqlc.arg('resource_id'), sqlc.arg('source_id'), sqlc.arg('observed_at'),
    sqlc.narg('stars'), sqlc.narg('forks'), sqlc.narg('open_issues'), sqlc.arg('extra')
)
ON CONFLICT (resource_id, source_id, observed_at) DO NOTHING;
