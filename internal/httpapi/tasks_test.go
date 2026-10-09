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

func newTaskBody(id, listID uuid.UUID, title string) string {
	return fmt.Sprintf(`{"id":%q,"list_id":%q,"title":%q,"position":"a0"}`, id, listID, title)
}

func (a *testApp) createTask(t *testing.T, u user, listID uuid.UUID, title string) Task {
	t.Helper()
	return decode[Task](t, a.call(t, u, http.MethodPost, "/tasks", newTaskBody(newID(t), listID, title)), http.StatusCreated)
}

func (a *testApp) taskSeq(t *testing.T, id uuid.UUID) int64 {
	t.Helper()
	var seq int64
	if err := a.pool.QueryRow(context.Background(), "SELECT seq FROM tasks WHERE id = $1", id).Scan(&seq); err != nil {
		t.Fatal(err)
	}
	return seq
}

// fieldsOf returns the fields named in a 422 problem.
func fieldsOf(p Problem) []string {
	var fields []string
	if p.Errors != nil {
		for _, e := range *p.Errors {
			fields = append(fields, e.Field)
		}
	}
	return fields
}

func TestCreateTaskDefaults(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.createTask(t, alice, alice.inboxID, "Buy milk")

	if task.Status != TaskStatusOpen || task.Description != "" || task.Priority != 0 ||
		task.RepeatFrom != RepeatFromDue || *task.RecurrenceDoneCount != 0 || *task.Version != 1 {
		t.Errorf("defaults: %+v", task)
	}
	if len(task.TagIds) != 0 || len(*task.ChecklistItems) != 0 || len(*task.Reminders) != 0 {
		t.Errorf("collections should be empty arrays: %+v", task)
	}
	if !task.DueDate.IsNull() || !task.DueTime.IsNull() || !task.CompletedAt.IsNull() || !task.DeletedAt.IsNull() {
		t.Errorf("nullable fields should be null: %+v", task)
	}
}

func TestCreateTaskWithEverything(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	work := a.createTag(t, alice, "work")
	home := a.createTag(t, alice, "home")
	id, item1, item2 := newID(t), newID(t), newID(t)

	body := fmt.Sprintf(`{"id":%q,"list_id":%q,"title":"Report","description":"**Q3**","priority":3,
		"position":"b1","due_date":"2026-10-31","due_time":"17:30","due_tz":"Europe/Madrid",
		"repeat_from":"completion","tag_ids":[%q,%q,%q],
		"checklist_items":[{"id":%q,"title":"Draft","position":"a1"},{"id":%q,"title":"Send","is_done":true,"position":"a0"}],
		"reminders":[]}`,
		id, alice.inboxID, work.Id, home.Id, work.Id, item1, item2)
	task := decode[Task](t, a.call(t, alice, http.MethodPost, "/tasks", body), http.StatusCreated)

	if due, _ := task.DueDate.Get(); due.String() != "2026-10-31" {
		t.Errorf("due_date = %v", due)
	}
	if dueTime, _ := task.DueTime.Get(); dueTime != "17:30" {
		t.Errorf("due_time = %q", dueTime)
	}
	if tz, _ := task.DueTz.Get(); tz != "Europe/Madrid" || task.Priority != 3 || task.RepeatFrom != RepeatFromCompletion {
		t.Errorf("task = %+v", task)
	}
	// D-44: the repeated tag is kept once, in first-seen order.
	if !slices.Equal(task.TagIds, []uuid.UUID{work.Id, home.Id}) {
		t.Errorf("tag_ids = %v", task.TagIds)
	}
	// Items ordered by position, each a syncable row of its own.
	items := *task.ChecklistItems
	if len(items) != 2 || items[0].Id != item2 || !items[0].IsDone || items[1].Id != item1 || *items[1].Version != 1 {
		t.Errorf("checklist = %+v", items)
	}
	var seqs []int64
	rows, err := a.pool.Query(context.Background(),
		"SELECT seq FROM tasks WHERE id = $1 UNION ALL SELECT seq FROM checklist_items WHERE task_id = $1", id)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			t.Fatal(err)
		}
		seqs = append(seqs, seq)
	}
	slices.Sort(seqs)
	if len(seqs) != 3 || len(slices.Compact(seqs)) != 3 {
		t.Errorf("task and items seqs = %v, want three distinct values", seqs)
	}
}

