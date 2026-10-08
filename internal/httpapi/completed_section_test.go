package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

// day is the "current calendar day" the client asks about, as bounds in
// UTC (SPEC section 8: the server applies no day logic).
var (
	dayStart = testStart.Add(-9 * time.Hour) // 2026-10-08T00:00Z
	dayEnd   = dayStart.Add(24 * time.Hour)
)

func (a *testApp) completedSection(t *testing.T, u user, extra string) ListCompletions200JSONResponse {
	t.Helper()
	query := "completed_from=" + url.QueryEscape(dayStart.Format(time.RFC3339)) +
		"&completed_to=" + url.QueryEscape(dayEnd.Format(time.RFC3339)) + extra
	return decode[ListCompletions200JSONResponse](t, a.call(t, u, http.MethodGet, "/completions?"+query, ""), http.StatusOK)
}

func entryTaskIDs(page ListCompletions200JSONResponse) []uuid.UUID {
	ids := make([]uuid.UUID, len(page.Items))
	for i, e := range page.Items {
		ids[i] = e.Task.Id
	}
	return ids
}

// completeAt completes a task with a given completed_at.
func (a *testApp) completeAt(t *testing.T, u user, taskID uuid.UUID, at time.Time, extra string) uuid.UUID {
	t.Helper()
	id := newID(t)
	a.complete(t, u, taskID, id, fmt.Sprintf(`,"completed_at":%q%s`, at.Format(time.RFC3339), extra))
	return id
}

func TestCompletedSection(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	bob := a.signUp(t, "bob")

	morning := a.createTask(t, alice, alice.inboxID, "morning")
	early := a.createTask(t, alice, alice.inboxID, "early")
	yesterday := a.createTask(t, alice, alice.inboxID, "yesterday")
	deleted := a.createTask(t, alice, alice.inboxID, "deleted")
	undone := a.createTask(t, alice, alice.inboxID, "undone")
	bobs := a.createTask(t, bob, bob.inboxID, "bob's")

	a.completeAt(t, alice, morning.Id, dayStart.Add(8*time.Hour), "")
	a.completeAt(t, alice, early.Id, dayStart, "") // the lower bound is inclusive
	a.completeAt(t, alice, yesterday.Id, dayStart.Add(-time.Second), "")
	a.completeAt(t, alice, deleted.Id, dayStart.Add(time.Hour), "")
	a.call(t, alice, http.MethodDelete, "/tasks/"+deleted.Id.String(), "")
	undoneID := a.completeAt(t, alice, undone.Id, dayStart.Add(2*time.Hour), "")
	a.uncomplete(t, alice, undone.Id, undoneID)
	a.completeAt(t, bob, bobs.Id, dayStart.Add(time.Hour), "")

	// A skipped record (phase 4) is never part of the section.
	skipped := a.createTask(t, alice, alice.inboxID, "skipped")
	_, err := a.pool.Exec(context.Background(), `INSERT INTO task_completions
		(id, task_id, kind, completed_at, prev_recurrence_done_count, prev_status, task_seq)
		VALUES ($1, $2, 'skipped', $3, 0, 'open', 0)`, newID(t), skipped.Id, dayStart.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	page := a.completedSection(t, alice, "")
	// Newest first; only alice's live, completed, not undone records in
	// [from, to).
	if got, want := entryTaskIDs(page), []uuid.UUID{morning.Id, early.Id}; !slices.Equal(got, want) {
		t.Fatalf("section = %v, want %v", got, want)
	}
	entry := page.Items[0]
	if entry.Task.Title != "morning" || entry.Task.ListId != alice.inboxID || !entry.CanUndo || entry.Kind != CompletionEntryKindCompleted {
		t.Errorf("entry = %+v", entry)
	}
	// The title shown is the task's current title.
	a.call(t, alice, http.MethodPatch, "/tasks/"+morning.Id.String(), `{"title":"renamed"}`)
	if title := a.completedSection(t, alice, "").Items[0].Task.Title; title != "renamed" {
		t.Errorf("title = %q, want the current one", title)
	}
}

func TestCompletedSectionScope(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	work := a.createList(t, alice, "Work", "b0")
	tag := a.createTag(t, alice, "urgent")

	inInbox := a.createTask(t, alice, alice.inboxID, "inbox")
	inWork := a.createTask(t, alice, work.Id, "work")
	tagged := a.taskWith(t, alice, alice.inboxID, "tagged", "b", fmt.Sprintf(`,"tag_ids":[%q],"due_date":"2026-10-07"`, tag.Id))
	a.completeAt(t, alice, inInbox.Id, dayStart.Add(time.Hour), "")
	a.completeAt(t, alice, inWork.Id, dayStart.Add(2*time.Hour), "")
	a.completeAt(t, alice, tagged, dayStart.Add(3*time.Hour), `,"occurrence_due_date":"2026-10-07"`)

	check := func(extra string, want ...uuid.UUID) {
		t.Helper()
		if got := entryTaskIDs(a.completedSection(t, alice, extra)); !slices.Equal(got, want) {
			t.Errorf("section%s = %v, want %v", extra, got, want)
		}
	}
	check("&list_id="+work.Id.String(), inWork.Id)
	check("&tag_id="+tag.Id.String(), tagged)
	// due_from / due_to apply to occurrence_due_date.
	check("&due_from=2026-10-07&due_to=2026-10-07", tagged)
	check("&due_from=2026-10-08")
	// Pages, newest first.
	first := a.completedSection(t, alice, "&limit=2")
	next, err := first.NextCursor.Get()
	if len(first.Items) != 2 || err != nil {
		t.Fatalf("first page = %+v", first)
	}
	rest := a.completedSection(t, alice, "&limit=2&cursor="+url.QueryEscape(next))
	if got := entryTaskIDs(rest); !slices.Equal(got, []uuid.UUID{inInbox.Id}) || !rest.NextCursor.IsNull() {
		t.Errorf("second page = %v", got)
	}

	// D-36: a filter on something not the caller's is a 404.
	bob := a.signUp(t, "bob")
	wantProblem(t, a.call(t, alice, http.MethodGet, "/completions?completed_from="+url.QueryEscape(dayStart.Format(time.RFC3339))+
		"&completed_to="+url.QueryEscape(dayEnd.Format(time.RFC3339))+"&list_id="+bob.inboxID.String(), ""),
		http.StatusNotFound, ProblemCodeNotFound)
}
