package purge_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/brusapa/brinketask/internal/account"
	"github.com/brusapa/brinketask/internal/clock"
	"github.com/brusapa/brinketask/internal/purge"
	"github.com/brusapa/brinketask/internal/storage/storagetest"
	"github.com/brusapa/brinketask/internal/tasks"
)

func TestMain(m *testing.M) { storagetest.Main(m) }

var start = time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC)

type env struct {
	ctx    context.Context
	pool   *pgxpool.Pool
	clock  *clock.Fixed
	tasks  *tasks.Service
	purger *purge.Purger
	user   uuid.UUID
	inbox  uuid.UUID
}

func setup(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	pool := storagetest.NewPool(t)
	clk := clock.NewFixed(start)
	accounts := account.NewService(pool, clk)
	user, err := accounts.SignIn(ctx, account.Identity{Issuer: "https://id.example.com", Subject: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := accounts.Profile(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	return &env{
		ctx: ctx, pool: pool, clock: clk, tasks: tasks.NewService(pool, clk),
		purger: purge.New(pool, clk, slog.New(slog.NewTextHandler(io.Discard, nil))),
		user:   user, inbox: profile.InboxListID,
	}
}

func (e *env) task(t *testing.T, list uuid.UUID, title string) uuid.UUID {
	t.Helper()
	due := time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)
	task, _, err := e.tasks.CreateTask(e.ctx, e.user, tasks.NewTask{
		ID: uuid.New(), ListID: list, Title: title, Position: "a", RepeatFrom: "due", DueDate: &due,
	})
	if err != nil {
		t.Fatal(err)
	}
	return task.ID
}

func (e *env) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	// table comes from the test itself, never from input.
	if err := e.pool.QueryRow(e.ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *env) exists(t *testing.T, table string, id uuid.UUID) bool {
	t.Helper()
	var found bool
	if err := e.pool.QueryRow(e.ctx, "SELECT EXISTS (SELECT 1 FROM "+table+" WHERE id = $1)", id).Scan(&found); err != nil {
		t.Fatal(err)
	}
	return found
}

func (e *env) mustPurge(t *testing.T) purge.Result {
	t.Helper()
	r, err := e.purger.Purge(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// D-71: what was deleted more than 90 days ago goes, with what depends on
// it; what is live, or deleted more recently, stays.
func TestPurgeRemovesOldTombstones(t *testing.T) {
	e := setup(t)
	ctx := e.ctx

	// A list deleted with its task, which had an item and a reminder.
	list, _, err := e.tasks.CreateList(ctx, e.user, tasks.NewList{ID: uuid.New(), Name: "Old project", Position: "b"})
	if err != nil {
		t.Fatal(err)
	}
	inList := e.task(t, list.ID, "in the list")
	item, _, err := e.tasks.CreateChecklistItem(ctx, e.user, inList, tasks.NewChecklistItem{ID: uuid.New(), Title: "step", Position: "a"})
	if err != nil {
		t.Fatal(err)
	}
	at := start.Add(48 * time.Hour)
	reminder, _, err := e.tasks.CreateReminder(ctx, e.user, inList, tasks.NewReminder{ID: uuid.New(), Kind: "absolute", At: &at})
	if err != nil {
		t.Fatal(err)
	}
	// A task deleted on its own, with a completion record.
	alone := e.task(t, e.inbox, "deleted alone")
	if _, err := e.tasks.Complete(ctx, e.user, alone, tasks.CompleteInput{CompletionID: uuid.New(), OccurrenceDueDate: dueOf(t)}); err != nil {
		t.Fatal(err)
	}
	// A live done task keeps its record forever (SPEC section 1).
	done := e.task(t, e.inbox, "done and kept")
	if _, err := e.tasks.Complete(ctx, e.user, done, tasks.CompleteInput{CompletionID: uuid.New(), OccurrenceDueDate: dueOf(t)}); err != nil {
		t.Fatal(err)
	}
	// A live task with a deleted item and a deleted tag.
	live := e.task(t, e.inbox, "live")
	oldItem, _, err := e.tasks.CreateChecklistItem(ctx, e.user, live, tasks.NewChecklistItem{ID: uuid.New(), Title: "gone", Position: "a"})
	if err != nil {
		t.Fatal(err)
	}
	tag, _, err := e.tasks.CreateTag(ctx, e.user, tasks.NewTag{ID: uuid.New(), Name: "old"})
	if err != nil {
		t.Fatal(err)
	}

	for _, err := range []error{
		e.tasks.DeleteList(ctx, e.user, list.ID),
		e.tasks.DeleteTask(ctx, e.user, alone),
		e.tasks.DeleteChecklistItem(ctx, e.user, oldItem.ID),
		e.tasks.DeleteTag(ctx, e.user, tag.ID),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	// Deleted later: kept by a purge that runs 90 days after the first ones.
	recent := e.task(t, e.inbox, "deleted recently")
	e.clock.Advance(2 * 24 * time.Hour)
	if err := e.tasks.DeleteTask(ctx, e.user, recent); err != nil {
		t.Fatal(err)
	}

	// A delivery of the doomed reminder: purging the reminder first would
	// break its foreign key, so the order of the steps is tested too.
	device := uuid.New()
	_, err = e.pool.Exec(ctx, `INSERT INTO push_subscriptions (id, user_id, channel, endpoint, p256dh, auth, created_at)
		VALUES ($1, $2, 'webpush', 'https://push.example.com/1', 'k', 'a', $3)`, device, e.user, start)
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.pool.Exec(ctx, `INSERT INTO notification_deliveries (reminder_id, fire_at, subscription_id, status, created_at)
		VALUES ($1, $2, $3, 'sent', $2)`, reminder.ID, start, device)
	if err != nil {
		t.Fatal(err)
	}

	// 89 days after the first deletions: nothing yet.
	e.clock.Set(start.Add(89 * 24 * time.Hour))
	if r := e.mustPurge(t); r.Tasks+r.Lists+r.Tags+r.ChecklistItems != 0 {
		t.Fatalf("purged too early: %+v", r)
	}

	e.clock.Set(start.Add(91 * 24 * time.Hour))
	r := e.mustPurge(t)
	if r.Lists != 1 || r.Tasks != 2 || r.ChecklistItems != 2 || r.Tags != 1 || r.Reminders != 1 ||
		r.Completions != 1 || r.Deliveries != 1 {
		t.Errorf("result = %+v", r)
	}
	for table, id := range map[string]uuid.UUID{"lists": list.ID, "tasks": inList, "checklist_items": item.ID, "reminders": reminder.ID} {
		if e.exists(t, table, id) {
			t.Errorf("%s %s survived", table, id)
		}
	}
	for _, id := range []uuid.UUID{done, live, recent} {
		if !e.exists(t, "tasks", id) {
			t.Errorf("task %s was purged", id)
		}
	}
	if n := e.count(t, "task_completions"); n != 1 {
		t.Errorf("completion records left = %d, want the done task's one", n)
	}
	if n := e.count(t, "list_members"); n != 1 {
		t.Errorf("memberships left = %d, want the inbox's", n)
	}
}

func dueOf(t *testing.T) *time.Time {
	t.Helper()
	d := time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)
	return &d
}

// D-21: a cursor older than what the purge removed answers 410; a cursor
// taken after it still works.
func TestPurgeExpiresOldCursors(t *testing.T) {
	e := setup(t)
	ctx := e.ctx
	page, err := e.tasks.Changes(ctx, e.user, nil, 500)
	if err != nil {
		t.Fatal(err)
	}
	oldCursor := page.NextCursor
	task := e.task(t, e.inbox, "short-lived")
	if err := e.tasks.DeleteTask(ctx, e.user, task); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(91 * 24 * time.Hour)
	if r := e.mustPurge(t); r.PurgedUpTo == 0 {
		t.Fatalf("nothing purged: %+v", r)
	}

	if _, err := e.tasks.Changes(ctx, e.user, &oldCursor, 500); !errors.Is(err, tasks.ErrCursorExpired) {
		t.Errorf("old cursor: err = %v, want ErrCursorExpired", err)
	}
	fresh, err := e.tasks.Changes(ctx, e.user, nil, 500)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.tasks.Changes(ctx, e.user, &fresh.NextCursor, 500); err != nil {
		t.Errorf("new cursor: %v", err)
	}
}

// D-71: old deliveries, expired sessions and login requests go too.
func TestPurgeOperationalRows(t *testing.T) {
	e := setup(t)
	ctx := e.ctx
	_, err := e.pool.Exec(ctx, `
		INSERT INTO sessions (id_hash, user_id, created_at, last_seen_at, expires_at)
		VALUES (sha256('a'), $1, $2, $2, $3), (sha256('b'), $1, $2, $2, $4)`,
		e.user, start, start.Add(-time.Hour), start.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.pool.Exec(ctx, `
		INSERT INTO auth_requests (browser_hash, state, nonce, code_verifier, created_at, expires_at)
		VALUES (sha256('c'), 's', 'n', 'v', $1, $1)`, start.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	r := e.mustPurge(t)
	if r.Sessions != 1 || e.count(t, "sessions") != 1 || e.count(t, "auth_requests") != 0 {
		t.Errorf("result %+v, sessions %d, auth requests %d", r, e.count(t, "sessions"), e.count(t, "auth_requests"))
	}
}

// One purge at a time: a second one while the first holds the lock skips.
func TestPurgeRunsOnceAtATime(t *testing.T) {
	e := setup(t)
	tx, err := e.pool.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(e.ctx) }()
	if _, err := tx.Exec(e.ctx, "SELECT pg_advisory_xact_lock(7240618253::bigint)"); err != nil {
		t.Fatal(err)
	}
	if r := e.mustPurge(t); !r.Skipped {
		t.Errorf("result = %+v, want skipped", r)
	}
}
