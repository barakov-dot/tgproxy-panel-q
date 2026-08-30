package auth

import (
	"sync"
	"time"
)

// Default LoginLimiter parameters (plan.md §5: "Базовый rate-limit на
// попытки логина"). No specific numbers are mandated, so these are picked
// to absorb typos by the legitimate admin without giving a brute-forcer a
// meaningful number of guesses: 5 failures opens a 15-minute lockout, and
// the failure count itself only accumulates within a 5-minute window (a
// slow trickle of failed attempts, one every few minutes, never locks
// anyone out).
const (
	DefaultMaxAttempts = 5
	DefaultWindow      = 5 * time.Minute
	DefaultCooldown    = 15 * time.Minute
)

// LoginLimiter is a small in-memory brute-force guard, keyed by whatever the
// caller considers the source of a login attempt. It does not itself decide
// what that key is: a future internal/httpserver login handler is expected
// to key it by r.RemoteAddr. This package deliberately does not resolve
// X-Forwarded-For — chi's middleware.RealIP (or equivalent trusted-proxy
// handling) is the natural, single place to decide which header to trust,
// and duplicating that decision here would risk the two disagreeing.
//
// State is an unbounded map keyed by that caller-chosen string; at the
// ~50-user, single-admin scale plan.md targets this is fine without a
// bespoke eviction scheme.
type LoginLimiter struct {
	mu       sync.Mutex
	attempts map[string]*attemptState

	maxAttempts int
	window      time.Duration
	cooldown    time.Duration

	// now defaults to time.Now; tests override it to exercise window/cooldown
	// expiry without sleeping for real.
	now func() time.Time
}

type attemptState struct {
	count       int
	windowStart time.Time
	lockedUntil time.Time
}

// NewLoginLimiter builds a LoginLimiter that locks a key out for cooldown
// once it accumulates maxAttempts failures within window.
func NewLoginLimiter(maxAttempts int, window, cooldown time.Duration) *LoginLimiter {
	return &LoginLimiter{
		attempts:    make(map[string]*attemptState),
		maxAttempts: maxAttempts,
		window:      window,
		cooldown:    cooldown,
		now:         time.Now,
	}
}

// NewDefaultLoginLimiter builds a LoginLimiter using DefaultMaxAttempts,
// DefaultWindow, and DefaultCooldown.
func NewDefaultLoginLimiter() *LoginLimiter {
	return NewLoginLimiter(DefaultMaxAttempts, DefaultWindow, DefaultCooldown)
}

// Allow reports whether key is currently permitted to attempt a login. A
// caller should check this before running the (comparatively expensive)
// bcrypt check, then report the outcome via RecordFailure or RecordSuccess.
func (l *LoginLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	st, ok := l.attempts[key]
	if !ok {
		return true
	}
	return !l.now().Before(st.lockedUntil)
}

// RecordFailure records a failed attempt for key, locking it out for
// cooldown once maxAttempts failures have landed inside the current window.
func (l *LoginLimiter) RecordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	st, ok := l.attempts[key]
	if !ok || now.Sub(st.windowStart) > l.window {
		st = &attemptState{windowStart: now}
		l.attempts[key] = st
	}

	st.count++
	if st.count >= l.maxAttempts {
		st.lockedUntil = now.Add(l.cooldown)
	}
}

// RecordSuccess clears any failure history for key, so a correct login
// after a few typos doesn't leave a stale count lingering toward a future
// lockout.
func (l *LoginLimiter) RecordSuccess(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}
