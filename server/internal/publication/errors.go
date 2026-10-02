package publication

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

func mapDB(err error) error {
	if err == nil || errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var already *apperr.Error
	if errors.As(err, &already) {
		return err
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return uniqueConflict(pgErr.ConstraintName)
		case "23P01":
			return apperr.Invalid("这个位置在该时间段已经有推荐", apperr.FieldError{Field: "starts_at", Code: "overlap"})
		case "23503":
			return apperr.Invalid("关联的记录不存在", apperr.FieldError{Field: "resource_id", Code: "missing"})
		}
	}
	wrapped := apperr.Internal("保存失败")
	wrapped.Err = err
	return wrapped
}

func uniqueConflict(constraint string) error {
	switch constraint {
	case "resources_slug_key":
		err := apperr.EditConflict("标识已被占用")
		err.Fields = []apperr.FieldError{{Field: "slug", Code: "taken"}}
		return err
	case "resources_identity_key_idx":
		err := apperr.EditConflict("身份键已被占用")
		err.Fields = []apperr.FieldError{{Field: "identity_key", Code: "taken"}}
		return err
	case "tags_dimension_slug_key":
		return apperr.Invalid("标签标识已被占用", apperr.FieldError{Field: "slug", Code: "taken"})
	case "tag_aliases_dimension_normalized_alias_key":
		return apperr.Invalid("标签名称已被占用", apperr.FieldError{Field: "name", Code: "taken"})
	default:
		return apperr.EditConflict("记录与已有数据冲突")
	}
}

func invalid(message, field, code string) error {
	if field == "" {
		return apperr.Invalid(message)
	}
	return apperr.Invalid(message, apperr.FieldError{Field: field, Code: code})
}
