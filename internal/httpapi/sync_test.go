package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/brusapa/brinketask/internal/tasks"
)

func (a *testApp) changes(t *testing.T, u user, cursor string, limit int) ChangesPage {
	t.Helper()
	query := fmt.Sprintf("?limit=%d", limit)
	if cursor != "" {
		query += "&cursor=" + url.QueryEscape(cursor)
	}
	return decode[ChangesPage](t, a.call(t, u, http.MethodGet, "/sync/changes"+query, ""), http.StatusOK)
}

// replica is what a client keeps from /sync/changes (D-31): every resource
// by id, with its version, removed when a tombstone arrives.
type replica map[uuid.UUID]int

func (r replica) apply(page ChangesPage) {
	put := func(id uuid.UUID, version *int, deleted bool) {
		if deleted {
			delete(r, id)
		} else {
			r[id] = *version
		}
	}
	for _, l := range page.Lists {
		put(l.Id, l.Version, !l.DeletedAt.IsNull())
	}
	for _, task := range page.Tasks {
		put(task.Id, task.Version, !task.DeletedAt.IsNull())
	}
	for _, c := range page.ChecklistItems {
		put(c.Id, c.Version, !c.DeletedAt.IsNull())
	}
	for _, tag := range page.Tags {
		put(tag.Id, tag.Version, !tag.DeletedAt.IsNull())
	}
}

// pull reads pages from cursor until has_more is false and returns the
// final cursor.
func (a *testApp) pull(t *testing.T, u user, r replica, cursor string, limit int) string {
	t.Helper()
	for range 1000 {
		page := a.changes(t, u, cursor, limit)
		r.apply(page)
		cursor = page.NextCursor
		if !page.HasMore {
			return cursor
		}
	}
	t.Fatal("sync did not end")
	return ""
}

// liveState reads what a replica of u should hold, straight from the
// database.
func (a *testApp) liveState(t *testing.T, u user) replica {
	t.Helper()
	rows, err := a.pool.Query(context.Background(), `
		SELECT l.id, l.version FROM lists l JOIN list_members m ON m.list_id = l.id
		WHERE m.user_id = $1 AND l.deleted_at IS NULL
		UNION ALL
		SELECT t.id, t.version FROM tasks t JOIN list_members m ON m.list_id = t.list_id
		WHERE m.user_id = $1 AND t.deleted_at IS NULL
		UNION ALL
		SELECT c.id, c.version FROM checklist_items c JOIN tasks t ON t.id = c.task_id
		JOIN list_members m ON m.list_id = t.list_id
		WHERE m.user_id = $1 AND c.deleted_at IS NULL AND t.deleted_at IS NULL
		UNION ALL
		SELECT id, version FROM tags WHERE owner_id = $1 AND deleted_at IS NULL`, u.id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	state := replica{}
	for rows.Next() {
		var id uuid.UUID
		var version int
		if err := rows.Scan(&id, &version); err != nil {
			t.Fatal(err)
		}
		state[id] = version
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return state
}

func wantReplica(t *testing.T, got, want replica) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("replica has %d resources, database %d\n got %v\nwant %v", len(got), len(want), got, want)
	}
}

// Without a cursor: the full live state, with no tombstones and nothing of
// other users.
func TestSyncFullState(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	bob := a.signUp(t, "bob")
	a.createTag(t, alice, "work")
	gone := a.createTag(t, alice, "gone")
	a.call(t, alice, http.MethodDelete, "/tags/"+gone.Id.String(), "")
	task := a.createTask(t, alice, alice.inboxID, "kept")
	a.createItem(t, alice, task.Id, "item", "a")
	deleted := a.createTask(t, alice, alice.inboxID, "deleted")
	a.createItem(t, alice, deleted.Id, "item of a deleted task", "a")
	a.call(t, alice, http.MethodDelete, "/tasks/"+deleted.Id.String(), "")
	a.createTask(t, bob, bob.inboxID, "bob's")

	page := a.changes(t, alice, "", 100)
	if page.HasMore || len(page.Lists) != 1 || len(page.Tasks) != 1 || len(page.ChecklistItems) != 1 || len(page.Tags) != 1 {
		t.Fatalf("full state: %d lists, %d tasks, %d items, %d tags, has_more %v",
			len(page.Lists), len(page.Tasks), len(page.ChecklistItems), len(page.Tags), page.HasMore)
	}
	// Tasks come without their children, which have their own arrays.
	if page.Tasks[0].ChecklistItems != nil || page.Tasks[0].Reminders != nil {
		t.Errorf("task in sync carries children: %+v", page.Tasks[0])
	}
	r := replica{}
	r.apply(page)
	wantReplica(t, r, a.liveState(t, alice))
}

