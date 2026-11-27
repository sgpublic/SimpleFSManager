package auth

import (
	"sync"
	"time"
)

// RateLimiter tracks failed or repeated authentication attempts per client key
// using a sliding time window.
type RateLimiter struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu       sync.Mutex
	attempts map[string][]time.Time
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		limit:    limit,
		window:   window,
		now:      time.Now,
		attempts: make(map[string][]time.Time),
	}
}

// Allow records an attempt for key and reports whether the request is within
// the configured rate limit.
func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	cutoff := now.Add(-l.window)
	l.pruneLocked(cutoff)

	recent := l.attempts[key]
	if len(recent) >= l.limit {
		return false
	}
	l.attempts[key] = append(recent, now)
	return true
}

// Reset clears recorded attempts for key (for example, after a successful login).
func (l *RateLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

func (l *RateLimiter) pruneLocked(cutoff time.Time) {
	for key, timestamps := range l.attempts {
		kept := timestamps[:0]
		for _, ts := range timestamps {
			if ts.After(cutoff) {
				kept = append(kept, ts)
			}
		}
		if len(kept) == 0 {
			delete(l.attempts, key)
		} else {
			l.attempts[key] = kept
		}
	}
}
