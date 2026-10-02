package adminauth

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MemRepo is an in-memory Repo for tests. WithTx holds the mutex for the callback.
type MemRepo struct {
	mu       sync.Mutex
	admins   map[uuid.UUID]AdminRecord
	byName   map[string]uuid.UUID
	sessions map[uuid.UUID]memSession
	byToken  map[string]uuid.UUID
}

type memSession struct {
	NewSession
	RevokedAt *time.Time
}

func NewMemRepo() *MemRepo {
	return &MemRepo{
		admins:   map[uuid.UUID]AdminRecord{},
		byName:   map[string]uuid.UUID{},
		sessions: map[uuid.UUID]memSession{},
		byToken:  map[string]uuid.UUID{},
	}
}

func (m *MemRepo) WithTx(ctx context.Context, fn func(ctx context.Context, tx RepoTx) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fn(ctx, m)
}

func (m *MemRepo) FindAdmin(_ context.Context, username string) (AdminRecord, bool, error) {
	id, ok := m.byName[username]
	if !ok {
		return AdminRecord{}, false, nil
	}
	return m.admins[id], true, nil
}

func (m *MemRepo) InsertAdmin(_ context.Context, id uuid.UUID, username, passwordHash string) error {
	m.admins[id] = AdminRecord{ID: id, Username: username, PasswordHash: passwordHash, Status: "active"}
	m.byName[username] = id
	return nil
}

func (m *MemRepo) UpdatePassword(_ context.Context, id uuid.UUID, passwordHash string, _ time.Time) error {
	admin := m.admins[id]
	admin.PasswordHash = passwordHash
	m.admins[id] = admin
	return nil
}

func (m *MemRepo) TouchLogin(context.Context, uuid.UUID, time.Time) error { return nil }

func (m *MemRepo) AnyAdmin(context.Context) (bool, error) { return len(m.admins) > 0, nil }

func (m *MemRepo) LockAdminInit(context.Context) error { return nil }

func (m *MemRepo) InsertSession(_ context.Context, session NewSession) error {
	m.sessions[session.ID] = memSession{NewSession: session}
	m.byToken[session.TokenHash] = session.ID
	return nil
}

func (m *MemRepo) FindSession(_ context.Context, tokenHash string) (SessionRecord, bool, error) {
	id, ok := m.byToken[tokenHash]
	if !ok {
		return SessionRecord{}, false, nil
	}
	session := m.sessions[id]
	admin := m.admins[session.AdminID]
	var revoked *time.Time
	if session.RevokedAt != nil {
		t := session.RevokedAt.UTC()
		revoked = &t
	}
	return SessionRecord{
		ID:        session.ID,
		AdminID:   session.AdminID,
		CSRFHash:  session.CSRFHash,
		ExpiresAt: session.ExpiresAt.UTC(),
		RevokedAt: revoked,
		Username:  admin.Username,
		Status:    admin.Status,
	}, true, nil
}

func (m *MemRepo) TouchSession(_ context.Context, id uuid.UUID, now time.Time) error {
	session := m.sessions[id]
	session.LastSeenAt = now.UTC()
	m.sessions[id] = session
	return nil
}

func (m *MemRepo) RevokeSession(_ context.Context, id uuid.UUID, now time.Time) error {
	session, ok := m.sessions[id]
	if !ok || session.RevokedAt != nil {
		return nil
	}
	t := now.UTC()
	session.RevokedAt = &t
	m.sessions[id] = session
	return nil
}

func (m *MemRepo) RevokeAllSessions(_ context.Context, adminID uuid.UUID, now time.Time) error {
	t := now.UTC()
	for id, session := range m.sessions {
		if session.AdminID == adminID && session.RevokedAt == nil {
			session.RevokedAt = &t
			m.sessions[id] = session
		}
	}
	return nil
}

func (m *MemRepo) SetStatus(id uuid.UUID, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	admin := m.admins[id]
	admin.Status = status
	m.admins[id] = admin
}

func (m *MemRepo) SetCSRFHash(id uuid.UUID, hash string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session := m.sessions[id]
	session.CSRFHash = hash
	m.sessions[id] = session
}

func (m *MemRepo) SetCSRFHashByAdmin(adminID uuid.UUID, hash string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, session := range m.sessions {
		if session.AdminID == adminID {
			session.CSRFHash = hash
			m.sessions[id] = session
		}
	}
}
