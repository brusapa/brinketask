// Package storagetest gives integration tests a migrated database of their
// own.
//
// Migrating takes far longer than copying, so a test package migrates one
// template database, the first time a test asks for a pool, and each test
// gets a fresh database cloned from it (CREATE DATABASE ... TEMPLATE
// copies files, which takes milliseconds). The databases live on the
// server of TEST_DATABASE_URL (package testdb, D-75). A package that uses
// NewPool must call Main from its TestMain, so the template is dropped
// when its tests end:
//
//	func TestMain(m *testing.M) { storagetest.Main(m) }
package storagetest

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/brusapa/brinketask/internal/storage"
	"github.com/brusapa/brinketask/internal/testdb"
)

// The package's template, made by the first NewPool call. sync.Once runs
// the migration exactly once even when tests run in parallel.
var (
	templateOnce sync.Once
	templateName string
	dropTemplate func()
	templateErr  error
)

// Main runs the tests of the package and then drops the template. It
// replaces the default test runner: in Go, a package that defines
// TestMain(m) must call m.Run itself.
func Main(m *testing.M) {
	code := m.Run()
	if dropTemplate != nil {
		dropTemplate()
	}
	os.Exit(code)
}

// NewPool returns a pool connected to a new, fully migrated database. The
// database is dropped when the test ends.
func NewPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	templateOnce.Do(func() { templateErr = makeTemplate(ctx) })
	if templateErr != nil {
		t.Fatalf("storagetest: %v", templateErr)
	}

	db, drop, err := testdb.Create(ctx, templateName)
	if err != nil {
		t.Fatalf("storagetest: %v", err)
	}
	t.Cleanup(drop)

	pool, err := storage.Open(ctx, db.URL)
	if err != nil {
		t.Fatalf("storagetest: open: %v", err)
	}
	// Cleanups run last-in first-out: the pool closes before the drop.
	t.Cleanup(pool.Close)
	return pool
}

// makeTemplate creates the template database and applies the migrations.
func makeTemplate(ctx context.Context) error {
	db, drop, err := testdb.Create(ctx, "")
	dropTemplate = drop
	if err != nil {
		return fmt.Errorf("template: %w", err)
	}
	templateName = db.Name

	pool, err := storage.Open(ctx, db.URL)
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
