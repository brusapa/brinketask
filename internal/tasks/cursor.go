package tasks

import (
	"encoding/base64"
	"encoding/json"
)

// Cursors are opaque to clients (SPEC section 8): base64url-encoded JSON of
// a struct that only this package reads. Each kind of listing has its own
// struct with a Kind field, so a cursor from one listing is rejected by
// another.

func encodeCursor(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		// The cursor structs hold only strings, numbers and booleans.
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

// decodeCursor fills v from a cursor and checks its kind. Anything that
// does not decode is ErrBadCursor.
func decodeCursor(cursor string, kind string, v interface{ cursorKind() string }) error {
	data, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return ErrBadCursor
	}
	if err := json.Unmarshal(data, v); err != nil {
		return ErrBadCursor
	}
	if v.cursorKind() != kind {
		return ErrBadCursor
	}
	return nil
}
