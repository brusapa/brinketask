package httpapi

import (
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"

	"github.com/brusapa/brinketask/internal/httpapi/contract"
)

// MaxBodyBytes is the largest request body the API accepts (D-29).
const MaxBodyBytes = 1 << 20 // 1 MiB

// validateRequests returns a middleware that checks each request against
// the contract before the generated code decodes it:
//
//   - a body over MaxBodyBytes: 413 payload_too_large;
//   - a body with a Content-Type the operation does not declare: 415;
//   - a missing required body, or one that is not JSON: 400 malformed_request;
//   - anything else the schema forbids (types, patterns, lengths, unknown
//     fields, null in a non-nullable field): 422 validation_failed, with
//     one entry per problem in `errors`.
//
// Rules the schema cannot express (e.g. that a time zone exists) are
// checked by the handlers and answer 422 the same way.
func validateRequests(c *contract.Contract, logger *slog.Logger) MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// r.Pattern is the ServeMux pattern that routed this request;
			// the generated code registers one per operation.
			route, ok := c.RouteFor(r.Pattern)
			if !ok {
				logger.Error("route missing from the contract", "pattern", r.Pattern)
				writeProblem(w, newProblem(http.StatusInternalServerError, ProblemCodeInternal, ""))
				return
			}

			// MaxBytesReader makes the read fail once the limit is passed,
			// so an oversized body is never held in memory whole.
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
			if err != nil {
				var tooLarge *http.MaxBytesError
				if errors.As(err, &tooLarge) {
					writeProblem(w, newProblem(http.StatusRequestEntityTooLarge, ProblemCodePayloadTooLarge,
						"the request body exceeds 1 MiB"))
					return
				}
				writeProblem(w, newProblem(http.StatusBadRequest, ProblemCodeMalformedRequest,
					"the request body could not be read"))
				return
			}

			if problem, ok := checkContentType(r, route, body); !ok {
				writeProblem(w, problem)
				return
			}

			err = contract.ValidateRequest(r.Context(), r, route, pathParams(r, route), body)
			if err != nil {
				writeProblem(w, problemFromValidation(err))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// checkContentType rejects a body whose media type the operation does not
// accept. kin-openapi reports the same case only as a message, so it is
// checked here to give it its own status.
func checkContentType(r *http.Request, route *routers.Route, body []byte) (Problem, bool) {
	if len(body) == 0 || route.Operation.RequestBody == nil || route.Operation.RequestBody.Value == nil {
		return Problem{}, true
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err == nil && route.Operation.RequestBody.Value.Content.Get(mediaType) != nil {
		return Problem{}, true
	}
	accepted := make([]string, 0, len(route.Operation.RequestBody.Value.Content))
	for name := range route.Operation.RequestBody.Value.Content {
		accepted = append(accepted, name)
	}
	return newProblem(http.StatusUnsupportedMediaType, ProblemCodeUnsupportedMediaType,
		"Content-Type must be "+strings.Join(accepted, " or ")), false
}

// pathParams collects the values the mux extracted for the route's path
// parameters, which kin-openapi needs to validate them.
func pathParams(r *http.Request, route *routers.Route) map[string]string {
	params := map[string]string{}
	collect := func(list openapi3.Parameters) {
		for _, ref := range list {
			if ref.Value != nil && ref.Value.In == openapi3.ParameterInPath {
				params[ref.Value.Name] = r.PathValue(ref.Value.Name)
			}
		}
	}
	collect(route.PathItem.Parameters)
	collect(route.Operation.Parameters)
	return params
}

// problemFromValidation turns kin-openapi's errors into a problem. A body
// that cannot be decoded at all is a 400; every other violation is a 422
// listing the fields.
func problemFromValidation(err error) Problem {
	var fieldErrors []FieldError
	for _, e := range flatten(err) {
		var requestErr *openapi3filter.RequestError
		if !errors.As(e, &requestErr) {
			fieldErrors = append(fieldErrors, FieldError{Field: "", Message: e.Error()})
			continue
		}

		var parseErr *openapi3filter.ParseError
		switch {
		case requestErr.RequestBody != nil && errors.Is(requestErr.Err, openapi3filter.ErrInvalidRequired):
			return newProblem(http.StatusBadRequest, ProblemCodeMalformedRequest, "a request body is required")
		case requestErr.RequestBody != nil && errors.As(requestErr.Err, &parseErr):
			return newProblem(http.StatusBadRequest, ProblemCodeMalformedRequest, "the request body is not valid JSON")
		}

		// The field is a JSON Pointer into the body for body errors, and
		// the parameter name for parameter errors.
		prefix := ""
		if requestErr.Parameter != nil {
			prefix = requestErr.Parameter.Name
		}
		schemaErrs := schemaErrors(requestErr.Err)
		if len(schemaErrs) == 0 {
			fieldErrors = append(fieldErrors, FieldError{Field: prefix, Message: requestErr.Reason})
			continue
		}
		for _, schemaErr := range schemaErrs {
			field := prefix
			if requestErr.Parameter == nil {
				field = jsonPointer(schemaErr.JSONPointer())
			}
			fieldErrors = append(fieldErrors, FieldError{Field: field, Message: schemaErr.Reason})
		}
	}
	return newValidationProblem(fieldErrors)
}

// flatten unpacks openapi3.MultiError, which MultiError mode returns and
// which may nest, into a flat list. It checks the type of err itself
// rather than using errors.As, which would also look inside a
// RequestError and lose the request context around its causes.
func flatten(err error) []error {
	multi, ok := err.(openapi3.MultiError) //nolint:errorlint // see above
	if !ok {
		return []error{err}
	}
	var out []error
	for _, e := range multi {
		out = append(out, flatten(e)...)
	}
	return out
}

// schemaErrors returns the individual schema violations inside err.
func schemaErrors(err error) []*openapi3.SchemaError {
	var out []*openapi3.SchemaError
	for _, e := range flatten(err) {
		var schemaErr *openapi3.SchemaError
		if errors.As(e, &schemaErr) {
			out = append(out, schemaErr)
		}
	}
	return out
}

// jsonPointer builds an RFC 6901 pointer from path segments, escaping "~"
// and "/" inside them as the RFC requires.
func jsonPointer(segments []string) string {
	var b strings.Builder
	for _, s := range segments {
		b.WriteString("/")
		b.WriteString(strings.NewReplacer("~", "~0", "/", "~1").Replace(s))
	}
	if b.Len() == 0 {
		return "/"
	}
	return b.String()
}
