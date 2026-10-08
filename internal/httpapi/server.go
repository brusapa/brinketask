// Package httpapi serves the HTTP API described by api/openapi.yaml.
// api.gen.go is generated from the contract (`make generate`); the rest of
// the package is written by hand.
package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/brusapa/brinketask/internal/httpapi/contract"
)

// BaseURL is the prefix of every API route (`servers` in the contract).
const BaseURL = "/api/v1"

// Server implements the generated StrictServerInterface: one method per
// operation of the contract. The zero value answers 501 for everything not
// implemented yet, which the routing tests use.
type Server struct {
	accounts Accounts
}

// NewServer returns the API implementation.
func NewServer(accounts Accounts) Server {
	return Server{accounts: accounts}
}

// This line does not run anything; it makes compilation fail if Server stops
// satisfying the interface, e.g. after the contract gains an operation.
var _ StrictServerInterface = Server{}

// LoadContract returns the contract embedded in the binary, indexed for
// validation.
func LoadContract() (*contract.Contract, error) {
	spec, err := GetSwagger()
	if err != nil {
		return nil, fmt.Errorf("httpapi: load embedded contract: %w", err)
	}
	return contract.New(spec, BaseURL)
}

// Register adds every API route to mux, plus a catch-all that answers
// unknown /api/v1 paths with a 404 problem instead of the mux's plain-text
// 404.
//
// Each operation runs the given middlewares, first one outermost, then the
// request validation against the contract, then the handler.
func Register(mux *http.ServeMux, server StrictServerInterface, logger *slog.Logger, middlewares ...MiddlewareFunc) error {
	c, err := LoadContract()
	if err != nil {
		return err
	}

	strict := NewStrictHandlerWithOptions(server, nil, StrictHTTPServerOptions{
		// The body passed validation but could not be decoded into the
		// operation's type. Validation should make this unreachable.
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeProblem(w, newProblem(http.StatusBadRequest, ProblemCodeMalformedRequest, err.Error()))
		},
		// A handler returned an error. The client gets a generic problem;
		// the details go to the log only.
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
			writeProblem(w, newProblem(http.StatusInternalServerError, ProblemCodeInternal, ""))
		},
	})

	// The generated code wraps the handler with each middleware in list
	// order, so the last one ends up outermost. Reversing the list makes
	// the first middleware given here the first to run.
	ordered := []MiddlewareFunc{validateRequests(c, logger)}
	for i := len(middlewares) - 1; i >= 0; i-- {
		ordered = append(ordered, middlewares[i])
	}

	HandlerWithOptions(strict, StdHTTPServerOptions{
		BaseURL:     BaseURL,
		BaseRouter:  mux,
		Middlewares: ordered,
		// A path or query parameter could not be parsed (e.g. an id that is
		// not a UUID).
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeProblem(w, newProblem(http.StatusBadRequest, ProblemCodeMalformedRequest, err.Error()))
		},
	})

	// ServeMux picks the most specific matching pattern, so this pattern only
	// catches /api/v1/... requests that no operation above matched.
	mux.HandleFunc(BaseURL+"/", func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, newProblem(http.StatusNotFound, ProblemCodeNotFound, ""))
	})
	return nil
}
