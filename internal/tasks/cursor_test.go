package tasks

import (
	"errors"
	"testing"
)

type testCursor struct {
	Kind string `json:"k"`
	N    int    `json:"n"`
}

func (c testCursor) cursorKind() string { return c.Kind }

func TestCursorRoundTrip(t *testing.T) {
	encoded := encodeCursor(testCursor{Kind: "a", N: 7})
	var got testCursor
	if err := decodeCursor(encoded, "a", &got); err != nil {
		t.Fatal(err)
	}
	if got.N != 7 {
		t.Errorf("N = %d, want 7", got.N)
	}
	if err := decodeCursor(encoded, "b", &got); !errors.Is(err, ErrBadCursor) {
		t.Errorf("cursor of another kind: %v, want ErrBadCursor", err)
	}
	for _, bad := range []string{"", "!!", "bm90IGpzb24"} {
		if err := decodeCursor(bad, "a", &got); !errors.Is(err, ErrBadCursor) {
			t.Errorf("decodeCursor(%q) = %v, want ErrBadCursor", bad, err)
		}
	}
}
