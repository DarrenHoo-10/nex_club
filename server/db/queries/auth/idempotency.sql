-- name: InsertIdempotency :one
INSERT INTO idempotency_requests (
    id, principal_key, scope, idempotency_key, request_hash, status, expires_at
) VALUES (
    $1, $2, $3, $4, $5, 'processing', $6
)
ON CONFLICT (principal_key, scope, idempotency_key) DO NOTHING
RETURNING id;

-- name: CompleteIdempotency :execrows
UPDATE idempotency_requests
SET status = 'completed',
    response_status = sqlc.arg(response_status),
    response_body = sqlc.arg(response_body)
WHERE id = sqlc.arg(id)
  AND status = 'processing';

-- name: LockIdempotency :one
SELECT id, request_hash, status, response_status, response_body
FROM idempotency_requests
WHERE principal_key = $1
  AND scope = $2
  AND idempotency_key = $3
FOR UPDATE;
