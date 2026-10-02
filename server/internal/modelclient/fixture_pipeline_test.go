package modelclient

import (
	"context"
	"encoding/json"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/google/uuid"
	"testing"
)

func TestVersionedEditorialFixtures(t *testing.T) {
	for _, version := range []string{"v1", "v2"} {
		for _, stage := range []string{"prefilter", "structure", "score", "write"} {
			t.Run(stage+"."+version, func(t *testing.T) {
				req := ports.ModelRequest{ProcessingRunID: uuid.New(), SchemaName: stage + "." + version, ProfileVersion: "editorial." + version, MaxOutputTokens: 4096, Input: json.RawMessage(`{"text":"fixture"}`)}
				req.RequestKey = ports.ModelRequestKey(req.ProfileVersion, req.SchemaName, req.MaxOutputTokens, req.Input)
				res, err := (FakeClient{}).Complete(context.Background(), req)
				if err != nil {
					t.Fatal(err)
				}
				var output map[string]any
				if err := json.Unmarshal(res.Output, &output); err != nil {
					t.Fatal(err)
				}
				field := map[string]string{"prefilter": "label", "structure": "kind", "score": "score", "write": "blurb"}[stage]
				if _, ok := output[field]; !ok {
					t.Fatalf("missing %s", field)
				}
				if res.Mode != "fixture" || res.ProviderCallID != nil {
					t.Fatal("fixture looked like live receipt")
				}
			})
		}
	}
}
