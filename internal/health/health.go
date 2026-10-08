// Package health serves GET /healthz (SPEC section 10), an operational route
// outside the API contract (see the description in api/openapi.yaml).
package health

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// Path is where the health check is served.
const Path = "/healthz"

// Timeout bounds the database check, so a hung database turns into a 503
// instead of a request that never ends.
const Timeout = 2 * time.Second

// Pinger reports whether the database answers. *pgxpool.Pool satisfies it;
// tests pass a fake. In Go a type satisfies an interface just by having the
// methods; it does not declare it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Handler answers 200 "ok" when db answers within timeout, and 503
// "unavailable" otherwise. The body is plain text: the route is meant for
// container healthchecks and probes, not for API clients.
func Handler(db Pinger, timeout time.Duration, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		// Probes call this repeatedly; a cached answer would hide an outage.
		w.Header().Set("Cache-Control", "no-store")

		if err := db.Ping(ctx); err != nil {
			logger.Warn("health check failed", "error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("unavailable\n"))
			return
		}
		_, _ = w.Write([]byte("ok\n"))
	})
}

// Register serves the health check at GET /healthz. A GET pattern also
// matches HEAD requests.
func Register(mux *http.ServeMux, db Pinger, logger *slog.Logger) {
	mux.Handle(http.MethodGet+" "+Path, Handler(db, Timeout, logger))
}
