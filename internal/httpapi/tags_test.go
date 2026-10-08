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

func newTagBody(id uuid.UUID, name string) string {
	return fmt.Sprintf(`{"id":%q,"name":%q}`, id, name)
}

func (a *testApp) createTag(t *testing.T, u user, name string) Tag {
	t.Helper()
	return decode[Tag](t, a.call(t, u, http.MethodPost, "/tags", newTagBody(newID(t), name)), http.StatusCreated)
}

func TestCreateTag(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	id := newID(t)

	tag := decode[Tag](t, a.call(t, alice, http.MethodPost, "/tags",
		fmt.Sprintf(`{"id":%q,"name":"Urgent","color":"#FF0000"}`, id)), http.StatusCreated)
	if tag.Id != id || tag.Name != "Urgent" || *tag.Version != 1 {
		t.Errorf("created %+v", tag)
	}

	// D-04: repeating returns the tag unchanged.
	again := decode[Tag](t, a.call(t, alice, http.MethodPost, "/tags", newTagBody(id, "Other")), http.StatusOK)
	if again.Name != "Urgent" || *again.Version != 1 {
		t.Errorf("repeat returned %+v", again)
	}

	// A live name is unique per user, ignoring case.
	wantProblem(t, a.call(t, alice, http.MethodPost, "/tags", newTagBody(newID(t), "URGENT")),
		http.StatusConflict, ProblemCodeConflict)

	// Another user may use the name, but not the id (D-23).
	bob := a.signUp(t, "bob")
	decode[Tag](t, a.call(t, bob, http.MethodPost, "/tags", newTagBody(newID(t), "urgent")), http.StatusCreated)
	wantProblem(t, a.call(t, bob, http.MethodPost, "/tags", newTagBody(id, "Mine")),
		http.StatusConflict, ProblemCodeConflict)
}

func TestListTags(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	bob := a.signUp(t, "bob")
	work := a.createTag(t, alice, "work")
	errand := a.createTag(t, alice, "Errand")
	a.createTag(t, bob, "bob's")
	gone := a.createTag(t, alice, "gone")
	a.call(t, alice, http.MethodDelete, "/tags/"+gone.Id.String(), "")

	page := decode[ListTags200JSONResponse](t, a.call(t, alice, http.MethodGet, "/tags", ""), http.StatusOK)
	var got []uuid.UUID
	for _, tag := range page.Items {
		got = append(got, tag.Id)
	}
	// Live tags only, by name ignoring case.
	if want := []uuid.UUID{errand.Id, work.Id}; !slices.Equal(got, want) {
		t.Errorf("tags = %v, want %v", got, want)
	}
}

func TestPatchTag(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	tag := a.createTag(t, alice, "work")
	a.createTag(t, alice, "home")
	path := "/tags/" + tag.Id.String()

	// "work" to "Work": changing the case of its own name does not clash
	// with itself.
	patched := decode[Tag](t, a.call(t, alice, http.MethodPatch, path, `{"name":"Work","color":"#00FF00"}`), http.StatusOK)
	if patched.Name != "Work" || *patched.Version != 2 {
		t.Errorf("patched %+v", patched)
	}
	wantProblem(t, a.call(t, alice, http.MethodPatch, path, `{"name":"HOME"}`), http.StatusConflict, ProblemCodeConflict)

	// D-41: no change, no write.
	same := decode[Tag](t, a.call(t, alice, http.MethodPatch, path, `{"name":"Work"}`), http.StatusOK)
	if *same.Version != 2 {
		t.Errorf("no-op patch wrote: version %d", *same.Version)
	}
}

