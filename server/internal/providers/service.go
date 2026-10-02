package providers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/darrenhoo/nex_club/server/internal/providers/sqlc"
)

// Profile is an immutable price card. Currency is taken from here, not the caller.
type Profile struct {
	Version         string
	ProviderKey     string
	Model           string
	Currency        string
	InputPerToken   string
	OutputPerToken  string
	MaxOutputTokens int
	MaxEstimate     string
}

type storedProfile struct {
	version     string
	providerKey string
	model       string
	currency    string
	input       Amount
	output      Amount
	maxOutput   int
	maxEstimate Amount
	hasMax      bool
}

// Service is the budgeted model port.
type Service struct {
	pool     *pgxpool.Pool
	provider ChatProvider
	now      func() time.Time

	mu             sync.Mutex
	profiles       map[string]storedProfile
	defaultProfile string
	daily          Amount
	dailyOK        bool
	scopeLimits    map[string]Amount

	// Test hooks. afterPrepared runs after the prepared commit and before sent.
	afterPrepared func(context.Context, uuid.UUID) error
	settleHook    func(string) error
	afterSettle   func() error
}

// New builds a service. now may be nil.
func New(pool *pgxpool.Pool, provider ChatProvider, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{
		pool:        pool,
		provider:    provider,
		now:         func() time.Time { return now().UTC() },
		profiles:    map[string]storedProfile{},
		scopeLimits: map[string]Amount{},
	}
}

func (s *Service) clock() time.Time {
	if s.now == nil {
		return time.Now().UTC()
	}
	return s.now()
}

// RegisterProfile adds a price card tests and the process can select by version.
func (s *Service) RegisterProfile(p Profile) error {
	version := strings.TrimSpace(p.Version)
	key := strings.TrimSpace(p.ProviderKey)
	model := strings.TrimSpace(p.Model)
	currency := strings.TrimSpace(p.Currency)
	if version == "" || key == "" || model == "" {
		return apperr.Invalid("模型配置不完整")
	}
	if currency != "CNY" && currency != "USD" {
		return apperr.Invalid("币种只支持 CNY 或 USD")
	}
	if p.MaxOutputTokens <= 0 {
		return apperr.Invalid("输出上限不合法")
	}
	in, err := ParseAmount(p.InputPerToken)
	if err != nil {
		return apperr.Invalid("输入单价不合法")
	}
	out, err := ParseAmount(p.OutputPerToken)
	if err != nil {
		return apperr.Invalid("输出单价不合法")
	}
	stored := storedProfile{
		version: version, providerKey: key, model: model, currency: currency,
		input: in, output: out, maxOutput: p.MaxOutputTokens,
	}
	if strings.TrimSpace(p.MaxEstimate) != "" {
		maxEst, err := ParseAmount(p.MaxEstimate)
		if err != nil {
			return apperr.Invalid("预估上限不合法")
		}
		stored.maxEstimate = maxEst
		stored.hasMax = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.profiles[version]; ok {
		if old.providerKey != stored.providerKey || old.model != stored.model || old.currency != stored.currency || old.input.Cmp(stored.input) != 0 || old.output.Cmp(stored.output) != 0 || old.maxOutput != stored.maxOutput || old.hasMax != stored.hasMax || old.maxEstimate.Cmp(stored.maxEstimate) != 0 {
			return apperr.Invalid("不能修改已注册的模型配置版本")
		}
		return nil
	}
	s.profiles[version] = stored
	if s.defaultProfile == "" {
		s.defaultProfile = version
	}
	return nil
}

func (s *Service) DefaultProfileVersion() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.defaultProfile
}

// SetDefaultDailyLimit applies to day and task windows that have no override.
func (s *Service) SetDefaultDailyLimit(amount string) error {
	parsed, err := ParseAmount(amount)
	if err != nil {
		return apperr.Invalid("预算上限不合法")
	}
	s.mu.Lock()
	s.daily = parsed
	s.dailyOK = true
	s.mu.Unlock()
	return nil
}

// SetScopeLimit overrides the limit for one exact scope key.
func (s *Service) SetScopeLimit(scopeKey, amount string) error {
	parsed, err := ParseAmount(amount)
	if err != nil {
		return apperr.Invalid("预算上限不合法")
	}
	s.mu.Lock()
	s.scopeLimits[scopeKey] = parsed
	s.mu.Unlock()
	return nil
}

func (s *Service) profile(version string) (storedProfile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.profiles[version]
	return p, ok
}

func (s *Service) limitFor(scope string) (Amount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.scopeLimits[scope]; ok {
		return a, nil
	}
	if !s.dailyOK {
		return Amount{}, apperr.Invalid("未配置预算上限")
	}
	return s.daily, nil
}

// Complete reserves budget, sends at most once, and settles in one transaction.
func (s *Service) Complete(ctx context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	prof, est, err := s.prepare(req)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	if s.provider == nil || strings.TrimSpace(s.provider.Key()) == "" {
		return ports.ModelResponse{}, apperr.Invalid("供应商未配置")
	}
	if s.provider.Key() != prof.providerKey {
		return ports.ModelResponse{}, apperr.Invalid("供应商与配置不一致")
	}
	id, resp, send, err := s.reserve(ctx, req, prof, est)
	if isUnique(err) {
		id, resp, send, err = s.reserve(ctx, req, prof, est)
	}
	if err != nil {
		return ports.ModelResponse{}, err
	}
	if !send {
		return resp, nil
	}
	return s.dispatch(ctx, id, req, prof)
}

