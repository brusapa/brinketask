package storage

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

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

// Migrations 00007 and 00008 (D-66, D-10): a delivery still pending from
// before them has no recorded occurrence, and its task may have advanced
// since the reminder fired. The upgrade skips it rather than keeping the
// task's current date, which would send the next occurrence in the old
// reminder.
func TestUpgradeSkipsDeliveriesOfUnknownOccurrence(t *testing.T) {
	ctx := context.Background()
	pool := openTestDB(t)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 6); err != nil {
		t.Fatalf("migrate to 6: %v", err)
	}

	// Before the upgrade: a daily task's 2026-10-08 reminder fired, its
	// first attempt failed, and completing that occurrence moved the task
	// to 2026-10-09. A second delivery was sent before.
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, oidc_issuer, oidc_subject, created_at, updated_at)
		VALUES ('00000000-0000-7000-8000-000000000001', 'https://id.example.com', 'alice', now(), now());
		INSERT INTO lists (id, owner_id, name, position, is_inbox, version, seq, created_at, updated_at)
		VALUES ('00000000-0000-7000-8000-000000000002', '00000000-0000-7000-8000-000000000001', 'Inbox', 'a0', true, 1, 1, now(), now());
		INSERT INTO list_members (list_id, user_id, role)
		VALUES ('00000000-0000-7000-8000-000000000002', '00000000-0000-7000-8000-000000000001', 'owner');
		INSERT INTO tasks (id, list_id, title, status, priority, position, due_date, rrule, recurrence_start,
		                   repeat_from, version, seq, created_at, updated_at)
		VALUES ('00000000-0000-7000-8000-000000000003', '00000000-0000-7000-8000-000000000002', 'Daily', 'open', 0, 'a',
		        '2026-10-09', 'FREQ=DAILY', '2026-10-08', 'due', 2, 2, now(), now());
		INSERT INTO reminders (id, task_id, kind, at, version, seq, created_at, updated_at)
		VALUES ('00000000-0000-7000-8000-000000000004', '00000000-0000-7000-8000-000000000003', 'absolute',
		        '2026-10-08T09:00:00Z', 1, 3, now(), now());
		INSERT INTO push_subscriptions (id, user_id, channel, endpoint, p256dh, auth, created_at)
		VALUES ('00000000-0000-7000-8000-000000000005', '00000000-0000-7000-8000-000000000001', 'webpush',
		        'https://push.example.com/1', 'key', 'auth', now());
		INSERT INTO notification_deliveries (id, reminder_id, fire_at, subscription_id, status, attempts, next_attempt_at, created_at)
		VALUES ('00000000-0000-7000-8000-000000000006', '00000000-0000-7000-8000-000000000004', '2026-10-08T09:00:00Z',
		        '00000000-0000-7000-8000-000000000005', 'pending', 1, '2026-10-08T09:00:30Z', now()),
		       ('00000000-0000-7000-8000-000000000007', '00000000-0000-7000-8000-000000000004', '2026-10-07T09:00:00Z',
		        '00000000-0000-7000-8000-000000000005', 'sent', 1, NULL, now())`)
	if err != nil {
		t.Fatal(err)
	}

	if err := Migrate(ctx, pool, discardLogger); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	rows, err := pool.Query(ctx, `
		SELECT status, next_attempt_at IS NULL, occurrence_due_date IS NULL
		FROM notification_deliveries ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var status string
		var noAttempt, noOccurrence bool
		if err := rows.Scan(&status, &noAttempt, &noOccurrence); err != nil {
			t.Fatal(err)
		}
		got = append(got, fmt.Sprintf("%s next_attempt_null=%t occurrence_null=%t", status, noAttempt, noOccurrence))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{
		// 00007 filled in a guess; the delivery no longer goes out.
		"skipped next_attempt_null=true occurrence_null=false",
		"sent next_attempt_null=true occurrence_null=true",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("deliveries after the upgrade:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
