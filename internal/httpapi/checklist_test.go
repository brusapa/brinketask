package httpapi

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func newItemBody(id uuid.UUID, title, position string) string {
	return fmt.Sprintf(`{"id":%q,"title":%q,"position":%q}`, id, title, position)
}

func (a *testApp) createItem(t *testing.T, u user, taskID uuid.UUID, title, position string) ChecklistItem {
	t.Helper()
	return decode[ChecklistItem](t, a.call(t, u, http.MethodPost, "/tasks/"+taskID.String()+"/checklist-items",
		newItemBody(newID(t), title, position)), http.StatusCreated)
}

func TestChecklistItems(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.createTask(t, alice, alice.inboxID, "Trip")
	itemsPath := "/tasks/" + task.Id.String() + "/checklist-items"
	id := newID(t)

	item := decode[ChecklistItem](t, a.call(t, alice, http.MethodPost, itemsPath, newItemBody(id, "Passport", "b0")), http.StatusCreated)
	if item.Id != id || *item.TaskId != task.Id || item.IsDone || *item.Version != 1 {
		t.Errorf("created %+v", item)
	}
	// D-04: repeat returns it unchanged.
	again := decode[ChecklistItem](t, a.call(t, alice, http.MethodPost, itemsPath, newItemBody(id, "Other", "c0")), http.StatusOK)
	if again.Title != "Passport" || *again.Version != 1 {
		t.Errorf("repeat returned %+v", again)
	}
	a.createItem(t, alice, task.Id, "Tickets", "a0")

	// The task shows its live items by position.
	got := decode[Task](t, a.call(t, alice, http.MethodGet, "/tasks/"+task.Id.String(), ""), http.StatusOK)
	items := *got.ChecklistItems
	if len(items) != 2 || items[0].Title != "Tickets" || items[1].Title != "Passport" {
		t.Errorf("checklist = %+v", items)
	}
	// The item is its own resource: the task's version does not move.
	if *got.Version != 1 {
		t.Errorf("task version = %d after adding items, want 1", *got.Version)
	}

	path := "/checklist-items/" + id.String()
	done := decode[ChecklistItem](t, a.call(t, alice, http.MethodPatch, path, `{"is_done":true}`), http.StatusOK)
	if !done.IsDone || done.Title != "Passport" || *done.Version != 2 {
		t.Errorf("patched %+v", done)
	}
	same := decode[ChecklistItem](t, a.call(t, alice, http.MethodPatch, path, `{"is_done":true}`), http.StatusOK)
	if *same.Version != 2 {
		t.Error("no-op patch wrote (D-41)")
	}

	if rec := a.call(t, alice, http.MethodDelete, path, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rec.Code)
	}
	if rec := a.call(t, alice, http.MethodDelete, path, ""); rec.Code != http.StatusNoContent {
		t.Errorf("second delete = %d", rec.Code)
	}
	wantProblem(t, a.call(t, alice, http.MethodPatch, path, `{"is_done":false}`), http.StatusNotFound, ProblemCodeNotFound)
	got = decode[Task](t, a.call(t, alice, http.MethodGet, "/tasks/"+task.Id.String(), ""), http.StatusOK)
	if len(*got.ChecklistItems) != 1 {
		t.Errorf("deleted item still listed: %+v", *got.ChecklistItems)
	}
}

// D-45: deleting a task leaves its items alone; while it is deleted they
// cannot be touched, and they come back with it.
func TestChecklistOfDeletedTask(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.createTask(t, alice, alice.inboxID, "Trip")
	item := a.createItem(t, alice, task.Id, "Passport", "a0")
	taskPath := "/tasks/" + task.Id.String()
	itemPath := "/checklist-items/" + item.Id.String()

	a.call(t, alice, http.MethodDelete, taskPath, "")
	wantProblem(t, a.call(t, alice, http.MethodPatch, itemPath, `{"is_done":true}`), http.StatusNotFound, ProblemCodeNotFound)
	wantProblem(t, a.call(t, alice, http.MethodDelete, itemPath, ""), http.StatusNotFound, ProblemCodeNotFound)
	wantProblem(t, a.call(t, alice, http.MethodPost, taskPath+"/checklist-items", newItemBody(newID(t), "x", "b")),
		http.StatusNotFound, ProblemCodeNotFound)

	restored := decode[Task](t, a.call(t, alice, http.MethodPost, taskPath+"/restore", ""), http.StatusOK)
	if items := *restored.ChecklistItems; len(items) != 1 || items[0].Id != item.Id || *items[0].Version != 1 {
		t.Errorf("checklist after restore = %+v", items)
	}
}

func TestChecklistOfOthersIsNotFound(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	bob := a.signUp(t, "bob")
	task := a.createTask(t, alice, alice.inboxID, "Trip")
	item := a.createItem(t, alice, task.Id, "Passport", "a0")
	itemPath := "/checklist-items/" + item.Id.String()

	wantProblem(t, a.call(t, bob, http.MethodPost, "/tasks/"+task.Id.String()+"/checklist-items", newItemBody(newID(t), "x", "b")),
		http.StatusNotFound, ProblemCodeNotFound)
	wantProblem(t, a.call(t, bob, http.MethodPatch, itemPath, `{"is_done":true}`), http.StatusNotFound, ProblemCodeNotFound)
	wantProblem(t, a.call(t, bob, http.MethodDelete, itemPath, ""), http.StatusNotFound, ProblemCodeNotFound)

	// D-23: bob reusing alice's item id in his own task is a conflict.
	bobsTask := a.createTask(t, bob, bob.inboxID, "Mine")
	wantProblem(t, a.call(t, bob, http.MethodPost, "/tasks/"+bobsTask.Id.String()+"/checklist-items", newItemBody(item.Id, "x", "b")),
		http.StatusConflict, ProblemCodeConflict)
}