// D-04, D-23 for tasks.
func TestCreateTaskIsIdempotent(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	id := newID(t)
	created := decode[Task](t, a.call(t, alice, http.MethodPost, "/tasks", newTaskBody(id, alice.inboxID, "One")), http.StatusCreated)
	seq := a.taskSeq(t, id)

	again := decode[Task](t, a.call(t, alice, http.MethodPost, "/tasks", newTaskBody(id, alice.inboxID, "Two")), http.StatusOK)
	if again.Title != "One" || *again.Version != *created.Version || a.taskSeq(t, id) != seq {
		t.Errorf("repeat changed the task: %+v", again)
	}

	// A deleted task is returned as it is, not revived.
	a.call(t, alice, http.MethodDelete, "/tasks/"+id.String(), "")
	deleted := decode[Task](t, a.call(t, alice, http.MethodPost, "/tasks", newTaskBody(id, alice.inboxID, "One")), http.StatusOK)
	if deleted.DeletedAt.IsNull() {
		t.Error("repeat revived a deleted task")
	}

	bob := a.signUp(t, "bob")
	wantProblem(t, a.call(t, bob, http.MethodPost, "/tasks", newTaskBody(id, bob.inboxID, "Mine")),
		http.StatusConflict, ProblemCodeConflict)
}

