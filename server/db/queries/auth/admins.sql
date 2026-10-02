-- name: FindAdminByUsername :one
SELECT id, username, password_hash, status
FROM admin_users
WHERE username = $1;

-- name: AnyAdmin :one
SELECT EXISTS (SELECT 1 FROM admin_users) AS present;

-- name: InsertAdmin :exec
INSERT INTO admin_users (id, username, password_hash)
VALUES ($1, $2, $3);

-- name: UpdateAdminPassword :exec
UPDATE admin_users
SET password_hash = $2, updated_at = $3
WHERE id = $1;

-- name: TouchAdminLogin :exec
UPDATE admin_users
SET last_login_at = $2, updated_at = $2
WHERE id = $1;
