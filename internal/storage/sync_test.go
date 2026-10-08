package storage_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/brusapa/brinketask/internal/storage/dbgen"
	"github.com/brusapa/brinketask/internal/storage/storagetest"
)

// Concurrent writers each take a distinct seq, and together they take every
// number with no gaps (D-07).
func TestNextSeqIsUniqueAndGapless(t *testing.T) {
	ctx := context.Background()
	pool := storagetest.NewPool(t)

	const writers = 20
	seqs := make([]int64, writers)
	errs := make([]error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
				seq, err := dbgen.New(tx).NextSeq(ctx)
				seqs[i] = seq
				return err
			})
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}
	slices.Sort(seqs)
	for i, seq := range seqs {
		if want := int64(i + 1); seq != want {
			t.Fatalf("sorted seqs = %v, want 1..%d", seqs, writers)
		}
	}
}

// A second writer waits until the first commits, so a change with seq N+1
// can never be visible before the change with seq N. This is what lets
// /sync/changes use "seq > cursor" without missing writes.
func TestNextSeqSerializesWriters(t *testing.T) {
	ctx := context.Background()
	pool := storagetest.NewPool(t)

	first, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Rollback(ctx) }()
	if _, err := dbgen.New(first).NextSeq(ctx); err != nil {
		t.Fatal(err)
	}

	// A channel carries the second writer's result back to this goroutine.
	second := make(chan int64, 1)
	go func() {
		var seq int64
		_ = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			var err error
			seq, err = dbgen.New(tx).NextSeq(ctx)
			return err
		})
		second <- seq
	}()

	select {
	case seq := <-second:
		t.Fatalf("second writer got seq %d while the first was still open", seq)
	case <-time.After(300 * time.Millisecond):
	}

	if err := first.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case seq := <-second:
		if seq != 2 {
			t.Fatalf("second writer got seq %d, want 2", seq)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second writer still blocked after the first committed")
	}
}

func TestSyncStateHasExactlyOneRow(t *testing.T) {
	ctx := context.Background()
	pool := storagetest.NewPool(t)

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
