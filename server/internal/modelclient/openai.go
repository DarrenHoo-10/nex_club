package modelclient

import (
	"context"
	"os"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/darrenhoo/nex_club/server/internal/providers"
)

// OpenAIClient is the configured adapter. Receipts and budget stay in providers.Service.
type OpenAIClient struct {
	provider *providers.OpenAIProvider
}

func newOpenAIClient() (OpenAIClient, error) {
	provider, err := providers.NewOpenAI(os.Getenv("NEX_MODEL_BASE_URL"), os.Getenv("NEX_MODEL_NAME"), "NEX_MODEL_API_KEY")
	if err != nil {
		return OpenAIClient{}, err
	}
	return OpenAIClient{provider: provider}, nil
}

func (c OpenAIClient) Complete(context.Context, ports.ModelRequest) (ports.ModelResponse, error) {
	if c.provider == nil {
		return ports.ModelResponse{}, apperr.Unavailable("模型适配器未配置")
	}
	return ports.ModelResponse{}, apperr.Unavailable("模型预算服务未装配")
}
