package modelclient

import (
	"context"
	"fmt"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/config"
	"github.com/darrenhoo/nex_club/server/internal/ports"
)

// DisabledClient is the default model port. It never returns fixture text.
type DisabledClient struct{}

func (DisabledClient) Complete(context.Context, ports.ModelRequest) (ports.ModelResponse, error) {
	return ports.ModelResponse{}, apperr.ModelDisabled()
}

// Select refuses a production fixture and refuses both switches at once.
func Select(cfg config.Config) (ports.ModelClient, error) {
	if cfg.ModelEnabled && cfg.ModelFixture {
		return nil, fmt.Errorf("NEX_MODEL_ENABLED and NEX_MODEL_FIXTURE cannot both be true")
	}
	if cfg.ModelFixture && cfg.Environment == "production" {
		return nil, fmt.Errorf("production refuses NEX_MODEL_FIXTURE=true")
	}
	if cfg.ModelFixture {
		if cfg.Environment == "development" || cfg.Environment == "test" {
			return FakeClient{}, nil
		}
		return nil, fmt.Errorf("NEX_MODEL_FIXTURE is only allowed in development or test")
	}
	if cfg.ModelEnabled {
		client, err := newOpenAIClient()
		if err != nil {
			return nil, fmt.Errorf("NEX_MODEL_ENABLED is set but no model adapter is configured")
		}
		return client, nil
	}
	return DisabledClient{}, nil
}
