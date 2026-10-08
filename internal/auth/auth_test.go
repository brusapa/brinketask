package auth_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/brusapa/brinketask/internal/account"
	"github.com/brusapa/brinketask/internal/auth"
	"github.com/brusapa/brinketask/internal/clock"
	"github.com/brusapa/brinketask/internal/config"
	"github.com/brusapa/brinketask/internal/httpapi"
	"github.com/brusapa/brinketask/internal/session"
	"github.com/brusapa/brinketask/internal/storage/storagetest"
)

func TestMain(m *testing.M) { storagetest.Main(m) }

const publicURL = "http://localhost:8080"

var start = time.Date(2026, time.October, 8, 9, 0, 0, 0, time.UTC)

// app is the server under test, with its own database, clock and provider.
type app struct {
	pool     *pgxpool.Pool
	clock    *clock.Fixed
	idp      *fakeIDP
	sessions *session.Manager
	mux      *http.ServeMux
	logs     *syncBuffer
}

// syncBuffer collects log output; the handler may log from the request
// goroutine while the test reads.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newApp(t *testing.T) *app {
	t.Helper()
	pool := storagetest.NewPool(t)
	clk := clock.NewFixed(start)
	idp := newFakeIDP(t, clk)
	logs := &syncBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	sessions := session.NewManager(pool, clk, 7*24*time.Hour, 30*24*time.Hour)

	handler := auth.NewHandler(
		config.OIDC{Issuer: idp.issuer(), ClientID: clientID, ClientSecret: clientSecret},
		publicURL, pool, clk, account.NewService(pool, clk), sessions, logger)
	mux := http.NewServeMux()
	handler.Register(mux)
	return &app{pool: pool, clock: clk, idp: idp, sessions: sessions, mux: mux, logs: logs}
}

func (a *app) do(t *testing.T, method, target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, target, nil)
	for _, c := range cookies {
		// Sent the way a browser sends cookies back.
		req.Header.Add("Cookie", c.Name+"="+c.Value)
	}
	rec := httptest.NewRecorder()
	a.mux.ServeHTTP(rec, req)
	return rec
}

// startLogin calls /auth/login and returns where the browser is sent and
// the cookie that binds the login to this browser.
func (a *app) startLogin(t *testing.T) (authURL string, browser *http.Cookie) {
	t.Helper()
	rec := a.do(t, http.MethodGet, auth.LoginPath)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login status = %d, want 303; body %s", rec.Code, rec.Body)
	}
	return rec.Header().Get("Location"), cookieNamed(t, rec, "brinketask_auth")
}

// login runs the whole flow for subject and returns the callback response.
func (a *app) login(t *testing.T, subject string) *httptest.ResponseRecorder {
	t.Helper()
	authURL, browser := a.startLogin(t)
	back := a.idp.authorize(authURL, subject, nil)
	return a.do(t, http.MethodGet, back.RequestURI(), browser)
}

func cookieNamed(t *testing.T, rec *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %s cookie in %v", name, rec.Header().Values("Set-Cookie"))
	return nil
}

// wantRedirect checks where the callback sent the browser: "/" on success,
// "/?auth_error=<code>" on failure.
func wantRedirect(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Location"); got != want {
		t.Fatalf("redirected to %q, want %q", got, want)
	}
}

