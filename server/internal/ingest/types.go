package ingest

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	NormalizationVersion = "hash.v2"

	ModeContent  = "content"
	ModeSignal   = "signal"
	ModeInternal = "internal"

	StatusCreated    = "created"
	StatusRevised    = "revised"
	StatusUnchanged  = "unchanged"
	StatusDiscovered = "discovered"
	StatusRejected   = "rejected"
	StatusStale      = "stale_source"
)

// FetchArgs is the River job for one source run.
type FetchArgs struct {
	SourceRunID uuid.UUID `json:"source_run_id"`
}

func (FetchArgs) Kind() string { return "ingest.fetch" }

// StartRequest is the P7 handoff. HasBody is true when body_text is non-empty.
type StartRequest struct {
	RawItemID     uuid.UUID
	RawRevisionID uuid.UUID
	SourceID      uuid.UUID
	HasBody       bool
}

// PipelineStarter enqueues editorial work in the caller's transaction.
// A nil starter records nothing.
type PipelineStarter interface {
	Start(ctx context.Context, tx pgx.Tx, req StartRequest) error
}

// Source is the locked source row the adapters and AcceptTx share.
type Source struct {
	RunID         uuid.UUID
	PreviewKey    string
	ID            uuid.UUID
	Key           string
	Name          string
	Kind          string
	Mode          string
	Trust         string
	Enabled       bool
	Config        json.RawMessage
	CredentialRef string
	Checkpoint    json.RawMessage
	EditVersion   int64
	Interval      time.Duration
	AutoFields    []string
	AllowFulltext bool
	NextFetchAt   *time.Time
}

// IncomingItem is the anti-corruption item. Adapters fill it; AcceptTx does not parse vendor fields.
type IncomingItem struct {
	PublishedDateOnly bool
	Platform          string
	PlatformID        string
	SourceItemKey     string
	URL               string
	Title             string
	Excerpt           string
	BodyText          string
	BodyHTML          string
	Author            string
	Language          string
	PublishedAt       *time.Time
	UpdatedAt         *time.Time
	FetchedAt         time.Time
	GUID              string
	Permalink         bool
	GitHubID          string
	Payload           json.RawMessage
	IsBackfill        bool
	Truncated         bool
	BadDate           bool
}

// MetricSample is one observation. Nil counts stay unknown and are not stored as zero.
type MetricSample struct {
	IdentityKey string
	Stars       *int64
	Forks       *int64
	OpenIssues  *int64
	ObservedAt  time.Time
	Extra       json.RawMessage
}

// FetchBatch is the adapter result. Checkpoint is saved only after every Accept succeeds.
type FetchBatch struct {
	Items      []IncomingItem
	Checkpoint json.RawMessage
	Metrics    []MetricSample
}

// Adapter fetches one source kind. Implementations live under internal/sources.
type Adapter interface {
	Kind() string
	Fetch(ctx context.Context, src Source) (FetchBatch, error)
}

// Identity is the first strategy hit. NeedsReview is the source: prefix, not a column.
type Identity struct {
	Key           string
	CanonicalURL  string
	SourceItemKey string
	NeedsReview   bool
}

// ItemResult is the in-memory accept result. ResourceID is a lookup hit and is not written back.
type ItemResult struct {
	SourceItemKey string
	Status        string
	RawItemID     *uuid.UUID
	RevisionID    *uuid.UUID
	ResourceID    *uuid.UUID
	Code          string
}
