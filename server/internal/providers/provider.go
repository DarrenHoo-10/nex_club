package providers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/ports"
)

// ChatProvider is one vendor adapter. Service does not parse vendor JSON.
type ChatProvider interface {
	Key() string
	Send(ctx context.Context, req ports.ModelRequest) (Result, error)
}

// Result is the vendor-neutral receipt of one send.
type Result struct {
	ProviderRequestID string
	Output            json.RawMessage
	InputTokens       int64
	OutputTokens      int64
	ActualCost        Amount
	Currency          string
}

// ProviderError classifies a send failure without vendor payloads.
type ProviderError struct {
	Kind errorKind
	Err  error
}

type errorKind int

const (
	kindDNS errorKind = iota + 1
	kindParameter
	kindRateLimit
	kindAfterSend
)

func (e *ProviderError) Error() string {
	if e == nil {
		return "provider error"
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "provider error"
}

func (e *ProviderError) Unwrap() error { return e.Err }

// DNSError is a failure before the connection exists. The reservation is released.
func DNSError(err error) error { return &ProviderError{Kind: kindDNS, Err: err} }

// ParameterError is a definite 4xx parameter rejection. It is not retried.
func ParameterError(err error) error { return &ProviderError{Kind: kindParameter, Err: err} }

// RateLimitError is an HTTP 429 after the request was sent. The reservation stays held.
func RateLimitError(err error) error { return &ProviderError{Kind: kindRateLimit, Err: err} }

// AfterSendError is a network or server failure after the request may have been billed.
func AfterSendError(err error) error { return &ProviderError{Kind: kindAfterSend, Err: err} }

func budgetExhausted() *apperr.Error {
	return apperr.New("budget_exhausted", "预算已用尽", http.StatusTooManyRequests)
}

func providerUnknown() *apperr.Error {
	return apperr.New("provider_unknown", "供应商结果不明，额度仍保留", http.StatusServiceUnavailable)
}

func providerFailed(message string) *apperr.Error {
	return apperr.New("provider_failed", message, http.StatusBadGateway)
}

func providerInflight() *apperr.Error {
	return apperr.New("provider_inflight", "已有未完成的调用", http.StatusConflict)
}

func providerUnsettled() *apperr.Error {
	return apperr.New("provider_unsettled", "成功回执缺少实际费用，无法结算", http.StatusConflict)
}
