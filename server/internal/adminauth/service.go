package adminauth

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/clock"
	"github.com/darrenhoo/nex_club/server/internal/platform/id"
)

const SessionTTL = 12 * time.Hour

type AuthSession struct {
	ID        uuid.UUID
	AdminID   catalog.AdminID
	Username  string
	ExpiresAt time.Time
	CSRFHash  string
}

type LoginResult struct {
	Session AuthSession
	Token   string
	CSRF    string
}

// Limited is a rate-limit failure. Unwrap yields the apperr value.
type Limited struct {
	RetryAfter int
	Err        *apperr.Error
}

func (e *Limited) Error() string {
	if e == nil || e.Err == nil {
		return "尝试次数过多，请稍后再试"
	}
	return e.Err.Error()
}

func (e *Limited) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type Service struct {
	repo    Repo
	hasher  PasswordHasher
	limiter LoginLimiter
	secret  []byte
	clock   clock.Clock
	ids     id.Generator
	ttl     time.Duration

	dummyOnce sync.Once
	dummy     string
	dummyErr  error
}

func NewService(repo Repo, hasher PasswordHasher, limiter LoginLimiter, secret []byte, clk clock.Clock, ids id.Generator) (*Service, error) {
	if repo == nil || hasher == nil {
		return nil, errors.New("admin service is incomplete")
	}
	if limiter == nil {
		limiter = NewMemoryLimiter()
	}
	if clk == nil {
		clk = clock.Real{}
	}
	if ids == nil {
		ids = id.Random{}
	}
	copied := append([]byte(nil), secret...)
	return &Service{repo: repo, hasher: hasher, limiter: limiter, secret: copied, clock: clk, ids: ids, ttl: SessionTTL}, nil
}

func (s *Service) now() time.Time { return s.clock.Now().UTC() }

func (s *Service) Login(ctx context.Context, username, password, ip string) (LoginResult, error) {
	if len(s.secret) < 32 {
		return LoginResult{}, errors.New("session secret must be at least 32 bytes")
	}
	now := s.now()
	norm, normErr := NormalizeUsername(username)
	key := norm
	if normErr != nil {
		key = rawLimitKey(username)
	}
	if ip == "" {
		ip = "unknown"
	}
	if retry, blocked := s.limiter.TooMany(key, ip, now); blocked {
		return LoginResult{}, &Limited{RetryAfter: retry, Err: apperr.RateLimited("尝试次数过多，请稍后再试")}
	}
	var admin AdminRecord
	var found bool
	if normErr == nil {
		err := s.repo.WithTx(ctx, func(ctx context.Context, tx RepoTx) error {
			var err error
			admin, found, err = tx.FindAdmin(ctx, norm)
			return err
		})
		if err != nil {
			return LoginResult{}, err
		}
	}
	active := found && admin.Status == "active"
	hash := admin.PasswordHash
	if !active {
		dummy, err := s.dummyHash()
		if err != nil {
			return LoginResult{}, err
		}
		hash = dummy
	}
	match, err := s.hasher.Verify(hash, password)
	if err != nil || !active || !match {
		s.limiter.Fail(key, ip, now)
		return LoginResult{}, apperr.Unauthenticated("用户名或口令不正确")
	}
	issued, err := s.issue(admin.ID, admin.Username, now)
	if err != nil {
		return LoginResult{}, err
	}
	err = s.repo.WithTx(ctx, func(ctx context.Context, tx RepoTx) error {
		again, ok, err := tx.FindAdmin(ctx, admin.Username)
		if err != nil {
			return err
		}
		if !ok || again.Status != "active" {
			return errInactive
		}
		if err := tx.InsertSession(ctx, issued.record); err != nil {
			return err
		}
		return tx.TouchLogin(ctx, admin.ID, now)
	})
	if errors.Is(err, errInactive) {
		s.limiter.Fail(key, ip, now)
		return LoginResult{}, apperr.Unauthenticated("用户名或口令不正确")
	}
	if err != nil {
		return LoginResult{}, err
	}
	s.limiter.Success(key)
	return issued.result, nil
}

func (s *Service) Authenticate(ctx context.Context, cookie string) (AuthSession, error) {
	if len(s.secret) < 32 {
		return AuthSession{}, errors.New("session secret must be at least 32 bytes")
	}
	raw, err := DecodeToken(cookie)
	if err != nil {
		return AuthSession{}, apperr.Unauthenticated("未登录")
	}
	var session AuthSession
	var valid bool
	err = s.repo.WithTx(ctx, func(ctx context.Context, tx RepoTx) error {
		rec, ok, err := tx.FindSession(ctx, HashToken(s.secret, raw))
		if err != nil || !ok {
			return err
		}
		now := s.now()
		if rec.Status != "active" || rec.RevokedAt != nil || !now.Before(rec.ExpiresAt) {
			return nil
		}
		if err := tx.TouchSession(ctx, rec.ID, now); err != nil {
			return err
		}
		session = AuthSession{
			ID:        rec.ID,
			AdminID:   catalog.AdminID(rec.AdminID),
			Username:  rec.Username,
			ExpiresAt: rec.ExpiresAt,
			CSRFHash:  rec.CSRFHash,
		}
		valid = true
		return nil
	})
	if err != nil {
		return AuthSession{}, err
	}
	if !valid {
		return AuthSession{}, apperr.Unauthenticated("未登录")
	}
	return session, nil
}