func (a *app) users(t *testing.T) int {
	t.Helper()
	var n int
	if err := a.pool.QueryRow(context.Background(), "SELECT count(*) FROM users").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// SPEC section 7: authorization code with PKCE (S256), state and nonce,
// scopes openid profile email.
func TestLoginRedirectsToProvider(t *testing.T) {
	a := newApp(t)
	authURL, browser := a.startLogin(t)

	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	checks := map[string]string{
		"response_type":         "code",
		"client_id":             clientID,
		"redirect_uri":          publicURL + "/auth/callback",
		"scope":                 "openid profile email",
		"code_challenge_method": "S256",
	}
	for name, want := range checks {
		if got := q.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	for _, name := range []string{"state", "nonce", "code_challenge"} {
		if len(q.Get(name)) < 32 {
			t.Errorf("%s = %q, want a long random value", name, q.Get(name))
		}
	}
	if q.Get("state") == q.Get("nonce") {
		t.Error("state and nonce are the same value")
	}

	if !browser.HttpOnly || !browser.Secure || browser.SameSite != http.SameSiteLaxMode {
		t.Errorf("brinketask_auth flags: HttpOnly=%v Secure=%v SameSite=%v", browser.HttpOnly, browser.Secure, browser.SameSite)
	}
	if browser.Path != "/auth/callback" || browser.MaxAge != 600 {
		t.Errorf("brinketask_auth path %q max-age %d, want /auth/callback and 600", browser.Path, browser.MaxAge)
	}
}

func TestLoginCreatesUserAndSession(t *testing.T) {
	a := newApp(t)
	rec := a.login(t, "alice")
	wantRedirect(t, rec, "/")

	cookie := cookieNamed(t, rec, session.CookieName)
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("session cookie flags: HttpOnly=%v Secure=%v SameSite=%v", cookie.HttpOnly, cookie.Secure, cookie.SameSite)
	}
	s, err := a.sessions.Resolve(context.Background(), cookie.Value)
	if err != nil {
		t.Fatalf("session from the callback does not resolve: %v", err)
	}

	var issuer, subject, email, name string
	err = a.pool.QueryRow(context.Background(),
		"SELECT oidc_issuer, oidc_subject, email, display_name FROM users WHERE id = $1", s.UserID).
		Scan(&issuer, &subject, &email, &name)
	if err != nil {
		t.Fatal(err)
	}
	if issuer != a.idp.issuer() || subject != "alice" || email != "alice@example.com" || name != "User alice" {
		t.Errorf("user = %s %s %s %s", issuer, subject, email, name)
	}

	// The login in progress is used up and its cookie cleared.
	if c := cookieNamed(t, rec, "brinketask_auth"); c.MaxAge >= 0 {
		t.Errorf("brinketask_auth not cleared: %+v", c)
	}
	var pending int
	if err := a.pool.QueryRow(context.Background(), "SELECT count(*) FROM auth_requests").Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Errorf("%d auth requests left, want 0", pending)
	}
}

// Logging in again is the same user (D-14), with a new session.
func TestSecondLoginIsSameUser(t *testing.T) {
	a := newApp(t)
	first := cookieNamed(t, a.login(t, "alice"), session.CookieName)
	second := cookieNamed(t, a.login(t, "alice"), session.CookieName)
	if first.Value == second.Value {
		t.Error("both logins got the same session")
	}
	s1, err1 := a.sessions.Resolve(context.Background(), first.Value)
	s2, err2 := a.sessions.Resolve(context.Background(), second.Value)
	if err1 != nil || err2 != nil || s1.UserID != s2.UserID {
		t.Errorf("sessions resolve to %v (%v) and %v (%v), want the same user", s1.UserID, err1, s2.UserID, err2)
	}
	if n := a.users(t); n != 1 {
		t.Errorf("%d users, want 1", n)
	}
}

// P9: display name from "name", else "preferred_username"; email kept even
// if unverified.
func TestDisplayNameFallsBackToUsername(t *testing.T) {
	a := newApp(t)
	authURL, browser := a.startLogin(t)
	back := a.idp.authorize(authURL, "bob", map[string]any{
		"name": nil, "preferred_username": "bobby", "email_verified": false,
	})
	wantRedirect(t, a.do(t, http.MethodGet, back.RequestURI(), browser), "/")

	var name, email string
	err := a.pool.QueryRow(context.Background(), "SELECT display_name, email FROM users").Scan(&name, &email)
	if err != nil {
		t.Fatal(err)
	}
	if name != "bobby" || email != "bob@example.com" {
		t.Errorf("display_name, email = %q, %q; want bobby, bob@example.com", name, email)
	}
}

