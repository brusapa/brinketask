// Package storage owns the PostgreSQL connection pool and the schema
// migrations.
package storage

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/brusapa/brinketask/migrations"
)

// Open creates a connection pool and checks that the database answers.
// The caller must Close the pool.
func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		// pgx errors for a malformed URL do not include the password.
		return nil, fmt.Errorf("storage: open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("storage: ping: %w", err)
	}
	return pool, nil
}

// Migrate applies every pending migration embedded in the binary. Running it
// again with nothing pending is a no-op.
func Migrate(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) error {
	// goose works on database/sql. OpenDBFromPool wraps the pgx pool in a
	// *sql.DB that borrows its connections, so no second pool is created.
	db := stdlib.OpenDBFromPool(pool)
	defer func() { _ = db.Close() }()

	// The advisory lock makes concurrent starts (e.g. two replicas during a
	// rolling deploy) apply migrations one after the other instead of racing.
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("storage: migration lock: %w", err)
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS,
		goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("storage: migrations: %w", err)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("storage: apply migrations: %w", err)
	}
	for _, result := range results {
		logger.Info("migration applied",
			"version", result.Source.Version,
			"duration_ms", result.Duration.Milliseconds())
	}
	return nil
}
