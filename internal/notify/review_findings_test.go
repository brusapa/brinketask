package notify_test

// Reproductions of findings in REVIEW.md that are confirmed. Each test
// describes the behaviour the specification asks for and fails until the
// finding is fixed.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/brusapa/brinketask/internal/account"
	"github.com/brusapa/brinketask/internal/notify"
	"github.com/brusapa/brinketask/internal/tasks"
)

// H1 (SPEC section 7, D-32): when a browser moves to another user, a
// delivery still queued for the previous owner's reminder is not sent to
// it. Otherwise the new user's browser shows the previous owner's task.
func TestReviewH1ReassignedDeviceGetsNoQueuedReminderOfPreviousOwner(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	sub := e.push.NewDevice(t, "shared")
	if _, _, err := e.devices.Register(ctx, e.user, notify.NewDevice{
		ID: uuid.New(), Endpoint: sub.Endpoint, P256dh: sub.P256dh, Auth: sub.Auth,
	}); err != nil {
		t.Fatal(err)
	}
	e.task(t, "Alice secret", start)
	e.push.Answer("shared", http.StatusServiceUnavailable)
	e.tick(t) // the first attempt fails; a retry is pending

	// Bob signs in on the same browser, which registers again (D-32).
	bob, err := account.NewService(e.pool, e.clock).SignIn(ctx, account.Identity{Issuer: "https://id.example.com", Subject: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.devices.Register(ctx, bob, notify.NewDevice{
		ID: uuid.New(), Endpoint: sub.Endpoint, P256dh: sub.P256dh, Auth: sub.Auth,
	}); err != nil {
		t.Fatal(err)
	}

	e.clock.Advance(30 * time.Second)
	e.tick(t)
	for _, m := range e.push.Messages() {
		if bytes.Contains(m.Payload, []byte("Alice secret")) {
			t.Errorf("bob's device received alice's reminder: %s", m.Payload)
		}
	}
}

// M4 (SPEC section 6): editing only a task's title does not cancel a
// reminder that is due and not yet picked up by the scheduler. The title
// is not one of the inputs that recompute next_fire_at.
func TestReviewM4TitleEditKeepsDueReminder(t *testing.T) {
	e := setup(t)
	e.device(t, "phone")
	taskID, _ := e.task(t, "Call the plumber", start.Add(time.Second))
	e.clock.Advance(2 * time.Second) // due, but the next poll is later

	title := "Call the plumber today"
	if _, err := e.tasks.PatchTask(context.Background(), e.user, taskID, tasks.TaskPatch{Title: &title}); err != nil {
		t.Fatal(err)
	}
	e.tick(t)
	if got := len(e.push.Messages()); got != 1 {
		t.Errorf("messages = %d, want the reminder, one second late", got)
	}
}

// M5 (D-10): a retried delivery speaks of the occurrence its reminder
// fired for. Once that occurrence is completed, a retry must not present
// the next one, whose Complete action would complete it.
func TestReviewM5RetryDoesNotPresentNextOccurrence(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.device(t, "phone")
	today := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	rule := "FREQ=DAILY"
	task, _, err := e.tasks.CreateTask(ctx, e.user, tasks.NewTask{
		ID: uuid.New(), ListID: e.inbox, Title: "Daily", Position: "a", RepeatFrom: "due", DueDate: &today, Rrule: &rule,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.tasks.CreateReminder(ctx, e.user, task.ID, tasks.NewReminder{ID: uuid.New(), Kind: "absolute", At: &start}); err != nil {
		t.Fatal(err)
	}
	e.push.Answer("phone", http.StatusServiceUnavailable)
	e.tick(t) // the first attempt fails

	if _, err := e.tasks.Complete(ctx, e.user, task.ID, tasks.CompleteInput{CompletionID: uuid.New(), OccurrenceDueDate: &today}); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(30 * time.Second)
	e.tick(t)
	for _, m := range e.push.Messages() {
		var p map[string]any
		if err := json.Unmarshal(m.Payload, &p); err != nil {
			t.Fatal(err)
		}
		if p["due_date"] == "2026-10-09" {
			t.Errorf("the retry of the 2026-10-08 reminder presents the next occurrence: %v", p)
		}
	}
}

// M5 (SPEC section 6): a device disabled by a 404 or 410 gets nothing
// more, including deliveries that were already queued for it.
func TestReviewM5DisabledDeviceGetsNoQueuedDelivery(t *testing.T) {
	e := setup(t)
	e.device(t, "phone")
	e.task(t, "First", start)
	e.task(t, "Second", start)
	// Both first attempts fail; the first retry learns the device is gone.
	e.push.Answer("phone", http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusGone)
	e.tick(t)
	e.clock.Advance(30 * time.Second)
	e.tick(t)
	if got := len(e.push.Messages()); got != 0 {
		t.Errorf("a disabled device received %d message(s)", got)
	}
}

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
