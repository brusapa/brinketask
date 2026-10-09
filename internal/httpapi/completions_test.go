package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
)

func (a *testApp) complete(t *testing.T, u user, taskID, completionID uuid.UUID, extra string) *CompletionResult {
	t.Helper()
	body := fmt.Sprintf(`{"completion_id":%q%s}`, completionID, extra)
	result := decode[CompletionResult](t, a.call(t, u, http.MethodPost, "/tasks/"+taskID.String()+"/complete", body), http.StatusOK)
	return &result
}

func (a *testApp) uncomplete(t *testing.T, u user, taskID, completionID uuid.UUID) *CompletionResult {
	t.Helper()
	body := fmt.Sprintf(`{"completion_id":%q}`, completionID)
	result := decode[CompletionResult](t, a.call(t, u, http.MethodPost, "/tasks/"+taskID.String()+"/uncomplete", body), http.StatusOK)
	return &result
}

func (a *testApp) completionCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := a.pool.QueryRow(context.Background(), "SELECT count(*) FROM task_completions").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCompleteTask(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.createTask(t, alice, alice.inboxID, "Call mum")
	a.clock.Advance(time.Hour)
	completionID := newID(t)

	result := a.complete(t, alice, task.Id, completionID, "")
	if !result.Applied || result.Task.Status != TaskStatusDone || *result.Task.Version != 2 {
		t.Fatalf("result = %+v", result)
	}
	completedAt, _ := result.Task.CompletedAt.Get()
	if !completedAt.Equal(testStart.Add(time.Hour)) {
		t.Errorf("completed_at = %v, want the injected clock", completedAt)
	}
	completion, err := result.Completion.Get()
	if err != nil || completion.Id != completionID || completion.Kind != CompletionKindCompleted || !completion.CompletedAt.Equal(completedAt) {
		t.Errorf("completion = %+v (%v)", completion, err)
	}
	seq := a.taskSeq(t, task.Id)

	// D-24: the same completion_id again is applied=true with the same
	// record, and writes nothing.
	again := a.complete(t, alice, task.Id, completionID, "")
	if c, _ := again.Completion.Get(); !again.Applied || c.Id != completionID || a.taskSeq(t, task.Id) != seq {
		t.Errorf("repeat = %+v", again)
	}
	// D-37: another completion of a done task is a no-op.
	other := a.complete(t, alice, task.Id, newID(t), "")
	if other.Applied || !other.Completion.IsNull() || a.completionCount(t) != 1 {
		t.Errorf("completing a done task = %+v", other)
	}
}

// D-10: the client says which occurrence it completes.
func TestCompleteChecksTheOccurrence(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := decode[Task](t, a.call(t, alice, http.MethodPost, "/tasks", fmt.Sprintf(
		`{"id":%q,"list_id":%q,"title":"x","position":"a","due_date":"2026-10-31"}`, newID(t), alice.inboxID)), http.StatusCreated)
	path := "/tasks/" + task.Id.String() + "/complete"

	problem := wantProblem(t, a.call(t, alice, http.MethodPost, path, fmt.Sprintf(`{"completion_id":%q}`, newID(t))),
		http.StatusUnprocessableEntity, ProblemCodeValidationFailed)
	if fieldsOf(problem)[0] != "/occurrence_due_date" {
		t.Errorf("fields = %v", fieldsOf(problem))
	}
	stale := a.complete(t, alice, task.Id, newID(t), `,"occurrence_due_date":"2026-10-30"`)
	if stale.Applied || stale.Task.Status != TaskStatusOpen || a.completionCount(t) != 0 {
		t.Errorf("stale completion = %+v", stale)
	}
	done := a.complete(t, alice, task.Id, newID(t), `,"occurrence_due_date":"2026-10-31"`)
	if !done.Applied {
		t.Errorf("matching completion = %+v", done)
	}
	if c, _ := done.Completion.Get(); c.OccurrenceDueDate.MustGet().String() != "2026-10-31" {
		t.Errorf("occurrence = %+v", c)
	}
}

