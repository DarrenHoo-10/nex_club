package adminauth

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/darrenhoo/nex_club/server/internal/adminauth/sqlc"
	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/ports"
)

type AuditItem struct {
	ID           int64
	ActorAdminID *catalog.AdminID
	ActorType    string
	Action       string
	TargetType   string
	TargetID     string
	Changes      json.RawMessage
	RequestID    *string
	JobID        *string
	CreatedAt    time.Time
}

// Auditor inserts audit rows on the caller's transaction. List reads through DB.
type Auditor struct {
	db sqlc.DBTX
}

func NewAuditor(db sqlc.DBTX) Auditor { return Auditor{db: db} }

func (a Auditor) Record(ctx context.Context, tx pgx.Tx, event ports.AuditEvent) error {
	if tx == nil {
		return apperr.Internal("审计事务缺失")
	}
	switch event.ActorType {
	case "admin", "system", "ingest":
	default:
		return apperr.Invalid("审计主体类型不合法")
	}
	if event.ActorType == "admin" && event.ActorAdminID == nil {
		return apperr.Invalid("管理员审计缺少操作者")
	}
	if strings.TrimSpace(event.Action) == "" || strings.TrimSpace(event.TargetType) == "" {
		return apperr.Invalid("审计动作或目标不合法")
	}
	changes, err := Sanitize(event.Changes)
	if err != nil {
		return err
	}
	var actor pgtype.UUID
	if event.ActorType == "admin" && event.ActorAdminID != nil {
		actor = pgtype.UUID{Bytes: event.ActorAdminID.UUID(), Valid: true}
	}
	return sqlc.New(tx).InsertAudit(ctx, sqlc.InsertAuditParams{
		ActorAdminID: actor,
		ActorType:    event.ActorType,
		Action:       event.Action,
		TargetType:   event.TargetType,
		TargetID:     event.TargetID,
		Changes:      changes,
		RequestID:    emptyStringPtr(event.RequestID),
		JobID:        emptyStringPtr(event.JobID),
	})
}

func (a Auditor) List(ctx context.Context, targetType, targetID string) ([]AuditItem, error) {
	if a.db == nil {
		return nil, apperr.Internal("审计查询未配置")
	}
	rows, err := sqlc.New(a.db).ListAuditByTarget(ctx, sqlc.ListAuditByTargetParams{
		TargetType: targetType,
		TargetID:   targetID,
	})
	if err != nil {
		return nil, err
	}
	items := make([]AuditItem, 0, len(rows))
	for _, row := range rows {
		changes := row.Changes
		if len(changes) == 0 {
			changes = json.RawMessage(`{}`)
		}
		items = append(items, AuditItem{
			ID:           row.ID,
			ActorAdminID: adminPtr(row.ActorAdminID),
			ActorType:    row.ActorType,
			Action:       row.Action,
			TargetType:   row.TargetType,
			TargetID:     row.TargetID,
			Changes:      changes,
			RequestID:    row.RequestID,
			JobID:        row.JobID,
			CreatedAt:    row.CreatedAt.UTC(),
		})
	}
	return items, nil
}

func emptyStringPtr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func adminPtr(id pgtype.UUID) *catalog.AdminID {
	if !id.Valid {
		return nil
	}
	admin := catalog.AdminID(id.Bytes)
	return &admin
}
