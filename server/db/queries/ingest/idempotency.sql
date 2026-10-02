-- name: GetIngestBatch :one
SELECT id, principal_key, scope, idempotency_key, request_hash, status, response_status, response_body,
       request_meta, expires_at
FROM idempotency_requests
WHERE principal_key = sqlc.arg('principal_key')
  AND scope = 'ingest.batch.v1'
  AND idempotency_key = sqlc.arg('idempotency_key')
  AND parent_id IS NULL;

-- name: InsertIngestBatch :one
INSERT INTO idempotency_requests (
    id, principal_key, scope, idempotency_key, request_hash, status, expires_at, request_meta
) VALUES (
    sqlc.arg('id'), sqlc.arg('principal_key'), 'ingest.batch.v1', sqlc.arg('idempotency_key'),
    sqlc.arg('request_hash'), 'processing', sqlc.arg('expires_at'), sqlc.arg('request_meta')
)
ON CONFLICT (principal_key, scope, idempotency_key) DO NOTHING
RETURNING id, principal_key, scope, idempotency_key, request_hash, status, response_status, response_body,
          request_meta, expires_at;

-- name: LockIngestParentShare :one
SELECT id, principal_key, scope, idempotency_key, request_hash, status, response_status, response_body,
       request_meta
FROM idempotency_requests
WHERE id = sqlc.arg('id')
  AND principal_key = sqlc.arg('principal_key')
FOR SHARE;

-- name: LockIngestParentUpdate :one
SELECT id, principal_key, scope, idempotency_key, request_hash, status, response_status, response_body,
       request_meta
FROM idempotency_requests
WHERE id = sqlc.arg('id')
  AND principal_key = sqlc.arg('principal_key')
FOR UPDATE;

-- name: InsertIngestChild :one
INSERT INTO idempotency_requests (
    id, principal_key, scope, idempotency_key, request_hash, status, expires_at,
    parent_id, item_index, request_meta
) VALUES (
    sqlc.arg('id'), sqlc.arg('principal_key'), sqlc.arg('scope'), sqlc.arg('idempotency_key'),
    sqlc.arg('request_hash'), 'processing', sqlc.arg('expires_at'),
    sqlc.arg('parent_id'), sqlc.arg('item_index'), '{}'::jsonb
)
ON CONFLICT DO NOTHING
RETURNING id, status, request_hash, response_status, response_body, item_index;

-- name: LockIngestChild :one
SELECT id, status, request_hash, response_status, response_body, item_index
FROM idempotency_requests
WHERE principal_key = sqlc.arg('principal_key')
  AND scope = sqlc.arg('scope')
  AND idempotency_key = sqlc.arg('idempotency_key')
FOR UPDATE;

-- name: CompleteIngestRequest :execrows
UPDATE idempotency_requests
SET status = 'completed',
    response_status = sqlc.arg('response_status'),
    response_body = sqlc.arg('response_body'),
    expires_at = sqlc.arg('expires_at')
WHERE id = sqlc.arg('id')
  AND principal_key = sqlc.arg('principal_key')
  AND status = 'processing';

-- name: ListIngestChildren :many
SELECT item_index, status, response_status, response_body
FROM idempotency_requests
WHERE parent_id = sqlc.arg('parent_id')
  AND principal_key = sqlc.arg('principal_key')
ORDER BY item_index;
