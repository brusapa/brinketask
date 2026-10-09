package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

// newRecurring creates a task with a rule and returns it.
func (a *testApp) newRecurring(t *testing.T, u user, due, rule, extra string) Task {
	t.Helper()
	body := fmt.Sprintf(`{"id":%q,"list_id":%q,"title":"recurring","position":"a0","due_date":%q,"rrule":%q%s}`,
		newID(t), u.inboxID, due, rule, extra)
	return decode[Task](t, a.call(t, u, http.MethodPost, "/tasks", body), http.StatusCreated)
}

// seriesStart reads the internal DTSTART of a task (D-56).
func (a *testApp) seriesStart(t *testing.T, id uuid.UUID) *time.Time {
	t.Helper()
	var start *time.Time
	if err := a.pool.QueryRow(context.Background(), "SELECT recurrence_start FROM tasks WHERE id = $1", id).Scan(&start); err != nil {
		t.Fatal(err)
	}
	return start
}

func wantStart(t *testing.T, got *time.Time, want string) {
	t.Helper()
	switch {
	case want == "" && got != nil:
		t.Errorf("series start = %s, want none", got.Format(time.DateOnly))
	case want != "" && (got == nil || got.Format(time.DateOnly) != want):
		t.Errorf("series start = %v, want %s", got, want)
	}
}

// D-59: rules are stored in canonical form.
func TestCreateRecurringTask(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.newRecurring(t, alice, "2026-10-12", "byday=we,mo;freq=weekly;interval=1", "")
	if rule, _ := task.Rrule.Get(); rule != "FREQ=WEEKLY;BYDAY=MO,WE" {
		t.Errorf("rrule = %q", rule)
	}
	if task.RepeatFrom != RepeatFromDue || *task.RecurrenceDoneCount != 0 {
		t.Errorf("task = %+v", task)
	}
	wantStart(t, a.seriesStart(t, task.Id), "2026-10-12")
}

