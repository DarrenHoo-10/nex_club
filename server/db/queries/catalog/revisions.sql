-- name: InsertRevision :exec
INSERT INTO resource_revisions (
    id, resource_id, revision_no, schema_version, payload, origin, created_by, change_reason, created_at
) VALUES (
    $1, $2, $3, 1, $4, $5, $6, $7, $8
);

-- name: NextRevisionNo :one
SELECT (COALESCE(MAX(revision_no), 0) + 1)::bigint AS next_no
FROM resource_revisions
WHERE resource_id = $1;

-- name: GetRevision :one
SELECT id, resource_id, revision_no, schema_version, payload, origin, created_by, change_reason, created_at
FROM resource_revisions
WHERE resource_id = $1 AND id = $2;

-- name: ListRevisions :many
SELECT id, resource_id, revision_no, schema_version, payload, origin, created_by, change_reason, created_at
FROM resource_revisions
WHERE resource_id = $1
ORDER BY revision_no DESC;
