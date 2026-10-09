package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

// taskWith creates a task from extra JSON fields and returns its id.
func (a *testApp) taskWith(t *testing.T, u user, listID uuid.UUID, title, position, extra string) uuid.UUID {
	t.Helper()
	id := newID(t)
	body := fmt.Sprintf(`{"id":%q,"list_id":%q,"title":%q,"position":%q%s}`, id, listID, title, position, extra)
	decode[Task](t, a.call(t, u, http.MethodPost, "/tasks", body), http.StatusCreated)
	return id
}

// queryIDs runs GET /tasks with query and returns the ids of the first page.
func (a *testApp) queryIDs(t *testing.T, u user, query string) []uuid.UUID {
	t.Helper()
	page := decode[TaskPage](t, a.call(t, u, http.MethodGet, "/tasks?"+query, ""), http.StatusOK)
	ids := make([]uuid.UUID, len(page.Items))
	for i, task := range page.Items {
		ids[i] = task.Id
	}
	return ids
}

// allPages follows next_cursor to the end and returns every id in order.
func (a *testApp) allPages(t *testing.T, u user, query string) []uuid.UUID {
	t.Helper()
	var ids []uuid.UUID
	values, err := url.ParseQuery(query)
	if err != nil {
		t.Fatal(err)
	}
	for range 100 {
		page := decode[TaskPage](t, a.call(t, u, http.MethodGet, "/tasks?"+values.Encode(), ""), http.StatusOK)
		for _, task := range page.Items {
			ids = append(ids, task.Id)
		}
		next, err := page.NextCursor.Get()
		if page.NextCursor.IsNull() || err != nil {
			return ids
		}
		values.Set("cursor", next)
	}
	t.Fatal("pagination did not end")
	return nil
}

func wantIDs(t *testing.T, got, want []uuid.UUID) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("tasks = %v\n want %v", got, want)
	}
}

func TestQueryTasksDefaultsAndOrder(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	bob := a.signUp(t, "bob")
	work := a.createList(t, alice, "Work", "b0") // after the inbox ("a0")

	w2 := a.taskWith(t, alice, work.Id, "w2", "b", "")
	i1 := a.taskWith(t, alice, alice.inboxID, "i1", "a", "")
	w1 := a.taskWith(t, alice, work.Id, "w1", "a", "")
	i2 := a.taskWith(t, alice, alice.inboxID, "i2", "b", "")
	dropped := a.taskWith(t, alice, alice.inboxID, "dropped", "c", "")
	a.call(t, alice, http.MethodPatch, "/tasks/"+dropped.String(), `{"status":"dropped"}`)
	deleted := a.taskWith(t, alice, alice.inboxID, "deleted", "d", "")
	a.call(t, alice, http.MethodDelete, "/tasks/"+deleted.String(), "")
	a.taskWith(t, bob, bob.inboxID, "bob's", "a", "")

	// Open, live, the caller's own, by list position then task position (D-43).
	wantIDs(t, a.queryIDs(t, alice, ""), []uuid.UUID{i1, i2, w1, w2})
	wantIDs(t, a.queryIDs(t, alice, "status=dropped"), []uuid.UUID{dropped})
	wantIDs(t, a.queryIDs(t, alice, "list_id="+work.Id.String()), []uuid.UUID{w1, w2})
}

func TestQueryTasksFilters(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	tag := a.createTag(t, alice, "urgent")

	a.taskWith(t, alice, alice.inboxID, "no date", "a", "")
	oct30 := a.taskWith(t, alice, alice.inboxID, "oct30", "b", `,"due_date":"2026-10-30"`)
	oct31 := a.taskWith(t, alice, alice.inboxID, "oct31", "c", fmt.Sprintf(`,"due_date":"2026-10-31","tag_ids":[%q]`, tag.Id))
	nov1 := a.taskWith(t, alice, alice.inboxID, "nov1", "d", `,"due_date":"2026-11-01"`)

	// Bounds are inclusive; tasks without a date never match a date filter.
	wantIDs(t, a.queryIDs(t, alice, "due_from=2026-10-31"), []uuid.UUID{oct31, nov1})
	wantIDs(t, a.queryIDs(t, alice, "due_to=2026-10-31"), []uuid.UUID{oct30, oct31})
	wantIDs(t, a.queryIDs(t, alice, "due_from=2026-10-31&due_to=2026-10-31"), []uuid.UUID{oct31})
	wantIDs(t, a.queryIDs(t, alice, "tag_id="+tag.Id.String()), []uuid.UUID{oct31})
}

// D-36: filtering by something that is not the caller's, or deleted, is a
// 404.
func TestQueryTasksFilterNotFound(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	bob := a.signUp(t, "bob")
	bobsTag := a.createTag(t, bob, "bob")
	oldList := a.createList(t, alice, "Old", "z")
	a.call(t, alice, http.MethodDelete, "/lists/"+oldList.Id.String(), "")

	for _, query := range []string{
		"list_id=" + bob.inboxID.String(),
		"list_id=" + oldList.Id.String(),
		"list_id=" + newID(t).String(),
		"tag_id=" + bobsTag.Id.String(),
	} {
		wantProblem(t, a.call(t, alice, http.MethodGet, "/tasks?"+query, ""), http.StatusNotFound, ProblemCodeNotFound)
	}
}

