package httpapi

import (
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The test clock is 2026-10-08 09:00 UTC (11:00 in Madrid, the default
// profile zone); the default reminder time is 09:00.

func (a *testApp) addReminder(t *testing.T, u user, taskID uuid.UUID, body string, status int) Reminder {
	t.Helper()
	return decode[Reminder](t, a.call(t, u, http.MethodPost, "/tasks/"+taskID.String()+"/reminders", body), status)
}

func relativeBody(t *testing.T, offset int) string {
	t.Helper()
	return fmt.Sprintf(`{"id":%q,"kind":"relative","offset_minutes":%d}`, newID(t), offset)
}

func absoluteBody(t *testing.T, at string) string {
	t.Helper()
	return fmt.Sprintf(`{"id":%q,"kind":"absolute","at":%q}`, newID(t), at)
}

func nextFire(t *testing.T, r Reminder) string {
	t.Helper()
	if r.NextFireAt.IsNull() {
		return ""
	}
	return r.NextFireAt.MustGet().UTC().Format(time.RFC3339)
}

func (a *testApp) getReminders(t *testing.T, u user, taskID uuid.UUID) map[uuid.UUID]Reminder {
	t.Helper()
	task := decode[Task](t, a.call(t, u, http.MethodGet, "/tasks/"+taskID.String(), ""), http.StatusOK)
	result := map[uuid.UUID]Reminder{}
	for _, r := range *task.Reminders {
		result[r.Id] = r
	}
	return result
}

func TestCreateReminders(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.taskWith(t, alice, alice.inboxID, "Dentist", "a", `,"due_date":"2026-10-10","due_time":"10:00"`)

	id := newID(t)
	body := fmt.Sprintf(`{"id":%q,"kind":"relative","offset_minutes":15}`, id)
	r := a.addReminder(t, alice, task, body, http.StatusCreated)
	// Floating 10:00 in Madrid (UTC+2) minus 15 minutes.
	if nextFire(t, r) != "2026-10-10T07:45:00Z" || *r.Version != 1 || r.Kind != ReminderKindRelative {
		t.Errorf("reminder = %+v, next %s", r, nextFire(t, r))
	}
	// D-04: the same id again is the same reminder.
	again := a.addReminder(t, alice, task, fmt.Sprintf(`{"id":%q,"kind":"relative","offset_minutes":60}`, id), http.StatusOK)
	if *again.Version != 1 || again.OffsetMinutes.MustGet() != 15 {
		t.Errorf("repeat = %+v", again)
	}
	if _, ok := a.getReminders(t, alice, task)[id]; !ok {
		t.Error("the task does not list its reminder")
	}
	// D-23: someone else's id is a conflict.
	bob := a.signUp(t, "bob")
	bobsTask := a.taskWith(t, bob, bob.inboxID, "x", "a", `,"due_date":"2026-10-10"`)
	wantProblem(t, a.call(t, bob, http.MethodPost, "/tasks/"+bobsTask.String()+"/reminders", body), http.StatusConflict, ProblemCodeConflict)

	// D-67: an absolute reminder in the past is kept, with nothing pending.
	past := a.addReminder(t, alice, task, absoluteBody(t, "2026-10-01T10:00:00Z"), http.StatusCreated)
	if !past.NextFireAt.IsNull() {
		t.Errorf("past absolute next = %s", nextFire(t, past))
	}
	future := a.addReminder(t, alice, task, absoluteBody(t, "2026-10-09T18:30:00Z"), http.StatusCreated)
	if nextFire(t, future) != "2026-10-09T18:30:00Z" {
		t.Errorf("absolute next = %s", nextFire(t, future))
	}
}

func TestReminderValidation(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	undated := a.taskWith(t, alice, alice.inboxID, "Someday", "a", "")
	dated := a.taskWith(t, alice, alice.inboxID, "Dated", "b", `,"due_date":"2026-10-10"`)
	for _, tt := range []struct {
		name  string
		task  uuid.UUID
		body  string
		field string
	}{
		{"relative without a due date", undated, relativeBody(t, 0), "/offset_minutes"},
		{"relative with at", dated, fmt.Sprintf(`{"id":%q,"kind":"relative","offset_minutes":5,"at":"2026-10-09T10:00:00Z"}`, newID(t)), "/offset_minutes"},
		{"absolute without at", dated, fmt.Sprintf(`{"id":%q,"kind":"absolute"}`, newID(t)), "/at"},
		{"absolute with an offset", dated, fmt.Sprintf(`{"id":%q,"kind":"absolute","offset_minutes":5,"at":"2026-10-09T10:00:00Z"}`, newID(t)), "/at"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			problem := wantProblem(t, a.call(t, alice, http.MethodPost, "/tasks/"+tt.task.String()+"/reminders", tt.body),
				http.StatusUnprocessableEntity, ProblemCodeValidationFailed)
			if !slices.Contains(fieldsOf(problem), tt.field) {
				t.Errorf("fields = %v, want %s", fieldsOf(problem), tt.field)
			}
		})
	}
	// The contract itself limits the offset and the kind.
	wantProblem(t, a.call(t, alice, http.MethodPost, "/tasks/"+dated.String()+"/reminders", relativeBody(t, 40321)),
		http.StatusUnprocessableEntity, ProblemCodeValidationFailed)
	wantProblem(t, a.call(t, alice, http.MethodPost, "/tasks/"+dated.String()+"/reminders",
		fmt.Sprintf(`{"id":%q,"kind":"snooze","at":"2026-10-09T10:00:00Z"}`, newID(t))),
		http.StatusUnprocessableEntity, ProblemCodeValidationFailed)
}

// SPEC section 4: five reminders at most; snooze ones do not count.
func TestReminderLimit(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.taskWith(t, alice, alice.inboxID, "Busy", "a", `,"due_date":"2026-10-10"`)
	for i := range 5 {
		a.addReminder(t, alice, task, relativeBody(t, i), http.StatusCreated)
	}
	wantProblem(t, a.call(t, alice, http.MethodPost, "/tasks/"+task.String()+"/reminders", relativeBody(t, 99)),
		http.StatusConflict, ProblemCodeConflict)
	a.snooze(t, alice, task, newID(t), "2026-10-08T09:10:00Z", http.StatusCreated)
}

func TestPatchAndDeleteReminder(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.taskWith(t, alice, alice.inboxID, "Call", "a", `,"due_date":"2026-10-10"`)
	r := a.addReminder(t, alice, task, relativeBody(t, 0), http.StatusCreated)
	path := "/reminders/" + r.Id.String()

	// All-day: 09:00 in Madrid; 1440 minutes before is the day before.
	if nextFire(t, r) != "2026-10-10T07:00:00Z" {
		t.Fatalf("next = %s", nextFire(t, r))
	}
	patched := decode[Reminder](t, a.call(t, alice, http.MethodPatch, path, `{"offset_minutes":1440}`), http.StatusOK)
	if nextFire(t, patched) != "2026-10-09T07:00:00Z" || *patched.Version != 2 {
		t.Errorf("patched = %+v, next %s", patched, nextFire(t, patched))
	}
	same := decode[Reminder](t, a.call(t, alice, http.MethodPatch, path, `{"offset_minutes":1440}`), http.StatusOK)
	if *same.Version != 2 {
		t.Error("a no-op patch wrote (D-41)")
	}
	wantProblem(t, a.call(t, alice, http.MethodPatch, path, `{"at":"2026-10-09T10:00:00Z"}`),
		http.StatusUnprocessableEntity, ProblemCodeValidationFailed)

	if rec := a.call(t, alice, http.MethodDelete, path, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rec.Code)
	}
	if rec := a.call(t, alice, http.MethodDelete, path, ""); rec.Code != http.StatusNoContent {
		t.Errorf("second delete = %d", rec.Code)
	}
	wantProblem(t, a.call(t, alice, http.MethodPatch, path, `{"offset_minutes":5}`), http.StatusNotFound, ProblemCodeNotFound)
	if len(a.getReminders(t, alice, task)) != 0 {
		t.Error("a deleted reminder is still listed")
	}
}