// SPEC section 5, R-3 and D-26: outside the subset, or inconsistent, is 422
// on the right field.
func TestRecurrenceValidation(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	tests := []struct{ name, fields, field string }{
		{"unsupported part", `"due_date":"2026-10-12","rrule":"FREQ=DAILY;BYHOUR=9"`, "/rrule"},
		{"BYDAY with MONTHLY", `"due_date":"2026-10-12","rrule":"FREQ=MONTHLY;BYDAY=MO"`, "/rrule"},
		{"UNTIL as date-time", `"due_date":"2026-10-12","rrule":"FREQ=DAILY;UNTIL=20261231T000000Z"`, "/rrule"},
		{"no due date", `"rrule":"FREQ=DAILY"`, "/rrule"},
		{"completion mode with BYDAY", `"due_date":"2026-10-12","rrule":"FREQ=WEEKLY;BYDAY=MO","repeat_from":"completion"`, "/repeat_from"},
		{"completion mode with BYMONTHDAY", `"due_date":"2026-10-12","rrule":"FREQ=MONTHLY;BYMONTHDAY=-1","repeat_from":"completion"`, "/repeat_from"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"id":%q,"list_id":%q,"title":"x","position":"a0",%s}`, newID(t), alice.inboxID, tt.fields)
			problem := wantProblem(t, a.call(t, alice, http.MethodPost, "/tasks", body), http.StatusUnprocessableEntity, ProblemCodeValidationFailed)
			if !slices.Contains(fieldsOf(problem), tt.field) {
				t.Errorf("fields = %v, want %s", fieldsOf(problem), tt.field)
			}
		})
	}

	task := a.newRecurring(t, alice, "2026-10-12", "FREQ=WEEKLY;BYDAY=MO", "")
	path := "/tasks/" + task.Id.String()
	// Switching an existing BYDAY rule to the completion mode is refused too.
	problem := wantProblem(t, a.call(t, alice, http.MethodPatch, path, `{"repeat_from":"completion"}`),
		http.StatusUnprocessableEntity, ProblemCodeValidationFailed)
	if !slices.Contains(fieldsOf(problem), "/repeat_from") {
		t.Errorf("fields = %v", fieldsOf(problem))
	}
	// D-26: the date cannot go while the rule stays; both can go together.
	wantProblem(t, a.call(t, alice, http.MethodPatch, path, `{"due_date":null}`),
		http.StatusUnprocessableEntity, ProblemCodeValidationFailed)
	cleared := decode[Task](t, a.call(t, alice, http.MethodPatch, path, `{"due_date":null,"rrule":null}`), http.StatusOK)
	if !cleared.Rrule.IsNull() {
		t.Errorf("rrule = %v", cleared.Rrule)
	}
}

// D-56: the series starts on the due date when the rule, the mode or the
// date is set, and loses its start with the rule.
func TestSeriesStartFollowsEdits(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.newRecurring(t, alice, "2026-10-12", "FREQ=MONTHLY", "")
	path := "/tasks/" + task.Id.String()

	a.call(t, alice, http.MethodPatch, path, `{"title":"renamed"}`)
	wantStart(t, a.seriesStart(t, task.Id), "2026-10-12")
	a.call(t, alice, http.MethodPatch, path, `{"due_date":"2026-10-31"}`)
	wantStart(t, a.seriesStart(t, task.Id), "2026-10-31")
	a.call(t, alice, http.MethodPatch, path, `{"rrule":null}`)
	wantStart(t, a.seriesStart(t, task.Id), "")
	a.call(t, alice, http.MethodPatch, path, `{"rrule":"FREQ=YEARLY"}`)
	wantStart(t, a.seriesStart(t, task.Id), "2026-10-31")

	// Sending the same rule in another spelling changes nothing (D-41).
	before := decode[Task](t, a.call(t, alice, http.MethodGet, path, ""), http.StatusOK)
	same := decode[Task](t, a.call(t, alice, http.MethodPatch, path, `{"rrule":"freq=yearly;interval=1"}`), http.StatusOK)
	if *same.Version != *before.Version {
		t.Errorf("an equivalent rule wrote: version %d to %d", *before.Version, *same.Version)
	}
}

// completeOn completes a task's occurrence due on day with a new id.
func (a *testApp) completeOn(t *testing.T, u user, taskID uuid.UUID, day string) *CompletionResult {
	t.Helper()
	return a.complete(t, u, taskID, newID(t), fmt.Sprintf(`,"occurrence_due_date":%q`, day))
}

func dueOf(t *testing.T, task Task) string {
	t.Helper()
	if task.DueDate.IsNull() {
		return ""
	}
	return task.DueDate.MustGet().String()
}

// The test clock is Thursday 2026-10-08, 11:00 in Madrid, the default
// profile zone.

func TestCompleteRecurringTaskAdvances(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.newRecurring(t, alice, "2026-10-08", "FREQ=DAILY", "")
	ticked := a.createItem(t, alice, task.Id, "ticked", "a")
	a.call(t, alice, http.MethodPatch, "/checklist-items/"+ticked.Id.String(), `{"is_done":true}`)
	unticked := a.createItem(t, alice, task.Id, "unticked", "b")

	id := newID(t)
	result := a.complete(t, alice, task.Id, id, `,"occurrence_due_date":"2026-10-08"`)
	if !result.Applied || result.Task.Status != TaskStatusOpen || dueOf(t, result.Task) != "2026-10-09" ||
		*result.Task.RecurrenceDoneCount != 1 || !result.Task.CompletedAt.IsNull() {
		t.Fatalf("result = %+v", result.Task)
	}
	completion, _ := result.Completion.Get()
	if completion.Kind != CompletionKindCompleted || completion.OccurrenceDueDate.MustGet().String() != "2026-10-08" {
		t.Errorf("completion = %+v", completion)
	}

	// R-7: ticked items are unticked, each a write; the others untouched.
	items := map[uuid.UUID]ChecklistItem{}
	for _, item := range *result.Task.ChecklistItems {
		items[item.Id] = item
	}
	if items[ticked.Id].IsDone || *items[ticked.Id].Version != 3 {
		t.Errorf("ticked item after advancing = %+v", items[ticked.Id])
	}
	if *items[unticked.Id].Version != 1 {
		t.Errorf("unticked item was written: %+v", items[unticked.Id])
	}

	// D-24: the same id again changes nothing. D-10: the old occurrence
	// with a new id is stale.
	again := a.complete(t, alice, task.Id, id, `,"occurrence_due_date":"2026-10-08"`)
	if !again.Applied || dueOf(t, again.Task) != "2026-10-09" {
		t.Errorf("repeat = %+v", again.Task)
	}
	stale := a.completeOn(t, alice, task.Id, "2026-10-08")
	if stale.Applied || dueOf(t, stale.Task) != "2026-10-09" {
		t.Errorf("stale = %+v", stale.Task)
	}

	// Undo restores the date and the count; not the checklist (D-60).
	undone := a.uncomplete(t, alice, task.Id, id)
	if !undone.Applied || dueOf(t, undone.Task) != "2026-10-08" || *undone.Task.RecurrenceDoneCount != 0 {
		t.Errorf("undo = %+v", undone.Task)
	}
	for _, item := range *undone.Task.ChecklistItems {
		if item.IsDone {
			t.Errorf("undo re-ticked %s", item.Title)
		}
	}
}

// R-5 with COUNT: the last occurrence leaves the task done; undo reopens.
func TestRecurringCountEndsTheSeries(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.newRecurring(t, alice, "2026-10-08", "FREQ=DAILY;COUNT=2", "")
	first := a.completeOn(t, alice, task.Id, "2026-10-08")
	if dueOf(t, first.Task) != "2026-10-09" || first.Task.Status != TaskStatusOpen {
		t.Fatalf("after the first = %+v", first.Task)
	}
	id := newID(t)
	last := a.complete(t, alice, task.Id, id, `,"occurrence_due_date":"2026-10-09"`)
	if last.Task.Status != TaskStatusDone || last.Task.CompletedAt.IsNull() || dueOf(t, last.Task) != "2026-10-09" ||
		*last.Task.RecurrenceDoneCount != 2 {
		t.Errorf("after the last = %+v", last.Task)
	}
	reopened := a.uncomplete(t, alice, task.Id, id)
	if reopened.Task.Status != TaskStatusOpen || !reopened.Task.CompletedAt.IsNull() || *reopened.Task.RecurrenceDoneCount != 1 {
		t.Errorf("after undo = %+v", reopened.Task)
	}
}

// R-5 with UNTIL (inclusive, D-59).
func TestRecurringUntilEndsTheSeries(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.newRecurring(t, alice, "2026-10-08", "FREQ=DAILY;UNTIL=20261009", "")
	if r := a.completeOn(t, alice, task.Id, "2026-10-08"); dueOf(t, r.Task) != "2026-10-09" {
		t.Fatalf("due = %s", dueOf(t, r.Task))
	}
	if r := a.completeOn(t, alice, task.Id, "2026-10-09"); r.Task.Status != TaskStatusDone {
		t.Errorf("status = %s", r.Task.Status)
	}
}

// R-2: overdue occurrences are dropped; today counts.
func TestRecurringOverdueSkipsAhead(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.newRecurring(t, alice, "2026-09-03", "FREQ=WEEKLY", "") // a Thursday
	r := a.completeOn(t, alice, task.Id, "2026-09-03")
	if dueOf(t, r.Task) != "2026-10-08" || *r.Task.RecurrenceDoneCount != 1 {
		t.Errorf("due = %s, count %d; want today and 1 (D-57)", dueOf(t, r.Task), *r.Task.RecurrenceDoneCount)
	}
}

// R-3, and D-61 for a backdated completion.
func TestRecurringFromCompletion(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.newRecurring(t, alice, "2026-09-20", "FREQ=DAILY;INTERVAL=3", `,"repeat_from":"completion"`)
	r := a.completeOn(t, alice, task.Id, "2026-09-20")
	if dueOf(t, r.Task) != "2026-10-11" {
		t.Errorf("due = %s, want three days after today", dueOf(t, r.Task))
	}
	backdated := a.complete(t, alice, task.Id, newID(t),
		`,"occurrence_due_date":"2026-10-11","completed_at":"2026-10-01T10:00:00Z"`)
	if dueOf(t, backdated.Task) != "2026-10-04" {
		t.Errorf("due = %s, want three days after the backdated completion", dueOf(t, backdated.Task))
	}
}

// R-1 through the stored series start (D-56): the 31st comes back.
func TestRecurringEndOfMonth(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.newRecurring(t, alice, "2026-10-31", "FREQ=MONTHLY", "")
	r := a.completeOn(t, alice, task.Id, "2026-10-31")
	if dueOf(t, r.Task) != "2026-11-30" {
		t.Fatalf("due = %s", dueOf(t, r.Task))
	}
	if r := a.completeOn(t, alice, task.Id, "2026-11-30"); dueOf(t, r.Task) != "2026-12-31" {
		t.Errorf("due = %s, want the 31st back", dueOf(t, r.Task))
	}
}

// R-4: a fixed time keeps its local time across the October change in
// Madrid, so the instant moves by 25 hours from one day to the next.
func TestRecurringFixedTimeAcrossDaylightSaving(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.newRecurring(t, alice, "2026-10-24", "FREQ=DAILY", `,"due_time":"09:00","due_tz":"Europe/Madrid"`)
	r := a.completeOn(t, alice, task.Id, "2026-10-24")
	if dueOf(t, r.Task) != "2026-10-25" || r.Task.DueTime.MustGet() != "09:00" || r.Task.DueTz.MustGet() != "Europe/Madrid" {
		t.Fatalf("task = %+v", r.Task)
	}
	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatal(err)
	}
	before := time.Date(2026, 10, 24, 9, 0, 0, 0, madrid)
	after := time.Date(2026, 10, 25, 9, 0, 0, 0, madrid)
	if after.Sub(before) != 25*time.Hour {
		t.Errorf("the occurrences are %v apart, want 25h", after.Sub(before))
	}
}

// D-58: a new rule starts the count again.
func TestNewRuleResetsCount(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.newRecurring(t, alice, "2026-10-08", "FREQ=DAILY;COUNT=5", "")
	a.completeOn(t, alice, task.Id, "2026-10-08")
	path := "/tasks/" + task.Id.String()
	moved := decode[Task](t, a.call(t, alice, http.MethodPatch, path, `{"due_date":"2026-10-20"}`), http.StatusOK)
	if *moved.RecurrenceDoneCount != 1 {
		t.Errorf("moving the date reset the count: %d", *moved.RecurrenceDoneCount)
	}
	changed := decode[Task](t, a.call(t, alice, http.MethodPatch, path, `{"rrule":"FREQ=DAILY;COUNT=3"}`), http.StatusOK)
	if *changed.RecurrenceDoneCount != 0 {
		t.Errorf("count = %d after a new rule", *changed.RecurrenceDoneCount)
	}
}

// SPEC section 11: the Completed section shows a completed occurrence of
// a recurring task, although the task stays open (D-09).
func TestCompletedSectionShowsRecurringOccurrences(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.newRecurring(t, alice, "2026-10-08", "FREQ=DAILY", "")
	a.completeOn(t, alice, task.Id, "2026-10-08")

	page := a.completedSection(t, alice, "&due_to=2026-10-08")
	if len(page.Items) != 1 || page.Items[0].Task.Id != task.Id || !page.Items[0].CanUndo {
		t.Fatalf("section = %+v", page.Items)
	}
	if rule, _ := page.Items[0].Task.Rrule.Get(); rule != "FREQ=DAILY" {
		t.Errorf("summary rrule = %q", rule)
	}
	open := decode[Task](t, a.call(t, alice, http.MethodGet, "/tasks/"+task.Id.String(), ""), http.StatusOK)
	if open.Status != TaskStatusOpen {
		t.Errorf("task status = %s", open.Status)
	}
}

// Advancing reaches other clients through /sync/changes: the task and the
// unticked items, each with its own seq.
func TestAdvanceIsSynced(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.newRecurring(t, alice, "2026-10-08", "FREQ=DAILY", "")
	item := a.createItem(t, alice, task.Id, "step", "a")
	a.call(t, alice, http.MethodPatch, "/checklist-items/"+item.Id.String(), `{"is_done":true}`)
	cursor := a.pull(t, alice, replica{}, "", 100)

	a.completeOn(t, alice, task.Id, "2026-10-08")
	page := a.changes(t, alice, cursor, 100)
	if len(page.Tasks) != 1 || dueOf(t, page.Tasks[0]) != "2026-10-09" {
		t.Errorf("tasks = %+v", page.Tasks)
	}
	if len(page.ChecklistItems) != 1 || page.ChecklistItems[0].IsDone {
		t.Errorf("items = %+v", page.ChecklistItems)
	}
}
