package opaque

import (
	"bytes"
	"testing"
)

func TestNewAndHash(t *testing.T) {
	value, hash := New()
	if len(value) != 43 {
		t.Errorf("value has %d characters, want 43 (32 bytes in unpadded base64)", len(value))
	}
	again, ok := Hash(value)
	if !ok || !bytes.Equal(again, hash) {
		t.Errorf("Hash(value) = %x, %v; want %x, true", again, ok, hash)
	}
	if bytes.Contains(hash, []byte(value)) {
		t.Error("hash contains the value")
	}

	other, _ := New()
	if other == value {
		t.Error("two calls returned the same value")
	}
}

func TestHashRejectsForeignValues(t *testing.T) {
	for _, value := range []string{"", "short", "not base64 at all!!", string(make([]byte, 43))} {
		if _, ok := Hash(value); ok {
			t.Errorf("Hash(%q) accepted", value)
		}
	}
}
