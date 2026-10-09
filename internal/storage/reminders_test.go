package storage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/brusapa/brinketask/internal/storage/storagetest"
)

// seedTask makes a user, a list and a task with fixed ids.
func seedTask(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO users (id, oidc_issuer, oidc_subject, created_at, updated_at)
		VALUES ('00000000-0000-7000-8000-000000000001', 'i', 'a', now(), now());
		INSERT INTO lists (id, owner_id, name, position, is_inbox, version, seq, created_at, updated_at)
		VALUES ('00000000-0000-7000-8000-0000000000b1', '00000000-0000-7000-8000-000000000001', 'x', 'a0', false, 1, 1, now(), now());
		INSERT INTO tasks (id, list_id, title, status, priority, position, repeat_from, version, seq, created_at, updated_at)
		VALUES ('00000000-0000-7000-8000-0000000000c1', '00000000-0000-7000-8000-0000000000b1', 't', 'open', 0, 'a0', 'due', 1, 2, now(), now())`)
	if err != nil {
		t.Fatal(err)
	}
}

// Each kind of reminder has exactly its own fields (SPEC section 4).
func TestReminderKindFields(t *testing.T) {
	ctx := context.Background()
	pool := storagetest.NewPool(t)
	seedTask(t, pool)
	insert := func(seq int, kind string, offset *int, at *string) error {
		_, err := pool.Exec(ctx, `INSERT INTO reminders (id, task_id, kind, offset_minutes, at, version, seq, created_at, updated_at)
			VALUES (gen_random_uuid(), '00000000-0000-7000-8000-0000000000c1', $1, $2, $3::timestamptz, 1, $4, now(), now())`,
			kind, offset, at, seq)
		return err
	}
	ten := 10
	at := "2026-10-09T10:00:00Z"
	for i, tt := range []struct {
		kind   string
		offset *int
		at     *string
		ok     bool
	}{
		{"relative", &ten, nil, true},
		{"absolute", nil, &at, true},
		{"snooze", nil, &at, true},
		{"relative", nil, &at, false},
		{"relative", &ten, &at, false},
		{"absolute", &ten, nil, false},
		{"snooze", nil, nil, false},
		{"other", nil, &at, false},
	} {
		err := insert(10+i, tt.kind, tt.offset, tt.at)
		if (err == nil) != tt.ok {
			t.Errorf("%s offset=%v at=%v: err = %v", tt.kind, tt.offset != nil, tt.at != nil, err)
		}
	}
}

// A firing gets one delivery per device, however often it is inserted.
func TestDeliveriesAreUnique(t *testing.T) {
	ctx := context.Background()
	pool := storagetest.NewPool(t)
	seedTask(t, pool)
	_, err := pool.Exec(ctx, `
		INSERT INTO reminders (id, task_id, kind, at, version, seq, created_at, updated_at)
		VALUES ('00000000-0000-7000-8000-0000000000d1', '00000000-0000-7000-8000-0000000000c1', 'absolute', now(), 1, 3, now(), now());
		INSERT INTO push_subscriptions (id, user_id, channel, endpoint, p256dh, auth, created_at)
		VALUES ('00000000-0000-7000-8000-0000000000e1', '00000000-0000-7000-8000-000000000001', 'webpush', 'https://push.example/1', 'k', 'a', now())`)
	if err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO notification_deliveries (reminder_id, fire_at, subscription_id, status, next_attempt_at, created_at)
		VALUES ('00000000-0000-7000-8000-0000000000d1', '2026-10-09T10:00:00Z', '00000000-0000-7000-8000-0000000000e1', 'pending', now(), now())`
	if _, err := pool.Exec(ctx, insert); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, insert); err == nil || !strings.Contains(err.Error(), "notification_deliveries_once") {
		t.Errorf("second delivery: err = %v", err)
	}
	// Deleting the device deletes its deliveries (D-32: a hard delete).
	if _, err := pool.Exec(ctx, `DELETE FROM push_subscriptions`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM notification_deliveries`).Scan(&n); err != nil || n != 0 {
		t.Errorf("deliveries left: %d (%v)", n, err)
	}
}