// Every way a callback can be forged, replayed or tampered with ends in
// login_failed, without a session or a user.
func TestCallbackRejections(t *testing.T) {
	tests := []struct {
		name string
		// callback returns the URL and cookies of the forged callback.
		callback func(t *testing.T, a *app) (string, []*http.Cookie)
	}{
		{
			name: "no browser cookie",
			callback: func(t *testing.T, a *app) (string, []*http.Cookie) {
				authURL, _ := a.startLogin(t)
				return a.idp.authorize(authURL, "alice", nil).RequestURI(), nil
			},
		},
		{
			// Login CSRF: the attacker's callback URL opened in the victim's
			// browser, which carries the cookie of its own login.
			name: "callback of another browser's login",
			callback: func(t *testing.T, a *app) (string, []*http.Cookie) {
				attackerURL, _ := a.startLogin(t)
				_, victimCookie := a.startLogin(t)
				return a.idp.authorize(attackerURL, "attacker", nil).RequestURI(), []*http.Cookie{victimCookie}
			},
		},
		{
			name: "wrong state",
			callback: func(t *testing.T, a *app) (string, []*http.Cookie) {
				authURL, browser := a.startLogin(t)
				back := a.idp.authorize(authURL, "alice", nil)
				q := back.Query()
				q.Set("state", "forged")
				back.RawQuery = q.Encode()
				return back.RequestURI(), []*http.Cookie{browser}
			},
		},
		{
			name: "login older than 10 minutes",
			callback: func(t *testing.T, a *app) (string, []*http.Cookie) {
				authURL, browser := a.startLogin(t)
				back := a.idp.authorize(authURL, "alice", nil)
				a.clock.Advance(10 * time.Minute)
				return back.RequestURI(), []*http.Cookie{browser}
			},
		},
		{
			name: "nonce of another login",
			callback: func(t *testing.T, a *app) (string, []*http.Cookie) {
				authURL, browser := a.startLogin(t)
				return a.idp.authorize(authURL, "alice", map[string]any{"nonce": "replayed"}).RequestURI(),
					[]*http.Cookie{browser}
			},
		},
		{
			name: "token for another client",
			callback: func(t *testing.T, a *app) (string, []*http.Cookie) {
				authURL, browser := a.startLogin(t)
				return a.idp.authorize(authURL, "alice", map[string]any{"aud": "other-app"}).RequestURI(),
					[]*http.Cookie{browser}
			},
		},
		{
			name: "token from another issuer",
			callback: func(t *testing.T, a *app) (string, []*http.Cookie) {
				authURL, browser := a.startLogin(t)
				return a.idp.authorize(authURL, "alice", map[string]any{"iss": "https://evil.example.com"}).RequestURI(),
					[]*http.Cookie{browser}
			},
		},
		{
			// Expiry is checked against the injected clock (D-17).
			name: "expired token",
			callback: func(t *testing.T, a *app) (string, []*http.Cookie) {
				authURL, browser := a.startLogin(t)
				expired := a.clock.Now().Add(-time.Minute).Unix()
				return a.idp.authorize(authURL, "alice", map[string]any{"exp": expired}).RequestURI(),
					[]*http.Cookie{browser}
			},
		},
		{
			name: "code the provider does not know",
			callback: func(t *testing.T, a *app) (string, []*http.Cookie) {
				authURL, browser := a.startLogin(t)
				back := a.idp.authorize(authURL, "alice", nil)
				q := back.Query()
				q.Set("code", "made-up")
				back.RawQuery = q.Encode()
				return back.RequestURI(), []*http.Cookie{browser}
			},
		},
		{
			name: "no code",
			callback: func(t *testing.T, a *app) (string, []*http.Cookie) {
				authURL, browser := a.startLogin(t)
				state := mustQuery(t, authURL).Get("state")
				return "/auth/callback?state=" + state, []*http.Cookie{browser}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newApp(t)
			target, cookies := tt.callback(t, a)
			rec := a.do(t, http.MethodGet, target, cookies...)
			wantRedirect(t, rec, "/?auth_error=login_failed")
			for _, c := range rec.Result().Cookies() {
				if c.Name == session.CookieName && c.Value != "" {
					t.Error("a session cookie was set")
				}
			}
			if n := a.users(t); n != 0 {
				t.Errorf("%d users created, want 0", n)
			}
		})
	}
}

// Each login can be finished once: replaying the callback fails (state and
// the browser binding are single use).
func TestCallbackIsSingleUse(t *testing.T) {
	a := newApp(t)
	authURL, browser := a.startLogin(t)
	back := a.idp.authorize(authURL, "alice", nil)
	wantRedirect(t, a.do(t, http.MethodGet, back.RequestURI(), browser), "/")
	wantRedirect(t, a.do(t, http.MethodGet, back.RequestURI(), browser), "/?auth_error=login_failed")
}

