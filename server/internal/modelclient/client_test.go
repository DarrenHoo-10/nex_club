package modelclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/config"
	"github.com/darrenhoo/nex_club/server/internal/ports"
)

func TestDisabledClientReturnsNoOutput(t *testing.T) {
	resp, err := DisabledClient{}.Complete(context.Background(), ports.ModelRequest{})
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != "model_disabled" {
		t.Fatalf("err=%v resp=%+v", err, resp)
	}
	if len(resp.Output) != 0 || resp.ProviderCallID != nil || resp.Mode != "" {
		t.Fatalf("fixture output leaked: %+v", resp)
	}
}

func TestSelectGates(t *testing.T) {
	t.Setenv("NEX_MODEL_BASE_URL", "")
	t.Setenv("NEX_MODEL_NAME", "")
	t.Setenv("NEX_MODEL_API_KEY", "")
	client, err := Select(config.Config{Environment: "development"})
	if err != nil {
		t.Fatal(err)
	}
	disabled, ok := client.(DisabledClient)
	if !ok {
		t.Fatalf("client %T", client)
	}
	resp, err := disabled.Complete(context.Background(), ports.ModelRequest{})
	if len(resp.Output) != 0 || resp.Mode != "" {
		t.Fatalf("generated text: %+v", resp)
	}
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != "model_disabled" {
		t.Fatal(err)
	}

	if _, err := Select(config.Config{Environment: "production", ModelFixture: true}); err == nil || !contains(err, "NEX_MODEL_FIXTURE") {
		t.Fatalf("production fixture: %v", err)
	}
	if _, err := Select(config.Config{Environment: "development", ModelEnabled: true, ModelFixture: true}); err == nil || !contains(err, "cannot both") {
		t.Fatalf("both: %v", err)
	}
	if _, err := Select(config.Config{Environment: "production", ModelEnabled: true, ModelFixture: true}); err == nil {
		t.Fatal("production with both switches started")
	}
	dev, err := Select(config.Config{Environment: "development", ModelFixture: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := dev.(FakeClient); !ok {
		t.Fatalf("development fixture: %T", dev)
	}
	testClient, err := Select(config.Config{Environment: "test", ModelFixture: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := testClient.(FakeClient); !ok {
		t.Fatalf("test fixture: %T", testClient)
	}
	if _, err := Select(config.Config{Environment: "production", ModelEnabled: true}); err == nil || !contains(err, "no model adapter is configured") {
		t.Fatalf("enabled without adapter: %v", err)
	}
}

func TestSelectOpenAIDoesNotCallNetwork(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer srv.Close()
	t.Setenv("NEX_MODEL_BASE_URL", srv.URL+"/v1")
	t.Setenv("NEX_MODEL_NAME", "gpt-test")
	t.Setenv("NEX_MODEL_API_KEY", "secret")
	client, err := Select(config.Config{Environment: "production", ModelEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := client.(OpenAIClient); !ok {
		t.Fatalf("client %T", client)
	}
	_, err = client.Complete(context.Background(), ports.ModelRequest{})
	if err == nil {
		t.Fatal("expected unwired budget error")
	}
	if hits != 0 {
		t.Fatalf("adapter made %d network calls", hits)
	}
}

func TestFakeClientFixtureAndValidation(t *testing.T) {
	run := uuid.New()
	ok := ports.ModelRequest{
		ProcessingRunID: run,
		RequestKey:      "k",
		SchemaName:      "score",
		ProfileVersion:  "v1",
		MaxOutputTokens: 16,
		Input:           []byte(`{"text":"a"}`),
	}
	resp, err := FakeClient{}.Complete(context.Background(), ok)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Mode != "fixture" || resp.ProviderCallID != nil || string(resp.Output) != `{"score":1,"reason":"fixture"}` {
		t.Fatalf("%+v", resp)
	}
	bad := []ports.ModelRequest{
		{},
		{RequestKey: "k", SchemaName: "score", ProfileVersion: "v1", MaxOutputTokens: 1},
		{ProcessingRunID: run, SchemaName: "score", ProfileVersion: "v1", MaxOutputTokens: 1},
		{ProcessingRunID: run, RequestKey: "k", ProfileVersion: "v1", MaxOutputTokens: 1},
		{ProcessingRunID: run, RequestKey: "k", SchemaName: "score", MaxOutputTokens: 1},
		{ProcessingRunID: run, RequestKey: "k", SchemaName: "score", ProfileVersion: "v1"},
		{ProcessingRunID: run, RequestKey: "k", SchemaName: "../score", ProfileVersion: "v1", MaxOutputTokens: 1},
	}
	for _, req := range bad {
		resp, err := FakeClient{}.Complete(context.Background(), req)
		if err == nil || len(resp.Output) != 0 || resp.Mode != "" {
			t.Fatalf("req=%+v err=%v resp=%+v", req, err, resp)
		}
	}
}

func contains(err error, part string) bool {
	return err != nil && len(part) > 0 && (err.Error() == part || len(err.Error()) >= len(part) && containsRaw(err.Error(), part))
}

func containsRaw(s, part string) bool {
	return len(s) >= len(part) && (s == part || len(part) == 0 || indexOf(s, part) >= 0)
}

func indexOf(s, part string) int {
	for i := 0; i+len(part) <= len(s); i++ {
		if s[i:i+len(part)] == part {
			return i
		}
	}
	return -1
}