// With a cursor: exactly what changed since, tombstones included, and
// nothing more once the client is up to date.
func TestSyncChangesSinceCursor(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	list := a.createList(t, alice, "Work", "b0")
	inList := a.createTask(t, alice, list.Id, "in the list")
	tag := a.createTag(t, alice, "urgent")
	tagged := a.taskWith(t, alice, alice.inboxID, "tagged", "a", fmt.Sprintf(`,"tag_ids":[%q]`, tag.Id))
	untouched := a.createTask(t, alice, alice.inboxID, "untouched")
	item := a.createItem(t, alice, untouched.Id, "item", "a")

	r := replica{}
	cursor := a.pull(t, alice, r, "", 100)

	a.call(t, alice, http.MethodDelete, "/lists/"+list.Id.String(), "")
	a.call(t, alice, http.MethodDelete, "/tags/"+tag.Id.String(), "")
	a.call(t, alice, http.MethodDelete, "/checklist-items/"+item.Id.String(), "")

	page := a.changes(t, alice, cursor, 100)
	changed := map[uuid.UUID]bool{}
	tombstones := map[uuid.UUID]bool{}
	for _, l := range page.Lists {
		changed[l.Id], tombstones[l.Id] = true, !l.DeletedAt.IsNull()
	}
	for _, task := range page.Tasks {
		changed[task.Id], tombstones[task.Id] = true, !task.DeletedAt.IsNull()
	}
	for _, c := range page.ChecklistItems {
		changed[c.Id], tombstones[c.Id] = true, !c.DeletedAt.IsNull()
	}
	for _, g := range page.Tags {
		changed[g.Id], tombstones[g.Id] = true, !g.DeletedAt.IsNull()
	}
	want := map[uuid.UUID]bool{list.Id: true, inList.Id: true, tag.Id: true, tagged: false, item.Id: true}
	if fmt.Sprint(tombstones) != fmt.Sprint(want) {
		t.Errorf("changes (id: tombstone) = %v\n want %v", tombstones, want)
	}
	if changed[untouched.Id] || changed[alice.inboxID] {
		t.Error("unchanged resources were sent")
	}

	r.apply(page)
	wantReplica(t, r, a.liveState(t, alice))

	// Up to date: nothing new, and the cursor stays.
	empty := a.changes(t, alice, page.NextCursor, 100)
	if empty.HasMore || len(empty.Lists)+len(empty.Tasks)+len(empty.ChecklistItems)+len(empty.Tags) != 0 {
		t.Errorf("second call returned changes: %+v", empty)
	}
	if empty.NextCursor != page.NextCursor {
		t.Error("the cursor moved without changes")
	}
}

func TestSyncPagination(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	for i := range 7 {
		task := a.createTask(t, alice, alice.inboxID, fmt.Sprint(i))
		a.createItem(t, alice, task.Id, "item", "a")
	}
	a.createTag(t, alice, "t")

	for _, limit := range []int{1, 2, 5} {
		r := replica{}
		cursor := ""
		pages := 0
		for {
			page := a.changes(t, alice, cursor, limit)
			n := len(page.Lists) + len(page.Tasks) + len(page.ChecklistItems) + len(page.Tags)
			if n > limit {
				t.Fatalf("page of %d rows with limit %d", n, limit)
			}
			r.apply(page)
			cursor = page.NextCursor
			pages++
			if !page.HasMore {
				break
			}
		}
		wantReplica(t, r, a.liveState(t, alice))
		if want := (16 + limit - 1) / limit; pages < want {
			t.Errorf("limit %d: %d pages, want at least %d", limit, pages, want)
		}
	}
}

// D-21: a cursor below the purge watermark answers 410; a malformed one 400.
func TestSyncCursorErrors(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	for range 3 {
		a.createTask(t, alice, alice.inboxID, "x")
	}
	old := a.changes(t, alice, "", 1).NextCursor
	current := a.pull(t, alice, replica{}, "", 100)

	if _, err := a.pool.Exec(context.Background(), "UPDATE sync_state SET purged_up_to_seq = seq"); err != nil {
		t.Fatal(err)
	}
	wantProblem(t, a.call(t, alice, http.MethodGet, "/sync/changes?cursor="+url.QueryEscape(old), ""),
		http.StatusGone, ProblemCodeCursorExpired)
	// A cursor at the watermark is still complete.
	a.changes(t, alice, current, 100)
	// Without a cursor the client always starts over.
	a.changes(t, alice, "", 100)

	wantProblem(t, a.call(t, alice, http.MethodGet, "/sync/changes?cursor=garbage", ""),
		http.StatusBadRequest, ProblemCodeMalformedRequest)
}

// SPEC section 11: concurrent writes leave no change outside the cursor. A
// client keeps pulling while writers create, edit and delete; once they
// stop and the client catches up, its replica equals the database.
func TestSyncUnderConcurrentWrites(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	ctx := context.Background()

	const writers, rounds = 4, 15
	errs := make(chan error, writers*rounds*3)
	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range rounds {
				id := uuid.New()
				_, _, err := a.tasks.CreateTask(ctx, alice.id, tasks.NewTask{
					ID: id, ListID: alice.inboxID, Title: fmt.Sprintf("w%d-%d", w, i), Position: "a", RepeatFrom: "due",
				})
				if err != nil {
					errs <- err
					continue
				}
				title := "edited"
				if _, err := a.tasks.PatchTask(ctx, alice.id, id, tasks.TaskPatch{Title: &title}); err != nil {
					errs <- err
				}
				if i%3 == 0 {
					if err := a.tasks.DeleteTask(ctx, alice.id, id); err != nil {
						errs <- err
					}
				}
			}
		}()
	}

	// The test goroutine plays the client while the writers run. The
	// writers report through a channel because only this goroutine may
	// fail the test.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	r := replica{}
	cursor := ""
	for running := true; running; {
		select {
		case <-done:
			running = false
		default:
		}
		cursor = a.pull(t, alice, r, cursor, 7)
	}
	close(errs)
	for err := range errs {
		t.Fatalf("writer: %v", err)
	}
	a.pull(t, alice, r, cursor, 7)
	wantReplica(t, r, a.liveState(t, alice))
}