func (s *Service) prepare(req ports.ModelRequest) (storedProfile, Amount, error) {
	if req.ProcessingRunID == uuid.Nil {
		return storedProfile{}, Amount{}, apperr.Invalid("缺少处理运行")
	}
	if req.MaxOutputTokens <= 0 {
		return storedProfile{}, Amount{}, apperr.Invalid("输出上限不合法")
	}
	prof, ok := s.profile(strings.TrimSpace(req.ProfileVersion))
	if !ok {
		return storedProfile{}, Amount{}, apperr.New("model_profile_unavailable", "该轮冻结的模型配置已不可用，请恢复原配置或显式重跑", 503)
	}
	if req.MaxOutputTokens > prof.maxOutput {
		return storedProfile{}, Amount{}, apperr.Invalid("输出上限超过配置")
	}
	if strings.TrimSpace(req.SchemaName) == "" {
		return storedProfile{}, Amount{}, apperr.Invalid("缺少返回模式")
	}
	want := CanonicalRequestKey(req.ProfileVersion, req.SchemaName, req.MaxOutputTokens, req.Input)
	if req.RequestKey != want {
		return storedProfile{}, Amount{}, apperr.Invalid("请求键与配置不一致")
	}
	input := req.Input
	if encoder, ok := s.provider.(interface {
		requestBody(ports.ModelRequest) ([]byte, error)
	}); ok {
		var err error
		input, err = encoder.requestBody(req)
		if err != nil {
			return storedProfile{}, Amount{}, err
		}
	}
	est, err := prof.estimate(input, req.MaxOutputTokens)
	if err != nil {
		return storedProfile{}, Amount{}, err
	}
	return prof, est, nil
}

func (p storedProfile) estimate(input []byte, maxOut int) (Amount, error) {
	total := p.input.MulInt(int64(len(input))).Add(p.output.MulInt(int64(maxOut)))
	if p.hasMax && total.Cmp(p.maxEstimate) > 0 {
		return Amount{}, apperr.Invalid("请求超过配置允许的预估费用")
	}
	return total, nil
}

func (s *Service) dispatch(ctx context.Context, id uuid.UUID, req ports.ModelRequest, prof storedProfile) (ports.ModelResponse, error) {
	if s.afterPrepared != nil {
		if err := s.afterPrepared(ctx, id); err != nil {
			return ports.ModelResponse{}, err
		}
	}
	won, err := s.markSent(ctx, id)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	if !won {
		return s.afterLost(ctx, id)
	}
	result, err := s.provider.Send(ctx, req)
	if err != nil {
		return ports.ModelResponse{}, s.classify(ctx, id, err)
	}
	if err := normalizeResult(prof, &result); err != nil {
		return ports.ModelResponse{}, s.markUnknown(ctx, id, "invalid_result")
	}
	if err := s.settleSuccess(ctx, id, result); err != nil {
		return ports.ModelResponse{}, err
	}
	if s.afterSettle != nil {
		if err := s.afterSettle(); err != nil {
			return ports.ModelResponse{}, err
		}
	}
	return liveResponse(id, result.Output)
}

func normalizeResult(prof storedProfile, res *Result) error {
	if res.InputTokens < 0 || res.OutputTokens < 0 {
		return errors.New("invalid usage")
	}
	if res.Currency == "" {
		res.ActualCost = prof.input.MulInt(res.InputTokens).Add(prof.output.MulInt(res.OutputTokens))
		res.Currency = prof.currency
	}
	if res.Currency != prof.currency {
		return errors.New("currency")
	}
	if len(res.Output) == 0 || !json.Valid(res.Output) {
		return errors.New("output")
	}
	return nil
}

func (s *Service) classify(ctx context.Context, id uuid.UUID, err error) error {
	var pe *ProviderError
	if errors.As(err, &pe) {
		switch pe.Kind {
		case kindDNS:
			return s.failSent(ctx, id, "dns_failure")
		case kindParameter:
			return s.failSent(ctx, id, "parameter_error")
		case kindRateLimit:
			return s.markUnknown(ctx, id, "rate_limited")
		default:
			return s.markUnknown(ctx, id, "network_error")
		}
	}
	return s.markUnknown(ctx, id, "network_error")
}

func (s *Service) afterLost(ctx context.Context, id uuid.UUID) (ports.ModelResponse, error) {
	call, err := queries(s.pool).ReadCall(ctx, id)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	switch call.Status {
	case "succeeded":
		return s.replayID(ctx, id)
	case "unknown":
		return ports.ModelResponse{}, providerUnknown()
	case "failed":
		return ports.ModelResponse{}, providerFailed("调用未发出，预占已释放")
	default:
		return ports.ModelResponse{}, providerInflight()
	}
}

func (s *Service) markSent(ctx context.Context, id uuid.UUID) (bool, error) {
	won := false
	err := s.tx(ctx, func(ctx context.Context, q *sqlc.Queries) error {
		_, err := q.MarkSent(ctx, sqlc.MarkSentParams{SentAt: pgTime(s.clock()), ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		won = true
		return nil
	})
	if err != nil {
		call, readErr := queries(s.pool).ReadCall(ctx, id)
		if readErr == nil && call.Status == "sent" {
			return true, nil
		}
		return false, err
	}
	return won, nil
}

func liveResponse(id uuid.UUID, output json.RawMessage) (ports.ModelResponse, error) {
	if len(output) == 0 || !json.Valid(output) {
		return ports.ModelResponse{}, apperr.Invalid("回执缺少输出")
	}
	out := append(json.RawMessage(nil), output...)
	callID := id
	return ports.ModelResponse{Output: out, ProviderCallID: &callID, Mode: "live"}, nil
}

var _ ports.ModelClient = (*Service)(nil)
