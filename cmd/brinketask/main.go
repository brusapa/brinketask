// Command brinketask is the single server binary: API, scheduler and web
// client (SPEC section 2).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	// Embeds the IANA time zone database (about 450 KB) in the binary. The
	// distroless image has no /usr/share/zoneinfo, and time zones such as
	// users.timezone and due_tz must resolve everywhere.
	_ "time/tzdata"

	"github.com/brusapa/brinketask/internal/account"
	"github.com/brusapa/brinketask/internal/auth"
	"github.com/brusapa/brinketask/internal/clock"
	"github.com/brusapa/brinketask/internal/config"
	"github.com/brusapa/brinketask/internal/health"
	"github.com/brusapa/brinketask/internal/httpapi"
	"github.com/brusapa/brinketask/internal/session"
	"github.com/brusapa/brinketask/internal/storage"
)

// shutdownTimeout bounds how long in-flight requests may run after SIGTERM.
const shutdownTimeout = 10 * time.Second

func main() {
	if err := run(); err != nil {
		// The logger may not exist yet (configuration errors), so write directly.
		fmt.Fprintln(os.Stderr, "brinketask:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	// ctx is cancelled on SIGINT (Ctrl+C) or SIGTERM (container stop).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := storage.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	// Migrations run before the server accepts requests (SPEC section 10).
	if err := storage.Migrate(ctx, pool, logger); err != nil {
		return err
	}

	clk := clock.System{}
	sessions := session.NewManager(pool, clk, cfg.SessionIdleTimeout, cfg.SessionMaxAge)

	mux := http.NewServeMux()
	err = httpapi.Register(mux, httpapi.Server{}, logger,
		httpapi.Authenticate(sessions, logger),
	)
	if err != nil {
		return err
	}
	auth.NewHandler(cfg.OIDC, cfg.PublicURL, pool, clk, account.NewService(pool, clk), sessions, logger).
		Register(mux)
	health.Register(mux, pool, logger)

	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// ListenAndServe blocks, so it runs in its own goroutine and reports its
	// result through a channel. The buffer of 1 lets it finish even if nobody
	// reads the channel any more.
	serveErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", cfg.ListenAddr)
		serveErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		logger.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http server shutdown: %w", err)
	}
	// After a successful Shutdown, ListenAndServe returns ErrServerClosed.
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server: %w", err)
	}
	return nil
}
