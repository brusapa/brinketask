package notify_test

// Reproductions of findings in REVIEW.md that are confirmed. Each test
// describes the behaviour the specification asks for and fails until the
// finding is fixed.

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/brusapa/brinketask/internal/notify"
)

// M7 (D-32, /push/subscriptions): a browser that renewed its subscription
// registers the new endpoint under its known device id; reminders then go
// to the new endpoint.
func TestReviewM7RenewedSubscriptionMovesToNewEndpoint(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	id := uuid.New()
	old := e.push.NewDevice(t, "old")
	if _, _, err := e.devices.Register(ctx, e.user, notify.NewDevice{ID: id, Endpoint: old.Endpoint, P256dh: old.P256dh, Auth: old.Auth}); err != nil {
		t.Fatal(err)
	}
	renewed := e.push.NewDevice(t, "renewed")
	if _, _, err := e.devices.Register(ctx, e.user, notify.NewDevice{ID: id, Endpoint: renewed.Endpoint, P256dh: renewed.P256dh, Auth: renewed.Auth}); err != nil {
		t.Fatal(err)
	}

	var endpoint string
	if err := e.pool.QueryRow(ctx, "SELECT endpoint FROM push_subscriptions WHERE id = $1", id).Scan(&endpoint); err != nil {
		t.Fatal(err)
	}
	if endpoint != renewed.Endpoint {
		t.Errorf("stored endpoint = %s, want the renewed %s", endpoint, renewed.Endpoint)
	}
	e.task(t, "Renewed", start)
	e.tick(t)
	if len(e.push.Messages()) != 1 || len(e.push.Failures()) != 0 {
		t.Errorf("messages %d, failures %v", len(e.push.Messages()), e.push.Failures())
	}
}

// M8 (D-04): registering the same new device from several requests at
// once is idempotent: every request succeeds with the same device. A
// trigger slows the insert so the requests overlap every time.
func TestReviewM8ConcurrentRegistrationIsIdempotent(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	_, err := e.pool.Exec(ctx, `
		CREATE FUNCTION review_slow_insert() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN PERFORM pg_sleep(0.2); RETURN NEW; END $$;
		CREATE TRIGGER review_slow_insert BEFORE INSERT ON push_subscriptions
		FOR EACH ROW EXECUTE FUNCTION review_slow_insert();`)
	if err != nil {
		t.Fatal(err)
	}
	sub := e.push.NewDevice(t, "phone")
	in := notify.NewDevice{ID: uuid.New(), Endpoint: sub.Endpoint, P256dh: sub.P256dh, Auth: sub.Auth}

	const requests = 4
	var wg sync.WaitGroup
	errs := make(chan error, requests)
	for range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := e.devices.Register(ctx, e.user, in)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent registration: %v", err)
		}
	}
}