func TestCompletedAtIsClampedToNow(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	past := a.createTask(t, alice, alice.inboxID, "past")
	future := a.createTask(t, alice, alice.inboxID, "future")

	backdated := testStart.Add(-2 * time.Hour).Format(time.RFC3339)
	r := a.complete(t, alice, past.Id, newID(t), fmt.Sprintf(`,"completed_at":%q`, backdated))
	if at, _ := r.Task.CompletedAt.Get(); !at.Equal(testStart.Add(-2 * time.Hour)) {
		t.Errorf("backdated completed_at = %v", at)
	}
	ahead := testStart.Add(time.Hour).Format(time.RFC3339)
	r = a.complete(t, alice, future.Id, newID(t), fmt.Sprintf(`,"completed_at":%q`, ahead))
	if at, _ := r.Task.CompletedAt.Get(); !at.Equal(testStart) {
		t.Errorf("future completed_at = %v, want clamped to now", at)
	}
}

func TestCompleteConflicts(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	first := a.createTask(t, alice, alice.inboxID, "first")
	second := a.createTask(t, alice, alice.inboxID, "second")
	used := newID(t)
	a.complete(t, alice, first.Id, used, "")

	// A completion id of another task.
	wantProblem(t, a.call(t, alice, http.MethodPost, "/tasks/"+second.Id.String()+"/complete",
		fmt.Sprintf(`{"completion_id":%q}`, used)), http.StatusConflict, ProblemCodeConflict)
	// D-37: a dropped task must be reopened first.
	a.call(t, alice, http.MethodPatch, "/tasks/"+second.Id.String(), `{"status":"dropped"}`)
	wantProblem(t, a.call(t, alice, http.MethodPost, "/tasks/"+second.Id.String()+"/complete",
		fmt.Sprintf(`{"completion_id":%q}`, newID(t))), http.StatusConflict, ProblemCodeConflict)
}

func TestUncomplete(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.createTask(t, alice, alice.inboxID, "x")
	first := newID(t)
	a.complete(t, alice, task.Id, first, "")

	undone := a.uncomplete(t, alice, task.Id, first)
	if !undone.Applied || undone.Task.Status != TaskStatusOpen || !undone.Task.CompletedAt.IsNull() || *undone.Task.Version != 3 {
		t.Fatalf("undo = %+v", undone)
	}
	seq := a.taskSeq(t, task.Id)
	// Repeating it, or an unknown id, changes nothing.
	if again := a.uncomplete(t, alice, task.Id, first); again.Applied || a.taskSeq(t, task.Id) != seq {
		t.Errorf("repeat undo = %+v", again)
	}
	if unknown := a.uncomplete(t, alice, task.Id, newID(t)); unknown.Applied {
		t.Errorf("unknown undo = %+v", unknown)
	}
	// D-39: a late retry of the undone completion does not complete again.
	if retry := a.complete(t, alice, task.Id, first, ""); retry.Applied || retry.Task.Status != TaskStatusOpen {
		t.Errorf("retry of an undone completion = %+v", retry)
	}
	// A new completion works.
	if again := a.complete(t, alice, task.Id, newID(t), ""); !again.Applied {
		t.Errorf("new completion after undo = %+v", again)
	}
}

// D-25: undo restores the saved state, even after the date was edited; a
// time that did not exist before goes away together with its zone.
func TestUncompleteRestoresTheSavedState(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := decode[Task](t, a.call(t, alice, http.MethodPost, "/tasks", fmt.Sprintf(
		`{"id":%q,"list_id":%q,"title":"x","position":"a","due_date":"2026-10-31"}`, newID(t), alice.inboxID)), http.StatusCreated)
	id := newID(t)
	a.complete(t, alice, task.Id, id, `,"occurrence_due_date":"2026-10-31"`)
	a.call(t, alice, http.MethodPatch, "/tasks/"+task.Id.String(),
		`{"due_date":"2026-11-15","due_time":"10:00","due_tz":"Europe/Madrid"}`)

	undone := a.uncomplete(t, alice, task.Id, id)
	if d, _ := undone.Task.DueDate.Get(); d.String() != "2026-10-31" {
		t.Errorf("due_date = %v, want the saved 2026-10-31", d)
	}
	if !undone.Task.DueTime.IsNull() || !undone.Task.DueTz.IsNull() {
		t.Errorf("due_time %v due_tz %v, want both null", undone.Task.DueTime, undone.Task.DueTz)
	}
}

