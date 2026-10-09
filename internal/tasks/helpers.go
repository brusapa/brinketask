package tasks

import (
	"errors"
	"fmt"

	"github.com/oapi-codegen/nullable"
)

// wrap adds the operation to unexpected errors and passes the errors the API
// maps to a status (not found, conflict, validation...) through unchanged,
// so callers can still recognize them with errors.Is and errors.As.
func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	var conflictErr *ConflictError
	var validationErr *ValidationError
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrBadCursor) || errors.Is(err, ErrCursorExpired) ||
		errors.As(err, &conflictErr) || errors.As(err, &validationErr) {
		return err
	}
	return fmt.Errorf("tasks: %s: %w", op, err)
}

// nullableToPointer converts a specified merge-patch value: JSON null
// becomes nil, anything else a pointer to the value.
func nullableToPointer[T any](n nullable.Nullable[T]) *T {
	if n.IsNull() {
		return nil
	}
	value := n.MustGet()
	return &value
}

// equalPointers reports whether both are nil or both point to equal values.
func equalPointers[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
