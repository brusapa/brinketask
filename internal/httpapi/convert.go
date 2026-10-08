package httpapi

import (
	"time"

	"github.com/oapi-codegen/nullable"

	"github.com/brusapa/brinketask/internal/tasks"
)

// Conversions between the rows of package tasks and the generated API
// types. The generated types use pointers for optional fields and
// nullable.Nullable for fields that may be null.

func listToAPI(l tasks.List) List {
	version := int(l.Version)
	isInbox := l.IsInbox
	role := ListRole(l.Role)
	return List{
		Id:        l.ID,
		Name:      l.Name,
		Color:     pointerToNullable(l.Color),
		Position:  l.Position,
		IsInbox:   &isInbox,
		Role:      &role,
		Version:   &version,
		CreatedAt: timePointer(l.CreatedAt),
		UpdatedAt: timePointer(l.UpdatedAt),
		DeletedAt: pointerToNullable(l.DeletedAt),
	}
}

// pointerToNullable maps nil to JSON null and anything else to its value.
func pointerToNullable[T any](p *T) nullable.Nullable[T] {
	if p == nil {
		return nullable.NewNullNullable[T]()
	}
	return nullable.NewNullableWithValue(*p)
}

// timePointer returns a pointer to a copy of t, in UTC as the API requires
// (SPEC section 8).
func timePointer(t time.Time) *time.Time {
	utc := t.UTC()
	return &utc
}

// nullableToPointer converts an optional, nullable request field where
// absent and null mean the same (no value): nil, or a pointer to the value.
func nullableToPointer[T any](n nullable.Nullable[T]) *T {
	if !n.IsSpecified() || n.IsNull() {
		return nil
	}
	value := n.MustGet()
	return &value
}

func tagToAPI(t tasks.Tag) Tag {
	version := int(t.Version)
	return Tag{
		Id:        t.ID,
		Name:      t.Name,
		Color:     pointerToNullable(t.Color),
		Version:   &version,
		CreatedAt: timePointer(t.CreatedAt),
		UpdatedAt: timePointer(t.UpdatedAt),
		DeletedAt: pointerToNullable(t.DeletedAt),
	}
}
