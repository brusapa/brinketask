package httpapi

import (
	"fmt"
	"net/http"
)

// SameOrigin rejects state-changing requests that a browser sent from
// another origin, answering 403 forbidden (SPEC section 7: CSRF).
//
// It uses the standard library's CrossOriginProtection:
//
//   - GET, HEAD and OPTIONS always pass; they must not change state.
//   - With Sec-Fetch-Site, which every current browser sends, only
//     "same-origin" and "none" (typed in the address bar) pass.
//   - Without it, the Origin header must match the Host header or publicURL.
//   - A request with neither header cannot come from a browser page (curl,
//     scripts), so it passes; it carries no ambient cookie of a victim.
//
// publicURL is trusted explicitly so a reverse proxy that rewrites Host
// does not break same-origin requests from older browsers.
func SameOrigin(publicURL string) (MiddlewareFunc, error) {
	protection := http.NewCrossOriginProtection()
	if err := protection.AddTrustedOrigin(publicURL); err != nil {
		return nil, fmt.Errorf("httpapi: trusted origin %q: %w", publicURL, err)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := protection.Check(r); err != nil {
				writeProblem(w, newProblem(http.StatusForbidden, ProblemCodeForbidden,
					"cross-origin requests may not change state"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}
