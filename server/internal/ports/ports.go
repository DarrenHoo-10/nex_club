package ports

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
)

type Tx interface {
	Within(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error
}

type AuditEvent struct {
	ActorType    string
	ActorAdminID *catalog.AdminID
	Action       string
	TargetType   string
	TargetID     string
	Changes      json.RawMessage
	RequestID    string
	JobID        string
}

type Auditor interface {
	Record(ctx context.Context, tx pgx.Tx, event AuditEvent) error
}

type RankingEnqueuer interface {
	// Enqueue inserts a ranking job on the same pgx.Tx as the publish transaction.
	// An empty kind refreshes tool, tutorial, and repo.
	Enqueue(ctx context.Context, tx pgx.Tx, kind catalog.Kind, reason string) error
}

type CreateDraftCommand struct {
	Kind              catalog.Kind
	Slug              string
	Title             string
	Aliases           []string
	Summary           string
	BodyMarkdown      *string
	CoverURLs         []string
	PrimaryCategoryID *catalog.TagID
	TagIDs            []catalog.TagID
	QualityScore      int
	Recommendation    *string
	Details           json.RawMessage
	ChangeReason      string
	Actor             *catalog.AdminID
	Origin            string
	IsDemo            bool
	FreshnessEligible bool
	IdentityKey       *string
}

type SaveDraftCommand struct {
	ResourceID        catalog.ResourceID
	EditVersion       int64
	Title             string
	Aliases           []string
	Summary           string
	BodyMarkdown      *string
	CoverURLs         []string
	PrimaryCategoryID *catalog.TagID
	TagIDs            []catalog.TagID
	QualityScore      int
	Recommendation    *string
	Details           json.RawMessage
	UnlockFields      []string
	ChangeReason      string
	Actor             *catalog.AdminID
	Origin            string
}

type PublishCommand struct {
	ResourceID  catalog.ResourceID
	RevisionID  catalog.RevisionID
	EditVersion int64
	Actor       *catalog.AdminID
}

type VisibilityCommand struct {
	ResourceID  catalog.ResourceID
	EditVersion int64
	Status      string
	Reason      string
	Actor       *catalog.AdminID
}

type DraftResult struct {
	ResourceID  catalog.ResourceID
	RevisionID  catalog.RevisionID
	EditVersion int64
}

type PublishResult struct {
	ResourceID  catalog.ResourceID
	RevisionID  catalog.RevisionID
	EditVersion int64
	Slug        string
}

type Publisher interface {
	CreateDraftTx(ctx context.Context, tx pgx.Tx, cmd CreateDraftCommand) (DraftResult, error)
	SaveDraftTx(ctx context.Context, tx pgx.Tx, cmd SaveDraftCommand) (DraftResult, error)
	PublishTx(ctx context.Context, tx pgx.Tx, cmd PublishCommand) (PublishResult, error)
	SetVisibilityTx(ctx context.Context, tx pgx.Tx, cmd VisibilityCommand) error
}

type IdentityLookup interface {
	FindByIdentity(ctx context.Context, kind catalog.Kind, identityKey string) (catalog.ResourceID, bool, error)
	FindByIdentityTx(ctx context.Context, tx pgx.Tx, kind catalog.Kind, identityKey string) (catalog.ResourceID, bool, error)
}

type ModelRequest struct {
	ProcessingRunID uuid.UUID
	RequestKey      string
	Purpose         string
	Input           json.RawMessage
	SchemaName      string
	ProfileVersion  string
	MaxOutputTokens int
}

type ModelResponse struct {
	Output         json.RawMessage
	ProviderCallID *uuid.UUID
	Mode           string
}

type ModelClient interface {
	Complete(ctx context.Context, req ModelRequest) (ModelResponse, error)
}

type WriteResult struct {
	Status int
	Body   json.RawMessage
}

// Clock is duplicated at the edge so ports stay free of the platform clock package.
type Now func() time.Time

type ReviewMode string

const (
	ReviewManual    ReviewMode = "manual"
	ReviewAutomatic ReviewMode = "automatic"
)

// FieldDecision is accept, reject, or rewrite. A missing path is reject.
type FieldDecision string

type Decision struct {
	ProposalID  uuid.UUID
	EditVersion int64
	Mode        ReviewMode
	Fields      map[string]FieldDecision
	Rewrites    map[string]json.RawMessage
	Unlock      []string
	Reason      string
}

type Reviewer interface {
	DecideTx(ctx context.Context, tx pgx.Tx, decision Decision) (WriteResult, error)
}
