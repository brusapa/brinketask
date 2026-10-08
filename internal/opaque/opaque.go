// Package opaque makes the random identifiers that the server hands to a
// browser in a cookie and stores only as a hash: the session identifier
// (SPEC section 7) and the identifier that binds a login in progress to
// its browser. Someone who reads the database cannot use the hashes as
// cookies.
package opaque

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// size is the number of random bytes: 256 bits, far beyond guessing.
const size = 32

// encoding is URL-safe base64 without padding, so the value can sit in a
// cookie without escaping.
var encoding = base64.RawURLEncoding

// New returns a fresh identifier for the cookie and the hash to store.
func New() (value string, hash []byte) {
	raw := make([]byte, size)
	// crypto/rand.Read never fails on supported platforms (Go 1.24+): it
	// aborts the program instead of returning weak randomness.
	_, _ = rand.Read(raw)
	sum := sha256.Sum256(raw)
	return encoding.EncodeToString(raw), sum[:]
}

// Hash returns the stored form of a value received from a browser, or
// ok=false when it cannot be one of ours (wrong encoding or length), so
// callers can reject it without a database lookup.
func Hash(value string) (hash []byte, ok bool) {
	raw, err := encoding.DecodeString(value)
	if err != nil || len(raw) != size {
		return nil, false
	}
	sum := sha256.Sum256(raw)
	return sum[:], true
}
