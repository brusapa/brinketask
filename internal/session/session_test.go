package session_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/brusapa/brinketask/internal/account"
	"github.com/brusapa/brinketask/internal/clock"
	"github.com/brusapa/brinketask/internal/session"
	"github.com/brusapa/brinketask/internal/storage/storagetest"
)

const (
	idle   = 7 * 24 * time.Hour  // D-29 defaults
	maxAge = 30 * 24 * time.Hour //
	day    = 24 * time.Hour
)

var start = time.Date(2026, time.October, 8, 9, 0, 0, 0, time.UTC)

type fixture struct {
	pool    *pgxpool.Pool
	clock   *clock.Fixed
	manager *session.Manager
	userID  uuid.UUID
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	pool := storagetest.NewPool(t)
	clk := clock.NewFixed(start)
	userID, err := account.NewService(pool, clk).SignIn(context.Background(), account.Identity{
		Issuer: "https://id.example.com", Subject: "sub-alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	return fixture{pool: pool, clock: clk, manager: session.NewManager(pool, clk, idle, maxAge), userID: userID}
}

func (f fixture) create(t *testing.T) string {
	t.Helper()
	value, expires, err := f.manager.Create(context.Background(), f.userID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if want := f.clock.Now().Add(maxAge); !expires.Equal(want) {
		t.Errorf("cookie expiry = %v, want the absolute limit %v", expires, want)
	}
	return value
}

func (f fixture) valid(t *testing.T, value string) bool {
	t.Helper()
	s, err := f.manager.Resolve(context.Background(), value)
	if errors.Is(err, session.ErrNoSession) {
		return false
	}
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if s.UserID != f.userID {
		t.Fatalf("session resolved to user %v, want %v", s.UserID, f.userID)
	}
	return true
}

func (f fixture) sessions(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), "SELECT count(*) FROM sessions").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// SPEC section 7: only the hash of the identifier is stored.
func TestOnlyTheHashIsStored(t *testing.T) {
	f := newFixture(t)
	value := f.create(t)

	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		t.Fatalf("cookie value is not base64url: %v", err)
	}
	want := sha256.Sum256(raw)

	var stored []byte
	var row string
	err = f.pool.QueryRow(context.Background(), "SELECT id_hash, s::text FROM sessions s").Scan(&stored, &row)
	if err != nil {
		t.Fatal(err)
	}
	if string(stored) != string(want[:]) {
		t.Errorf("stored %x, want sha256 of the identifier %x", stored, want)
	}
	if strings.Contains(row, value) {
		t.Errorf("the row contains the identifier in clear: %s", row)
	}
}

func TestUnknownAndMalformedIdentifiers(t *testing.T) {
	f := newFixture(t)
	f.create(t)
	for _, value := range []string{"", "garbage", strings.Repeat("A", 43)} {
		if f.valid(t, value) {
			t.Errorf("identifier %q resolved", value)
		}
	}
}

// Idle limit: 7 days without use end the session, and the row is removed.
func TestIdleExpiry(t *testing.T) {
	f := newFixture(t)
	value := f.create(t)

	f.clock.Advance(idle - time.Second)
	if !f.valid(t, value) {
		t.Fatal("session expired before the idle limit")
	}
	// That use slid the expiry: another almost-7 days are fine.
	f.clock.Advance(idle - time.Second)
	if !f.valid(t, value) {
		t.Fatal("use did not slide the expiry")
	}
	f.clock.Advance(idle)
	if f.valid(t, value) {
		t.Fatal("session valid after 7 days without use")
	}
	if n := f.sessions(t); n != 0 {
		t.Errorf("%d session rows after expiry, want 0", n)
	}
}

// Absolute limit: 30 days end the session even if it is used every day.
func TestAbsoluteExpiry(t *testing.T) {
	f := newFixture(t)
	value := f.create(t)

	for range 29 {
		f.clock.Advance(day)
		if !f.valid(t, value) {
			t.Fatalf("session expired on day %v while in daily use", f.clock.Now().Sub(start)/day)
		}
	}
	f.clock.Advance(day - time.Second)
	if !f.valid(t, value) {
		t.Fatal("session expired before 30 days")
	}
	f.clock.Advance(time.Second)
	if f.valid(t, value) {
		t.Fatal("session valid at 30 days")
	}
}

// Use is written at most once a minute, not on every request.
func TestUseIsRecordedAtMostOncePerMinute(t *testing.T) {
	f := newFixture(t)
	value := f.create(t)
	lastSeen := func() time.Time {
		var at time.Time
		if err := f.pool.QueryRow(context.Background(), "SELECT last_seen_at FROM sessions").Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at
	}

	f.clock.Advance(30 * time.Second)
	f.valid(t, value)
	if got := lastSeen(); !got.Equal(start) {
		t.Errorf("last_seen_at = %v after 30 s, want unchanged %v", got, start)
	}
	f.clock.Advance(30 * time.Second)
	f.valid(t, value)
	if got, want := lastSeen(), start.Add(time.Minute); !got.Equal(want) {
		t.Errorf("last_seen_at = %v after 1 min, want %v", got, want)
	}
}

func TestDeleteIsIdempotent(t *testing.T) {
	f := newFixture(t)
	value := f.create(t)
	other := f.create(t)

	for range 2 {
		if err := f.manager.Delete(context.Background(), value); err != nil {
			t.Fatalf("Delete: %v", err)
		}
	}
	if f.valid(t, value) {
		t.Error("deleted session still valid")
	}
	if !f.valid(t, other) {
		t.Error("deleting one session ended another")
	}
	if err := f.manager.Delete(context.Background(), "garbage"); err != nil {
		t.Errorf("Delete(garbage) = %v, want nil", err)
	}
}

func TestCreateRemovesTheUsersExpiredSessions(t *testing.T) {
	f := newFixture(t)
	f.create(t)
	f.create(t)
	f.clock.Advance(idle)
	f.create(t)
	if n := f.sessions(t); n != 1 {
		t.Errorf("%d session rows, want only the new one", n)
	}
}

func TestCookieAttributes(t *testing.T) {
	rec := httptest.NewRecorder()
	expires := start.Add(maxAge)
	session.SetCookie(rec, "value", expires)
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("%d cookies, want 1", len(cookies))
	}
	c := cookies[0]
	if c.Name != "brinketask_session" || c.Value != "value" || c.Path != "/" {
		t.Errorf("cookie = %s=%s path %s", c.Name, c.Value, c.Path)
	}
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie flags: HttpOnly=%v Secure=%v SameSite=%v; want HttpOnly, Secure, Lax", c.HttpOnly, c.Secure, c.SameSite)
	}
	if !c.Expires.Equal(expires) {
		t.Errorf("Expires = %v, want %v", c.Expires, expires)
	}

	rec = httptest.NewRecorder()
	session.ClearCookie(rec)
	header := rec.Header().Get("Set-Cookie")
	if !strings.Contains(header, "brinketask_session=;") || !strings.Contains(header, "Max-Age=0") {
		t.Errorf("clear cookie header = %q", header)
	}
}
