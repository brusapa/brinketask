package tasks

import (
	"errors"
	"strings"
)

// ErrNotFound means the resource does not exist, belongs to someone else or
// is deleted (D-22). The API answers 404 in every case.
var ErrNotFound = errors.New("tasks: not found")

// ErrBadCursor means a pagination cursor is not one the server issued.
var ErrBadCursor = errors.New("tasks: invalid cursor")

// ErrCursorExpired means a sync cursor is older than the last purge (D-21).
var ErrCursorExpired = errors.New("tasks: sync cursor expired")

// ConflictError means the request contradicts the state of the resource
// (409). Reason is shown to the client; it never contains user content.
type ConflictError struct {
	Reason string
}

func (e *ConflictError) Error() string { return "tasks: conflict: " + e.Reason }

func conflict(reason string) error { return &ConflictError{Reason: reason} }

// FieldError names one invalid field of a request: a JSON Pointer into the
// body, or a parameter name.
type FieldError struct {
	Field   string
	Message string
}

// ValidationError lists the fields that break a rule the contract cannot
// express (422).
type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		parts[i] = f.Field + ": " + f.Message
	}
	return "tasks: invalid request: " + strings.Join(parts, "; ")
}

// invalid returns a *ValidationError for fields, or nil when there are none.
// It returns the error interface so callers can write
// `if err := invalid(fields); err != nil`.
func invalid(fields []FieldError) error {
	if len(fields) == 0 {
		return nil
	}
	return &ValidationError{Fields: fields}
}

// prefixFields puts prefix before the field pointers of a validation
// error, so an error about a reminder inside a task body names it, e.g.
// "/reminders/0/offset_minutes". Other errors pass unchanged.
func prefixFields(err error, prefix string) error {
	var v *ValidationError
	if !errors.As(err, &v) {
		return err
	}
	fields := make([]FieldError, len(v.Fields))
	for i, f := range v.Fields {
		fields[i] = FieldError{Field: prefix + f.Field, Message: f.Message}
	}
	return &ValidationError{Fields: fields}
}