func (s *Service) Logout(ctx context.Context, sessionID uuid.UUID) error {
	now := s.now()
	return s.repo.WithTx(ctx, func(ctx context.Context, tx RepoTx) error {
		return tx.RevokeSession(ctx, sessionID, now)
	})
}

func (s *Service) CreateInitialAdmin(ctx context.Context, username, password string, reset bool) (catalog.AdminID, error) {
	norm, err := NormalizeUsername(username)
	if err != nil {
		return catalog.AdminID{}, err
	}
	if err := ValidatePassword(password); err != nil {
		return catalog.AdminID{}, err
	}
	var (
		exists bool
		any    bool
	)
	err = s.repo.WithTx(ctx, func(ctx context.Context, tx RepoTx) error {
		if err := tx.LockAdminInit(ctx); err != nil {
			return err
		}
		var err error
		any, err = tx.AnyAdmin(ctx)
		if err != nil {
			return err
		}
		_, exists, err = tx.FindAdmin(ctx, norm)
		return err
	})
	if err != nil {
		return catalog.AdminID{}, err
	}
	if !reset && (exists || any) {
		return catalog.AdminID{}, errAdminExists
	}
	if reset && !exists {
		return catalog.AdminID{}, apperr.NotFound("没有可重置的管理员")
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return catalog.AdminID{}, err
	}
	var id catalog.AdminID
	err = s.repo.WithTx(ctx, func(ctx context.Context, tx RepoTx) error {
		if err := tx.LockAdminInit(ctx); err != nil {
			return err
		}
		againAny, err := tx.AnyAdmin(ctx)
		if err != nil {
			return err
		}
		again, ok, err := tx.FindAdmin(ctx, norm)
		if err != nil {
			return err
		}
		now := s.now()
		if ok {
			if !reset {
				return errAdminExists
			}
			if err := tx.UpdatePassword(ctx, again.ID, hash, now); err != nil {
				return err
			}
			if err := tx.RevokeAllSessions(ctx, again.ID, now); err != nil {
				return err
			}
			id = catalog.AdminID(again.ID)
			return nil
		}
		if againAny {
			return errAdminExists
		}
		newID := s.ids.New()
		if err := tx.InsertAdmin(ctx, newID, norm, hash); err != nil {
			return err
		}
		id = catalog.AdminID(newID)
		return nil
	})
	return id, err
}

func (s *Service) issue(adminID uuid.UUID, username string, now time.Time) (issuedSession, error) {
	token := make([]byte, tokenBytes)
	csrf := make([]byte, tokenBytes)
	if _, err := rand.Read(token); err != nil {
		return issuedSession{}, err
	}
	if _, err := rand.Read(csrf); err != nil {
		return issuedSession{}, err
	}
	expires := now.Add(s.ttl)
	record := NewSession{
		ID:         s.ids.New(),
		AdminID:    adminID,
		TokenHash:  HashToken(s.secret, token),
		CSRFHash:   HashCSRF(csrf),
		ExpiresAt:  expires,
		LastSeenAt: now,
	}
	return issuedSession{
		record: record,
		result: LoginResult{
			Session: AuthSession{
				ID:        record.ID,
				AdminID:   catalog.AdminID(adminID),
				Username:  username,
				ExpiresAt: expires,
				CSRFHash:  record.CSRFHash,
			},
			Token: EncodeToken(token),
			CSRF:  EncodeToken(csrf),
		},
	}, nil
}

func (s *Service) dummyHash() (string, error) {
	s.dummyOnce.Do(func() {
		s.dummy, s.dummyErr = s.hasher.Hash("nex-club-dummy-password")
	})
	return s.dummy, s.dummyErr
}

func rawLimitKey(username string) string {
	name := strings.ToLower(strings.TrimSpace(username))
	if name == "" {
		return "invalid"
	}
	if len(name) > 128 {
		name = name[:128]
	}
	return "raw:" + name
}

type issuedSession struct {
	record NewSession
	result LoginResult
}

var (
	errInactive    = errors.New("inactive admin")
	errAdminExists = apperr.New("already_exists", "管理员已存在，拒绝覆盖", 409)
)
