package auth_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/brusapa/brinketask/internal/clock"
)

const (
	clientID     = "brinketask-test"
	clientSecret = "client-secret-for-tests"
	keyID        = "test-key"
)

// fakeIDP is an in-process OpenID provider: discovery, signing keys and a
// token endpoint that signs RS256 ID tokens. The authorization step, which
// a real provider does with the user in a browser, is the authorize method.
type fakeIDP struct {
	t      *testing.T
	server *httptest.Server
	key    *rsa.PrivateKey
	clock  clock.Clock

	// down makes discovery fail, as when the provider is unreachable.
	down atomic.Bool

	mu     sync.Mutex
	grants map[string]grant // by authorization code
}

// grant is what the provider remembers between authorize and token.
type grant struct {
	challenge   string
	redirectURI string
	claims      map[string]any
}

func newFakeIDP(t *testing.T, clk clock.Clock) *fakeIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &fakeIDP{t: t, key: key, clock: clk, grants: map[string]grant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", idp.discovery)
	mux.HandleFunc("GET /jwks", idp.jwks)
	mux.HandleFunc("POST /token", idp.token)
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

func (idp *fakeIDP) issuer() string { return idp.server.URL }

func (idp *fakeIDP) discovery(w http.ResponseWriter, _ *http.Request) {
	if idp.down.Load() {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, map[string]any{
		"issuer":                                idp.issuer(),
		"authorization_endpoint":                idp.issuer() + "/authorize",
		"token_endpoint":                        idp.issuer() + "/token",
		"jwks_uri":                              idp.issuer() + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (idp *fakeIDP) jwks(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key: &idp.key.PublicKey, KeyID: keyID, Algorithm: string(jose.RS256), Use: "sig",
	}}})
}

// authorize plays the provider's login page: it checks the request the app
// sent the browser to, lets the user in and returns the URL the provider
// would redirect the browser back to. overrides replace claims of the ID
// token that the token endpoint will issue, to simulate a misbehaving or
// malicious provider.
func (idp *fakeIDP) authorize(authURL string, subject string, overrides map[string]any) *url.URL {
	idp.t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		idp.t.Fatal(err)
	}
	if got := u.Scheme + "://" + u.Host + u.Path; got != idp.issuer()+"/authorize" {
		idp.t.Fatalf("login redirected to %s, want the authorization endpoint", got)
	}
	q := u.Query()
	if q.Get("client_id") != clientID || q.Get("response_type") != "code" {
		idp.t.Fatalf("authorization request %v", q)
	}

	claims := map[string]any{
		"iss":   idp.issuer(),
		"sub":   subject,
		"aud":   clientID,
		"iat":   idp.clock.Now().Unix(),
		"exp":   idp.clock.Now().Add(5 * time.Minute).Unix(),
		"nonce": q.Get("nonce"),
		"email": subject + "@example.com",
		"name":  "User " + subject,
	}
	maps.Copy(claims, overrides)
	for name, value := range claims {
		if value == nil {
			delete(claims, name)
		}
	}

	code := randomString(idp.t)
	idp.mu.Lock()
	idp.grants[code] = grant{challenge: q.Get("code_challenge"), redirectURI: q.Get("redirect_uri"), claims: claims}
	idp.mu.Unlock()

	back, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		idp.t.Fatal(err)
	}
	back.RawQuery = url.Values{"code": {code}, "state": {q.Get("state")}}.Encode()
	return back
}

// token is the token endpoint (RFC 6749 section 4.1.3) with PKCE (RFC 7636).
func (idp *fakeIDP) token(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := r.BasicAuth()
	if !ok || id != clientID || secret != clientSecret {
		tokenError(w, http.StatusUnauthorized, "invalid_client")
		return
	}
	if err := r.ParseForm(); err != nil || r.PostForm.Get("grant_type") != "authorization_code" {
		tokenError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	code := r.PostForm.Get("code")
	idp.mu.Lock()
	g, found := idp.grants[code]
	delete(idp.grants, code) // codes are single use
	idp.mu.Unlock()
	if !found || r.PostForm.Get("redirect_uri") != g.redirectURI {
		tokenError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	// S256: the challenge is the base64url SHA-256 of the verifier.
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		tokenError(w, http.StatusBadRequest, "invalid_grant")
		return
	}

	writeJSON(w, map[string]any{
		"access_token": randomString(idp.t),
		"token_type":   "Bearer",
		"expires_in":   300,
		"id_token":     idp.sign(g.claims),
	})
}

func (idp *fakeIDP) sign(claims map[string]any) string {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: idp.key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", keyID))
	if err != nil {
		idp.t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		idp.t.Fatal(err)
	}
	signed, err := signer.Sign(payload)
	if err != nil {
		idp.t.Fatal(err)
	}
	compact, err := signed.CompactSerialize()
	if err != nil {
		idp.t.Fatal(err)
	}
	return compact
}

func tokenError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func randomString(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return strings.TrimRight(base64.RawURLEncoding.EncodeToString(b), "=")
}
