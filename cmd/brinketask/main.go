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
	"strings"
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
	"github.com/brusapa/brinketask/internal/notify"
	"github.com/brusapa/brinketask/internal/purge"
	"github.com/brusapa/brinketask/internal/session"
	"github.com/brusapa/brinketask/internal/storage"
	"github.com/brusapa/brinketask/internal/tasks"
	"github.com/brusapa/brinketask/internal/webpush"
	"github.com/brusapa/brinketask/internal/webui"
	"github.com/brusapa/brinketask/web"
)

// shutdownTimeout bounds how long in-flight requests may run after SIGTERM.
const shutdownTimeout = 10 * time.Second

func main() {
	// `brinketask vapid-keys` prints a new VAPID key pair and exits (D-68);
	// without arguments the binary is the server.
	if len(os.Args) > 1 {
		if err := command(os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "brinketask:", err)
			os.Exit(2)
		}
		return
	}
	if err := run(); err != nil {
		// The logger may not exist yet (configuration errors), so write directly.
		fmt.Fprintln(os.Stderr, "brinketask:", err)
		os.Exit(1)
	}
}

// command runs a subcommand.
func command(args []string) error {
	if len(args) != 1 || args[0] != "vapid-keys" {
		return fmt.Errorf("unknown command %q; the only one is vapid-keys", strings.Join(args, " "))
	}
	public, private, err := webpush.GenerateKeys()
	if err != nil {
		return err
	}
	// Printed as environment lines, ready for an env file. The private key
	// goes only to standard output, never to a log.
	fmt.Printf("VAPID_PUBLIC_KEY=%s\nVAPID_PRIVATE_KEY=%s\n", public, private)
	return nil
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

	sameOrigin, err := httpapi.SameOrigin(cfg.PublicURL)
	if err != nil {
		return err
	}
	limiter := httpapi.NewRateLimiter(clk, httpapi.RequestsPerSecond, httpapi.RequestBurst)

	mux := http.NewServeMux()
	// Order matters: the CSRF check needs nothing, the rate limit needs the
	// session that Authenticate resolves.
	accounts := account.NewService(pool, clk)
	taskService := tasks.NewService(pool, clk)
	// Changing the profile zone or default time recomputes reminders (SPEC
	// section 6), in the transaction of the change.
	accounts.OnSettingsChanged(taskService.RecomputeUserReminders)
	sender := webpush.NewSender(cfg.Push.Keys, cfg.Push.Subject, &http.Client{Timeout: 30 * time.Second}, clk)
	devices := notify.NewDevices(pool, clk, sender, logger)
	defer devices.Wait()
	err = httpapi.Register(mux, httpapi.NewServer(accounts, taskService, devices, cfg.Push.Keys.Public), logger,
		sameOrigin,
		httpapi.Authenticate(sessions, logger),
		httpapi.RateLimit(limiter),
	)
	if err != nil {
		return err
	}
	auth.NewHandler(cfg.OIDC, cfg.PublicURL, pool, clk, accounts, sessions, logger).
		Register(mux, sameOrigin)
	health.Register(mux, pool, logger)
	// Last: the client answers every path no other route claims.
	clientFiles, _ := web.Files()
	if err := webui.Register(mux, clientFiles, logger); err != nil {
		return err
	}

	// The reminder scheduler and the purge run in this process (SPEC
	// section 2, D-71) until shutdown, which waits for them to stop.
	scheduler := notify.NewScheduler(pool, clk, taskService, sender, cfg.ReminderMaxLateness, logger)
	defer background(ctx, func(ctx context.Context) { scheduler.Run(ctx, cfg.SchedulerInterval) })()
	purger := purge.New(pool, clk, logger)
	defer background(ctx, func(ctx context.Context) { purger.Run(ctx, purge.Interval) })()

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

// background runs work in a goroutine until ctx ends or the returned stop
// function is called; stop waits for work to return. Deferred, it makes
// shutdown wait for background jobs before the database pool closes.
func background(ctx context.Context, work func(context.Context)) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		work(ctx)
	}()
	return func() {
		cancel()
		<-done
	}
}
