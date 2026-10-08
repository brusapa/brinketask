// Package storagetest gives integration tests a migrated database of their
// own.
//
// Starting a PostgreSQL container takes seconds, so a test package shares
// one container and each test gets a fresh database cloned from a migrated
// template (CREATE DATABASE ... TEMPLATE copies files, which takes
// milliseconds). A package that uses NewPool must call Main from its
// TestMain, so the container is removed when its tests end:
//
//	func TestMain(m *testing.M) { storagetest.Main(m) }
package storagetest

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/brusapa/brinketask/internal/storage"
	"github.com/brusapa/brinketask/internal/testdb"
)

const templateName = "brinketask_template"

// shared is the package's container, started by the first NewPool call.
// sync.Once runs the start exactly once even when tests run in parallel.
var (
	startOnce sync.Once
	adminURL  string // connection URL of the container's default database
	terminate func()
	startErr  error

	// Each test database gets a distinct name from this counter.
	databases atomic.Int64
)

// Main runs the tests of the package and then removes the shared container.
// It replaces the default test runner: in Go, a package that defines
// TestMain(m) must call m.Run itself.
func Main(m *testing.M) {
	code := m.Run()
	if terminate != nil {
		terminate()
	}
	os.Exit(code)
}

// NewPool returns a pool connected to a new, fully migrated database. The
// database is dropped when the test ends.
func NewPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	startOnce.Do(func() { startErr = start(ctx) })
	if startErr != nil {
		t.Fatalf("storagetest: %v", startErr)
	}

	name := fmt.Sprintf("test_%d", databases.Add(1))
	if err := adminExec(ctx, "CREATE DATABASE "+name+" TEMPLATE "+templateName); err != nil {
		t.Fatalf("storagetest: create database: %v", err)
	}
	t.Cleanup(func() {
		// FORCE closes connections a failed test may have left open.
		_ = adminExec(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)")
	})

	pool, err := storage.Open(ctx, withDatabase(adminURL, name))
	if err != nil {
		t.Fatalf("storagetest: open: %v", err)
	}
	// Cleanups run last-in first-out: the pool closes before the drop.
	t.Cleanup(pool.Close)
	return pool
}

// start runs the container and builds the migrated template database.
func start(ctx context.Context) error {
	var err error
	adminURL, terminate, err = testdb.Run(ctx)
	if err != nil {
		return err
	}
	if err := adminExec(ctx, "CREATE DATABASE "+templateName); err != nil {
		return fmt.Errorf("create template: %w", err)
	}

	pool, err := storage.Open(ctx, withDatabase(adminURL, templateName))
	if err != nil {
		return fmt.Errorf("open template: %w", err)
	}
	// A database can only be used as a template while nobody is connected
	// to it, so the pool is closed before any test clones it.
	defer pool.Close()
	if err := storage.Migrate(ctx, pool, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		return fmt.Errorf("migrate template: %w", err)
	}
	return nil
}

// adminExec runs a statement on the default database. CREATE and DROP
// DATABASE cannot run inside a transaction, so this uses a plain
// connection instead of a pool.
func adminExec(ctx context.Context, sql string) error {
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()
	_, err = conn.Exec(ctx, sql)
	return err
}

// withDatabase returns rawURL pointing at another database on the same
// server. The URLs come from testdb, so they always parse.
func withDatabase(rawURL, database string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		panic(err)
	}
	u.Path = "/" + database
	return u.String()
}
