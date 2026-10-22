package httpapi

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/brusapa/brinketask/internal/storage/storagetest"
	"github.com/brusapa/brinketask/internal/tasks"
)

// Every write locks a task before its children and takes a seq last
// (D-07). These tests stop two writes at the seq, the point where an
// inverted order would have them wait for each other, then let them go:
// both must succeed instead of PostgreSQL aborting one (40P01).

// concurrently runs first until it waits for a lock, then second until a
// second session waits too, then releases the seq lock and returns both
// errors.
func (a *testApp) concurrently(t *testing.T, first, second func() error) (error, error) {
	t.Helper()
	release := storagetest.HoldSeqLock(t, a.pool)
	firstErr, secondErr := make(chan error, 1), make(chan error, 1)
	go func() { firstErr <- first() }()
	storagetest.WaitForLockWaiters(t, a.pool, 1)
	go func() { secondErr <- second() }()
	storagetest.WaitForLockWaiters(t, a.pool, 2)
	release()
	return <-firstErr, <-secondErr
}

// R-7: completing a recurring task unticks its items while one of them is
// being edited.
func TestCompleteWhileItemIsPatched(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.newRecurring(t, alice, "2026-10-08", "FREQ=DAILY", "")
	item := a.createItem(t, alice, task.Id, "Water the plants", "a0")
	if rec := a.call(t, alice, http.MethodPatch, "/checklist-items/"+item.Id.String(), `{"is_done":true}`); rec.Code != http.StatusOK {
		t.Fatalf("tick: %d %s", rec.Code, rec.Body)
	}
	ctx := context.Background()
	occurrence := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	completionID := newID(t)
	title := "Water all the plants"

	completeErr, patchErr := a.concurrently(t,
		func() error {
			_, err := a.tasks.Complete(ctx, alice.id, task.Id, tasks.CompleteInput{CompletionID: completionID, OccurrenceDueDate: &occurrence})
			return err
		},
		func() error {
			_, err := a.tasks.PatchChecklistItem(ctx, alice.id, item.Id, tasks.ChecklistItemPatch{Title: &title})
			return err
		})
	if completeErr != nil || patchErr != nil {
		t.Errorf("complete: %v; item patch: %v", completeErr, patchErr)
	}
}

// R-8: completing deletes an absolute reminder while it is being edited.
func TestCompleteWhileReminderIsPatched(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.newRecurring(t, alice, "2026-10-08", "FREQ=DAILY", "")
	r := a.addReminder(t, alice, task.Id, absoluteBody(t, "2026-10-08T15:00:00Z"), http.StatusCreated)
	ctx := context.Background()
	occurrence := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	completionID := newID(t)
	at := time.Date(2026, 10, 8, 16, 0, 0, 0, time.UTC)

	completeErr, patchErr := a.concurrently(t,
		func() error {
			_, err := a.tasks.Complete(ctx, alice.id, task.Id, tasks.CompleteInput{CompletionID: completionID, OccurrenceDueDate: &occurrence})
			return err
		},
		func() error {
			_, err := a.tasks.PatchReminder(ctx, alice.id, r.Id, tasks.ReminderPatch{At: &at})
			return err
		})
	// The patch runs after the completion, which deleted the reminder (R-8):
	// not found is the right answer, a deadlock is not.
	if completeErr != nil || (patchErr != nil && !errors.Is(patchErr, tasks.ErrNotFound)) {
		t.Errorf("complete: %v; reminder patch: %v", completeErr, patchErr)
	}
}

// D-20: a task restored while its list is being deleted is not left live
// in the deleted list; it goes with the list.
func TestRestoreTaskWhileListIsDeleted(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	list := a.createList(t, alice, "Errands", "b0")
	task := a.createTask(t, alice, list.Id, "Groceries")
	if rec := a.call(t, alice, http.MethodDelete, "/tasks/"+task.Id.String(), ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete task: %d %s", rec.Code, rec.Body)
	}
	ctx := context.Background()

	restoreErr, deleteErr := a.concurrently(t,
		func() error {
			_, err := a.tasks.RestoreTask(ctx, alice.id, task.Id)
			return err
		},
		func() error {
			return a.tasks.DeleteList(ctx, alice.id, list.Id)
		})
	if restoreErr != nil || deleteErr != nil {
		t.Fatalf("restore: %v; delete list: %v", restoreErr, deleteErr)
	}

	var taskLive bool
	var deletedWith *uuid.UUID
	err := a.pool.QueryRow(ctx, "SELECT deleted_at IS NULL, deleted_with_list_id FROM tasks WHERE id = $1", task.Id).
		Scan(&taskLive, &deletedWith)
	if err != nil {
		t.Fatal(err)
	}
	if taskLive || deletedWith == nil || *deletedWith != list.Id {
		t.Errorf("task live %v, deleted with %v; want deleted with the list", taskLive, deletedWith)
	}
}
