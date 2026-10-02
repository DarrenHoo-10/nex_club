-- name: InsertProposal :one
INSERT INTO change_proposals (
    id, processing_run_id, proposal_no, resource_id, base_edit_version,
    proposed_kind, proposed_payload, field_changes, status, created_at, updated_at
) VALUES (
    $1, $2, 1, $3, $4, $5, $6, $7, 'pending', $8, $8
)
ON CONFLICT (processing_run_id, proposal_no) DO NOTHING
RETURNING id;

-- name: GetProposalByRun :one
SELECT id, status
FROM change_proposals
WHERE processing_run_id = $1 AND proposal_no = 1;

-- name: LockProposal :one
SELECT id, processing_run_id, proposal_no, resource_id, base_edit_version, proposed_kind,
       proposed_payload, field_changes, status, review_decisions, reviewed_by, reviewed_at,
       applied_resource_id, applied_revision_id, created_at, updated_at
FROM change_proposals
WHERE id = $1
FOR UPDATE;

-- name: GetProposal :one
SELECT id, processing_run_id, proposal_no, resource_id, base_edit_version, proposed_kind,
       proposed_payload, field_changes, status, review_decisions, reviewed_by, reviewed_at,
       applied_resource_id, applied_revision_id, created_at, updated_at
FROM change_proposals
WHERE id = $1;

-- name: ListProposalsByStatus :many
SELECT id, processing_run_id, resource_id, base_edit_version, proposed_kind,
       proposed_payload, field_changes, status, created_at, updated_at
FROM change_proposals
WHERE status = $1
ORDER BY created_at DESC, id
LIMIT 200;

-- name: UpdateProposalReview :exec
UPDATE change_proposals
SET status = $2,
    review_decisions = $3,
    reviewed_by = $4,
    reviewed_at = $5,
    applied_resource_id = $6,
    applied_revision_id = $7,
    updated_at = $5
WHERE id = $1;

-- name: LockResource :one
SELECT id, kind, identity_key, slug, status, draft_revision_id, edit_version, field_locks
FROM resources
WHERE id = $1
FOR UPDATE;

-- name: FindResourceByIdentity :one
SELECT id
FROM resources
WHERE kind = $1 AND identity_key = $2;

-- name: FindAppliedResourceForItem :one
SELECT cp.applied_resource_id
FROM change_proposals cp
JOIN processing_runs pr ON pr.id = cp.processing_run_id
JOIN raw_item_revisions rr ON rr.id = pr.raw_revision_id
JOIN resources r ON r.id = cp.applied_resource_id
WHERE rr.raw_item_id = $1
  AND r.kind = $2
  AND cp.applied_resource_id IS NOT NULL
ORDER BY cp.updated_at DESC
LIMIT 1;

-- name: GetPublication :one
SELECT resource_id, revision_id, kind, title, aliases, summary, body_markdown, cover_urls,
       primary_category_id, quality_score, recommendation_reason, details
FROM resource_publications
WHERE resource_id = $1;

-- name: ListPublicationTagIDs :many
SELECT tag_id
FROM resource_tags
WHERE resource_id = $1
ORDER BY tag_id;

-- name: InsertEvidence :exec
INSERT INTO resource_evidence (
    id, resource_id, resource_revision_id, proposal_id, raw_revision_id,
    field_path, evidence_excerpt, locator, created_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
);

-- name: InsertAudit :exec
INSERT INTO audit_logs (
    actor_admin_id, actor_type, action, target_type, target_id, changes, request_id, job_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
);

-- name: CountProposalsForRevision :one
SELECT count(*)::bigint
FROM change_proposals cp
JOIN processing_runs pr ON pr.id = cp.processing_run_id
WHERE pr.raw_revision_id = $1;
