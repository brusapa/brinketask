// Package auth serves the browser login of SPEC section 7: the OpenID
// Connect authorization code flow with PKCE, state and nonce, with the
// server as a confidential client (D-13). These routes are operational
// routes outside the API contract (see its description):
//
//	GET  /auth/login     redirects to the provider
//	GET  /auth/callback  the provider redirects back here
//	POST /auth/logout    ends the session
//
// The callback never shows text of its own: it redirects to "/" on success
// and to "/?auth_error=<code>" on failure, and the web client translates
// the code (CLAUDE.md, i18n).
package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"

	"github.com/brusapa/brinketask/internal/account"
	"github.com/brusapa/brinketask/internal/clock"
	"github.com/brusapa/brinketask/internal/config"
	"github.com/brusapa/brinketask/internal/opaque"
	"github.com/brusapa/brinketask/internal/session"
	"github.com/brusapa/brinketask/internal/storage/dbgen"
)

// Paths of the routes.
const (
	LoginPath    = "/auth/login"
	CallbackPath = "/auth/callback"
	LogoutPath   = "/auth/logout"
)

// Values of the auth_error query parameter on the redirect after a failed
// callback. The web client translates them.
const (
	// The user refused, or the provider does not allow them to use the app
	// (allowed groups of the OIDC client).
	errorAccessDenied = "access_denied"
	// The login could not be completed: expired, replayed, forged, or a
	// token that does not verify.
	errorLoginFailed = "login_failed"
	// The provider could not be reached.
	errorProviderUnavailable = "provider_unavailable"
)

const (
	// browserCookie binds a login in progress to the browser that started
	// it (table auth_requests). Without it, an attacker could make a victim
	// finish the attacker's own login (login CSRF).
	browserCookie = "brinketask_auth"
	// requestLifetime is how long the user has to finish logging in at the
	// provider.
	requestLifetime = 10 * time.Minute
	// providerTimeout bounds every request to the provider.
	providerTimeout = 10 * time.Second
)

// scopes are those of SPEC section 7.
var scopes = []string{oidc.ScopeOpenID, "profile", "email"}

// Handler serves the /auth routes.
type Handler struct {
	cfg        config.OIDC
	publicURL  string
	pool       *pgxpool.Pool
	clock      clock.Clock
	accounts   *account.Service
	sessions   *session.Manager
	logger     *slog.Logger
	httpClient *http.Client

	// Discovery happens on the first login that needs it, not at startup,
	// so the server starts (and /healthz answers) while the provider is
	// down. mu guards the cached result; a failed discovery is retried on
	// the next login.
	mu       sync.Mutex
	provider *oidc.Provider
}

// NewHandler returns the /auth handler. publicURL is the origin the browser
// uses; the redirect URL registered at the provider is
// publicURL + "/auth/callback".
func NewHandler(cfg config.OIDC, publicURL string, pool *pgxpool.Pool, clk clock.Clock,
	accounts *account.Service, sessions *session.Manager, logger *slog.Logger,
) *Handler {
	return &Handler{
		cfg:        cfg,
		publicURL:  publicURL,
		pool:       pool,
		clock:      clk,
		accounts:   accounts,
		sessions:   sessions,
		logger:     logger,
		httpClient: &http.Client{Timeout: providerTimeout},
	}
}

// Register adds the /auth routes to mux. logout is wrapped by the given
// middlewares (the same-origin check), first one outermost.
func (h *Handler) Register(mux *http.ServeMux, logoutMiddlewares ...func(http.Handler) http.Handler) {
	mux.HandleFunc("GET "+LoginPath, h.login)
	mux.HandleFunc("GET "+CallbackPath, h.callback)

	var logout http.Handler = http.HandlerFunc(h.logout)
	for i := len(logoutMiddlewares) - 1; i >= 0; i-- {
		logout = logoutMiddlewares[i](logout)
	}
	mux.Handle("POST "+LogoutPath, logout)
}

// discover returns the provider's configuration, fetching it the first time.
func (h *Handler) discover(ctx context.Context) (*oidc.Provider, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.provider != nil {
		return h.provider, nil
	}
	// ClientContext makes go-oidc use our client, with its timeout, for
	// discovery and later for fetching the signing keys.
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, h.httpClient), h.cfg.Issuer)
	if err != nil {
		return nil, err
	}
	h.provider = provider
	return provider, nil
}

