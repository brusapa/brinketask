// Package testdb gives integration tests databases of their own on a
// real PostgreSQL (SPEC section 11: tests run against a real PostgreSQL,
// not a mock).
//
// The server is the one whose superuser connection URL is in the
// environment variable TEST_DATABASE_URL, normally the throwaway server of
// deploy/with-test-postgres.sh (D-75):
//
//	nix develop -c with-test-postgres make go-test
//
// Without the variable, every test that needs a database fails; none is
// skipped.
package testdb

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
)

// EnvVar names the variable that holds the server's connection URL.
const EnvVar = "TEST_DATABASE_URL"

// errNoServer explains how to provide a server; it is the whole message a
// test prints when the variable is missing.
var errNoServer = errors.New(EnvVar + " is not set: integration tests need a PostgreSQL 18 server. " +
	"Run them through deploy/with-test-postgres.sh, which starts a throwaway one, " +
	"for example `nix develop -c with-test-postgres make go-test` (D-75)")

// Each database of this process gets a distinct number from this counter.
var databases atomic.Int64

// Database is one database created on the test server.
type Database struct {
	Name string // as in CREATE DATABASE
	URL  string // connection URL, as the superuser of TEST_DATABASE_URL
}

// ServerURL returns the connection URL in TEST_DATABASE_URL, or an error
// that tells how to set it.
func ServerURL() (string, error) {
	raw := os.Getenv(EnvVar)
	if raw == "" {
		return "", errNoServer
	}
	if _, err := url.Parse(raw); err != nil {
		return "", fmt.Errorf("%s is not a URL: %w", EnvVar, err)
	}
	return raw, nil
}

// Start creates a fresh, empty database and returns its connection URL.
// The database is dropped when the test finishes.
func Start(t *testing.T) string {
	t.Helper()
	db, drop, err := Create(context.Background(), "")
	if err != nil {
		t.Fatalf("testdb: %v", err)
	}
	t.Cleanup(drop)
	return db.URL
}

// Create makes a new database on the test server: a copy of the database
// named template, or an empty one when template is "". It returns the
// database and a function that drops it, for code that outlives a single
// test (see package storagetest); tests call Start. drop is never nil and
// is safe to call when err is not nil.
//
// The name holds the process id: `go test ./...` runs the test packages as
// separate processes, in parallel, against the same server.
func Create(ctx context.Context, template string) (db Database, drop func(), err error) {
	drop = func() {}
	server, err := ServerURL()
	if err != nil {
		return Database{}, drop, err
	}

	name := fmt.Sprintf("test_%d_%d", os.Getpid(), databases.Add(1))
	// template0 is empty by definition; template1, PostgreSQL's default,
	// may hold whatever the server's administrator added to it.
	source := "template0"
	if template != "" {
		source = template
	}
	// Identifier.Sanitize quotes the names, as SQL identifiers.
	statement := "CREATE DATABASE " + pgx.Identifier{name}.Sanitize() +
		" TEMPLATE " + pgx.Identifier{source}.Sanitize()
	if err := exec(ctx, server, statement); err != nil {
		return Database{}, drop, fmt.Errorf("create database: %w", err)
	}

	drop = func() {
		// FORCE closes connections a failed test may have left open.
		_ = exec(context.Background(), server,
			"DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	}
	return Database{Name: name, URL: withDatabase(server, name)}, drop, nil
}

// exec runs a statement on the server's default database. CREATE and DROP
// DATABASE cannot run inside a transaction, so this uses a plain
// connection instead of a pool.
func exec(ctx context.Context, server, sql string) error {
	conn, err := pgx.Connect(ctx, server)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()
	_, err = conn.Exec(ctx, sql)
	return err
}

// withDatabase returns rawURL pointing at another database on the same
// server. ServerURL has checked that the URL parses. For a Unix socket
// URL (postgresql:///postgres?host=/tmp/...) the database is the path too.
func withDatabase(rawURL, database string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		panic(err)
	}
	u.Path = "/" + database
	return u.String()
}
