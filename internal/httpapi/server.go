// Package httpapi serves the HTTP API described by api/openapi.yaml.
// api.gen.go is generated from the contract (`make generate`); the rest of
// the package is written by hand.
package httpapi

import (
	"log/slog"
	"net/http"
)

// BaseURL is the prefix of every API route (`servers` in the contract).
const BaseURL = "/api/v1"

// Server implements the generated StrictServerInterface: one method per
// operation of the contract.
type Server struct{}

// This line does not run anything; it makes compilation fail if Server stops
// satisfying the interface, e.g. after the contract gains an operation.
var _ StrictServerInterface = Server{}

// Register adds every API route to mux, plus a catch-all that answers
// unknown /api/v1 paths with a 404 problem instead of the mux's plain-text
// 404.
func Register(mux *http.ServeMux, server StrictServerInterface, logger *slog.Logger) {
	strict := NewStrictHandlerWithOptions(server, nil, StrictHTTPServerOptions{
		// The request body could not be decoded into the operation's type.
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeProblem(w, newProblem(http.StatusBadRequest, codeValidationFailed, err.Error()))
		},
		// A handler returned an error. The client gets a generic problem;
		// the details go to the log only.
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
			writeProblem(w, newProblem(http.StatusInternalServerError, codeInternal, ""))
		},
	})

	HandlerWithOptions(strict, StdHTTPServerOptions{
		BaseURL:    BaseURL,
		BaseRouter: mux,
		// A path or query parameter could not be parsed (e.g. an id that is
		// not a UUID).
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeProblem(w, newProblem(http.StatusBadRequest, codeValidationFailed, err.Error()))
		},
	})

	// ServeMux picks the most specific matching pattern, so this pattern only
	// catches /api/v1/... requests that no operation above matched.
	mux.HandleFunc(BaseURL+"/", func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, newProblem(http.StatusNotFound, codeNotFound, ""))
	})
}
