package httpapi

// Reproductions of findings in REVIEW.md that are confirmed. Each test
// describes the behaviour the specification asks for and fails until the
// finding is fixed.

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/brusapa/brinketask/internal/tasks"
)

// holdSeqLock takes the lock of the sync_state row (D-07) in a transaction
// of its own, so every writer that asks for a seq waits. Calling the
// returned function commits and lets them go. Tests use it to stop
// concurrent operations at a known point.
func (a *testApp) holdSeqLock(t *testing.T) func() {
	t.Helper()
	ctx := context.Background()
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "UPDATE sync_state SET seq = seq"); err != nil {
		t.Fatal(err)
	}
	released := false
	release := func() {
		if !released {
			released = true
			if err := tx.Commit(ctx); err != nil {
				t.Error(err)
			}
		}
	}
	t.Cleanup(release)
	return release
}

// waitForLockWaiters blocks until n sessions of the test database wait
// for a lock.
func (a *testApp) waitForLockWaiters(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		err := a.pool.QueryRow(context.Background(),
			"SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'").
			Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d sessions wait for a lock, want %d", waiting, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

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

// M1 (D-07, R-7): completing a recurring task while one of its checklist
// items is being edited must not deadlock. Complete locks the task, then
// the seq, then the items (R-7); the item patch locks the item, then the
// seq. PostgreSQL aborts one of them (40P01), which becomes a 500.
func TestReviewM1CompleteAndItemPatchDoNotDeadlock(t *testing.T) {
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
	release := a.holdSeqLock(t)

	errs := make(chan error, 2)
	go func() {
		_, err := a.tasks.Complete(ctx, alice.id, task.Id, tasks.CompleteInput{
			CompletionID: completionID, OccurrenceDueDate: &occurrence,
		})
		errs <- err
	}()
	a.waitForLockWaiters(t, 1) // complete holds the task and waits for a seq
	go func() {
		_, err := a.tasks.PatchChecklistItem(ctx, alice.id, item.Id, tasks.ChecklistItemPatch{Title: &title})
		errs <- err
	}()
	a.waitForLockWaiters(t, 2) // the patch holds the item and waits for a seq
	release()

	for range 2 {
		if err := <-errs; err != nil {
			t.Errorf("concurrent complete and item patch: %v", err)
		}
	}
}

// M2 (D-20, /tasks/{id}/restore): a task restored while its list is being
// deleted must not end up live inside the deleted list.
func TestReviewM2RestoreRacingListDeleteLeavesNoLiveTaskInDeletedList(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	list := a.createList(t, alice, "Errands", "b0")
	task := a.createTask(t, alice, list.Id, "Groceries")
	if rec := a.call(t, alice, http.MethodDelete, "/tasks/"+task.Id.String(), ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete task: %d %s", rec.Code, rec.Body)
	}

	ctx := context.Background()
	release := a.holdSeqLock(t)
	errs := make(chan error, 2)
	go func() {
		_, err := a.tasks.RestoreTask(ctx, alice.id, task.Id)
		errs <- err
	}()
	a.waitForLockWaiters(t, 1) // the restore has seen a live list
	go func() {
		errs <- a.tasks.DeleteList(ctx, alice.id, list.Id)
	}()
	a.waitForLockWaiters(t, 2) // the delete has picked the list's live tasks
	release()
	for range 2 {
		<-errs // either may lose (409); what matters is the state left
	}

	var taskLive, listLive bool
	err := a.pool.QueryRow(ctx,
		"SELECT t.deleted_at IS NULL, l.deleted_at IS NULL FROM tasks t JOIN lists l ON l.id = t.list_id WHERE t.id = $1",
		task.Id).Scan(&taskLive, &listLive)
	if err != nil {
		t.Fatal(err)
	}
	if taskLive && !listLive {
		t.Error("the task is live in a deleted list")
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
