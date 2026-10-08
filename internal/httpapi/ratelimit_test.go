package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/brusapa/brinketask/internal/clock"
	"github.com/brusapa/brinketask/internal/session"
)

// twoSessions resolves two cookie values to two sessions of one user.
type twoSessions struct{}

func (twoSessions) Resolve(_ context.Context, value string) (session.Session, error) {
	if value != "a" && value != "b" {
		return session.Session{}, session.ErrNoSession
	}
	return session.Session{UserID: uuid.MustParse("0192f1a0-0000-7000-8000-00000000000a"), Key: value}, nil
}

func TestRateLimitPerSession(t *testing.T) {
	clk := clock.NewFixed(time.Date(2026, time.October, 8, 9, 0, 0, 0, time.UTC))
	limiter := NewRateLimiter(clk, RequestsPerSecond, RequestBurst)
	mux := http.NewServeMux()
	err := Register(mux, whoAmIServer{}, discardLogger, Authenticate(twoSessions{}, discardLogger), RateLimit(limiter))
	if err != nil {
		t.Fatal(err)
	}
	get := func(cookie string) *http.Request {
		req := newRequest(t, http.MethodGet, "/api/v1/me", "", "")
		addCookie(req, session.CookieName, cookie)
		return req
	}

	// D-29: a burst of 60 passes, the 61st is refused.
	for i := range RequestBurst {
		rec := serve(t, mux, get("a"))
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d of the burst refused", i+1)
		}
	}
	rec := serve(t, mux, get("a"))
	decodeProblem(t, rec, http.StatusTooManyRequests, ProblemCodeRateLimited)
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 without Retry-After")
	}

	// Another session of the same user has its own bucket.
	if rec := serve(t, mux, get("b")); rec.Code == http.StatusTooManyRequests {
		t.Error("second session limited by the first one's traffic")
	}

	// Tokens come back at 20 per second: one every 50 ms.
	clk.Advance(50 * time.Millisecond)
	if rec := serve(t, mux, get("a")); rec.Code == http.StatusTooManyRequests {
		t.Error("refused after a token was refilled")
	}
	if rec := serve(t, mux, get("a")); rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d after using the refilled token, want 429", rec.Code)
	}
}

func TestRateLimiterForgetsIdleSessions(t *testing.T) {
	clk := clock.NewFixed(time.Date(2026, time.October, 8, 9, 0, 0, 0, time.UTC))
	limiter := NewRateLimiter(clk, RequestsPerSecond, RequestBurst)
	limiter.allow("old")
	clk.Advance(idleLimiter)
	limiter.allow("new")
	if n := limiter.size(); n != 1 {
		t.Errorf("%d buckets after the idle one expired, want 1", n)
	}
}
