package notify_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/brusapa/brinketask/internal/notify"
)

// D-32: a browser that renewed its subscription registers the new
// endpoint under the device id it knows; reminders then go there, with
// the new keys.
func TestRenewedSubscriptionMovesToNewEndpoint(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	id := uuid.New()
	old := e.push.NewDevice(t, "old")
	if _, _, err := e.devices.Register(ctx, e.user, notify.NewDevice{ID: id, Endpoint: old.Endpoint, P256dh: old.P256dh, Auth: old.Auth}); err != nil {
		t.Fatal(err)
	}
	renewed := e.push.NewDevice(t, "renewed")
	device, created, err := e.devices.Register(ctx, e.user, notify.NewDevice{ID: id, Endpoint: renewed.Endpoint, P256dh: renewed.P256dh, Auth: renewed.Auth})
	if err != nil || created || device.ID != id || device.Endpoint != renewed.Endpoint {
		t.Fatalf("renewed registration: created %v, %+v, %v", created, device, err)
	}

	e.task(t, "Renewed", start)
	e.tick(t)
	messages := e.push.Messages()
	if len(messages) != 1 || messages[0].Device != "renewed" || len(e.push.Failures()) != 0 {
		t.Errorf("messages %+v, failures %v", messages, e.push.Failures())
	}
}

// D-04: registering the same new device from several requests at once
// is idempotent: every request succeeds with the same device. A trigger
// slows the insert so the requests overlap every time.
func TestConcurrentRegistrationIsIdempotent(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	_, err := e.pool.Exec(ctx, `
		CREATE FUNCTION slow_insert() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN PERFORM pg_sleep(0.2); RETURN NEW; END $$;
		CREATE TRIGGER slow_insert BEFORE INSERT ON push_subscriptions
		FOR EACH ROW EXECUTE FUNCTION slow_insert();`)
	if err != nil {
		t.Fatal(err)
	}
	sub := e.push.NewDevice(t, "phone")
	in := notify.NewDevice{ID: uuid.New(), Endpoint: sub.Endpoint, P256dh: sub.P256dh, Auth: sub.Auth}

	const requests = 4
	var wg sync.WaitGroup
	results := make(chan error, requests)
	for range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			device, _, err := e.devices.Register(ctx, e.user, in)
			if err == nil && device.ID != in.ID {
				t.Errorf("registered as %s, want %s", device.ID, in.ID)
			}
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Errorf("concurrent registration: %v", err)
		}
	}
}
