package catalog

import "github.com/darrenhoo/nex_club/server/internal/platform/apperr"

type Origin string

const (
	OriginManual   Origin = "manual"
	OriginImport   Origin = "import"
	OriginPipeline Origin = "pipeline"
)

func ParseOrigin(s string) (Origin, error) {
	o := Origin(s)
	switch o {
	case OriginManual, OriginImport, OriginPipeline:
		return o, nil
	default:
		return "", apperr.Invalid("来源不正确", apperr.FieldError{Field: "origin", Code: "invalid"})
	}
}

// LocksFields reports whether a human or seed write protects changed paths.
func (o Origin) LocksFields() bool {
	return o == OriginManual || o == OriginImport
}
