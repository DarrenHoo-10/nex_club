-- name: InsertAudit :exec
INSERT INTO audit_logs (
    actor_admin_id, actor_type, action, target_type, target_id, changes, request_id, job_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
);

-- name: ListAuditByTarget :many
SELECT
    id,
    actor_admin_id,
    actor_type,
    action,
    target_type,
    target_id,
    changes,
    request_id,
    job_id,
    created_at
FROM audit_logs
WHERE target_type = $1 AND target_id = $2
ORDER BY created_at DESC, id DESC
LIMIT 100;
