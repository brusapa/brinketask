// Package contract checks HTTP traffic against api/openapi.yaml with
// kin-openapi. The server validates every request with it before the
// generated code decodes the body; tests validate real responses with it
// (SPEC section 11, "Contract").
package contract

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
)

// Contract is the loaded specification, indexed by route.
type Contract struct {
	// routes maps a net/http ServeMux pattern, e.g. "PATCH /api/v1/me", to
	// the operation of the contract it serves. The generated server
	// registers exactly these patterns, so a request's r.Pattern finds its
	// operation without a second router.
	routes map[string]*routers.Route
	// mux matches requests that did not go through the server's mux (in
	// tests) to one of the patterns above.
	mux *http.ServeMux
}

// New indexes spec, whose paths are served under baseURL.
func New(spec *openapi3.T, baseURL string) (*Contract, error) {
	c := &Contract{routes: map[string]*routers.Route{}, mux: http.NewServeMux()}
	for path, item := range spec.Paths.Map() {
		for method, operation := range item.Operations() {
			pattern := method + " " + baseURL + path
			c.routes[pattern] = &routers.Route{
				Spec:      spec,
				Path:      path,
				PathItem:  item,
				Method:    method,
				Operation: operation,
			}
			// The handler is never called; mux.Handler only reports which
			// pattern a request matches.
			c.mux.Handle(pattern, http.NotFoundHandler())
		}
	}
	if len(c.routes) == 0 {
		return nil, fmt.Errorf("contract: no operations in the specification")
	}
	return c, nil
}

// RouteFor returns the operation that a request served by the server's mux
// was routed to, given its r.Pattern.
func (c *Contract) RouteFor(pattern string) (*routers.Route, bool) {
	route, ok := c.routes[pattern]
	return route, ok
}

// Match finds the operation for a request that has not been routed.
func (c *Contract) Match(r *http.Request) (*routers.Route, bool) {
	_, pattern := c.mux.Handler(r)
	return c.RouteFor(pattern)
}

// options are shared by request and response validation.
func options() *openapi3filter.Options {
	return &openapi3filter.Options{
		// Report every problem, not only the first one.
		MultiError: true,
		// Authentication is the server's job (one middleware, SPEC section
		// 7); the contract only declares the schemes.
		AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
	}
}

// ValidateRequest checks r against route. pathParams holds the values of
// the route's path parameters. The body, if any, must already be buffered:
// validation reads r.Body, and this function puts the same bytes back so
// the handler can read them again.
func ValidateRequest(ctx context.Context, r *http.Request, route *routers.Route, pathParams map[string]string, body []byte) error {
	r.Body = io.NopCloser(bytes.NewReader(body))
	err := openapi3filter.ValidateRequest(ctx, &openapi3filter.RequestValidationInput{
		Request:    r,
		PathParams: pathParams,
		Route:      route,
		Options:    options(),
	})
	r.Body = io.NopCloser(bytes.NewReader(body))
	return err
}

// ValidateResponse checks a response to r against the contract: its status
// must be declared for the operation (or covered by `default`) and its
// headers and body must match the schema.
func (c *Contract) ValidateResponse(r *http.Request, status int, header http.Header, body []byte) error {
	route, ok := c.Match(r)
	if !ok {
		return fmt.Errorf("contract: %s %s is not an operation of the contract", r.Method, r.URL.Path)
	}
	opts := options()
	opts.IncludeResponseStatus = true
	return openapi3filter.ValidateResponse(r.Context(), &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request: r,
			Route:   route,
			Options: opts,
		},
		Status:  status,
		Header:  header,
		Body:    io.NopCloser(bytes.NewReader(body)),
		Options: opts,
	})
}