// D-43, sort=due: by date with no date last; within a date, all-day first,
// then by time.
func TestQueryTasksByDue(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	noDate := a.taskWith(t, alice, alice.inboxID, "no date", "a", "")
	late := a.taskWith(t, alice, alice.inboxID, "late", "b", `,"due_date":"2026-10-31","due_time":"18:00"`)
	allDay := a.taskWith(t, alice, alice.inboxID, "all day", "c", `,"due_date":"2026-10-31"`)
	early := a.taskWith(t, alice, alice.inboxID, "early", "d", `,"due_date":"2026-10-31","due_time":"08:30"`)
	before := a.taskWith(t, alice, alice.inboxID, "before", "e", `,"due_date":"2026-10-30","due_time":"23:00"`)

	want := []uuid.UUID{before, allDay, early, late, noDate}
	wantIDs(t, a.queryIDs(t, alice, "sort=due"), want)
	// The same order across pages of one.
	wantIDs(t, a.allPages(t, alice, "sort=due&limit=1"), want)
}

func TestQueryTasksPagination(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	work := a.createList(t, alice, "Work", "b0")
	var want []uuid.UUID
	for _, p := range []string{"a", "b", "c"} {
		want = append(want, a.taskWith(t, alice, alice.inboxID, p, p, ""))
	}
	for _, p := range []string{"a", "b"} {
		want = append(want, a.taskWith(t, alice, work.Id, p, p, ""))
	}
	// Equal positions are ordered by id.
	tie1 := a.taskWith(t, alice, work.Id, "tie", "c", "")
	tie2 := a.taskWith(t, alice, work.Id, "tie", "c", "")
	if tie2.String() < tie1.String() {
		tie1, tie2 = tie2, tie1
	}
	want = append(want, tie1, tie2)

	for _, limit := range []int{1, 2, 3, 7, 100} {
		wantIDs(t, a.allPages(t, alice, fmt.Sprintf("limit=%d", limit)), want)
	}

	for _, bad := range []string{"cursor=garbage", "sort=due&cursor=" + firstCursor(t, a, alice, "limit=1")} {
		wantProblem(t, a.call(t, alice, http.MethodGet, "/tasks?"+bad, ""), http.StatusBadRequest, ProblemCodeMalformedRequest)
	}
}

func firstCursor(t *testing.T, a *testApp, u user, query string) string {
	t.Helper()
	page := decode[TaskPage](t, a.call(t, u, http.MethodGet, "/tasks?"+query, ""), http.StatusOK)
	next, err := page.NextCursor.Get()
	if err != nil {
		t.Fatal("no next cursor")
	}
	return url.QueryEscape(next)
}

// D-42: case- and accent-insensitive substring of title or description.
func TestQueryTasksSearch(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	cafe := a.taskWith(t, alice, alice.inboxID, "Café con Ñoño", "a", "")
	described := a.taskWith(t, alice, alice.inboxID, "Groceries", "b", `,"description":"buy CAFE beans"`)
	percent := a.taskWith(t, alice, alice.inboxID, "100% done", "c", "")
	a.taskWith(t, alice, alice.inboxID, "1000 done", "d", "")

	wantIDs(t, a.queryIDs(t, alice, "q=cafe"), []uuid.UUID{cafe, described})
	wantIDs(t, a.queryIDs(t, alice, "q="+url.QueryEscape("ÑOÑO")), []uuid.UUID{cafe})
	// "%" is a literal, not a wildcard.
	wantIDs(t, a.queryIDs(t, alice, "q="+url.QueryEscape("100%")), []uuid.UUID{percent})
}

// D-40: the trash holds tasks deleted on their own in the last 30 days,
// whatever their status, and not those deleted with their list.
func TestQueryTasksTrash(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	list := a.createList(t, alice, "Work", "b0")
	open := a.taskWith(t, alice, alice.inboxID, "open", "a", "")
	dropped := a.taskWith(t, alice, alice.inboxID, "dropped", "b", "")
	a.call(t, alice, http.MethodPatch, "/tasks/"+dropped.String(), `{"status":"dropped"}`)
	a.taskWith(t, alice, list.Id, "deleted with its list", "a", "")
	a.taskWith(t, alice, alice.inboxID, "live", "c", "")

	for _, id := range []uuid.UUID{open, dropped} {
		a.call(t, alice, http.MethodDelete, "/tasks/"+id.String(), "")
	}
	a.call(t, alice, http.MethodDelete, "/lists/"+list.Id.String(), "")

	wantIDs(t, a.queryIDs(t, alice, "deleted=true"), []uuid.UUID{open, dropped})
	wantIDs(t, a.queryIDs(t, alice, "deleted=true&status=dropped"), []uuid.UUID{dropped})

	a.clock.Advance(30 * 24 * time.Hour)
	wantIDs(t, a.queryIDs(t, alice, "deleted=true"), []uuid.UUID{})
}

// The page carries each task's checklist, like GET /tasks/{id}.
func TestQueryTasksIncludesChecklist(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	task := a.createTask(t, alice, alice.inboxID, "Trip")
	a.createItem(t, alice, task.Id, "Passport", "a0")
	page := decode[TaskPage](t, a.call(t, alice, http.MethodGet, "/tasks", ""), http.StatusOK)
	if len(page.Items) != 1 || len(*page.Items[0].ChecklistItems) != 1 {
		t.Errorf("page = %+v", page.Items)
	}
}
