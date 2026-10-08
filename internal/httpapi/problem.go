package httpapi

import (
	"encoding/json"
	"net/http"
)

// Stable values of Problem.code (SPEC section 8). Clients switch on these,
// so existing values never change meaning.
const (
	codeValidationFailed = "validation_failed"
	codeNotFound         = "not_found"
	codeNotImplemented   = "not_implemented"
	codeInternal         = "internal"
)

const problemContentType = "application/problem+json"

// newProblem builds an RFC 9457 problem. "about:blank" is the RFC's type for
// problems that need no further explanation than the HTTP status.
func newProblem(status int, code string, detail string) Problem {
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

// writeProblem sends a problem from code that does not go through a
// generated response type (routing and decoding errors).
func writeProblem(w http.ResponseWriter, problem Problem) {
	w.Header().Set("Content-Type", problemContentType)
	w.WriteHeader(problem.Status)
	// An encoding error here means the client went away; there is nobody
	// left to report it to.
	_ = json.NewEncoder(w).Encode(problem)
}

// problemResponse has the same fields as every generated
// "<Operation>defaultApplicationProblemPlusJSONResponse" type.
type problemResponse struct {
	Body       Problem
	StatusCode int
}

func notImplemented() problemResponse {
	return problemResponse{
		Body:       newProblem(http.StatusNotImplemented, codeNotImplemented, ""),
		StatusCode: http.StatusNotImplemented,
	}
}