func TestProviderRefusal(t *testing.T) {
	for providerError, want := range map[string]string{
		"access_denied":   "/?auth_error=access_denied",
		"invalid_request": "/?auth_error=login_failed",
	} {
		t.Run(providerError, func(t *testing.T) {
			a := newApp(t)
			authURL, browser := a.startLogin(t)
			state := mustQuery(t, authURL).Get("state")
			target := "/auth/callback?" + url.Values{"error": {providerError}, "state": {state}}.Encode()
			wantRedirect(t, a.do(t, http.MethodGet, target, browser), want)
		})
	}
}

// P6: discovery happens at the first login. The server works while the
// provider is down, login says provider_unavailable, and the next attempt
// after the provider recovers succeeds.
func TestProviderUnavailableAtLogin(t *testing.T) {
	a := newApp(t)
	a.idp.down.Store(true)
	wantRedirect(t, a.do(t, http.MethodGet, auth.LoginPath), "/?auth_error=provider_unavailable")

	a.idp.down.Store(false)
	wantRedirect(t, a.login(t, "alice"), "/")
}

func TestLogout(t *testing.T) {
	a := newApp(t)
	cookie := cookieNamed(t, a.login(t, "alice"), session.CookieName)

	for range 2 { // logging out twice is harmless
		rec := a.do(t, http.MethodPost, auth.LogoutPath, cookie)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("logout status = %d, want 204", rec.Code)
		}
		if c := cookieNamed(t, rec, session.CookieName); c.MaxAge >= 0 || c.Value != "" {
			t.Errorf("session cookie not cleared: %+v", c)
		}
	}
	if _, err := a.sessions.Resolve(context.Background(), cookie.Value); !errors.Is(err, session.ErrNoSession) {
		t.Errorf("session after logout: %v, want ErrNoSession", err)
	}

	if rec := a.do(t, http.MethodPost, auth.LogoutPath); rec.Code != http.StatusNoContent {
		t.Errorf("logout without a session = %d, want 204", rec.Code)
	}
	if rec := a.do(t, http.MethodGet, auth.LogoutPath); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET logout = %d, want 405", rec.Code)
	}
}

// CLAUDE.md, secrets: no code, state, nonce, token or session identifier
// reaches the logs, on success or failure.
func TestLogsCarryNoSecrets(t *testing.T) {
	a := newApp(t)
	authURL, browser := a.startLogin(t)
	back := a.idp.authorize(authURL, "alice", map[string]any{"aud": "other-app"})
	a.do(t, http.MethodGet, back.RequestURI(), browser)
	ok := a.login(t, "alice")

	q := mustQuery(t, authURL)
	secrets := []string{
		q.Get("state"), q.Get("nonce"), q.Get("code_challenge"),
		back.Query().Get("code"), browser.Value,
		cookieNamed(t, ok, session.CookieName).Value, clientSecret,
	}
	logs := a.logs.String()
	if logs == "" {
		t.Fatal("nothing was logged; the test would prove nothing")
	}
	for _, secret := range secrets {
		if strings.Contains(logs, secret) {
			t.Errorf("logs contain %q:\n%s", secret, logs)
		}
	}
}

func mustQuery(t *testing.T, rawURL string) url.Values {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

// SPEC section 7: logout changes state, so a cross-origin POST is refused.
// Otherwise any site could log the user out.
func TestLogoutRejectsCrossOrigin(t *testing.T) {
	a := newApp(t)
	sameOrigin, err := httpapi.SameOrigin(publicURL)
	if err != nil {
		t.Fatal(err)
	}
	// A fresh mux, with logout behind the same-origin check as in main.
	pool, clk := a.pool, a.clock
	handler := auth.NewHandler(
		config.OIDC{Issuer: a.idp.issuer(), ClientID: clientID, ClientSecret: clientSecret},
		publicURL, pool, clk, account.NewService(pool, clk), a.sessions, slog.New(slog.DiscardHandler))
	a.mux = http.NewServeMux()
	handler.Register(a.mux, sameOrigin)
	cookie := cookieNamed(t, a.login(t, "alice"), session.CookieName)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, publicURL+auth.LogoutPath, nil)
	req.Header.Add("Cookie", cookie.Name+"="+cookie.Value)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	a.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site logout = %d, want 403", rec.Code)
	}
	if _, err := a.sessions.Resolve(context.Background(), cookie.Value); err != nil {
		t.Errorf("cross-site logout ended the session: %v", err)
	}

	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec = httptest.NewRecorder()
	a.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("same-origin logout = %d, want 204", rec.Code)
	}
}
