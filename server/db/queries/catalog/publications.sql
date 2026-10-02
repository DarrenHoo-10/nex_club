-- name: UpsertPublication :exec
INSERT INTO resource_publications (
    resource_id, revision_id, kind, title, aliases, summary, body_markdown,
    cover_urls, primary_category_id, quality_score, recommendation_reason,
    details, search_text, content_updated_at, projected_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    $8, $9, $10, $11,
    $12, $13, $14, $15
)
ON CONFLICT (resource_id) DO UPDATE SET
    revision_id = EXCLUDED.revision_id,
    kind = EXCLUDED.kind,
    title = EXCLUDED.title,
    aliases = EXCLUDED.aliases,
    summary = EXCLUDED.summary,
    body_markdown = EXCLUDED.body_markdown,
    cover_urls = EXCLUDED.cover_urls,
    primary_category_id = EXCLUDED.primary_category_id,
    quality_score = EXCLUDED.quality_score,
    recommendation_reason = EXCLUDED.recommendation_reason,
    details = EXCLUDED.details,
    search_text = EXCLUDED.search_text,
    content_updated_at = EXCLUDED.content_updated_at,
    projected_at = EXCLUDED.projected_at;

-- name: GetPublication :one
SELECT resource_id, revision_id, kind, title, aliases, summary, body_markdown,
       cover_urls, primary_category_id, quality_score, recommendation_reason,
       details, search_text, content_updated_at, projected_at
FROM resource_publications
WHERE resource_id = $1;

-- name: DeleteResourceTags :exec
DELETE FROM resource_tags
WHERE resource_id = $1;

-- name: InsertResourceTag :exec
INSERT INTO resource_tags (resource_id, tag_id, assigned_by)
VALUES ($1, $2, $3)
ON CONFLICT (resource_id, tag_id) DO NOTHING;

-- name: ListResourceTagIDs :many
SELECT tag_id
FROM resource_tags
WHERE resource_id = $1
ORDER BY tag_id;

-- name: CopyTagRelations :exec
INSERT INTO resource_tags (resource_id, tag_id, assigned_by)
SELECT src.resource_id, $2, src.assigned_by
FROM resource_tags AS src
WHERE src.tag_id = $1
ON CONFLICT (resource_id, tag_id) DO NOTHING;

-- name: DeleteTagRelations :exec
DELETE FROM resource_tags
WHERE tag_id = $1;

-- name: ListAffectedResources :many
SELECT resource_id
FROM resource_tags
WHERE tag_id = $1
UNION
SELECT resource_id
FROM resource_publications
WHERE primary_category_id = $1
ORDER BY resource_id;

-- name: LockResourceIDs :many
SELECT id
FROM resources
WHERE id = ANY($1::uuid[])
ORDER BY id
FOR UPDATE;
