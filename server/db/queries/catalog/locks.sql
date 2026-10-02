-- name: LockTaxonomyShared :exec
SELECT pg_advisory_xact_lock_shared(1313821761::int4, 1::int4);

-- name: LockTaxonomyExclusive :exec
SELECT pg_advisory_xact_lock(1313821761::int4, 1::int4);
