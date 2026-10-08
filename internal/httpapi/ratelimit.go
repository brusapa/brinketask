package httpapi

import (
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/brusapa/brinketask/internal/clock"
)

// Per-session request limits (D-29): a steady 20 requests per second, with
// bursts of up to 60, which a page load or a sync after a long time away
// can need.
const (
	RequestsPerSecond = 20
	RequestBurst      = 60
)

// Limiter state of a session that has made no request for idleLimiter is
// dropped; a new one starts with a full burst, which is what it would have
// refilled to anyway.
const idleLimiter = 10 * time.Minute

// RateLimiter keeps a token bucket per session, in process memory. With a
// single server instance this is enough; it is not shared between
// replicas.
type RateLimiter struct {
	clock clock.Clock
	limit rate.Limit
	burst int

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

type bucket struct {
	limiter  *rate.Limiter
	lastUsed time.Time
}

// NewRateLimiter allows perSecond requests per second per session, with
// bursts of burst.
func NewRateLimiter(clk clock.Clock, perSecond float64, burst int) *RateLimiter {
	return &RateLimiter{
		clock:     clk,
		limit:     rate.Limit(perSecond),
		burst:     burst,
		buckets:   map[string]*bucket{},
		lastSweep: clk.Now(),
	}
}

// allow takes one token from key's bucket, reporting whether there was one.
func (l *RateLimiter) allow(key string) bool {
	now := l.clock.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	// Drop idle buckets now and then, so memory stays bounded by the
	// number of recently active sessions.
	if now.Sub(l.lastSweep) >= idleLimiter {
		for k, b := range l.buckets {
			if now.Sub(b.lastUsed) >= idleLimiter {
				delete(l.buckets, k)
			}
		}
		l.lastSweep = now
	}

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{limiter: rate.NewLimiter(l.limit, l.burst)}
		l.buckets[key] = b
	}
	b.lastUsed = now
	// AllowN with an explicit time, rather than Allow, keeps the limiter on
	// the injected clock.
	return b.limiter.AllowN(now, 1)
}

// size reports how many sessions have a bucket (for tests).
func (l *RateLimiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// RateLimit answers 429 rate_limited when the caller's session exceeds its
// limit. It must run after Authenticate, which identifies the session.
func RateLimit(l *RateLimiter) MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s, ok := sessionFrom(r.Context())
			if ok && !l.allow(s.Key) {
				// One token comes back every 1/20 s; a second is a
				// conservative hint.
				w.Header().Set("Retry-After", "1")
				writeProblem(w, newProblem(http.StatusTooManyRequests, ProblemCodeRateLimited, ""))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
