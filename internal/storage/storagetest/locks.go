package storagetest

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// HoldSeqLock takes the lock of the sync_state row (D-07) in a transaction
// of its own, so every write that asks for a seq waits there. Calling the
// returned function commits and lets the writers go; the test's cleanup
// does it too. Concurrency tests use it to stop operations at a known
// point and release them together.
func HoldSeqLock(t *testing.T, pool *pgxpool.Pool) (release func()) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "UPDATE sync_state SET seq = seq"); err != nil {
		t.Fatal(err)
	}
	released := false
	release = func() {
		if released {
			return
		}
		released = true
		if err := tx.Commit(ctx); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(release)
	return release
}

// WaitForLockWaiters blocks until n sessions of the test's database wait
// for a lock, or fails the test after ten seconds.
func WaitForLockWaiters(t *testing.T, pool *pgxpool.Pool, n int) {
	t.Helper()
	// Real time: this waits for other connections, not for domain code.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		err := pool.QueryRow(context.Background(),
			"SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'").
			Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d sessions wait for a lock, want %d", waiting, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