func (h *Handler) oauth2Config(provider *oidc.Provider) *oauth2.Config {
	endpoint := provider.Endpoint()
	// client_secret_basic, the OIDC default. Without it x/oauth2 guesses,
	// retrying a rejected exchange with the secret in the body, which turns
	// a clear "invalid_grant" into a confusing second error.
	endpoint.AuthStyle = oauth2.AuthStyleInHeader
	return &oauth2.Config{
		ClientID:     h.cfg.ClientID,
		ClientSecret: h.cfg.ClientSecret,
		Endpoint:     endpoint,
		RedirectURL:  h.publicURL + CallbackPath,
		Scopes:       scopes,
	}
}

// login starts the flow: it records state, nonce and the PKCE verifier for
// this browser and sends the browser to the provider.
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	provider, err := h.discover(ctx)
	if err != nil {
		h.logger.Warn("oidc discovery failed", "error", err)
		h.redirectWithError(w, r, errorProviderUnavailable)
		return
	}

	now := h.clock.Now()
	state, _ := opaque.New()
	nonce, _ := opaque.New()
	verifier := oauth2.GenerateVerifier()
	browserValue, browserHash := opaque.New()

	q := dbgen.New(h.pool)
	if err := q.DeleteExpiredAuthRequests(ctx, now); err != nil {
		h.fail(w, r, "delete expired auth requests", err)
		return
	}
	err = q.InsertAuthRequest(ctx, dbgen.InsertAuthRequestParams{
		BrowserHash:  browserHash,
		State:        state,
		Nonce:        nonce,
		CodeVerifier: verifier,
		Now:          now,
		ExpiresAt:    now.Add(requestLifetime),
	})
	if err != nil {
		h.fail(w, r, "insert auth request", err)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:  browserCookie,
		Value: browserValue,
		// Only the callback needs it.
		Path:     CallbackPath,
		MaxAge:   int(requestLifetime / time.Second),
		HttpOnly: true,
		Secure:   true,
		// Lax: the provider's redirect back is a top-level GET navigation
		// from another site, which Lax cookies accompany.
		SameSite: http.SameSiteLaxMode,
	})
	authURL := h.oauth2Config(provider).AuthCodeURL(state,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier))
	http.Redirect(w, r, authURL, http.StatusSeeOther)
}

// callback finishes the flow: it checks that this browser started a login
// with this state, exchanges the code, verifies the ID token and its nonce,
// signs the user up if needed and starts a session.
func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// The login in progress is used up whatever happens next.
	clearBrowserCookie(w)

	request, ok := h.consumeRequest(w, r)
	if !ok {
		return
	}

	query := r.URL.Query()
	if subtle.ConstantTimeCompare([]byte(query.Get("state")), []byte(request.State)) != 1 {
		h.loginFailed(w, r, "state mismatch")
		return
	}
	// RFC 6749 section 4.1.2.1: the provider reports refusals in "error".
	if providerError := query.Get("error"); providerError != "" {
		h.logger.Info("login refused by the provider", "error", providerError)
		if providerError == errorAccessDenied {
			h.redirectWithError(w, r, errorAccessDenied)
		} else {
			h.redirectWithError(w, r, errorLoginFailed)
		}
		return
	}
	code := query.Get("code")
	if code == "" {
		h.loginFailed(w, r, "no code")
		return
	}

	provider, err := h.discover(ctx)
	if err != nil {
		h.logger.Warn("oidc discovery failed", "error", err)
		h.redirectWithError(w, r, errorProviderUnavailable)
		return
	}

	// The exchange authenticates the server with its client secret and
	// proves with the PKCE verifier that it started this login.
	token, err := h.oauth2Config(provider).Exchange(oidc.ClientContext(ctx, h.httpClient), code,
		oauth2.VerifierOption(request.CodeVerifier))
	if err != nil {
		var refused *oauth2.RetrieveError
		if errors.As(err, &refused) {
			// The provider answered and rejected the code.
			h.loginFailed(w, r, "code exchange rejected: "+refused.ErrorCode)
			return
		}
		h.logger.Warn("oidc token request failed", "error", err)
		h.redirectWithError(w, r, errorProviderUnavailable)
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		h.loginFailed(w, r, "no id_token in the token response")
		return
	}

	// Verify checks the signature, the issuer, the audience (our client id)
	// and the expiry, the latter against the injected clock (D-17).
	verifier := provider.Verifier(&oidc.Config{ClientID: h.cfg.ClientID, Now: h.clock.Now})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		h.loginFailed(w, r, "id token rejected: "+err.Error())
		return
	}
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(request.Nonce)) != 1 {
		h.loginFailed(w, r, "nonce mismatch")
		return
	}

	identity, err := identityFrom(idToken)
	if err != nil {
		h.loginFailed(w, r, err.Error())
		return
	}
	userID, err := h.accounts.SignIn(ctx, identity)
	if err != nil {
		h.fail(w, r, "sign in", err)
		return
	}
	value, expires, err := h.sessions.Create(ctx, userID)
	if err != nil {
		h.fail(w, r, "create session", err)
		return
	}
	session.SetCookie(w, value, expires)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// consumeRequest takes the login in progress of this browser. It fails
