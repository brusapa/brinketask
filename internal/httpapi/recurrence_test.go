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
