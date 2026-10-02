package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/darrenhoo/nex_club/server/internal/ports"
)

func TestOpenAIRequiresCredentials(t *testing.T) {
	t.Setenv("NEX_MODEL_API_KEY", "")
	if _, err := NewOpenAI("https://example.test/v1", "m", "NEX_MODEL_API_KEY"); err == nil {
		t.Fatal("expected missing key")
	}
	if _, err := NewOpenAI("", "m", "NEX_MODEL_API_KEY"); err == nil {
		t.Fatal("expected missing url")
	}
}

func TestOpenAISendClassifiesAndDoesNotLogSecrets(t *testing.T) {
	const secret = "sk-test-secret"
	const prompt = "PROMPT_SHOULD_NOT_BE_LOGGED"
	var sawAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer "+secret {
			sawAuth = true
		}
		switch r.URL.Path {
		case "/v1/chat/completions":
			var payload struct {
				Messages       []struct{ Role, Content string }
				ResponseFormat map[string]string `json:"response_format"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if len(payload.Messages) != 2 || payload.Messages[0].Role != "system" || !strings.Contains(payload.Messages[0].Content, "label") || payload.Messages[1].Content != prompt || payload.ResponseFormat["type"] != "json_object" {
				t.Error("stage task/schema not sent to model")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"c1","choices":[{"message":{"content":"{\"ok\":true}"}}],"usage":{"prompt_tokens":2,"completion_tokens":1,"cost":"0.02000000","currency":"USD"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("NEX_MODEL_API_KEY", secret)
	p, err := NewOpenAI(srv.URL+"/v1?api_key="+secret, "gpt-test", "NEX_MODEL_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	res, err := p.Send(context.Background(), ports.ModelRequest{Input: []byte(prompt), SchemaName: "prefilter.v1", MaxOutputTokens: 8})
	if err != nil {
		t.Fatal(err)
	}
	if !sawAuth || res.ProviderRequestID != "c1" || res.Currency != "USD" || res.ActualCost.String() != "0.02000000" {
		t.Fatalf("auth=%v result=%+v", sawAuth, res)
	}
	if string(res.Output) != `{"ok":true}` {
		t.Fatalf("output %s", res.Output)
	}
	text := logs.String()
	if strings.Contains(text, secret) || strings.Contains(text, prompt) || strings.Contains(text, "api_key=") {
		t.Fatalf("log leaked request data: %s", text)
	}
	if !strings.Contains(text, "body_bytes") {
		t.Fatalf("log %s", text)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/rate/") {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer bad.Close()
	p400, err := NewOpenAI(bad.URL+"/v1", "gpt-test", "NEX_MODEL_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	_, err = p400.Send(context.Background(), ports.ModelRequest{Input: []byte("x"), MaxOutputTokens: 1})
	var pe *ProviderError
	if !errors.As(err, &pe) || pe.Kind != kindParameter {
		t.Fatalf("400: %v", err)
	}
	p429, err := NewOpenAI(bad.URL+"/rate/v1", "gpt-test", "NEX_MODEL_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	_, err = p429.Send(context.Background(), ports.ModelRequest{Input: []byte("x"), MaxOutputTokens: 1})
	if !errors.As(err, &pe) || pe.Kind != kindRateLimit {
		t.Fatalf("429: %v", err)
	}
	dns := classifyTransport(&net.DNSError{Err: "no such host", Name: "missing.test", IsNotFound: true})
	if !errors.As(dns, &pe) || pe.Kind != kindDNS {
		t.Fatal(dns)
	}
}

func TestRedactURLDropsQuery(t *testing.T) {
	got := redactURL("https://user:pass@example.test/v1/chat?api_key=secret")
	if strings.Contains(got, "secret") || strings.Contains(got, "pass") || strings.Contains(got, "?") {
		t.Fatal(got)
	}
}
