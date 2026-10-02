package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/darrenhoo/nex_club/server/internal/ports"
)

// OpenAIProvider calls an OpenAI-compatible chat endpoint.
// The secret is read from the environment variable named by credentialRef and is never logged.
type OpenAIProvider struct {
	baseURL       string
	model         string
	credentialRef string
	client        *http.Client
}

// NewOpenAI refuses to construct an adapter unless the base URL, model, and key are all present.
func NewOpenAI(baseURL, model, credentialRef string) (*OpenAIProvider, error) {
	baseURL = strings.TrimSpace(baseURL)
	model = strings.TrimSpace(model)
	credentialRef = strings.TrimSpace(credentialRef)
	if baseURL == "" || model == "" || credentialRef == "" || strings.TrimSpace(os.Getenv(credentialRef)) == "" {
		return nil, errors.New("model adapter is not configured")
	}
	return &OpenAIProvider{
		baseURL:       baseURL,
		model:         model,
		credentialRef: credentialRef,
		client: &http.Client{
			Timeout: 60 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (p *OpenAIProvider) Key() string { return "openai" }

func (p *OpenAIProvider) requestBody(req ports.ModelRequest) ([]byte, error) {
	return json.Marshal(map[string]any{
		"model": p.model,
		"messages": []map[string]string{
			{"role": "system", "content": ports.ModelSystemPrompt(req.SchemaName)},
			{"role": "user", "content": string(req.Input)},
		},
		"response_format": map[string]string{"type": "json_object"},
		"max_tokens":      req.MaxOutputTokens,
	})
}

func (p *OpenAIProvider) Send(ctx context.Context, req ports.ModelRequest) (Result, error) {
	key := strings.TrimSpace(os.Getenv(p.credentialRef))
	if key == "" {
		return Result{}, errors.New("model adapter is not configured")
	}
	body, err := p.requestBody(req)
	if err != nil {
		return Result{}, err
	}
	endpoint, err := p.endpoint()
	if err != nil {
		return Result{}, err
	}
	slog.Info("provider request", "provider", p.Key(), "url", redactURL(endpoint), "body_bytes", len(body))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+key)
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return Result{}, classifyTransport(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, AfterSendError(err)
	}
	slog.Info("provider response", "provider", p.Key(), "status", resp.StatusCode, "body_bytes", len(raw))
	if resp.StatusCode == http.StatusTooManyRequests {
		return Result{}, RateLimitError(fmt.Errorf("provider status %d", resp.StatusCode))
	}
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return Result{}, ParameterError(fmt.Errorf("provider status %d", resp.StatusCode))
	}
	if resp.StatusCode >= 500 || resp.StatusCode < 200 {
		return Result{}, AfterSendError(fmt.Errorf("provider status %d", resp.StatusCode))
	}
	return parseOpenAI(raw)
}

func parseOpenAI(raw []byte) (Result, error) {
	var body struct {
		ID      string `json:"id"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64           `json:"prompt_tokens"`
			CompletionTokens int64           `json:"completion_tokens"`
			Cost             json.RawMessage `json:"cost"`
			Currency         string          `json:"currency"`
		} `json:"usage"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&body); err != nil {
		return Result{}, AfterSendError(err)
	}
	if len(body.Choices) == 0 {
		return Result{}, AfterSendError(errors.New("provider response has no output"))
	}
	content := strings.TrimSpace(body.Choices[0].Message.Content)
	var output json.RawMessage
	if json.Valid([]byte(content)) && (strings.HasPrefix(content, "{") || strings.HasPrefix(content, "[")) {
		output = json.RawMessage(content)
	} else {
		encoded, err := json.Marshal(content)
		if err != nil {
			return Result{}, AfterSendError(err)
		}
		output = encoded
	}
	result := Result{
		ProviderRequestID: body.ID,
		Output:            output,
		InputTokens:       body.Usage.PromptTokens,
		OutputTokens:      body.Usage.CompletionTokens,
	}
	currency := strings.TrimSpace(body.Usage.Currency)
	if len(body.Usage.Cost) > 0 && string(body.Usage.Cost) != "null" && (currency == "CNY" || currency == "USD") {
		cost, err := ParseAmount(strings.Trim(string(body.Usage.Cost), `"`))
		if err != nil {
			return Result{}, AfterSendError(err)
		}
		result.ActualCost = cost
		result.Currency = currency
	}
	return result, nil
}

func (p *OpenAIProvider) endpoint() (string, error) {
	u, err := url.Parse(p.baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("model adapter is not configured")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/chat/completions"
	return u.String(), nil
}

func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.RawQuery = ""
	u.ForceQuery = false
	u.User = nil
	return u.String()
}

func classifyTransport(err error) error {
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return DNSError(err)
	}
	return AfterSendError(err)
}