// insertTask writes a task row directly; the task API arrives with its own
// commit.
func (a *testApp) insertTask(t *testing.T, listID uuid.UUID, tagIDs []uuid.UUID, deleted bool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := newID(t)
	var seq int64
	if err := a.pool.QueryRow(ctx, "UPDATE sync_state SET seq = seq + 1 RETURNING seq").Scan(&seq); err != nil {
		t.Fatal(err)
	}
	var deletedAt any
	if deleted {
		deletedAt = testStart
	}
	_, err := a.pool.Exec(ctx, `INSERT INTO tasks (id, list_id, title, status, priority, position, repeat_from,
		tag_ids, version, seq, created_at, updated_at, deleted_at)
		VALUES ($1, $2, 'task', 'open', 0, 'a0', 'due', $3, 1, $4, $5, $5, $6)`,
		id, listID, tagIDs, seq, testStart, deletedAt)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (a *testApp) taskTags(t *testing.T, id uuid.UUID) (tags []uuid.UUID, version int, seq int64) {
	t.Helper()
	err := a.pool.QueryRow(context.Background(), "SELECT tag_ids, version, seq FROM tasks WHERE id = $1", id).
		Scan(&tags, &version, &seq)
	if err != nil {
		t.Fatal(err)
	}
	return tags, version, seq
}

// Deleting a tag removes it from the caller's tasks, live and deleted, and
// each changed task is a write with a new seq, so sync clients learn about
// it (D-07).
func TestDeleteTagRemovesItFromTasks(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	urgent := a.createTag(t, alice, "urgent")
	other := a.createTag(t, alice, "other")

	live := a.insertTask(t, alice.inboxID, []uuid.UUID{urgent.Id, other.Id}, false)
	deleted := a.insertTask(t, alice.inboxID, []uuid.UUID{urgent.Id}, true)
	untouched := a.insertTask(t, alice.inboxID, []uuid.UUID{other.Id}, false)
	_, _, untouchedSeq := a.taskTags(t, untouched)

	path := "/tags/" + urgent.Id.String()
	if rec := a.call(t, alice, http.MethodDelete, path, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rec.Code)
	}

	seen := map[int64]bool{}
	for _, id := range []uuid.UUID{live, deleted} {
		tags, version, seq := a.taskTags(t, id)
		if slices.Contains(tags, urgent.Id) {
			t.Errorf("task %v still has the deleted tag: %v", id, tags)
		}
		if version != 2 || seq <= untouchedSeq || seen[seq] {
			t.Errorf("task %v: version %d seq %d, want version 2 and a new, distinct seq", id, version, seq)
		}
		seen[seq] = true
	}
	if tags, version, seq := a.taskTags(t, live); !slices.Equal(tags, []uuid.UUID{other.Id}) || version != 2 || seq == 0 {
		t.Errorf("live task tags = %v", tags)
	}
	if _, version, seq := a.taskTags(t, untouched); version != 1 || seq != untouchedSeq {
		t.Errorf("task without the tag was written: version %d", version)
	}

	// D-22: deleting again is a no-op; patching a deleted tag is a 404.
	if rec := a.call(t, alice, http.MethodDelete, path, ""); rec.Code != http.StatusNoContent {
		t.Errorf("second delete = %d", rec.Code)
	}
	wantProblem(t, a.call(t, alice, http.MethodPatch, path, `{"name":"x"}`), http.StatusNotFound, ProblemCodeNotFound)

	// Its name is free again.
	a.createTag(t, alice, "Urgent")
}

func TestTagsOfOthersAreNotFound(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	bob := a.signUp(t, "bob")
	tag := a.createTag(t, alice, "work")
	path := "/tags/" + tag.Id.String()

	wantProblem(t, a.call(t, bob, http.MethodPatch, path, `{"name":"mine"}`), http.StatusNotFound, ProblemCodeNotFound)
	wantProblem(t, a.call(t, bob, http.MethodDelete, path, ""), http.StatusNotFound, ProblemCodeNotFound)
	page := decode[ListTags200JSONResponse](t, a.call(t, bob, http.MethodGet, "/tags", ""), http.StatusOK)
	if len(page.Items) != 0 {
		t.Errorf("bob sees %d tags", len(page.Items))
	}
}

// Times in a write's response are those a later read returns: PostgreSQL
// keeps microseconds, so nanoseconds of the clock must not leak out.
func TestWriteResponsesMatchLaterReads(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	a.clock.Advance(123456789 * time.Nanosecond)

	created := a.createTag(t, alice, "work")
	page := decode[ListTags200JSONResponse](t, a.call(t, alice, http.MethodGet, "/tags", ""), http.StatusOK)
	if !page.Items[0].CreatedAt.Equal(*created.CreatedAt) || !page.Items[0].UpdatedAt.Equal(*created.UpdatedAt) {
		t.Errorf("created_at %v / updated_at %v in the response, %v / %v on read",
			created.CreatedAt, created.UpdatedAt, page.Items[0].CreatedAt, page.Items[0].UpdatedAt)
	}
}
