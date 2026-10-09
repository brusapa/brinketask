package notify_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/brusapa/brinketask/internal/account"
	"github.com/brusapa/brinketask/internal/clock"
	"github.com/brusapa/brinketask/internal/notify"
	"github.com/brusapa/brinketask/internal/storage/storagetest"
	"github.com/brusapa/brinketask/internal/tasks"
	"github.com/brusapa/brinketask/internal/webpush"
	"github.com/brusapa/brinketask/internal/webpush/webpushtest"
)

func TestMain(m *testing.M) { storagetest.Main(m) }

// 2026-10-08 09:00 UTC, 11:00 in Madrid (the default profile zone).
var start = time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)

type env struct {
	pool      *pgxpool.Pool
	clock     *clock.Fixed
	tasks     *tasks.Service
	devices   *notify.Devices
	scheduler *notify.Scheduler
	push      *webpushtest.Service
	user      uuid.UUID
	inbox     uuid.UUID
}

func setup(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	pool := storagetest.NewPool(t)
	clk := clock.NewFixed(start)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	accounts := account.NewService(pool, clk)
	taskService := tasks.NewService(pool, clk)
	userID, err := accounts.SignIn(ctx, account.Identity{Issuer: "https://id.example.com", Subject: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := accounts.Profile(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	public, private, err := webpush.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	keys, err := webpush.ParseKeys(public, private)
	if err != nil {
		t.Fatal(err)
	}
	push := webpushtest.New(t, public)
	sender := webpush.NewSender(keys, "mailto:test@example.com", http.DefaultClient, clk)
	devices := notify.NewDevices(pool, clk, sender, logger)
	devices.AllowLocalEndpoints()
	return &env{
		pool: pool, clock: clk, tasks: taskService, devices: devices, push: push,
		scheduler: notify.NewScheduler(pool, clk, taskService, sender, 12*time.Hour, logger),
		user:      userID, inbox: profile.InboxListID,
	}
}

func (e *env) device(t *testing.T, name string) uuid.UUID {
	t.Helper()
	sub := e.push.NewDevice(t, name)
	d, _, err := e.devices.Register(context.Background(), e.user, notify.NewDevice{
		ID: uuid.New(), Endpoint: sub.Endpoint, P256dh: sub.P256dh, Auth: sub.Auth,
	})
	if err != nil {
		t.Fatal(err)
	}
	return d.ID
}

// task creates a task with an absolute reminder at `at` and returns both ids.
func (e *env) task(t *testing.T, title string, at time.Time) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	due := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	task, _, err := e.tasks.CreateTask(ctx, e.user, tasks.NewTask{
		ID: uuid.New(), ListID: e.inbox, Title: title, Position: "a", RepeatFrom: "due", DueDate: &due,
	})
	if err != nil {
		t.Fatal(err)
	}
	r, _, err := e.tasks.CreateReminder(ctx, e.user, task.ID, tasks.NewReminder{ID: uuid.New(), Kind: "absolute", At: &at})
	if err != nil {
		t.Fatal(err)
	}
	return task.ID, r.ID
}

func (e *env) tick(t *testing.T) {
	t.Helper()
	if err := e.scheduler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func (e *env) deliveries(t *testing.T) map[string]int {
	t.Helper()
	rows, err := e.pool.Query(context.Background(), "SELECT status, count(*) FROM notification_deliveries GROUP BY status")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	result := map[string]int{}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			t.Fatal(err)
		}
		result[status] = n
	}
	return result
}

func (e *env) reminderState(t *testing.T, id uuid.UUID) (next, lastFired, deleted *time.Time, version int) {
	t.Helper()
	err := e.pool.QueryRow(context.Background(),
		"SELECT next_fire_at, last_fired_at, deleted_at, version FROM reminders WHERE id = $1", id).
		Scan(&next, &lastFired, &deleted, &version)
	if err != nil {
		t.Fatal(err)
	}
	return
}

// A due reminder reaches every device of its user, readable and with the
// data the service worker needs (D-65); it is then consumed.
func TestReminderIsDelivered(t *testing.T) {
	e := setup(t)
	e.device(t, "phone")
	e.device(t, "laptop")
	taskID, reminderID := e.task(t, "Call the plumber", start.Add(10*time.Minute))

	e.tick(t)
	if len(e.push.Messages()) != 0 {
		t.Fatal("sent before it was due")
	}
	e.clock.Advance(10 * time.Minute)
	e.tick(t)

	messages := e.push.Messages()
	if len(messages) != 2 {
		t.Fatalf("messages = %d, failures %v", len(messages), e.push.Failures())
	}
	var p map[string]any
	if err := json.Unmarshal(messages[0].Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p["type"] != "reminder" || p["title"] != "Call the plumber" || p["task_id"] != taskID.String() ||
		p["reminder_id"] != reminderID.String() || p["due_date"] != "2026-10-10" || p["is_inbox"] != true ||
		p["timezone"] != "Europe/Madrid" {
		t.Errorf("payload = %v", p)
	}
	next, lastFired, _, version := e.reminderState(t, reminderID)
	if next != nil || lastFired == nil || version != 2 {
		t.Errorf("reminder after firing: next %v, last %v, version %d", next, lastFired, version)
	}
	if d := e.deliveries(t); d["sent"] != 2 {
		t.Errorf("deliveries = %v", d)
	}
	// Nothing is sent twice.
	e.clock.Advance(time.Hour)
	e.tick(t)
	if len(e.push.Messages()) != 2 {
		t.Errorf("messages after another tick = %d", len(e.push.Messages()))
	}
}

// SPEC section 6: a reminder more than REMINDER_MAX_LATENESS late is
// skipped, not sent.
func TestLateRemindersAreSkipped(t *testing.T) {
	e := setup(t)
	e.device(t, "phone")
	_, reminderID := e.task(t, "Old", start.Add(time.Minute))
	e.clock.Advance(13 * time.Hour) // the server was down
	e.tick(t)
	if len(e.push.Messages()) != 0 {
		t.Error("a late reminder was sent")
	}
	if d := e.deliveries(t); d["skipped"] != 1 {
		t.Errorf("deliveries = %v", d)
	}
	if next, _, _, _ := e.reminderState(t, reminderID); next != nil {
		t.Error("the late reminder is still pending")
	}
}

// D-70: with no device the reminder is consumed all the same.
func TestReminderWithoutDevices(t *testing.T) {
	e := setup(t)
	_, reminderID := e.task(t, "Alone", start.Add(time.Minute))
	e.clock.Advance(time.Minute)
	e.tick(t)
	if next, last, _, _ := e.reminderState(t, reminderID); next != nil || last == nil {
		t.Errorf("not consumed: next %v, last %v", next, last)
	}
	// A device added later does not receive it.
	e.device(t, "phone")
	e.tick(t)
	if len(e.push.Messages()) != 0 {
		t.Error("an old reminder reached a new device")
	}
}

// D-66: retries after 30 s, 1 min, 2 min, 4 min; the fifth failure is
// final.
func TestRetries(t *testing.T) {
	e := setup(t)
	e.device(t, "phone")
	e.task(t, "Flaky", start)
	e.push.Answer("phone", 500, 503, 500, 500, 500)

	waits := []time.Duration{0, 30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute}
	for i, wait := range waits {
		e.clock.Advance(wait - time.Second)
		e.tick(t)
		if i > 0 && e.attempts(t) != i {
			t.Fatalf("attempt %d ran before its time", i+1)
		}
		e.clock.Advance(time.Second)
		e.tick(t)
		if got := e.attempts(t); got != i+1 {
			t.Fatalf("after wait %v: attempts = %d, want %d", wait, got, i+1)
		}
	}
	if d := e.deliveries(t); d["failed"] != 1 {
		t.Errorf("deliveries = %v", d)
	}
}

func (e *env) attempts(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), "SELECT attempts FROM notification_deliveries").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A retry that succeeds sends the message once.
func TestRetryThenSuccess(t *testing.T) {
	e := setup(t)
	e.device(t, "phone")
	e.task(t, "Eventually", start)
	e.push.Answer("phone", 500)
	e.tick(t)
	e.clock.Advance(30 * time.Second)
	e.tick(t)
	if len(e.push.Messages()) != 1 || e.deliveries(t)["sent"] != 1 {
		t.Errorf("messages %d, deliveries %v", len(e.push.Messages()), e.deliveries(t))
	}
}

// SPEC section 6: a 404 or 410 disables the device; it gets nothing more.
func TestGoneDeviceIsDisabled(t *testing.T) {
	e := setup(t)
	deviceID := e.device(t, "phone")
	e.task(t, "First", start)
	e.push.Answer("phone", http.StatusGone)
	e.tick(t)
	var disabled *time.Time
	if err := e.pool.QueryRow(context.Background(), "SELECT disabled_at FROM push_subscriptions WHERE id = $1", deviceID).Scan(&disabled); err != nil || disabled == nil {
		t.Fatalf("device not disabled (%v)", err)
	}
	e.task(t, "Second", start.Add(time.Minute))
	e.clock.Advance(time.Minute)
	e.tick(t)
	if d := e.deliveries(t); d["failed"] != 1 || len(d) != 1 {
		t.Errorf("deliveries = %v", d)
	}
}

// D-32: a snooze reminder becomes a tombstone once it fires.
func TestSnoozeBecomesATombstone(t *testing.T) {
	e := setup(t)
	taskID, _ := e.task(t, "Snoozed", start.Add(time.Hour))
	snooze, _, err := e.tasks.Snooze(context.Background(), e.user, taskID, uuid.New(), start.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(5 * time.Minute)
	e.tick(t)
	if _, _, deleted, _ := e.reminderState(t, snooze.ID); deleted == nil {
		t.Error("the fired snooze is not a tombstone")
	}
}

// SPEC section 11: no duplicate deliveries. Two schedulers tick at the same
// time over many due reminders; each reminder reaches each device once.
func TestConcurrentSchedulersSendOnce(t *testing.T) {
	e := setup(t)
	e.device(t, "phone")
	e.device(t, "tablet")
	const reminders = 30
	for range reminders {
		e.task(t, "Due", start)
	}
	other := notify.NewScheduler(e.pool, e.clock, e.tasks, webpushSender(t, e), 12*time.Hour,
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, s := range []*notify.Scheduler{e.scheduler, other} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.Tick(context.Background())
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := len(e.push.Messages()); got != reminders*2 {
		t.Errorf("messages = %d, want %d", got, reminders*2)
	}
	if d := e.deliveries(t); d["sent"] != reminders*2 || len(d) != 1 {
		t.Errorf("deliveries = %v", d)
	}
}

// webpushSender is the scheduler's sender as setup built it; a second
// scheduler needs the same keys, which the fake service checks.
func webpushSender(t *testing.T, e *env) notify.Sender {
	t.Helper()
	return notify.SenderOf(e.scheduler)
}

// recorder is an Observer that keeps what it is told.
type recorder struct {
	mu       sync.Mutex
	fired    []time.Duration
	outcomes []string
}

func (r *recorder) ReminderFired(late time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fired = append(r.fired, late)
}

func (r *recorder) Delivery(outcome string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.outcomes = append(r.outcomes, outcome)
}

// D-72: the scheduler reports what it does, for metrics.
func TestSchedulerReportsToObserver(t *testing.T) {
	e := setup(t)
	rec := &recorder{}
	e.scheduler.Observe(rec)
	e.device(t, "phone")
	e.task(t, "Observed", start)
	e.push.Answer("phone", 500)

	e.clock.Advance(5 * time.Second)
	e.tick(t) // fires 5 s late; the first attempt fails
	e.clock.Advance(30 * time.Second)
	e.tick(t) // the retry succeeds

	if len(rec.fired) != 1 || rec.fired[0] != 5*time.Second {
		t.Errorf("fired = %v, want one 5 s late", rec.fired)
	}
	if !slices.Equal(rec.outcomes, []string{"retry", "sent"}) {
		t.Errorf("outcomes = %v", rec.outcomes)
	}
}
