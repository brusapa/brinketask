package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/brusapa/brinketask/internal/clock"
	"github.com/brusapa/brinketask/internal/storage/dbgen"
	"github.com/brusapa/brinketask/internal/tasks"
	"github.com/brusapa/brinketask/internal/webpush"
)

// Sender sends one push message; *webpush.Sender is the real one.
type Sender interface {
	Send(ctx context.Context, sub webpush.Subscription, payload []byte) error
}

// Device is a push subscription as the API shows it.
type Device = dbgen.PushSubscription

// NewDevice is a subscription a browser registers.
type NewDevice struct {
	ID       uuid.UUID
	Endpoint string
	P256dh   string
	Auth     string
	Label    *string
}

// Devices keeps the users' push subscriptions (SPEC section 4,
// push_subscriptions). They are not syncable, and deleting one is final
// (D-32).
type Devices struct {
	pool   *pgxpool.Pool
	clock  clock.Clock
	sender Sender
	logger *slog.Logger
	// allowLocal lets tests register endpoints on 127.0.0.1 (see
	// checkEndpoint); the server never sets it.
	allowLocal bool
	// testPrefix: endpoints starting with it pass checkEndpoint; set only
	// by the end-to-end test (D-73).
	testPrefix string
	// sends tracks test messages still in flight, so shutdown can wait.
	sends sync.WaitGroup
}

// NewDevices returns the device service.
func NewDevices(pool *pgxpool.Pool, clk clock.Clock, sender Sender, logger *slog.Logger) *Devices {
	return &Devices{pool: pool, clock: clk, sender: sender, logger: logger}
}

// AllowLocalEndpoints accepts http and loopback endpoints; for tests with a
// fake push service only.
func (d *Devices) AllowLocalEndpoints() {
	d.allowLocal = true
}

// AllowEndpointPrefix accepts endpoints that start with prefix, for the
// end-to-end test's fake push service (D-73, PUSH_TEST_ENDPOINT_PREFIX).
// Every other endpoint still follows the SSRF rule.
func (d *Devices) AllowEndpointPrefix(prefix string) {
	d.testPrefix = prefix
}

// List returns the caller's devices, oldest first.
func (d *Devices) List(ctx context.Context, userID uuid.UUID) ([]Device, error) {
	devices, err := dbgen.New(d.pool).ListSubscriptionsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("notify: list devices: %w", err)
	}
	return devices, nil
}

// Register stores a device, idempotent on its id and on its endpoint: a
// known one gets its keys and label refreshed and is enabled again, and an
// endpoint registered by another user moves to the caller (D-32). An id of
// another user's device with another endpoint is a conflict (D-23).
// created is false when the device existed.
func (d *Devices) Register(ctx context.Context, userID uuid.UUID, in NewDevice) (Device, bool, error) {
	if err := d.checkEndpoint(in.Endpoint); err != nil {
		return Device{}, false, &tasks.ValidationError{Fields: []tasks.FieldError{{Field: "/endpoint", Message: err.Error()}}}
	}
	if err := checkKeys(in); err != nil {
		return Device{}, false, &tasks.ValidationError{Fields: []tasks.FieldError{{Field: "/keys", Message: err.Error()}}}
	}
	var result Device
	created := false
	now := d.clock.Now()
	err := pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		q := dbgen.New(tx)
		existing, err := q.LockSubscriptionByEndpoint(ctx, in.Endpoint)
		if errors.Is(err, pgx.ErrNoRows) {
			existing, err = q.LockSubscriptionByID(ctx, in.ID)
			if err == nil && existing.UserID != userID {
				return &tasks.ConflictError{Reason: "the id belongs to another user's device"}
			}
		}
		switch {
		case err == nil:
			err = q.RefreshSubscription(ctx, dbgen.RefreshSubscriptionParams{
				ID: existing.ID, UserID: userID, P256dh: in.P256dh, Auth: in.Auth, Label: in.Label,
			})
			if err != nil {
				return err
			}
			result, err = q.GetSubscriptionForUser(ctx, dbgen.GetSubscriptionForUserParams{ID: existing.ID, UserID: userID})
			return err
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		err = q.InsertSubscription(ctx, dbgen.InsertSubscriptionParams{
			ID: in.ID, UserID: userID, Channel: "webpush", Endpoint: in.Endpoint,
			P256dh: in.P256dh, Auth: in.Auth, Label: in.Label, Now: now,
		})
		if err != nil {
			return err
		}
		created = true
		result, err = q.GetSubscriptionForUser(ctx, dbgen.GetSubscriptionForUserParams{ID: in.ID, UserID: userID})
		return err
	})
	if err != nil {
		var conflict *tasks.ConflictError
		if errors.As(err, &conflict) {
			return Device{}, false, err
		}
		return Device{}, false, fmt.Errorf("notify: register device: %w", err)
	}
	return result, created, nil
}

// Delete removes one of the caller's devices for good (D-32).
func (d *Devices) Delete(ctx context.Context, userID, id uuid.UUID) error {
	n, err := dbgen.New(d.pool).DeleteSubscriptionForUser(ctx, dbgen.DeleteSubscriptionForUserParams{ID: id, UserID: userID})
	if err != nil {
		return fmt.Errorf("notify: delete device: %w", err)
	}
	if n == 0 {
		return tasks.ErrNotFound
	}
	return nil
}

// SendTest queues a test notification to one of the caller's devices. It
// is sent at once, without a delivery row (D-32), in the background: the
// API answers 202 without waiting for the push service.
func (d *Devices) SendTest(ctx context.Context, userID, id uuid.UUID) error {
	device, err := dbgen.New(d.pool).GetSubscriptionForUser(ctx, dbgen.GetSubscriptionForUserParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return tasks.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("notify: test device: %w", err)
	}
	d.sends.Add(1)
	go func() {
		defer d.sends.Done()
		// WithoutCancel keeps the request's values but not its end: the
		// request is answered before the send is done.
		sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sendTimeout)
		defer cancel()
		sub := webpush.Subscription{Endpoint: device.Endpoint, P256dh: device.P256dh, Auth: device.Auth}
		if err := d.sender.Send(sendCtx, sub, testPayload()); err != nil {
			// The device id identifies the problem; nothing personal is logged.
			d.logger.Warn("test notification failed", "device", device.ID, "error", err)
		}
	}()
	return nil
}

// Wait blocks until the test notifications in flight are done.
func (d *Devices) Wait() {
	d.sends.Wait()
}

// sendTimeout bounds one request to a push service.
const sendTimeout = 15 * time.Second

// checkEndpoint accepts the push service URL a browser gave. The server
// posts to it, so it must be https and must not name this machine or an
// address directly, which keeps a user from pointing it at internal
// services (SSRF). Browsers' push services all use https host names.
func (d *Devices) checkEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return errors.New("not an absolute URL")
	}
	if d.allowLocal || (d.testPrefix != "" && strings.HasPrefix(endpoint, d.testPrefix)) {
		return nil
	}
	if u.Scheme != "https" {
		return errors.New("must use https")
	}
	host := u.Hostname()
	if host == "localhost" || net.ParseIP(host) != nil {
		return errors.New("must be a push service host name")
	}
	return nil
}

// checkKeys makes sure the keys can encrypt a message, so a bad
// registration fails now and not at every reminder.
func checkKeys(in NewDevice) error {
	_, err := webpush.Encrypt(webpush.Subscription{Endpoint: in.Endpoint, P256dh: in.P256dh, Auth: in.Auth}, []byte("{}"))
	if err != nil {
		return errors.New("p256dh and auth are not a browser's subscription keys")
	}
	return nil
}
