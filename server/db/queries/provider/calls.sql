-- name: LockCallsByRequestKey :many
SELECT id, processing_run_id, source_run_id, provider_key, model, profile_version,
       request_key, attempt_no, status, provider_request_id, response_payload,
       input_tokens, output_tokens, currency, reserved_cost, actual_cost, error_code,
       created_at, sent_at, completed_at, preview_request_key, request_summary, assistant_turn_id
FROM provider_calls
WHERE request_key = sqlc.arg(request_key)
ORDER BY attempt_no
FOR UPDATE;

-- name: LockCall :one
SELECT id, processing_run_id, source_run_id, provider_key, model, profile_version,
       request_key, attempt_no, status, provider_request_id, response_payload,
       input_tokens, output_tokens, currency, reserved_cost, actual_cost, error_code,
       created_at, sent_at, completed_at, preview_request_key, request_summary, assistant_turn_id
FROM provider_calls
WHERE id = sqlc.arg(id)
FOR UPDATE;

-- name: ReadCall :one
SELECT id, processing_run_id, source_run_id, provider_key, model, profile_version,
       request_key, attempt_no, status, provider_request_id, response_payload,
       input_tokens, output_tokens, currency, reserved_cost, actual_cost, error_code,
       created_at, sent_at, completed_at, preview_request_key, request_summary, assistant_turn_id
FROM provider_calls
WHERE id = sqlc.arg(id);

-- name: InsertCall :one
INSERT INTO provider_calls (
    id, processing_run_id, assistant_turn_id, provider_key, model, profile_version,
    request_key, attempt_no, status, currency, reserved_cost, created_at
) VALUES (
    sqlc.arg(id), sqlc.arg(processing_run_id), sqlc.arg(assistant_turn_id), sqlc.arg(provider_key), sqlc.arg(model), sqlc.arg(profile_version),
    sqlc.arg(request_key), sqlc.arg(attempt_no), 'prepared', sqlc.arg(currency), sqlc.arg(reserved_cost), sqlc.arg(created_at)
)
RETURNING id;

-- name: MarkSent :one
UPDATE provider_calls
SET status = 'sent', sent_at = sqlc.arg(sent_at)
WHERE id = sqlc.arg(id) AND status = 'prepared'
RETURNING id;

-- name: MarkUnknown :execrows
UPDATE provider_calls
SET status = 'unknown', error_code = sqlc.arg(error_code), completed_at = sqlc.arg(completed_at)
WHERE id = sqlc.arg(id) AND status = 'sent';

-- name: MarkFailed :execrows
UPDATE provider_calls
SET status = 'failed', error_code = sqlc.arg(error_code), completed_at = sqlc.arg(completed_at)
WHERE id = sqlc.arg(id) AND status IN ('prepared', 'sent', 'unknown');

-- name: MarkSucceeded :execrows
UPDATE provider_calls
SET status = 'succeeded',
    provider_request_id = sqlc.arg(provider_request_id),
    response_payload = sqlc.arg(response_payload),
    input_tokens = sqlc.arg(input_tokens),
    output_tokens = sqlc.arg(output_tokens),
    actual_cost = sqlc.arg(actual_cost),
    error_code = sqlc.arg(error_code),
    completed_at = sqlc.arg(completed_at)
WHERE id = sqlc.arg(id) AND status IN ('sent', 'unknown', 'succeeded');

-- name: ListStalePrepared :many
SELECT id
FROM provider_calls
WHERE status = 'prepared' AND created_at < sqlc.arg(cutoff)
ORDER BY id
LIMIT 100;

-- name: ListStaleSent :many
SELECT id
FROM provider_calls
WHERE status = 'sent' AND sent_at IS NOT NULL AND sent_at < sqlc.arg(cutoff)
ORDER BY id
LIMIT 100;

-- name: ListCallsByStatus :many
SELECT id, provider_key, status, reserved_cost, created_at
FROM provider_calls
WHERE status = sqlc.arg(status)
ORDER BY created_at DESC, id
LIMIT 100;
