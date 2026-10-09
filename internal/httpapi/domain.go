package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/brusapa/brinketask/internal/tasks"
)

// domainProblem maps the errors package tasks returns for a rejected
// request to their problem. ok is false for any other error, which the
// handler returns as is and the client sees as a generic 500.
func domainProblem(err error) (problem Problem, ok bool) {
	var conflictErr *tasks.ConflictError
	var validationErr *tasks.ValidationError
	switch {
	case errors.Is(err, tasks.ErrNotFound):
		return newProblem(http.StatusNotFound, ProblemCodeNotFound, ""), true
	case errors.Is(err, tasks.ErrBadCursor):
		return newProblem(http.StatusBadRequest, ProblemCodeMalformedRequest, "the cursor is not valid"), true
	case errors.Is(err, tasks.ErrCursorExpired):
		return newProblem(http.StatusGone, ProblemCodeCursorExpired,
			"the cursor is older than tombstone retention; sync again without a cursor"), true
	case errors.As(err, &conflictErr):
		return newProblem(http.StatusConflict, ProblemCodeConflict, conflictErr.Reason), true
	case errors.As(err, &validationErr):
		fields := make([]FieldError, len(validationErr.Fields))
		for i, f := range validationErr.Fields {
			fields[i] = FieldError{Field: f.Field, Message: f.Message}
		}
		return newValidationProblem(fields), true
	}
	return Problem{}, false
}

// callerID returns the id of the user Authenticate resolved.
func callerID(ctx context.Context) (uuid.UUID, error) {
	s, ok := sessionFrom(ctx)
	if !ok {
		return uuid.UUID{}, errNoSession
	}
	return s.UserID, nil
}
