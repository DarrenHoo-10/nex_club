-- name: InsertRawItem :one
INSERT INTO raw_items (
    id, owner_source_id, identity_key, canonical_url, is_backfill, first_discovered_at, last_seen_at
) VALUES (
    sqlc.arg('id'), sqlc.arg('owner_source_id'), sqlc.arg('identity_key'),
    sqlc.narg('canonical_url'), sqlc.arg('is_backfill'),
    sqlc.arg('first_discovered_at'), sqlc.arg('last_seen_at')
)
ON CONFLICT (identity_key) DO NOTHING
RETURNING id, owner_source_id, identity_key, canonical_url, current_revision_id, is_backfill,
          first_discovered_at, last_seen_at;

-- name: LockRawItemByIdentity :one
SELECT id, owner_source_id, identity_key, canonical_url, current_revision_id, is_backfill,
       first_discovered_at, last_seen_at
FROM raw_items
WHERE identity_key = sqlc.arg('identity_key')
FOR UPDATE;

-- name: GetRawRevision :one
SELECT id, raw_item_id, revision_no, content_hash, normalization_version, title, excerpt, body_text,
       body_html, author, language, source_published_at, source_updated_at, raw_payload, fetched_at
FROM raw_item_revisions
WHERE id = sqlc.arg('id');

-- name: NextRevisionNo :one
SELECT (COALESCE(MAX(revision_no), 0) + 1)::bigint AS revision_no
FROM raw_item_revisions
WHERE raw_item_id = sqlc.arg('raw_item_id');

-- name: InsertRawRevision :one
INSERT INTO raw_item_revisions (
    id, raw_item_id, revision_no, content_hash, normalization_version, title, excerpt, body_text,
    body_html, author, language, source_published_at, source_updated_at, raw_payload, fetched_at
) VALUES (
    sqlc.arg('id'), sqlc.arg('raw_item_id'), sqlc.arg('revision_no'), sqlc.arg('content_hash'),
    sqlc.arg('normalization_version'), sqlc.arg('title'), sqlc.narg('excerpt'), sqlc.narg('body_text'),
    sqlc.narg('body_html'), sqlc.narg('author'), sqlc.narg('language'),
    sqlc.narg('source_published_at'), sqlc.narg('source_updated_at'), sqlc.arg('raw_payload'),
    sqlc.arg('fetched_at')
)
RETURNING id, revision_no;

-- name: SetCurrentRevision :exec
UPDATE raw_items
SET current_revision_id = sqlc.arg('current_revision_id'),
    last_seen_at = sqlc.arg('last_seen_at'),
    canonical_url = COALESCE(sqlc.narg('canonical_url'), canonical_url)
WHERE id = sqlc.arg('id');

-- name: TouchRawItem :exec
UPDATE raw_items
SET last_seen_at = sqlc.arg('last_seen_at'),
    canonical_url = COALESCE(sqlc.narg('canonical_url'), canonical_url)
WHERE id = sqlc.arg('id');

-- name: UpsertDiscovery :one
INSERT INTO raw_item_discoveries (
    id, raw_item_id, source_id, source_item_key, observed_url, first_seen_at, last_seen_at, seen_count
) VALUES (
    sqlc.arg('id'), sqlc.arg('raw_item_id'), sqlc.arg('source_id'), sqlc.arg('source_item_key'),
    sqlc.narg('observed_url'), sqlc.arg('seen_at'), sqlc.arg('seen_at'), 1
)
ON CONFLICT (source_id, source_item_key) DO UPDATE
SET raw_item_id = EXCLUDED.raw_item_id,
    observed_url = COALESCE(EXCLUDED.observed_url, raw_item_discoveries.observed_url),
    last_seen_at = EXCLUDED.last_seen_at,
    seen_count = raw_item_discoveries.seen_count + 1
RETURNING id, raw_item_id, seen_count;

-- name: DiscoveryContentHash :one
SELECT rev.content_hash
FROM raw_item_discoveries d
JOIN raw_items item ON item.id = d.raw_item_id
JOIN raw_item_revisions rev ON rev.id = item.current_revision_id
WHERE d.source_id = sqlc.arg('source_id')
  AND d.source_item_key = sqlc.arg('source_item_key');
