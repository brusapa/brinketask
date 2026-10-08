// Package tasks owns lists, tasks, checklist items, tags and completion
// records: their rules, their authorization and their sync bookkeeping.
//
// Every read and write is filtered by the caller's membership in
// list_members (CLAUDE.md); tags are filtered by owner. Every write to a
// syncable row increments its version and gives it a new seq from the
// global counter in the same transaction (D-07).
package tasks

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/brusapa/brinketask/internal/clock"
	"github.com/brusapa/brinketask/internal/storage/dbgen"
)

// RestoreWindow is how long a deleted list or task can be restored (SPEC
// section 8).
const RestoreWindow = 30 * 24 * time.Hour

// Service implements the operations of phase 2.
type Service struct {
	pool  *pgxpool.Pool
	clock clock.Clock
}

// NewService returns a Service backed by pool, reading time from clk.
func NewService(pool *pgxpool.Pool, clk clock.Clock) *Service {
	return &Service{pool: pool, clock: clk}
}

// inTx runs fn in a transaction, committing when it returns nil.
func (s *Service) inTx(ctx context.Context, fn func(q *dbgen.Queries) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return fn(dbgen.New(tx))
	})
}

// withRetry runs a creation once more when it lost a race: two requests
// inserting the same client-generated id at the same time. The loser's
// insert fails on the primary key; on the second run it finds the row and
// answers as an idempotent repeat (D-04).
func withRetry[T any](fn func() (T, bool, error)) (T, bool, error) {
	result, created, err := fn()
	if isUniqueViolation(err) {
		return fn()
	}
	return result, created, err
}

// restorable reports whether a resource deleted at deletedAt may still be
// restored at now.
func restorable(deletedAt, now time.Time) bool {
	return now.Sub(deletedAt) < RestoreWindow
}

func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// isUniqueViolation reports a PostgreSQL unique_violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// reserveSeqs takes n consecutive values of the change counter (D-07) and
// returns them in order.
func reserveSeqs(ctx context.Context, q *dbgen.Queries, n int) ([]int64, error) {
	if n == 0 {
		return nil, nil
	}
	last, err := q.ReserveSeqs(ctx, int64(n))
	if err != nil {
		return nil, err
	}
	seqs := make([]int64, n)
	for i := range seqs {
		seqs[i] = last - int64(n) + 1 + int64(i)
	}
	return seqs, nil
}

// constraintOf returns the name of the constraint a PostgreSQL error is
// about, or "".
func constraintOf(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.ConstraintName
	}
	return ""
}
