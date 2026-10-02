-- name: InsertAudit :exec
INSERT INTO audit_logs (
    actor_admin_id, actor_type, action, target_type, target_id, changes, request_id, job_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
);

-- name: ListAdminResources :many
SELECT r.id, r.kind, r.slug, r.status, r.edit_version, r.updated_at,
       r.draft_revision_id,
       p.revision_id AS published_revision_id,
       COALESCE(dr.payload->>'title', p.title, '')::text AS title
FROM resources r
LEFT JOIN resource_publications p ON p.resource_id = r.id
LEFT JOIN resource_revisions dr ON dr.resource_id = r.id AND dr.id = r.draft_revision_id
WHERE ($1::text = '' OR r.kind = $1)
  AND ($2::text = '' OR r.status = $2)
  AND ($3::text = '' OR strpos(lower(COALESCE(dr.payload->>'title', p.title, '')), lower($3)) > 0)
ORDER BY r.updated_at DESC, r.id;
