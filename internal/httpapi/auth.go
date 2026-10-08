package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/brusapa/brinketask/internal/session"
)

// SessionResolver turns a session cookie value into a session.
// *session.Manager implements it; tests pass a fake.
type SessionResolver interface {
	Resolve(ctx context.Context, value string) (session.Session, error)
}

// sessionKey is the context key under which Authenticate stores the
// session. A private struct type cannot collide with keys of other
// packages, which is the usual Go idiom for context keys.
type sessionKey struct{}

// sessionFrom returns the session Authenticate stored in ctx. Every
// operation runs behind Authenticate, so handlers can rely on ok.
func sessionFrom(ctx context.Context) (session.Session, bool) {
	s, ok := ctx.Value(sessionKey{}).(session.Session)
	return s, ok
}

// Authenticate resolves the caller from the session cookie and stores the
// session in the request context; without a valid session it answers 401.
// It is the single place that decides who the caller is (SPEC section 7),
// so the Bearer tokens planned for V2 will be added here. In V1 an
// Authorization header is ignored: only the cookie counts.
func Authenticate(sessions SessionResolver, logger *slog.Logger) MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(session.CookieName)
			if err != nil {
				writeUnauthenticated(w)
				return
			}
			s, err := sessions.Resolve(r.Context(), cookie.Value)
			if errors.Is(err, session.ErrNoSession) {
				writeUnauthenticated(w)
				return
			}
			if err != nil {
				// The error never contains the cookie value.
				logger.Error("resolve session", "error", err)
				writeProblem(w, newProblem(http.StatusInternalServerError, ProblemCodeInternal, ""))
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, s)))
		})
	}
}

func writeUnauthenticated(w http.ResponseWriter) {
	writeProblem(w, newProblem(http.StatusUnauthorized, ProblemCodeUnauthenticated, ""))
}
