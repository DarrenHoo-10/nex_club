package editorial

import (
	"context"
	"errors"
	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/editorial/sqlc"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/jackc/pgx/v5"
)

// PreviewDecisionTx merges review choices using the same logic as DecideTx, without writing anything.
func (s *Service) PreviewDecisionTx(ctx context.Context, tx pgx.Tx, decision ports.Decision) (catalog.Payload, catalog.Kind, error) {
	empty := catalog.Payload{}
	q := s.q(tx)
	if err := q.LockTaxonomyShared(ctx); err != nil {
		return empty, "", mapDB(err)
	}
	peek, err := q.GetProposal(ctx, decision.ProposalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, "", apperr.NotFound("建议不存在")
	}
	if err != nil {
		return empty, "", mapDB(err)
	}
	run, err := q.GetRun(ctx, peek.ProcessingRunID)
	if err != nil {
		return empty, "", mapDB(err)
	}
	rev, err := q.GetRawRevision(ctx, run.RawRevisionID)
	if err != nil {
		return empty, "", mapDB(err)
	}
	item, err := q.LockRawItem(ctx, rev.RawItemID)
	if err != nil {
		return empty, "", mapDB(err)
	}
	row, err := q.LockProposal(ctx, decision.ProposalID)
	if err != nil {
		return empty, "", mapDB(err)
	}
	if row.Status != "pending" {
		return empty, "", apperr.EditConflict("建议已经处理，请刷新列表")
	}
	current, ok := uuidFromPG(item.CurrentRevisionID)
	if !ok || current != run.RawRevisionID {
		return empty, "", apperr.EditConflict("原始资料已有更新")
	}
	for _, path := range decision.Unlock {
		if _, err := catalog.ParseFieldPath(catalog.Kind(row.ProposedKind), path); err != nil {
			return empty, "", err
		}
	}
	changes, err := parseChanges(row.FieldChanges)
	if err != nil {
		return empty, "", err
	}
	fields := map[string]ports.FieldDecision{}
	for path := range changes {
		fields[path] = "reject"
	}
	for path, action := range decision.Fields {
		if _, ok := changes[path]; !ok {
			return empty, "", apperr.Invalid("字段不属于该建议")
		}
		if action != "accept" && action != "reject" && action != "rewrite" {
			return empty, "", apperr.Invalid("审核动作不正确")
		}
		if action == "rewrite" && len(bytesTrim(decision.Rewrites[path])) == 0 {
			return empty, "", apperr.Invalid("请填写改写内容")
		}
		fields[path] = action
	}
	var resource *sqlc.LockResourceRow
	if id, ok := uuidFromPG(row.ResourceID); ok {
		locked, err := q.LockResource(ctx, id)
		if err != nil {
			return empty, "", mapDB(err)
		}
		if row.BaseEditVersion == nil || decision.EditVersion != *row.BaseEditVersion || decision.EditVersion != locked.EditVersion {
			return empty, "", apperr.EditConflict("资源版本已变化，请重新审核")
		}
		pub, err := q.GetPublication(ctx, id)
		var published *catalog.RevisionID
		if err == nil {
			v := catalog.RevisionID(pub.RevisionID)
			published = &v
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return empty, "", mapDB(err)
		}
		var draft *catalog.RevisionID
		if id, ok := uuidFromPG(locked.DraftRevisionID); ok {
			v := catalog.RevisionID(id)
			draft = &v
		}
		if catalog.HasUnpublishedDraft(draft, published) {
			return empty, "", apperr.EditConflict("请先处理已有的未发布草稿")
		}
		resource = &locked
	} else if decision.EditVersion != 0 {
		return empty, "", apperr.EditConflict("新资源版本不正确")
	}
	next, err := s.mergeDecision(ctx, q, row, resource, fields, decision)
	if err != nil {
		return empty, "", err
	}
	if err := catalog.ValidatePayload(next); err != nil {
		return empty, "", err
	}
	return next, catalog.Kind(row.ProposedKind), nil
}
