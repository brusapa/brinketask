package storage

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/brusapa/brinketask/internal/testdb"
	"github.com/brusapa/brinketask/migrations"
)

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func openTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := Open(context.Background(), testdb.Start(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func schemaVersion(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var version int64
	err := pool.QueryRow(context.Background(),
		"SELECT max(version_id) FROM goose_db_version WHERE is_applied").Scan(&version)
	if err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	return version
}

func TestMigrateFromEmptyAndAgain(t *testing.T) {
	ctx := context.Background()
	pool := openTestDB(t)

	if err := Migrate(ctx, pool, discardLogger); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	first := schemaVersion(t, pool)
	if first < 1 {
		t.Fatalf("schema version = %d after migrating, want >= 1", first)
	}

	// Every start of the binary runs Migrate; with nothing pending it must
	// succeed and change nothing.
	if err := Migrate(ctx, pool, discardLogger); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if second := schemaVersion(t, pool); second != first {
		t.Errorf("schema version changed from %d to %d on a no-op run", first, second)
	}
}

func TestSyncStateHasExactlyOneRow(t *testing.T) {
	ctx := context.Background()
	pool := openTestDB(t)
	if err := Migrate(ctx, pool, discardLogger); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var seq int64
	if err := pool.QueryRow(ctx, "SELECT seq FROM sync_state").Scan(&seq); err != nil {
		t.Fatalf("read sync_state: %v", err)
	}
	if seq != 0 {
		t.Errorf("initial seq = %d, want 0", seq)
	}

	if _, err := pool.Exec(ctx, "INSERT INTO sync_state (id, seq) VALUES (false, 0)"); err == nil {
		t.Error("inserting a second sync_state row succeeded, want a constraint violation")
	}
}

func TestOpenFailsWhenDatabaseUnreachable(t *testing.T) {
	// Port 1 on localhost has no listener, so the ping must fail fast.
	_, err := Open(context.Background(), "postgres://user:pw@127.0.0.1:1/db?connect_timeout=2")
	if err == nil {
		t.Fatal("Open succeeded against an unreachable database")
	}
}

// Migrations are forward-only (CLAUDE.md). A Down section would suggest
// otherwise, so none may exist.
func TestMigrationsAreForwardOnly(t *testing.T) {
	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no migrations embedded")
	}
	for _, name := range files {
		content, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), "-- +goose Up") {
			t.Errorf("%s: missing \"-- +goose Up\" annotation", name)
		}
		if strings.Contains(string(content), "+goose Down") {
			t.Errorf("%s: contains a Down section; migrations are forward-only", name)
		}
	}
}
