-- name: InsertCredential :one
INSERT INTO ingest_credentials (
    id, source_id, name, token_hash, scopes, created_by
) VALUES (
    sqlc.arg('id'), sqlc.arg('source_id'), sqlc.arg('name'), sqlc.arg('token_hash'),
    sqlc.arg('scopes'), sqlc.arg('created_by')
)
RETURNING id;

-- name: FindCredentialByHash :one
SELECT c.id, c.source_id, c.name, c.token_hash, c.scopes, c.expires_at, c.revoked_at, c.created_by,
       s.source_key, s.enabled, s.trust_tier, s.edit_version, s.participation_mode, s.kind
FROM ingest_credentials c
JOIN sources s ON s.id = c.source_id
WHERE c.token_hash = sqlc.arg('token_hash');

-- name: TouchCredentialUsed :exec
UPDATE ingest_credentials
SET last_used_at = sqlc.arg('last_used_at')
WHERE id = sqlc.arg('id');

-- name: AdminExists :one
SELECT id
FROM admin_users
WHERE id = sqlc.arg('id')
  AND status = 'active';
