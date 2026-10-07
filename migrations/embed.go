// Package migrations holds the SQL migrations, embedded into the binary
// (SPEC section 10). They are forward-only: a file contains only a
// "-- +goose Up" section and is never modified once merged.
package migrations

import "embed"

// FS contains every *.sql file of this directory. The //go:embed directive
// below is read by the Go compiler, which copies the files into the binary.
// It can only reach files in this directory or below, which is why this tiny
// package lives next to the SQL files.
//
//go:embed *.sql
var FS embed.FS
