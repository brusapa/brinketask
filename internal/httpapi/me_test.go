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
)

const testOrigin = "https://tasks.example.com"

// meApp is the API wired as in main, against a real database.
type meApp struct {
	pool     *pgxpool.Pool
	clock    *clock.Fixed
	accounts *account.Service
	sessions *session.Manager
	mux      *http.ServeMux
}

func newMeApp(t *testing.T) *meApp {
	t.Helper()
	pool := storagetest.NewPool(t)
	clk := clock.NewFixed(time.Date(2026, time.October, 8, 9, 0, 0, 0, time.UTC))
	accounts := account.NewService(pool, clk)
	sessions := session.NewManager(pool, clk, 7*24*time.Hour, 30*24*time.Hour)
	sameOrigin, err := SameOrigin(testOrigin)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	err = Register(mux, NewServer(accounts), discardLogger,
		sameOrigin,
		Authenticate(sessions, discardLogger),
		RateLimit(NewRateLimiter(clk, RequestsPerSecond, RequestBurst)),
	)
	if err != nil {
		t.Fatal(err)
	}
	return &meApp{pool: pool, clock: clk, accounts: accounts, sessions: sessions, mux: mux}
}

// signUp creates a user as the OIDC callback would and returns their id
// and a session cookie value.
func (a *meApp) signUp(t *testing.T, subject string) (uuid.UUID, string) {
	t.Helper()
	email := subject + "@example.com"
	userID, err := a.accounts.SignIn(context.Background(), account.Identity{
		Issuer: "https://id.example.com", Subject: subject, Email: &email,
	})
	if err != nil {
		t.Fatal(err)
	}
	value, _, err := a.sessions.Create(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	return userID, value
}

func (a *meApp) request(t *testing.T, method, cookie, body string) *httptest.ResponseRecorder {
	t.Helper()
	contentType := ""
	if body != "" {
		contentType = mergePatchType
	}
	req := newRequest(t, method, testOrigin+"/api/v1/me", contentType, body)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if cookie != "" {
		addCookie(req, session.CookieName, cookie)
	}
	return serve(t, a.mux, req)
}

func decodeUser(t *testing.T, rec *httptest.ResponseRecorder) User {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	var user User
	if err := json.Unmarshal(rec.Body.Bytes(), &user); err != nil {
		t.Fatal(err)
	}
	return user
}

func inboxOf(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(context.Background(), "SELECT id FROM lists WHERE owner_id = $1 AND is_inbox", userID).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestGetMe(t *testing.T) {
	a := newMeApp(t)
	aliceID, alice := a.signUp(t, "alice")
	bobID, bob := a.signUp(t, "bob")

	// Each session sees its own user, never the other one.
	for _, tt := range []struct {
		cookie string
		userID uuid.UUID
		email  string
	}{{alice, aliceID, "alice@example.com"}, {bob, bobID, "bob@example.com"}} {
		user := decodeUser(t, a.request(t, http.MethodGet, tt.cookie, ""))
		if user.Id != tt.userID || user.InboxListId != inboxOf(t, a.pool, tt.userID) {
			t.Errorf("user = %v inbox %v, want %v inbox %v", user.Id, user.InboxListId, tt.userID, inboxOf(t, a.pool, tt.userID))
		}
		if email, err := user.Email.Get(); err != nil || email != tt.email {
			t.Errorf("email = %q (%v), want %q", email, err, tt.email)
		}
		if !user.DisplayName.IsNull() {
			t.Errorf("display_name = %v, want null", user.DisplayName)
		}
		if user.Timezone != "Europe/Madrid" || user.AllDayReminderTime != "09:00" {
			t.Errorf("settings = %q, %q", user.Timezone, user.AllDayReminderTime)
		}
	}

	decodeProblem(t, a.request(t, http.MethodGet, "", ""), http.StatusUnauthorized, ProblemCodeUnauthenticated)
}

func TestPatchMe(t *testing.T) {
	a := newMeApp(t)
	aliceID, alice := a.signUp(t, "alice")
	_, bob := a.signUp(t, "bob")

	user := decodeUser(t, a.request(t, http.MethodPatch, alice, `{"timezone":"Atlantic/Canary"}`))
	if user.Id != aliceID || user.Timezone != "Atlantic/Canary" || user.AllDayReminderTime != "09:00" {
		t.Errorf("after patch: %+v", user)
	}
	user = decodeUser(t, a.request(t, http.MethodPatch, alice, `{"all_day_reminder_time":"08:15"}`))
	if user.Timezone != "Atlantic/Canary" || user.AllDayReminderTime != "08:15" {
		t.Errorf("second patch touched other fields: %+v", user)
	}
	// Repeating a patch is harmless.
	user = decodeUser(t, a.request(t, http.MethodPatch, alice, `{"all_day_reminder_time":"08:15"}`))
	if user.AllDayReminderTime != "08:15" {
		t.Errorf("repeated patch: %+v", user)
	}
	decodeUser(t, a.request(t, http.MethodPatch, alice, `{}`))

	// Alice's patches did not touch Bob.
	other := decodeUser(t, a.request(t, http.MethodGet, bob, ""))
	if other.Timezone != "Europe/Madrid" || other.AllDayReminderTime != "09:00" {
		t.Errorf("bob's settings changed: %+v", other)
	}
}

func TestPatchMeValidation(t *testing.T) {
	a := newMeApp(t)
	_, alice := a.signUp(t, "alice")

	tests := []struct {
		name, body, field string
	}{
		// Passes the schema, fails the rule that the zone exists.
		{"unknown zone", `{"timezone":"Mars/Olympus_Mons"}`, "/timezone"},
		{"server-local pseudo zone", `{"timezone":"Local"}`, "/timezone"},
		{"hour out of range", `{"all_day_reminder_time":"24:00"}`, "/all_day_reminder_time"},
		{"wrong format", `{"all_day_reminder_time":"9:00"}`, "/all_day_reminder_time"},
		{"null", `{"timezone":null}`, "/timezone"},
		{"unknown field", `{"locale":"es"}`, "/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problem := decodeProblem(t, a.request(t, http.MethodPatch, alice, tt.body),
				http.StatusUnprocessableEntity, ProblemCodeValidationFailed)
			if problem.Errors == nil || len(*problem.Errors) == 0 || (*problem.Errors)[0].Field != tt.field {
				t.Errorf("errors = %+v, want one for %q", problem.Errors, tt.field)
			}
		})
	}

	user := decodeUser(t, a.request(t, http.MethodGet, alice, ""))
	if user.Timezone != "Europe/Madrid" || user.AllDayReminderTime != "09:00" {
		t.Errorf("a rejected patch changed the profile: %+v", user)
	}
}

func TestPatchMeCrossOrigin(t *testing.T) {
	a := newMeApp(t)
	_, alice := a.signUp(t, "alice")
	req := newRequest(t, http.MethodPatch, testOrigin+"/api/v1/me", mergePatchType, `{"timezone":"Asia/Tokyo"}`)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	addCookie(req, session.CookieName, alice)
	decodeProblem(t, serve(t, a.mux, req), http.StatusForbidden, ProblemCodeForbidden)

	user := decodeUser(t, a.request(t, http.MethodGet, alice, ""))
	if user.Timezone != "Europe/Madrid" {
		t.Errorf("cross-origin patch applied: %+v", user)
	}
}
