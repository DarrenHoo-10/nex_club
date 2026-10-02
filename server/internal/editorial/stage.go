package editorial

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/darrenhoo/nex_club/server/internal/editorial/sqlc"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/ports"
)

// Stage is one filter. RuleVersion identifies the registered implementation.
type Stage interface {
	Name() string
	RuleVersion() string
	Run(ctx context.Context, in stageInput) (stageOutput, error)
}

// StageResolver loads the implementation frozen in the round plan.
type StageResolver interface {
	Resolve(name, frozenRuleVersion string) (Stage, bool)
}

type stageInput struct {
	Run        sqlc.GetRunRow
	Plan       stagePlan
	Revision   sqlc.GetRawRevisionRow
	Source     sqlc.GetSourceRow
	Text       string
	SourceText string
	Upstream   map[string]json.RawMessage
}

type stageOutput struct {
	Body         []byte
	BlockCode    string
	BlockMessage string
	FailCode     string
	FailMessage  string
}

func (s *Service) complete(ctx context.Context, runID uuid.UUID, plan stagePlan, purpose string, input any) (json.RawMessage, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	body, err := canonical(raw)
	if err != nil {
		return nil, err
	}
	client := s.model
	if client == nil {
		return nil, apperr.ModelDisabled()
	}
	resp, err := client.Complete(ctx, ports.ModelRequest{
		ProcessingRunID: runID,
		RequestKey:      ports.ModelRequestKey(plan.ProfileVersion, plan.SchemaName, plan.MaxOutputTokens, body),
		Purpose:         purpose,
		Input:           body,
		SchemaName:      plan.SchemaName,
		ProfileVersion:  plan.ProfileVersion,
		MaxOutputTokens: plan.MaxOutputTokens,
	})
	if err != nil {
		return nil, err
	}
	return resp.Output, nil
}
