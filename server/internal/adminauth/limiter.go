package adminauth

import (
	"sync"
	"time"
)

const (
	loginWindow    = 15 * time.Minute
	loginUserLimit = 5
	loginIPLimit   = 30
)

// LoginLimiter is a process-local fixed window. A successful login clears the username bucket.
type LoginLimiter interface {
	TooMany(username, ip string, now time.Time) (retryAfter int, blocked bool)
	Fail(username, ip string, now time.Time)
	Success(username string)
}

type bucket struct {
	start time.Time
	fails int
}

type MemoryLimiter struct {
	mu    sync.Mutex
	users map[string]*bucket
	ips   map[string]*bucket
}

func NewMemoryLimiter() *MemoryLimiter {
	return &MemoryLimiter{users: map[string]*bucket{}, ips: map[string]*bucket{}}
}

func (m *MemoryLimiter) TooMany(username, ip string, now time.Time) (int, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	userRetry, userBlocked := blocked(m.users[username], loginUserLimit, now)
	ipRetry, ipBlocked := blocked(m.ips[ip], loginIPLimit, now)
	if !userBlocked && !ipBlocked {
		return 0, false
	}
	if userRetry > ipRetry {
		return userRetry, true
	}
	return ipRetry, true
}

func (m *MemoryLimiter) Fail(username, ip string, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[username] = fail(m.users[username], now)
	m.ips[ip] = fail(m.ips[ip], now)
}

func (m *MemoryLimiter) Success(username string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.users, username)
}

func fail(b *bucket, now time.Time) *bucket {
	if b == nil || now.Sub(b.start) >= loginWindow {
		return &bucket{start: now, fails: 1}
	}
	b.fails++
	return b
}

func blocked(b *bucket, limit int, now time.Time) (int, bool) {
	if b == nil || now.Sub(b.start) >= loginWindow || b.fails < limit {
		return 0, false
	}
	retry := int((loginWindow - now.Sub(b.start) + time.Second - 1) / time.Second)
	if retry < 1 {
		retry = 1
	}
	return retry, true
}
