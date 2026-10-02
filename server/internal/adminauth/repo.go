package adminauth

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type AdminRecord struct {
	ID           uuid.UUID
	Username     string
	PasswordHash string
	Status       string
}

type SessionRecord struct {
	ID        uuid.UUID
	AdminID   uuid.UUID
	CSRFHash  string
	ExpiresAt time.Time
	RevokedAt *time.Time
	Username  string
	Status    string
}

type NewSession struct {
	ID         uuid.UUID
	AdminID    uuid.UUID
	TokenHash  string
	CSRFHash   string
	ExpiresAt  time.Time
	LastSeenAt time.Time
}

// RepoTx methods run inside Repo.WithTx. Implementations must not commit.
type RepoTx interface {
	FindAdmin(ctx context.Context, username string) (AdminRecord, bool, error)
	InsertAdmin(ctx context.Context, id uuid.UUID, username, passwordHash string) error
	UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string, now time.Time) error
	TouchLogin(ctx context.Context, id uuid.UUID, now time.Time) error
	AnyAdmin(ctx context.Context) (bool, error)
	LockAdminInit(ctx context.Context) error
	InsertSession(ctx context.Context, session NewSession) error
	FindSession(ctx context.Context, tokenHash string) (SessionRecord, bool, error)
	TouchSession(ctx context.Context, id uuid.UUID, now time.Time) error
	RevokeSession(ctx context.Context, id uuid.UUID, now time.Time) error
	RevokeAllSessions(ctx context.Context, adminID uuid.UUID, now time.Time) error
}

type Repo interface {
	WithTx(ctx context.Context, fn func(ctx context.Context, tx RepoTx) error) error
}
