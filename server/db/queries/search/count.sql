-- name: CountPublished :one
SELECT count(*)::bigint AS total
FROM resources r
WHERE r.kind = $1
  AND r.status = 'published'
  AND ($2::bool OR NOT r.is_demo);

-- name: TagsBySlugs :many
SELECT id, slug
FROM tags
WHERE slug = ANY($1::text[]);
