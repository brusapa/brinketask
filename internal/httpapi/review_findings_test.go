package httpapi

// Reproductions of findings in REVIEW.md that are confirmed. Each test
// describes the behaviour the specification asks for and fails until the
// finding is fixed.

import (
	"fmt"
	"net/http"
	"testing"
)

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
