package ingest

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/darrenhoo/nex_club/server/internal/ingest/sqlc"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/sources/push"
)

// CreateToken stores only the SHA-256 hex of the bearer string and returns the token once.
func (s *Service) CreateToken(ctx context.Context, sourceKey string, adminID uuid.UUID) (string, error) {
	if s == nil || s.pool == nil {
		return "", apperr.Internal("采集服务未装配")
	}
	sourceKey = strings.TrimSpace(sourceKey)
	if sourceKey == "" {
		return "", apperr.Invalid("缺少信源")
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := "nex_" + base64.RawURLEncoding.EncodeToString(buf)
	var created string
	err := s.within(ctx, func(ctx context.Context, tx pgx.Tx) error {
		q := sqlc.New(tx)
		if _, err := q.AdminExists(ctx, adminID); errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound("管理员不存在")
		} else if err != nil {
			return err
		}
		src, err := q.GetSourceByKey(ctx, sourceKey)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound("信源不存在")
		}
		if err != nil {
			return err
		}
		if _, err := q.InsertCredential(ctx, sqlc.InsertCredentialParams{
			ID:        uuid.New(),
			SourceID:  src.ID,
			Name:      "default",
			TokenHash: push.TokenHash(token),
			Scopes:    []string{"items:write"},
			CreatedBy: adminID,
		}); err != nil {
			return err
		}
		created = token
		return nil
	})
	if err != nil {
		return "", err
	}
	return created, nil
}
