package modelclient

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/ports"
)

//go:embed fixtures/*.json
var fixtureFS embed.FS

var schemaName = regexp.MustCompile(`^(prefilter|structure|score|write)(\.v[12])?$`)

// FakeClient reads a fixture by schema name. It does not write provider_calls.
type FakeClient struct{}

func (FakeClient) Complete(_ context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	if req.ProcessingRunID == uuid.Nil {
		return ports.ModelResponse{}, apperr.Invalid("缺少处理运行")
	}
	if req.RequestKey == "" {
		return ports.ModelResponse{}, apperr.Invalid("缺少请求键")
	}
	if !schemaName.MatchString(req.SchemaName) {
		return ports.ModelResponse{}, apperr.Invalid("缺少返回模式")
	}
	if req.ProfileVersion == "" {
		return ports.ModelResponse{}, apperr.Invalid("缺少模型配置")
	}
	if req.MaxOutputTokens <= 0 {
		return ports.ModelResponse{}, apperr.Invalid("输出上限不合法")
	}
	name := strings.TrimSuffix(strings.TrimSuffix(req.SchemaName, ".v1"), ".v2")
	raw, err := fixtureFS.ReadFile("fixtures/" + name + ".json")
	if err != nil {
		return ports.ModelResponse{}, apperr.Invalid("没有该模式的夹具")
	}
	if !json.Valid(raw) {
		return ports.ModelResponse{}, apperr.Invalid("夹具不是合法 JSON")
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return ports.ModelResponse{}, apperr.Invalid("夹具不是合法 JSON")
	}
	return ports.ModelResponse{Output: buf.Bytes(), Mode: "fixture"}, nil
}
