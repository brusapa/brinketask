package httpapi

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/brusapa/brinketask/internal/session"
)

// fakeSessions knows one valid cookie value.
type fakeSessions struct {
	value string
	s     session.Session
	err   error // returned for every value when set
}

func (f fakeSessions) Resolve(_ context.Context, value string) (session.Session, error) {
	if f.err != nil {
		return session.Session{}, f.err
	}
	if value != f.value {
		return session.Session{}, session.ErrNoSession
	}
	return f.s, nil
}

// whoAmIServer answers GET /me with a problem whose detail is the caller's
// user id, so the test can see what reached the handler.
type whoAmIServer struct {
	Server
}

func (whoAmIServer) GetMe(ctx context.Context, _ GetMeRequestObject) (GetMeResponseObject, error) {
	s, ok := sessionFrom(ctx)
	if !ok {
		return nil, errors.New("no session in context")
	}
	return GetMedefaultApplicationProblemPlusJSONResponse(
		toResponse(newProblem(http.StatusNotImplemented, ProblemCodeNotImplemented, s.UserID.String()))), nil
}

func TestAuthenticate(t *testing.T) {
	userID := uuid.MustParse("0192f1a0-0000-7000-8000-00000000000a")
	sessions := fakeSessions{value: "good", s: session.Session{UserID: userID, Key: "k"}}
	mux := http.NewServeMux()
	if err := Register(mux, whoAmIServer{}, discardLogger, Authenticate(sessions, discardLogger)); err != nil {
		t.Fatal(err)
	}

	t.Run("valid session reaches the handler as its user", func(t *testing.T) {
		req := newRequest(t, http.MethodGet, "/api/v1/me", "", "")
		addCookie(req, session.CookieName, "good")
		problem := decodeProblem(t, serve(t, mux, req), http.StatusNotImplemented, ProblemCodeNotImplemented)
		if problem.Detail == nil || *problem.Detail != userID.String() {
			t.Errorf("handler saw user %v, want %v", problem.Detail, userID)
		}
	})

	unauthenticated := map[string]func(*http.Request){
		"no cookie":              func(*http.Request) {},
		"unknown session":        func(r *http.Request) { addCookie(r, session.CookieName, "bad") },
		"cookie with other name": func(r *http.Request) { addCookie(r, "session", "good") },
		// bearerAuth is declared for V2 but not implemented.
		"bearer token only": func(r *http.Request) { r.Header.Set("Authorization", "Bearer good") },
	}
	for name, prepare := range unauthenticated {
		t.Run(name, func(t *testing.T) {
			req := newRequest(t, http.MethodGet, "/api/v1/me", "", "")
			prepare(req)
			decodeProblem(t, serve(t, mux, req), http.StatusUnauthorized, ProblemCodeUnauthenticated)
		})
	}

	// Authentication runs before validation: an anonymous caller learns
	// nothing about the body rules.
	t.Run("anonymous invalid body", func(t *testing.T) {
		req := newRequest(t, http.MethodPatch, "/api/v1/me", mergePatchType, `{"timezone":5}`)
		decodeProblem(t, serve(t, mux, req), http.StatusUnauthorized, ProblemCodeUnauthenticated)
	})
}

func TestAuthenticateStoreFailureIsInternal(t *testing.T) {
	mux := http.NewServeMux()
	sessions := fakeSessions{err: errors.New("connection refused")}
	if err := Register(mux, whoAmIServer{}, discardLogger, Authenticate(sessions, discardLogger)); err != nil {
		t.Fatal(err)
	}
	req := newRequest(t, http.MethodGet, "/api/v1/me", "", "")
	addCookie(req, session.CookieName, "good")
	decodeProblem(t, serve(t, mux, req), http.StatusInternalServerError, ProblemCodeInternal)
}

// addCookie sends a cookie with the request the way a browser does, as a
// plain Cookie header. (http.Cookie values are for Set-Cookie; gosec checks
// them for attributes that only make sense in a response.)
func addCookie(r *http.Request, name, value string) {
	r.Header.Add("Cookie", name+"="+value)
}
