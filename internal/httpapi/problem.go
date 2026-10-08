package httpapi

import (
	"encoding/json"
	"net/http"
)

// The values of Problem.code are the ProblemCode constants generated from
// the contract's enum (D-28). Clients switch on them, so existing values
// never change meaning.

const problemContentType = "application/problem+json"

// newProblem builds an RFC 9457 problem. "about:blank" is the RFC's type for
// problems that need no further explanation than the HTTP status.
func newProblem(status int, code ProblemCode, detail string) Problem {
	problem := Problem{
		Type:   "about:blank",
		Title:  http.StatusText(status),
		Status: status,
		Code:   code,
	}
	if detail != "" {
		problem.Detail = &detail
	}
	return problem
}

// newValidationProblem is the 422 answer for a request that violates the
// schema or a rule, listing each offending field.
func newValidationProblem(fieldErrors []FieldError) Problem {
	problem := newProblem(http.StatusUnprocessableEntity, ProblemCodeValidationFailed, "")
	problem.Errors = &fieldErrors
	return problem
}

// writeProblem sends a problem from code that does not go through a
// generated response type (middleware, routing and decoding errors).
func writeProblem(w http.ResponseWriter, problem Problem) {
	w.Header().Set("Content-Type", problemContentType)
	w.WriteHeader(problem.Status)
	// An encoding error here means the client went away; there is nobody
	// left to report it to.
	_ = json.NewEncoder(w).Encode(problem)
}

// problemResponse has the same fields as every generated
// "<Operation>defaultApplicationProblemPlusJSONResponse" type, so a value of
// it converts to any of them.
type problemResponse struct {
	Body       Problem
	StatusCode int
}

func toResponse(problem Problem) problemResponse {
	return problemResponse{Body: problem, StatusCode: problem.Status}
}

func notImplemented() problemResponse {
	return toResponse(newProblem(http.StatusNotImplemented, ProblemCodeNotImplemented, ""))
}
