package metrics

import (
	"sync"
	"time"
)

// Limiter counts accepted events per visitor over a rolling minute. It is in-memory and single-process.
type Limiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
}

type visitor struct {
	mu     sync.Mutex
	stamps []time.Time
}

func NewLimiter() *Limiter {
	return &Limiter{visitors: map[string]*visitor{}}
}

func (l *Limiter) lock(token string) *visitor {
	if l == nil {
		l = NewLimiter()
	}
	l.mu.Lock()
	v := l.visitors[token]
	if v == nil {
		v = &visitor{}
		l.visitors[token] = v
	}
	l.mu.Unlock()
	v.mu.Lock()
	return v
}

func (v *visitor) unlock() { v.mu.Unlock() }

func (v *visitor) prune(now time.Time) {
	cutoff := now.Add(-time.Minute)
	kept := v.stamps[:0]
	for _, stamp := range v.stamps {
		if stamp.After(cutoff) {
			kept = append(kept, stamp)
		}
	}
	v.stamps = kept
}

func (v *visitor) allow(now time.Time, limit, adding int) bool {
	v.prune(now)
	return len(v.stamps)+adding <= limit
}

func (v *visitor) add(now time.Time, n int) {
	for i := 0; i < n; i++ {
		v.stamps = append(v.stamps, now)
	}
}
