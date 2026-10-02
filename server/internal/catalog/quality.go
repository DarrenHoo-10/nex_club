package catalog

import "github.com/darrenhoo/nex_club/server/internal/platform/apperr"

type QualityScore int

func ParseQuality(n int) (QualityScore, error) {
	if n < 0 || n > 100 {
		return 0, apperr.Invalid("质量分必须在 0 到 100 之间", apperr.FieldError{Field: "quality_score", Code: "invalid"})
	}
	return QualityScore(n), nil
}

func ParseEditVersion(n int64) error {
	if n <= 0 {
		return apperr.Invalid("版本号不正确", apperr.FieldError{Field: "edit_version", Code: "invalid"})
	}
	return nil
}