// SPEC section 6: next_fire_at follows the task's due fields and status.
func TestRemindersFollowTheTask(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.taskWith(t, alice, alice.inboxID, "Meeting", "a", `,"due_date":"2026-10-10","due_time":"10:00"`)
	r := a.addReminder(t, alice, task, relativeBody(t, 0), http.StatusCreated)
	path := "/tasks/" + task.String()

	a.call(t, alice, http.MethodPatch, path, `{"due_time":"12:00"}`)
	if got := nextFire(t, a.getReminders(t, alice, task)[r.Id]); got != "2026-10-10T10:00:00Z" {
		t.Errorf("after moving the time: %s", got)
	}
	// A fixed zone: 12:00 in Tokyo.
	a.call(t, alice, http.MethodPatch, path, `{"due_tz":"Asia/Tokyo"}`)
	if got := nextFire(t, a.getReminders(t, alice, task)[r.Id]); got != "2026-10-10T03:00:00Z" {
		t.Errorf("fixed time: %s", got)
	}
	// D-26: losing the date keeps the reminder, with nothing pending.
	a.call(t, alice, http.MethodPatch, path, `{"due_date":null,"due_time":null,"due_tz":null}`)
	if got, ok := a.getReminders(t, alice, task)[r.Id]; !ok || !got.NextFireAt.IsNull() {
		t.Errorf("without a date: %+v", got)
	}
	a.call(t, alice, http.MethodPatch, path, `{"due_date":"2026-10-12"}`)
	if got := nextFire(t, a.getReminders(t, alice, task)[r.Id]); got != "2026-10-12T07:00:00Z" {
		t.Errorf("date back: %s", got)
	}
	// A dropped task has nothing pending; reopening brings it back.
	a.call(t, alice, http.MethodPatch, path, `{"status":"dropped"}`)
	if got := a.getReminders(t, alice, task)[r.Id]; !got.NextFireAt.IsNull() {
		t.Errorf("dropped: %s", nextFire(t, got))
	}
	a.call(t, alice, http.MethodPatch, path, `{"status":"open"}`)
	if got := nextFire(t, a.getReminders(t, alice, task)[r.Id]); got != "2026-10-12T07:00:00Z" {
		t.Errorf("reopened: %s", got)
	}
}