// D-34: recurrence and reminders arrive in later phases.
func TestCreateTaskValidation(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	bob := a.signUp(t, "bob")
	deletedList := a.createList(t, alice, "Old", "z")
	a.call(t, alice, http.MethodDelete, "/lists/"+deletedList.Id.String(), "")
	bobsTag := a.createTag(t, bob, "bob")
	deletedTag := a.createTag(t, alice, "gone")
	a.call(t, alice, http.MethodDelete, "/tags/"+deletedTag.Id.String(), "")
	item := newID(t)

	tests := []struct {
		name, fields, wantField string
	}{
		// D-35: the body names a list the caller cannot use.
		{"someone else's list", fmt.Sprintf(`"list_id":%q`, bob.inboxID), "/list_id"},
		{"deleted list", fmt.Sprintf(`"list_id":%q`, deletedList.Id), "/list_id"},
		{"unknown list", fmt.Sprintf(`"list_id":%q`, newID(t)), "/list_id"},
		// D-27.
		{"someone else's tag", fmt.Sprintf(`"list_id":%q,"tag_ids":[%q]`, alice.inboxID, bobsTag.Id), "/tag_ids"},
		{"deleted tag", fmt.Sprintf(`"list_id":%q,"tag_ids":[%q]`, alice.inboxID, deletedTag.Id), "/tag_ids"},
		// D-26 and SPEC section 4.
		{"time without date", fmt.Sprintf(`"list_id":%q,"due_time":"10:00"`, alice.inboxID), "/due_time"},
		{"zone without time", fmt.Sprintf(`"list_id":%q,"due_date":"2026-10-31","due_tz":"UTC"`, alice.inboxID), "/due_tz"},
		{"unknown zone", fmt.Sprintf(`"list_id":%q,"due_date":"2026-10-31","due_time":"10:00","due_tz":"Mars/Base"`, alice.inboxID), "/due_tz"},
		{"item id twice", fmt.Sprintf(`"list_id":%q,"checklist_items":[{"id":%q,"title":"a","position":"a"},{"id":%q,"title":"b","position":"b"}]`,
			alice.inboxID, item, item), "/checklist_items"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"id":%q,"title":"x","position":"a0",%s}`, newID(t), tt.fields)
			problem := wantProblem(t, a.call(t, alice, http.MethodPost, "/tasks", body),
				http.StatusUnprocessableEntity, ProblemCodeValidationFailed)
			if !slices.Contains(fieldsOf(problem), tt.wantField) {
				t.Errorf("fields = %v, want %q", fieldsOf(problem), tt.wantField)
			}
		})
	}

	// An item id already used elsewhere is a conflict, like any taken id.
	decode[Task](t, a.call(t, alice, http.MethodPost, "/tasks", fmt.Sprintf(
		`{"id":%q,"list_id":%q,"title":"x","position":"a0","checklist_items":[{"id":%q,"title":"a","position":"a"}]}`,
		newID(t), alice.inboxID, item)), http.StatusCreated)
	wantProblem(t, a.call(t, alice, http.MethodPost, "/tasks", fmt.Sprintf(
		`{"id":%q,"list_id":%q,"title":"y","position":"a0","checklist_items":[{"id":%q,"title":"a","position":"a"}]}`,
		newID(t), alice.inboxID, item)), http.StatusConflict, ProblemCodeConflict)
}

func TestPatchTask(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	work := a.createList(t, alice, "Work", "b0")
	tag := a.createTag(t, alice, "urgent")
	task := a.createTask(t, alice, alice.inboxID, "Draft")
	path := "/tasks/" + task.Id.String()

	patched := decode[Task](t, a.call(t, alice, http.MethodPatch, path, fmt.Sprintf(
		`{"title":"Final","list_id":%q,"priority":2,"due_date":"2026-11-02","due_time":"08:00","tag_ids":[%q]}`,
		work.Id, tag.Id)), http.StatusOK)
	if patched.Title != "Final" || patched.ListId != work.Id || patched.Priority != 2 || *patched.Version != 2 ||
		!slices.Equal(patched.TagIds, []uuid.UUID{tag.Id}) {
		t.Errorf("patched %+v", patched)
	}
	if patched.Description != "" || patched.Position != "a0" {
		t.Errorf("fields not in the patch changed: %+v", patched)
	}

	// D-26: clearing the date while a time remains is rejected; clearing
	// both in the same patch is fine.
	problem := wantProblem(t, a.call(t, alice, http.MethodPatch, path, `{"due_date":null}`),
		http.StatusUnprocessableEntity, ProblemCodeValidationFailed)
	if !slices.Contains(fieldsOf(problem), "/due_time") {
		t.Errorf("fields = %v", fieldsOf(problem))
	}
	cleared := decode[Task](t, a.call(t, alice, http.MethodPatch, path, `{"due_date":null,"due_time":null}`), http.StatusOK)
	if !cleared.DueDate.IsNull() || !cleared.DueTime.IsNull() {
		t.Errorf("after clearing: %+v", cleared)
	}

	// D-41: no change, no write.
	seq := a.taskSeq(t, task.Id)
	same := decode[Task](t, a.call(t, alice, http.MethodPatch, path, `{"title":"Final","due_date":null}`), http.StatusOK)
	if *same.Version != *cleared.Version || a.taskSeq(t, task.Id) != seq {
		t.Errorf("no-op patch wrote")
	}

	// D-35 for moves.
	bob := a.signUp(t, "bob")
	problem = wantProblem(t, a.call(t, alice, http.MethodPatch, path, fmt.Sprintf(`{"list_id":%q}`, bob.inboxID)),
		http.StatusUnprocessableEntity, ProblemCodeValidationFailed)
	if !slices.Contains(fieldsOf(problem), "/list_id") {
		t.Errorf("fields = %v", fieldsOf(problem))
	}
}

// D-38: open and dropped go back and forth; done only changes through
// uncomplete.
func TestPatchTaskStatus(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.createTask(t, alice, alice.inboxID, "x")
	path := "/tasks/" + task.Id.String()

	dropped := decode[Task](t, a.call(t, alice, http.MethodPatch, path, `{"status":"dropped"}`), http.StatusOK)
	if dropped.Status != TaskStatusDropped {
		t.Errorf("status = %s", dropped.Status)
	}
	reopened := decode[Task](t, a.call(t, alice, http.MethodPatch, path, `{"status":"open"}`), http.StatusOK)
	if reopened.Status != TaskStatusOpen {
		t.Errorf("status = %s", reopened.Status)
	}
	// The contract itself refuses "done" here.
	wantProblem(t, a.call(t, alice, http.MethodPatch, path, `{"status":"done"}`),
		http.StatusUnprocessableEntity, ProblemCodeValidationFailed)

	_, err := a.pool.Exec(context.Background(), "UPDATE tasks SET status = 'done', completed_at = now() WHERE id = $1", task.Id)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"open", "dropped"} {
		wantProblem(t, a.call(t, alice, http.MethodPatch, path, fmt.Sprintf(`{"status":%q}`, status)),
			http.StatusConflict, ProblemCodeConflict)
	}
	// Other fields of a done task can still change.
	decode[Task](t, a.call(t, alice, http.MethodPatch, path, `{"title":"y"}`), http.StatusOK)
}

func TestDeleteAndRestoreTask(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.createTask(t, alice, alice.inboxID, "x")
	path := "/tasks/" + task.Id.String()

	if rec := a.call(t, alice, http.MethodDelete, path, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rec.Code)
	}
	seq := a.taskSeq(t, task.Id)
	if rec := a.call(t, alice, http.MethodDelete, path, ""); rec.Code != http.StatusNoContent || a.taskSeq(t, task.Id) != seq {
		t.Errorf("second delete = %d, or it wrote", rec.Code)
	}
	wantProblem(t, a.call(t, alice, http.MethodGet, path, ""), http.StatusNotFound, ProblemCodeNotFound)
	wantProblem(t, a.call(t, alice, http.MethodPatch, path, `{"title":"y"}`), http.StatusNotFound, ProblemCodeNotFound)

	restored := decode[Task](t, a.call(t, alice, http.MethodPost, path+"/restore", ""), http.StatusOK)
	if !restored.DeletedAt.IsNull() || *restored.Version != 3 {
		t.Errorf("restored %+v", restored)
	}
	again := decode[Task](t, a.call(t, alice, http.MethodPost, path+"/restore", ""), http.StatusOK)
	if *again.Version != 3 {
		t.Error("restoring a live task wrote")
	}

	// The restore window, on the injected clock.
	a.call(t, alice, http.MethodDelete, path, "")
	a.clock.Advance(30 * 24 * time.Hour)
	wantProblem(t, a.call(t, alice, http.MethodPost, path+"/restore", ""), http.StatusConflict, ProblemCodeConflict)
}

// D-20: deleting a list deletes its live tasks with it; restoring the list
// restores exactly those. A task deleted on its own before stays deleted
// and cannot be restored while its list is deleted.
func TestListDeletionCascadesToTasks(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	list := a.createList(t, alice, "Work", "b0")
	keep := a.createTask(t, alice, list.Id, "with the list")
	alone := a.createTask(t, alice, list.Id, "deleted alone")
	other := a.createTask(t, alice, alice.inboxID, "other list")
	a.call(t, alice, http.MethodDelete, "/tasks/"+alone.Id.String(), "")
	aloneSeq := a.taskSeq(t, alone.Id)
	otherSeq := a.taskSeq(t, other.Id)

	a.call(t, alice, http.MethodDelete, "/lists/"+list.Id.String(), "")
	wantProblem(t, a.call(t, alice, http.MethodGet, "/tasks/"+keep.Id.String(), ""), http.StatusNotFound, ProblemCodeNotFound)
	if a.taskSeq(t, alone.Id) != aloneSeq || a.taskSeq(t, other.Id) != otherSeq {
		t.Error("the list deletion wrote tasks it should not touch")
	}
	// A task deleted with its list comes back only with the list.
	wantProblem(t, a.call(t, alice, http.MethodPost, "/tasks/"+keep.Id.String()+"/restore", ""),
		http.StatusConflict, ProblemCodeConflict)
	// Nor can one deleted on its own while its list is deleted.
	wantProblem(t, a.call(t, alice, http.MethodPost, "/tasks/"+alone.Id.String()+"/restore", ""),
		http.StatusConflict, ProblemCodeConflict)
	// And no task can move into the deleted list.
	wantProblem(t, a.call(t, alice, http.MethodPatch, "/tasks/"+other.Id.String(), fmt.Sprintf(`{"list_id":%q}`, list.Id)),
		http.StatusUnprocessableEntity, ProblemCodeValidationFailed)

	a.call(t, alice, http.MethodPost, "/lists/"+list.Id.String()+"/restore", "")
	back := decode[Task](t, a.call(t, alice, http.MethodGet, "/tasks/"+keep.Id.String(), ""), http.StatusOK)
	if *back.Version != 3 {
		t.Errorf("restored task version = %d, want 3 (create, delete, restore)", *back.Version)
	}
	wantProblem(t, a.call(t, alice, http.MethodGet, "/tasks/"+alone.Id.String(), ""), http.StatusNotFound, ProblemCodeNotFound)
	decode[Task](t, a.call(t, alice, http.MethodPost, "/tasks/"+alone.Id.String()+"/restore", ""), http.StatusOK)
}

func TestTasksOfOthersAreNotFound(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	bob := a.signUp(t, "bob")
	task := a.createTask(t, alice, alice.inboxID, "secret")
	path := "/tasks/" + task.Id.String()

	wantProblem(t, a.call(t, bob, http.MethodGet, path, ""), http.StatusNotFound, ProblemCodeNotFound)
	wantProblem(t, a.call(t, bob, http.MethodPatch, path, `{"title":"mine"}`), http.StatusNotFound, ProblemCodeNotFound)
	wantProblem(t, a.call(t, bob, http.MethodDelete, path, ""), http.StatusNotFound, ProblemCodeNotFound)
	wantProblem(t, a.call(t, bob, http.MethodPost, path+"/restore", ""), http.StatusNotFound, ProblemCodeNotFound)

	unchanged := decode[Task](t, a.call(t, alice, http.MethodGet, path, ""), http.StatusOK)
	if unchanged.Title != "secret" || *unchanged.Version != 1 {
		t.Errorf("bob changed alice's task: %+v", unchanged)
	}
}
