// Package session manages the browser sessions of SPEC section 7: an opaque
// identifier in an HttpOnly cookie, stored only as a hash, with a sliding
// expiry (SESSION_IDLE_TIMEOUT) and an absolute one (SESSION_MAX_AGE).
// Expiry is computed from the injected clock (D-17).
package session

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/brusapa/brinketask/internal/clock"
	"github.com/brusapa/brinketask/internal/opaque"
	"github.com/brusapa/brinketask/internal/storage/dbgen"
)

// CookieName is the session cookie, as declared by cookieAuth in the
// contract.
const CookieName = "brinketask_session"

// touchInterval limits how often a session's last use is written: at most
// once per minute per session, instead of on every request. The sliding
// expiry is therefore up to a minute short, which does not matter against
// days.
const touchInterval = time.Minute

// ErrNoSession means the identifier is unknown, malformed or expired. The
// caller answers 401 in every case, so they are not told apart.
var ErrNoSession = errors.New("session: no valid session")

// Session is a resolved, valid session.
type Session struct {
	UserID uuid.UUID
	// Key identifies the session without revealing its identifier (it is
	// the stored hash). Per-session limits use it.
	Key string
}

// Manager creates, resolves and deletes sessions.
type Manager struct {
	pool        *pgxpool.Pool
	clock       clock.Clock
	idleTimeout time.Duration
	maxAge      time.Duration
}

// NewManager returns a Manager. Sessions end after idleTimeout without use,
// and after maxAge in any case.
func NewManager(pool *pgxpool.Pool, clk clock.Clock, idleTimeout, maxAge time.Duration) *Manager {
	return &Manager{pool: pool, clock: clk, idleTimeout: idleTimeout, maxAge: maxAge}
}

// Create starts a session for userID and returns the identifier for the
// cookie and the moment the cookie should expire (the absolute limit). It
// also removes the user's sessions that have already expired.
func (m *Manager) Create(ctx context.Context, userID uuid.UUID) (value string, cookieExpires time.Time, err error) {
	now := m.clock.Now()
	value, hash := opaque.New()
	q := dbgen.New(m.pool)

	if err := q.DeleteExpiredSessions(ctx, dbgen.DeleteExpiredSessionsParams{UserID: userID, Now: now}); err != nil {
		return "", time.Time{}, fmt.Errorf("session: delete expired: %w", err)
	}
	err = q.InsertSession(ctx, dbgen.InsertSessionParams{
		IDHash:    hash,
		UserID:    userID,
		Now:       now,
		ExpiresAt: m.expiry(now, now),
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("session: insert: %w", err)
	}
	return value, now.Add(m.maxAge), nil
}

// Resolve returns the session for a cookie value and records its use,
// which slides its expiry. It returns ErrNoSession when the value does not
// name a valid session; an expired session is deleted on the way.
func (m *Manager) Resolve(ctx context.Context, value string) (Session, error) {
	hash, ok := opaque.Hash(value)
	if !ok {
		return Session{}, ErrNoSession
	}
	now := m.clock.Now()
	q := dbgen.New(m.pool)

	row, err := q.GetSession(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNoSession
	}
	if err != nil {
		return Session{}, fmt.Errorf("session: get: %w", err)
	}

	if !now.Before(row.ExpiresAt) {
		if err := q.DeleteSession(ctx, hash); err != nil {
			return Session{}, fmt.Errorf("session: delete expired: %w", err)
		}
		return Session{}, ErrNoSession
	}

	if now.Sub(row.LastSeenAt) >= touchInterval {
		err := q.TouchSession(ctx, dbgen.TouchSessionParams{
			IDHash:    hash,
			Now:       now,
			ExpiresAt: m.expiry(row.CreatedAt, now),
		})
		if err != nil {
			return Session{}, fmt.Errorf("session: touch: %w", err)
		}
	}
	return Session{UserID: row.UserID, Key: string(hash)}, nil
}

// Delete ends the session named by a cookie value. Deleting an unknown or
// already deleted session is not an error, so logging out twice is
// harmless.
func (m *Manager) Delete(ctx context.Context, value string) error {
	hash, ok := opaque.Hash(value)
	if !ok {
		return nil
	}
	if err := dbgen.New(m.pool).DeleteSession(ctx, hash); err != nil {
		return fmt.Errorf("session: delete: %w", err)
	}
	return nil
}

// expiry is the earlier of the idle limit, counted from the last use, and
// the absolute limit, counted from creation.
func (m *Manager) expiry(createdAt, lastSeen time.Time) time.Time {
	idle := lastSeen.Add(m.idleTimeout)
	absolute := createdAt.Add(m.maxAge)
	if idle.Before(absolute) {
		return idle
	}
	return absolute
}

// SetCookie sends the session cookie. HttpOnly keeps it from scripts,
// Secure from plain http (browsers make an exception for localhost), and
// SameSite=Lax from cross-site subrequests (SPEC section 7). It lives
// until the absolute limit; the idle limit is enforced by the server.
func SetCookie(w http.ResponseWriter, value string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearCookie tells the browser to drop the session cookie.
func ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1, // net/http sends this as "Max-Age=0": delete now
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}
