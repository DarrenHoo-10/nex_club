-- name: InsertWindow :exec
INSERT INTO provider_budget_windows (
    id, scope_key, currency, window_start, window_end, limit_amount, updated_at
) VALUES (
    sqlc.arg(id), sqlc.arg(scope_key), sqlc.arg(currency), sqlc.arg(window_start), sqlc.arg(window_end),
    sqlc.arg(limit_amount), sqlc.arg(updated_at)
)
ON CONFLICT (scope_key, currency, window_start, window_end) DO NOTHING;

-- name: LockWindows :many
SELECT id, scope_key, currency, window_start, window_end, limit_amount, reserved_amount, spent_amount
FROM provider_budget_windows
WHERE currency = sqlc.arg(currency)
  AND (
    (scope_key = sqlc.arg(scope_a) AND window_start = sqlc.arg(start_a) AND window_end = sqlc.arg(end_a))
    OR (scope_key = sqlc.arg(scope_b) AND window_start = sqlc.arg(start_b) AND window_end = sqlc.arg(end_b))
  )
ORDER BY id
FOR UPDATE;

-- name: LockWindowsByIDs :many
SELECT id, scope_key, currency, window_start, window_end, limit_amount, reserved_amount, spent_amount
FROM provider_budget_windows
WHERE id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY id
FOR UPDATE;

-- name: AddReserved :execrows
UPDATE provider_budget_windows
SET reserved_amount = reserved_amount + sqlc.arg(delta), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: SettleWindow :execrows
UPDATE provider_budget_windows
SET reserved_amount = reserved_amount - sqlc.arg(held),
    spent_amount = spent_amount + sqlc.arg(actual),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND reserved_amount >= sqlc.arg(held);

-- name: ReleaseWindow :execrows
UPDATE provider_budget_windows
SET reserved_amount = reserved_amount - sqlc.arg(held), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND reserved_amount >= sqlc.arg(held);

-- name: InsertReservation :exec
INSERT INTO provider_budget_reservations (
    provider_call_id, budget_window_id, reserved_amount, status, created_at, updated_at
) VALUES (
    sqlc.arg(provider_call_id), sqlc.arg(budget_window_id), sqlc.arg(reserved_amount), 'held',
    sqlc.arg(created_at), sqlc.arg(updated_at)
);

-- name: ListReservations :many
SELECT provider_call_id, budget_window_id, reserved_amount, settled_amount, status, created_at, updated_at
FROM provider_budget_reservations
WHERE provider_call_id = sqlc.arg(provider_call_id)
ORDER BY budget_window_id;

-- name: LockReservations :many
SELECT provider_call_id, budget_window_id, reserved_amount, settled_amount, status, created_at, updated_at
FROM provider_budget_reservations
WHERE provider_call_id = sqlc.arg(provider_call_id)
ORDER BY budget_window_id
FOR UPDATE;

-- name: SettleReservation :execrows
UPDATE provider_budget_reservations
SET status = 'settled', settled_amount = sqlc.arg(settled_amount), updated_at = sqlc.arg(updated_at)
WHERE provider_call_id = sqlc.arg(provider_call_id)
  AND budget_window_id = sqlc.arg(budget_window_id)
  AND status = 'held';

-- name: ReleaseReservation :execrows
UPDATE provider_budget_reservations
SET status = 'released', updated_at = sqlc.arg(updated_at)
WHERE provider_call_id = sqlc.arg(provider_call_id)
  AND budget_window_id = sqlc.arg(budget_window_id)
  AND status = 'held';
