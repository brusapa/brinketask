package httpapi

// Reproductions of findings in REVIEW.md that are confirmed. Each test
// describes the behaviour the specification asks for and fails until the
// finding is fixed.

import (
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"
)

func itemIDs(items []ChecklistItem) []uuid.UUID {
	ids := make([]uuid.UUID, len(items))
	for i, item := range items {
		ids[i] = item.Id
	}
	return ids
}

// H3 (SPEC section 8, D-45): a client that started while a task was
// deleted gets its checklist when the task is restored. The full state
// left the items out, so a later sync must bring them.
func TestReviewH3RestoredTaskBringsItsChecklistToSync(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.createTask(t, alice, alice.inboxID, "Trip")
	item := a.createItem(t, alice, task.Id, "Passport", "a0")
	if rec := a.call(t, alice, http.MethodDelete, "/tasks/"+task.Id.String(), ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}

	// A second device starts now: the full state has neither task nor item.
	full := a.changes(t, alice, "", 500)
	if slices.Contains(itemIDs(full.ChecklistItems), item.Id) {
		t.Fatal("the full state has an item of a deleted task")
	}

	if rec := a.call(t, alice, http.MethodPost, "/tasks/"+task.Id.String()+"/restore", ""); rec.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", rec.Code, rec.Body)
	}
	next := a.changes(t, alice, full.NextCursor, 500)
	if !slices.Contains(itemIDs(next.ChecklistItems), item.Id) {
		t.Errorf("after the restore, sync brings task(s) %d but not the checklist item", len(next.Tasks))
	}
}

// H3, through a list: restoring a list brings the checklists of the tasks
// that come back with it (D-20).
func TestReviewH3RestoredListBringsChecklistsToSync(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	list := a.createList(t, alice, "Errands", "b0")
	task := a.createTask(t, alice, list.Id, "Groceries")
	item := a.createItem(t, alice, task.Id, "Milk", "a0")
	if rec := a.call(t, alice, http.MethodDelete, "/lists/"+list.Id.String(), ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}

	full := a.changes(t, alice, "", 500)
	if rec := a.call(t, alice, http.MethodPost, "/lists/"+list.Id.String()+"/restore", ""); rec.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", rec.Code, rec.Body)
	}
	next := a.changes(t, alice, full.NextCursor, 500)
	if !slices.Contains(itemIDs(next.ChecklistItems), item.Id) {
		t.Errorf("after the restore, sync brings task(s) %d but not the checklist item", len(next.Tasks))
	}
}

// M3 (D-25, D-26): undoing a completion after the done task was given a
// date and a rule answers a deliberate response, not a 500, and leaves the
// task consistent.
func TestReviewM3UncompleteAfterAddingRecurrenceIsNot500(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.createTask(t, alice, alice.inboxID, "Undated")
	completionID := newID(t)
	a.complete(t, alice, task.Id, completionID, "")

	rec := a.call(t, alice, http.MethodPatch, "/tasks/"+task.Id.String(), `{"due_date":"2026-11-01","rrule":"FREQ=DAILY"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch of the done task: %d %s", rec.Code, rec.Body)
	}
	rec = a.call(t, alice, http.MethodPost, "/tasks/"+task.Id.String()+"/uncomplete",
		fmt.Sprintf(`{"completion_id":%q}`, completionID))
	if rec.Code == http.StatusInternalServerError {
		t.Errorf("uncomplete answered 500: %s", rec.Body)
	}
}
