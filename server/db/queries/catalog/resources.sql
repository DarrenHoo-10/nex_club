-- name: InsertResource :exec
INSERT INTO resources (
    id, kind, identity_key, slug, status, edit_version, field_locks,
    is_demo, freshness_eligible, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, 'draft', $5, $6, $7, $8, $9, $9
);

-- name: LockResource :one
SELECT id, kind, identity_key, slug, status, draft_revision_id, edit_version,
       field_locks, is_demo, freshness_eligible, first_published_at, updated_at
FROM resources
WHERE id = $1
FOR UPDATE;

-- name: LockResourceBySlug :one
SELECT id, kind, identity_key, slug, status, draft_revision_id, edit_version,
       field_locks, is_demo, freshness_eligible, first_published_at, updated_at
FROM resources
WHERE kind = $1 AND slug = $2
FOR UPDATE;

-- name: FindResourceBySlug :one
SELECT id, kind, identity_key, slug, status, draft_revision_id, edit_version,
       field_locks, is_demo, freshness_eligible, first_published_at, updated_at
FROM resources
WHERE kind = $1 AND slug = $2;

-- name: UpdateResourceDraft :one
UPDATE resources
SET draft_revision_id = $2,
    edit_version = $3,
    field_locks = $4,
    identity_key = $5,
    updated_at = $6
WHERE id = $1 AND edit_version = $7
RETURNING edit_version;

-- name: AttachDraft :exec
UPDATE resources
SET draft_revision_id = $2,
    field_locks = $3,
    identity_key = $4,
    updated_at = $5
WHERE id = $1;

-- name: UpdateResourcePublished :one
UPDATE resources
SET status = 'published',
    draft_revision_id = NULL,
    edit_version = $2,
    identity_key = $3,
    first_published_at = COALESCE(first_published_at, $4),
    updated_at = $5
WHERE id = $1 AND edit_version = $6
RETURNING edit_version, first_published_at;

-- name: UpdateResourceVisibility :one
UPDATE resources
SET status = $2,
    edit_version = $3,
    updated_at = $4
WHERE id = $1 AND edit_version = $5
RETURNING edit_version;

-- name: SetFirstPublishedAt :exec
UPDATE resources
SET first_published_at = $2
WHERE id = $1;

-- name: BumpResourceVersions :exec
UPDATE resources
SET edit_version = edit_version + 1,
    updated_at = $2
WHERE id = ANY($1::uuid[]);

-- name: FindIdentity :one
SELECT id
FROM resources
WHERE kind = $1 AND identity_key = $2;

-- name: ListResourceKinds :many
SELECT id, kind
FROM resources
WHERE id = ANY($1::uuid[])
ORDER BY id;
