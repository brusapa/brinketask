package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/brusapa/brinketask/internal/account"
	"github.com/brusapa/brinketask/internal/clock"
	"github.com/brusapa/brinketask/internal/session"
	"github.com/brusapa/brinketask/internal/storage/storagetest"
	"github.com/brusapa/brinketask/internal/tasks"
)

const testOrigin = "https://tasks.example.com"

var testStart = time.Date(2026, time.October, 8, 9, 0, 0, 0, time.UTC)

// testApp is the API wired as in main, against a real database (SPEC
// section 11: integration against PostgreSQL), with a fixed clock.
type testApp struct {
	pool     *pgxpool.Pool
	clock    *clock.Fixed
	accounts *account.Service
	tasks    *tasks.Service
	sessions *session.Manager
	mux      *http.ServeMux
}

func newTestApp(t *testing.T) *testApp {
	t.Helper()
	pool := storagetest.NewPool(t)
	clk := clock.NewFixed(testStart)
	accounts := account.NewService(pool, clk)
	taskService := tasks.NewService(pool, clk)
	// Changing the profile zone or default time recomputes reminders (SPEC
	// section 6), in the transaction of the change.
	accounts.OnSettingsChanged(taskService.RecomputeUserReminders)
	// Sessions outlive the 30-day restore window that some tests step over;
	// expiry has its own tests in package session.
	sessions := session.NewManager(pool, clk, 365*24*time.Hour, 365*24*time.Hour)
	sameOrigin, err := SameOrigin(testOrigin)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	err = Register(mux, NewServer(accounts, taskService), discardLogger,
		sameOrigin,
		Authenticate(sessions, discardLogger),
		// High enough that no test hits it; the limit has its own test.
		RateLimit(NewRateLimiter(clk, 1e6, 1e6)),
	)
	if err != nil {
		t.Fatal(err)
	}
	return &testApp{pool: pool, clock: clk, accounts: accounts, tasks: taskService, sessions: sessions, mux: mux}
}

// user is a signed-up user with a session.
type user struct {
	id      uuid.UUID
	cookie  string
	inboxID uuid.UUID
}

// signUp creates a user as the OIDC callback would, with a session.
func (a *testApp) signUp(t *testing.T, subject string) user {
	t.Helper()
	ctx := context.Background()
	email := subject + "@example.com"
	userID, err := a.accounts.SignIn(ctx, account.Identity{
		Issuer: "https://id.example.com", Subject: subject, Email: &email,
	})
	if err != nil {
		t.Fatal(err)
	}
	value, _, err := a.sessions.Create(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := a.accounts.Profile(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	return user{id: userID, cookie: value, inboxID: profile.InboxListID}
}

// call sends a same-origin request as u and checks the response against the
// contract. body is JSON or ""; PATCH bodies are sent as merge patches.
func (a *testApp) call(t *testing.T, u user, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	contentType := ""
	if body != "" {
		contentType = jsonType
		if method == http.MethodPatch {
			contentType = mergePatchType
		}
	}
	req := newRequest(t, method, testOrigin+"/api/v1"+path, contentType, body)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	addCookie(req, session.CookieName, u.cookie)
	return serve(t, a.mux, req)
}

// decode checks the status and decodes a JSON response into T. Go infers T
// from the variable the result is assigned to only when written as
// decode[T](...); the type is spelled at each call.
func decode[T any](t *testing.T, rec *httptest.ResponseRecorder, wantStatus int) T {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body %s", rec.Code, wantStatus, rec.Body)
	}
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %T: %v; body %s", v, err, rec.Body)
	}
	return v
}

// wantProblem checks a problem response.
func wantProblem(t *testing.T, rec *httptest.ResponseRecorder, status int, code ProblemCode) Problem {
	t.Helper()
	return decodeProblem(t, rec, status, code)
}

// newID returns a fresh client-side id, as clients generate them (D-03).
func newID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// advance moves the clock and gives u a fresh session, since sessions
// expire after 7 idle days (tests that jump weeks would get 401).
func (a *testApp) advance(t *testing.T, d time.Duration, u *user) {
	t.Helper()
	a.clock.Advance(d)
	value, _, err := a.sessions.Create(context.Background(), u.id)
	if err != nil {
		t.Fatal(err)
	}
	u.cookie = value
}
