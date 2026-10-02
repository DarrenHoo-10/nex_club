-- name: InsertSession :exec
INSERT INTO admin_sessions (
    id, admin_id, token_hash, csrf_secret_hash, expires_at, last_seen_at
) VALUES (
    $1, $2, $3, $4, $5, $6
);

-- name: FindSessionByTokenHash :one
SELECT
    s.id,
    s.admin_id,
    s.csrf_secret_hash,
    s.expires_at,
    s.revoked_at,
    u.username,
    u.status
FROM admin_sessions s
JOIN admin_users u ON u.id = s.admin_id
WHERE s.token_hash = $1;

-- name: TouchSession :exec
UPDATE admin_sessions
SET last_seen_at = $2
WHERE id = $1;

-- name: RevokeSession :exec
UPDATE admin_sessions
SET revoked_at = $2
WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeAdminSessions :exec
UPDATE admin_sessions
SET revoked_at = $2
WHERE admin_id = $1 AND revoked_at IS NULL;
