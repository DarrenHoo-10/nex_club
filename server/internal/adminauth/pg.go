package adminauth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/darrenhoo/nex_club/server/internal/adminauth/sqlc"
	"github.com/darrenhoo/nex_club/server/internal/store"
)

type PGRepo struct {
	Store *store.Store
}

func NewPGRepo(st *store.Store) *PGRepo {
	return &PGRepo{Store: st}
}

func (r *PGRepo) WithTx(ctx context.Context, fn func(ctx context.Context, tx RepoTx) error) error {
	return r.Store.Within(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return fn(ctx, pgTx{tx: tx, q: sqlc.New(tx)})
	})
}

type pgTx struct {
	tx pgx.Tx
	q  *sqlc.Queries
}

func (t pgTx) FindAdmin(ctx context.Context, username string) (AdminRecord, bool, error) {
	row, err := t.q.FindAdminByUsername(ctx, username)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminRecord{}, false, nil
	}
	if err != nil {
		return AdminRecord{}, false, err
	}
	return AdminRecord{ID: row.ID, Username: row.Username, PasswordHash: row.PasswordHash, Status: row.Status}, true, nil
}

func (t pgTx) InsertAdmin(ctx context.Context, id uuid.UUID, username, passwordHash string) error {
	return t.q.InsertAdmin(ctx, sqlc.InsertAdminParams{ID: id, Username: username, PasswordHash: passwordHash})
}

func (t pgTx) UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string, now time.Time) error {
	return t.q.UpdateAdminPassword(ctx, sqlc.UpdateAdminPasswordParams{
		ID: id, PasswordHash: passwordHash, UpdatedAt: now.UTC(),
	})
}

func (t pgTx) TouchLogin(ctx context.Context, id uuid.UUID, now time.Time) error {
	return t.q.TouchAdminLogin(ctx, sqlc.TouchAdminLoginParams{ID: id, LastLoginAt: pgTime(now)})
}

func (t pgTx) AnyAdmin(ctx context.Context) (bool, error) {
	return t.q.AnyAdmin(ctx)
}

func (t pgTx) LockAdminInit(ctx context.Context) error {
	_, err := t.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('nex.admin.init'), 0)`)
	return err
}

func (t pgTx) InsertSession(ctx context.Context, session NewSession) error {
	return t.q.InsertSession(ctx, sqlc.InsertSessionParams{
		ID:             session.ID,
		AdminID:        session.AdminID,
		TokenHash:      session.TokenHash,
		CsrfSecretHash: session.CSRFHash,
		ExpiresAt:      session.ExpiresAt.UTC(),
		LastSeenAt:     session.LastSeenAt.UTC(),
	})
}

func (t pgTx) FindSession(ctx context.Context, tokenHash string) (SessionRecord, bool, error) {
	row, err := t.q.FindSessionByTokenHash(ctx, tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionRecord{}, false, nil
	}
	if err != nil {
		return SessionRecord{}, false, err
	}
	return SessionRecord{
		ID:        row.ID,
		AdminID:   row.AdminID,
		CSRFHash:  row.CsrfSecretHash,
		ExpiresAt: row.ExpiresAt.UTC(),
		RevokedAt: timePtr(row.RevokedAt),
		Username:  row.Username,
		Status:    row.Status,
	}, true, nil
}

func (t pgTx) TouchSession(ctx context.Context, id uuid.UUID, now time.Time) error {
	return t.q.TouchSession(ctx, sqlc.TouchSessionParams{ID: id, LastSeenAt: now.UTC()})
}

func (t pgTx) RevokeSession(ctx context.Context, id uuid.UUID, now time.Time) error {
	return t.q.RevokeSession(ctx, sqlc.RevokeSessionParams{ID: id, RevokedAt: pgTime(now)})
}

func (t pgTx) RevokeAllSessions(ctx context.Context, adminID uuid.UUID, now time.Time) error {
	return t.q.RevokeAdminSessions(ctx, sqlc.RevokeAdminSessionsParams{AdminID: adminID, RevokedAt: pgTime(now)})
}

func pgTime(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t.UTC(), Valid: true}
}

func timePtr(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time.UTC()
	return &t
}