// D-67: deleting a task keeps its reminders, idle; restoring it brings
// them back. Meanwhile they answer 404, like checklist items (D-45).
func TestRemindersOfDeletedTasks(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	list := a.createList(t, alice, "Work", "b0")
	task := a.taskWith(t, alice, list.Id, "Report", "a", `,"due_date":"2026-10-10"`)
	r := a.addReminder(t, alice, task, relativeBody(t, 0), http.StatusCreated)

	a.call(t, alice, http.MethodDelete, "/tasks/"+task.String(), "")
	wantProblem(t, a.call(t, alice, http.MethodPatch, "/reminders/"+r.Id.String(), `{"offset_minutes":5}`), http.StatusNotFound, ProblemCodeNotFound)
	if pending := a.pendingReminders(t); pending != 0 {
		t.Errorf("%d reminders pending for a deleted task", pending)
	}
	restored := decode[Task](t, a.call(t, alice, http.MethodPost, "/tasks/"+task.String()+"/restore", ""), http.StatusOK)
	if len(*restored.Reminders) != 1 || nextFire(t, (*restored.Reminders)[0]) != "2026-10-10T07:00:00Z" {
		t.Errorf("after restore: %+v", *restored.Reminders)
	}

	// The same through the list.
	a.call(t, alice, http.MethodDelete, "/lists/"+list.Id.String(), "")
	if pending := a.pendingReminders(t); pending != 0 {
		t.Errorf("%d reminders pending after deleting the list", pending)
	}
	a.call(t, alice, http.MethodPost, "/lists/"+list.Id.String()+"/restore", "")
	if pending := a.pendingReminders(t); pending != 1 {
		t.Errorf("%d reminders pending after restoring the list, want 1", pending)
	}
}

