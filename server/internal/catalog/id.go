package catalog

import (
	"fmt"

	"github.com/google/uuid"
)

type ResourceID uuid.UUID
type RevisionID uuid.UUID
type TagID uuid.UUID
type AdminID uuid.UUID

func NewResourceID() ResourceID { return ResourceID(uuid.New()) }
func NewRevisionID() RevisionID { return RevisionID(uuid.New()) }
func NewTagID() TagID           { return TagID(uuid.New()) }
func NewAdminID() AdminID       { return AdminID(uuid.New()) }

func (id ResourceID) UUID() uuid.UUID { return uuid.UUID(id) }
func (id RevisionID) UUID() uuid.UUID { return uuid.UUID(id) }
func (id TagID) UUID() uuid.UUID      { return uuid.UUID(id) }
func (id AdminID) UUID() uuid.UUID    { return uuid.UUID(id) }

func (id ResourceID) String() string { return uuid.UUID(id).String() }
func (id RevisionID) String() string { return uuid.UUID(id).String() }
func (id TagID) String() string      { return uuid.UUID(id).String() }
func (id AdminID) String() string    { return uuid.UUID(id).String() }

func ParseResourceID(s string) (ResourceID, error) { return parseID[ResourceID](s) }
func ParseRevisionID(s string) (RevisionID, error) { return parseID[RevisionID](s) }
func ParseTagID(s string) (TagID, error)           { return parseID[TagID](s) }
func ParseAdminID(s string) (AdminID, error)       { return parseID[AdminID](s) }

func parseID[T ~[16]byte](s string) (T, error) {
	var zero T
	id, err := uuid.Parse(s)
	if err != nil {
		return zero, fmt.Errorf("invalid uuid %q", s)
	}
	return T(id), nil
}
