// Package purge removes for good what was deleted long ago (SPEC section 8,
// D-71): tombstones are kept for 90 days so clients can sync them, then
// they go, and the sync cursor learns where (D-21).
package purge

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/brusapa/brinketask/internal/clock"
	"github.com/brusapa/brinketask/internal/storage/dbgen"
)

// Retention is how long a deleted resource and its tombstone are kept
// (SPEC section 8).
const Retention = 90 * 24 * time.Hour

// Interval is how often the server purges.
const Interval = time.Hour

// Result counts what one purge removed.
type Result struct {
	// Skipped is set when another replica held the purge lock.
	Skipped        bool
	Lists          int
	Tasks          int
	ChecklistItems int
	Tags           int
	Reminders      int
	Completions    int
	Deliveries     int
	Sessions       int
	// PurgedUpTo is the highest seq removed, 0 when nothing syncable was.
	PurgedUpTo int64
}

// Purger runs the purge.
type Purger struct {
	pool   *pgxpool.Pool
	clock  clock.Clock
	logger *slog.Logger
	// observe, when set, receives every result (metrics).
	observe func(Result)
}

// New returns a purger.
func New(pool *pgxpool.Pool, clk clock.Clock, logger *slog.Logger) *Purger {
	return &Purger{pool: pool, clock: clk, logger: logger}
}

// OnResult sets a function that receives the result of every purge.
func (p *Purger) OnResult(observe func(Result)) {
	p.observe = observe
}

// Run purges now and then every interval, until ctx ends.
func (p *Purger) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := p.Purge(ctx); err != nil && ctx.Err() == nil {
			p.logger.Error("purge failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Purge removes, in one transaction, what was deleted more than Retention
// ago, together with what depends on it, children before parents because
// of the foreign keys; then it raises purged_up_to_seq to the highest seq
// removed (D-21). It also drops deliveries older than Retention and
// expired sessions and login requests (D-71). One replica at a time: if
// another holds the lock, it does nothing.
func (p *Purger) Purge(ctx context.Context) (Result, error) {
	var r Result
	now := p.clock.Now()
	cutoff := now.Add(-Retention)
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		q := dbgen.New(tx)
		locked, err := q.TryPurgeLock(ctx)
		if err != nil {
			return err
		}
		if !locked {
			r.Skipped = true
			return nil
		}
		return purge(ctx, q, cutoff, now, &r)
	})
	if err != nil {
		return Result{}, fmt.Errorf("purge: %w", err)
	}
	if r.Lists+r.Tasks+r.ChecklistItems+r.Tags+r.Reminders+r.Deliveries+r.Sessions > 0 {
		// Counts only; nothing about the users or their tasks.
		p.logger.Info("purged",
			"lists", r.Lists, "tasks", r.Tasks, "checklist_items", r.ChecklistItems, "tags", r.Tags,
			"reminders", r.Reminders, "completions", r.Completions, "deliveries", r.Deliveries,
			"sessions", r.Sessions, "purged_up_to_seq", r.PurgedUpTo)
	}
	if p.observe != nil {
		p.observe(r)
	}
	return r, nil
}

// purge runs the steps in an order the foreign keys accept.
func purge(ctx context.Context, q *dbgen.Queries, cutoff, now time.Time, r *Result) error {
	n, err := q.PurgeDeliveries(ctx, cutoff)
	if err != nil {
		return err
	}
	r.Deliveries = int(n)

	seqs, err := q.PurgeReminders(ctx, &cutoff)
	if err != nil {
		return err
	}
	r.Reminders = r.note(seqs)

	if seqs, err = q.PurgeChecklistItems(ctx, &cutoff); err != nil {
		return err
	}
	r.ChecklistItems = r.note(seqs)

	if n, err = q.PurgeCompletions(ctx, &cutoff); err != nil {
		return err
	}
	r.Completions = int(n)

	if seqs, err = q.PurgeTasks(ctx, &cutoff); err != nil {
		return err
	}
	r.Tasks = r.note(seqs)

	if _, err = q.PurgeListMembers(ctx, &cutoff); err != nil {
		return err
	}
	if seqs, err = q.PurgeLists(ctx, &cutoff); err != nil {
		return err
	}
	r.Lists = r.note(seqs)

	if seqs, err = q.PurgeTags(ctx, &cutoff); err != nil {
		return err
	}
	r.Tags = r.note(seqs)

	if r.PurgedUpTo > 0 {
		if err := q.RaisePurgedUpTo(ctx, r.PurgedUpTo); err != nil {
			return err
		}
	}

	if n, err = q.PurgeExpiredSessions(ctx, now); err != nil {
		return err
	}
	r.Sessions = int(n)
	return q.DeleteExpiredAuthRequests(ctx, now)
}

// note records the seqs a step removed and returns how many there were.
func (r *Result) note(seqs []int64) int {
	for _, s := range seqs {
		r.PurgedUpTo = max(r.PurgedUpTo, s)
	}
	return len(seqs)
}