// when the browser has no auth cookie, the login was already finished or
// never existed, or it has expired.
func (h *Handler) consumeRequest(w http.ResponseWriter, r *http.Request) (dbgen.AuthRequest, bool) {
	cookie, err := r.Cookie(browserCookie)
	if err != nil {
		h.loginFailed(w, r, "no auth cookie")
		return dbgen.AuthRequest{}, false
	}
	hash, ok := opaque.Hash(cookie.Value)
	if !ok {
		h.loginFailed(w, r, "malformed auth cookie")
		return dbgen.AuthRequest{}, false
	}
	request, err := dbgen.New(h.pool).ConsumeAuthRequest(r.Context(), hash)
	if errors.Is(err, pgx.ErrNoRows) {
		h.loginFailed(w, r, "unknown or used auth request")
		return dbgen.AuthRequest{}, false
	}
	if err != nil {
		h.fail(w, r, "consume auth request", err)
		return dbgen.AuthRequest{}, false
	}
	if !h.clock.Now().Before(request.ExpiresAt) {
		h.loginFailed(w, r, "auth request expired")
		return dbgen.AuthRequest{}, false
	}
	return request, true
}

// claims are the ID token claims the app reads besides iss and sub. Email
// is kept even when the provider has not verified it: it is informational
// only and never identifies the user (D-14).
type claims struct {
	Email             string `json:"email"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
}

func identityFrom(idToken *oidc.IDToken) (account.Identity, error) {
	var c claims
	if err := idToken.Claims(&c); err != nil {
		return account.Identity{}, fmt.Errorf("read claims: %w", err)
	}
	if idToken.Subject == "" {
		return account.Identity{}, errors.New("id token without subject")
	}
	identity := account.Identity{Issuer: idToken.Issuer, Subject: idToken.Subject}
	if c.Email != "" {
		identity.Email = &c.Email
	}
	// The display name is the "name" claim, or the username when the
	// provider has no full name.
	switch {
	case c.Name != "":
		identity.DisplayName = &c.Name
	case c.PreferredUsername != "":
		identity.DisplayName = &c.PreferredUsername
	}
	return identity, nil
}

// logout ends the session of the cookie, if any, and clears the cookie.
// Only the local session ends; the session at the provider is left alone.
// Logging out without a session, or twice, also answers 204.
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(session.CookieName); err == nil {
		if err := h.sessions.Delete(r.Context(), cookie.Value); err != nil {
			h.logger.Error("logout", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
	session.ClearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// loginFailed logs why a callback was rejected and sends the browser to the
// client with the generic login_failed code. reason never contains tokens,
// codes or cookie values.
func (h *Handler) loginFailed(w http.ResponseWriter, r *http.Request, reason string) {
	h.logger.Info("login failed", "reason", reason)
	h.redirectWithError(w, r, errorLoginFailed)
}

// fail handles an unexpected server error during login.
func (h *Handler) fail(w http.ResponseWriter, r *http.Request, step string, err error) {
	h.logger.Error("login error", "step", step, "error", err)
	h.redirectWithError(w, r, errorLoginFailed)
}

func (h *Handler) redirectWithError(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/?"+url.Values{"auth_error": {code}}.Encode(), http.StatusSeeOther)
}

func clearBrowserCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     browserCookie,
		Value:    "",
		Path:     CallbackPath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}