// Only the latest record of a task can be undone.
func TestUncompleteOnlyTheLatest(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.createTask(t, alice, alice.inboxID, "x")
	older := newID(t)
	// An older record, as recurring tasks (phase 4) will leave behind.
	_, err := a.pool.Exec(context.Background(), `INSERT INTO task_completions
		(id, task_id, kind, completed_at, prev_recurrence_done_count, prev_status, task_seq)
		VALUES ($1, $2, 'completed', $3, 0, 'open', 0)`, older, task.Id, testStart)
	if err != nil {
		t.Fatal(err)
	}
	a.complete(t, alice, task.Id, newID(t), "")
	wantProblem(t, a.call(t, alice, http.MethodPost, "/tasks/"+task.Id.String()+"/uncomplete",
		fmt.Sprintf(`{"completion_id":%q}`, older)), http.StatusConflict, ProblemCodeConflict)
}

func TestCompletionOfOthersOrDeletedTasks(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	bob := a.signUp(t, "bob")
	task := a.createTask(t, alice, alice.inboxID, "x")
	id := newID(t)
	body := fmt.Sprintf(`{"completion_id":%q}`, id)

	for _, action := range []string{"/complete", "/uncomplete"} {
		wantProblem(t, a.call(t, bob, http.MethodPost, "/tasks/"+task.Id.String()+action, body), http.StatusNotFound, ProblemCodeNotFound)
	}
	wantProblem(t, a.call(t, bob, http.MethodGet, "/tasks/"+task.Id.String()+"/completions", ""), http.StatusNotFound, ProblemCodeNotFound)

	a.call(t, alice, http.MethodDelete, "/tasks/"+task.Id.String(), "")
	wantProblem(t, a.call(t, alice, http.MethodPost, "/tasks/"+task.Id.String()+"/complete", body), http.StatusNotFound, ProblemCodeNotFound)
}

func TestTaskCompletionHistory(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.createTask(t, alice, alice.inboxID, "x")
	undone, second, third := newID(t), newID(t), newID(t)
	// An older live record, as recurring tasks (phase 4) will leave behind.
	_, err := a.pool.Exec(context.Background(), `INSERT INTO task_completions
		(id, task_id, kind, completed_at, prev_recurrence_done_count, prev_status, task_seq)
		VALUES ($1, $2, 'completed', $3, 0, 'open', 0)`, second, task.Id, testStart)
	if err != nil {
		t.Fatal(err)
	}
	a.complete(t, alice, task.Id, undone, "")
	a.uncomplete(t, alice, task.Id, undone)
	a.complete(t, alice, task.Id, third, "")

	path := "/tasks/" + task.Id.String() + "/completions"
	page := decode[ListTaskCompletions200JSONResponse](t, a.call(t, alice, http.MethodGet, path, ""), http.StatusOK)
	if len(page.Items) != 2 || page.Items[0].Id != third || page.Items[1].Id != second {
		t.Fatalf("history = %+v, want third then second, without the undone one", page.Items)
	}
	first := decode[ListTaskCompletions200JSONResponse](t, a.call(t, alice, http.MethodGet, path+"?limit=1", ""), http.StatusOK)
	next, err := first.NextCursor.Get()
	if len(first.Items) != 1 || first.Items[0].Id != third || err != nil {
		t.Fatalf("first page = %+v", first)
	}
	rest := decode[ListTaskCompletions200JSONResponse](t, a.call(t, alice, http.MethodGet,
		path+"?limit=1&cursor="+url.QueryEscape(next), ""), http.StatusOK)
	if len(rest.Items) != 1 || rest.Items[0].Id != second || !rest.NextCursor.IsNull() {
		t.Errorf("second page = %+v", rest)
	}
}
