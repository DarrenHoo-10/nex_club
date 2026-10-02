-- name: InsertTag :exec
INSERT INTO tags (id, dimension, name, slug, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, 'active', $5, $5);

-- name: GetTag :one
SELECT id, dimension, name, slug, status, merged_into_id
FROM tags
WHERE id = $1;

-- name: GetTagBySlug :one
SELECT id, dimension, name, slug, status, merged_into_id
FROM tags
WHERE dimension = $1 AND slug = $2;

-- name: LockTags :many
SELECT id, dimension, name, slug, status, merged_into_id
FROM tags
WHERE id = ANY($1::uuid[])
ORDER BY id
FOR UPDATE;

-- name: MarkTagMerged :execrows
UPDATE tags
SET status = 'merged',
    merged_into_id = $2,
    updated_at = $3
WHERE id = $1 AND status = 'active';

-- name: ListTags :many
SELECT id, dimension, name, slug, status, merged_into_id
FROM tags
ORDER BY dimension, slug;

-- name: InsertAlias :exec
INSERT INTO tag_aliases (id, tag_id, dimension, alias, normalized_alias, is_primary, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListAliases :many
SELECT id, tag_id, dimension, alias, normalized_alias, is_primary
FROM tag_aliases
WHERE tag_id = $1
ORDER BY is_primary DESC, alias;

-- name: ListAliasesByTags :many
SELECT tag_id, alias, normalized_alias, is_primary
FROM tag_aliases
WHERE tag_id = ANY($1::uuid[]);

-- name: DemotePrimaryAliases :exec
UPDATE tag_aliases
SET is_primary = false
WHERE tag_id = $1 AND is_primary;

-- name: MoveAlias :exec
UPDATE tag_aliases
SET tag_id = $2
WHERE id = $1;

-- name: DeleteAlias :exec
DELETE FROM tag_aliases
WHERE id = $1;

-- name: OtherAliasExists :one
SELECT EXISTS (
    SELECT 1
    FROM tag_aliases
    WHERE dimension = $1 AND normalized_alias = $2 AND tag_id <> $3
) AS alias_exists;

-- name: InsertAdminUser :exec
INSERT INTO admin_users (id, username, password_hash, status)
VALUES ($1, $2, $3, $4)
ON CONFLICT (id) DO NOTHING;

-- name: GetAdminByUsername :one
SELECT id
FROM admin_users
WHERE username = $1;