func (a *testApp) pendingReminders(t *testing.T) int {
	t.Helper()
	var n int
	if err := a.pool.QueryRow(t.Context(), "SELECT count(*) FROM reminders WHERE next_fire_at IS NOT NULL AND deleted_at IS NULL").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// R-8: completing deletes absolute and snooze reminders and stops relative
// ones; undoing does not bring the deleted ones back (SPEC section 5).
func TestCompletingDropsOneOffReminders(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.taskWith(t, alice, alice.inboxID, "Pay", "a", `,"due_date":"2026-10-10"`)
	relative := a.addReminder(t, alice, task, relativeBody(t, 0), http.StatusCreated)
	a.addReminder(t, alice, task, absoluteBody(t, "2026-10-09T10:00:00Z"), http.StatusCreated)
	a.snooze(t, alice, task, newID(t), "2026-10-08T09:30:00Z", http.StatusCreated)

	id := newID(t)
	done := a.complete(t, alice, task, id, `,"occurrence_due_date":"2026-10-10"`)
	if len(*done.Task.Reminders) != 1 || (*done.Task.Reminders)[0].Id != relative.Id || !(*done.Task.Reminders)[0].NextFireAt.IsNull() {
		t.Errorf("after completing: %+v", *done.Task.Reminders)
	}
	undone := a.uncomplete(t, alice, task, id)
	if len(*undone.Task.Reminders) != 1 || nextFire(t, (*undone.Task.Reminders)[0]) != "2026-10-10T07:00:00Z" {
		t.Errorf("after undo: %+v", *undone.Task.Reminders)
	}
}

// R-8 on a recurring task: relative reminders follow the new date.
func TestRecurringAdvanceMovesReminders(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.newRecurring(t, alice, "2026-10-08", "FREQ=DAILY", `,"due_time":"18:00"`)
	r := a.addReminder(t, alice, task.Id, relativeBody(t, 30), http.StatusCreated)
	a.addReminder(t, alice, task.Id, absoluteBody(t, "2026-10-08T15:00:00Z"), http.StatusCreated)
	if nextFire(t, r) != "2026-10-08T15:30:00Z" {
		t.Fatalf("next = %s", nextFire(t, r))
	}
	advanced := a.completeOn(t, alice, task.Id, "2026-10-08")
	reminders := *advanced.Task.Reminders
	if len(reminders) != 1 || nextFire(t, reminders[0]) != "2026-10-09T15:30:00Z" {
		t.Errorf("after advancing: %+v", reminders)
	}
}

// SPEC section 11: a change of the user's zone or default time moves the
// pending reminders that depend on it, and only those.
func TestProfileChangeRecomputesReminders(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	floating := a.taskWith(t, alice, alice.inboxID, "Floating", "a", `,"due_date":"2026-10-10","due_time":"10:00"`)
	fixed := a.taskWith(t, alice, alice.inboxID, "Fixed", "b", `,"due_date":"2026-10-10","due_time":"10:00","due_tz":"Europe/Madrid"`)
	allDay := a.taskWith(t, alice, alice.inboxID, "All day", "c", `,"due_date":"2026-10-10"`)
	rf := a.addReminder(t, alice, floating, relativeBody(t, 0), http.StatusCreated)
	rx := a.addReminder(t, alice, fixed, relativeBody(t, 0), http.StatusCreated)
	ra := a.addReminder(t, alice, allDay, relativeBody(t, 0), http.StatusCreated)

	a.call(t, alice, http.MethodPatch, "/me", `{"timezone":"America/New_York"}`)
	if got := nextFire(t, a.getReminders(t, alice, floating)[rf.Id]); got != "2026-10-10T14:00:00Z" {
		t.Errorf("floating after the zone change: %s", got)
	}
	if got := a.getReminders(t, alice, fixed)[rx.Id]; nextFire(t, got) != "2026-10-10T08:00:00Z" || *got.Version != 1 {
		t.Errorf("fixed after the zone change: %s, version %d", nextFire(t, got), *got.Version)
	}
	a.call(t, alice, http.MethodPatch, "/me", `{"all_day_reminder_time":"07:30"}`)
	if got := nextFire(t, a.getReminders(t, alice, allDay)[ra.Id]); got != "2026-10-10T11:30:00Z" {
		t.Errorf("all-day after the default time change: %s", got)
	}
}

func (a *testApp) snooze(t *testing.T, u user, taskID, reminderID uuid.UUID, until string, status int) Reminder {
	t.Helper()
	body := fmt.Sprintf(`{"reminder_id":%q,"until":%q}`, reminderID, until)
	return decode[Reminder](t, a.call(t, u, http.MethodPost, "/tasks/"+taskID.String()+"/snooze", body), status)
}

// D-32: a snooze is idempotent on its id and must be in the future.
func TestSnooze(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.taskWith(t, alice, alice.inboxID, "Later", "a", "")
	id := newID(t)
	r := a.snooze(t, alice, task, id, "2026-10-08T09:10:00Z", http.StatusCreated)
	if r.Kind != ReminderKindSnooze || nextFire(t, r) != "2026-10-08T09:10:00Z" {
		t.Errorf("snooze = %+v", r)
	}
	again := a.snooze(t, alice, task, id, "2026-10-08T10:00:00Z", http.StatusOK)
	if nextFire(t, again) != "2026-10-08T09:10:00Z" {
		t.Errorf("repeat = %s", nextFire(t, again))
	}
	problem := wantProblem(t, a.call(t, alice, http.MethodPost, "/tasks/"+task.String()+"/snooze",
		fmt.Sprintf(`{"reminder_id":%q,"until":"2026-10-08T08:59:00Z"}`, newID(t))),
		http.StatusUnprocessableEntity, ProblemCodeValidationFailed)
	if !slices.Contains(fieldsOf(problem), "/until") {
		t.Errorf("fields = %v", fieldsOf(problem))
	}
}

// Reminders in the body of a task create, in the same transaction.
func TestCreateTaskWithReminders(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	body := fmt.Sprintf(`{"id":%q,"list_id":%q,"title":"Flight","position":"a","due_date":"2026-10-20","due_time":"07:00",
		"reminders":[{"id":%q,"kind":"relative","offset_minutes":120},{"id":%q,"kind":"absolute","at":"2026-10-19T18:00:00Z"}]}`,
		newID(t), alice.inboxID, newID(t), newID(t))
	task := decode[Task](t, a.call(t, alice, http.MethodPost, "/tasks", body), http.StatusCreated)
	if len(*task.Reminders) != 2 {
		t.Fatalf("reminders = %+v", *task.Reminders)
	}

	undated := fmt.Sprintf(`{"id":%q,"list_id":%q,"title":"x","position":"a","reminders":[{"id":%q,"kind":"relative","offset_minutes":0}]}`,
		newID(t), alice.inboxID, newID(t))
	problem := wantProblem(t, a.call(t, alice, http.MethodPost, "/tasks", undated), http.StatusUnprocessableEntity, ProblemCodeValidationFailed)
	if !slices.Contains(fieldsOf(problem), "/reminders/0/offset_minutes") {
		t.Errorf("fields = %v", fieldsOf(problem))
	}
	var six string
	for i := range 6 {
		if i > 0 {
			six += ","
		}
		six += fmt.Sprintf(`{"id":%q,"kind":"absolute","at":"2026-10-19T18:00:00Z"}`, newID(t))
	}
	// The contract caps the array at five, so six is a schema violation.
	wantProblem(t, a.call(t, alice, http.MethodPost, "/tasks", fmt.Sprintf(
		`{"id":%q,"list_id":%q,"title":"x","position":"a","reminders":[%s]}`, newID(t), alice.inboxID, six)),
		http.StatusUnprocessableEntity, ProblemCodeValidationFailed)
}

// Reminders sync like checklist items: live ones in the full state,
// changes and tombstones after it.
func TestRemindersAreSynced(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.taskWith(t, alice, alice.inboxID, "x", "a", `,"due_date":"2026-10-10"`)
	kept := a.addReminder(t, alice, task, relativeBody(t, 0), http.StatusCreated)
	gone := a.addReminder(t, alice, task, relativeBody(t, 60), http.StatusCreated)
	a.call(t, alice, http.MethodDelete, "/reminders/"+gone.Id.String(), "")

	full := a.changes(t, alice, "", 100)
	if len(full.Reminders) != 1 || full.Reminders[0].Id != kept.Id {
		t.Errorf("full state reminders = %+v", full.Reminders)
	}
	a.call(t, alice, http.MethodPatch, "/reminders/"+kept.Id.String(), `{"offset_minutes":30}`)
	a.call(t, alice, http.MethodDelete, "/reminders/"+kept.Id.String(), "")
	changed := a.changes(t, alice, full.NextCursor, 100)
	if len(changed.Reminders) != 1 || changed.Reminders[0].DeletedAt.IsNull() {
		t.Errorf("changes = %+v", changed.Reminders)
	}
}
