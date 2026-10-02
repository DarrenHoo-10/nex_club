-- name: InsertFeatured :exec
INSERT INTO featured_slots (
    id, kind, placement, position, resource_id, starts_at, ends_at, enabled, created_by, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, true, $8, $9, $9
);

-- name: DisableFeatured :one
UPDATE featured_slots
SET enabled = false,
    updated_at = $2
WHERE id = $1
RETURNING id;

-- name: FeaturedOverlapExists :one
SELECT EXISTS (
    SELECT 1
    FROM featured_slots
    WHERE kind = sqlc.arg('kind')
      AND placement = sqlc.arg('placement')
      AND position = sqlc.arg('position')
      AND enabled
      AND tstzrange(starts_at, COALESCE(ends_at, 'infinity'::timestamptz), '[)')
          && tstzrange(sqlc.arg('starts_at')::timestamptz, COALESCE(sqlc.narg('ends_at')::timestamptz, 'infinity'::timestamptz), '[)')
) AS slot_exists;

-- name: ListFeatured :many
SELECT f.id, f.kind, f.placement, f.position, f.resource_id, r.slug,
       f.starts_at, f.ends_at, f.enabled
FROM featured_slots f
JOIN resources r ON r.id = f.resource_id
ORDER BY f.kind, f.placement, f.position, f.starts_at, f.id;
