package catalog

import "fmt"

type Kind string

const (
	KindTool     Kind = "tool"
	KindTutorial Kind = "tutorial"
	KindRepo     Kind = "repo"
)

func (k Kind) Valid() bool {
	switch k {
	case KindTool, KindTutorial, KindRepo:
		return true
	default:
		return false
	}
}

func ParseKind(s string) (Kind, error) {
	k := Kind(s)
	if !k.Valid() {
		return "", fmt.Errorf("invalid kind %q", s)
	}
	return k, nil
}
