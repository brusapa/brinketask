package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newListBody(id uuid.UUID, name, position string) string {
	return fmt.Sprintf(`{"id":%q,"name":%q,"position":%q}`, id, name, position)
}

// listSeq reads a list's seq, which the API does not show.
func (a *testApp) listSeq(t *testing.T, id uuid.UUID) int64 {
	t.Helper()
	var seq int64
	if err := a.pool.QueryRow(context.Background(), "SELECT seq FROM lists WHERE id = $1", id).Scan(&seq); err != nil {
		t.Fatal(err)
	}
	return seq
}

func (a *testApp) createList(t *testing.T, u user, name, position string) List {
	t.Helper()
	return decode[List](t, a.call(t, u, http.MethodPost, "/lists", newListBody(newID(t), name, position)), http.StatusCreated)
}

func TestCreateList(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	id := newID(t)

	body := fmt.Sprintf(`{"id":%q,"name":"Work","color":"#AA00ff","position":"b0"}`, id)
	list := decode[List](t, a.call(t, alice, http.MethodPost, "/lists", body), http.StatusCreated)
	if list.Id != id || list.Name != "Work" || list.Position != "b0" || *list.Role != ListRoleOwner || *list.IsInbox {
		t.Errorf("created %+v", list)
	}
	if color, _ := list.Color.Get(); color != "#AA00ff" {
		t.Errorf("color = %q", color)
	}
	if *list.Version != 1 || !list.CreatedAt.Equal(testStart) || !list.DeletedAt.IsNull() {
		t.Errorf("sync meta: version %d created %v deleted %v", *list.Version, list.CreatedAt, list.DeletedAt)
	}
	seq := a.listSeq(t, id)

	// D-04: the same id again, even with another body, returns the list as
	// it is, with 200, and writes nothing.
	again := decode[List](t, a.call(t, alice, http.MethodPost, "/lists", newListBody(id, "Other", "c0")), http.StatusOK)
	if again.Name != "Work" || *again.Version != 1 {
		t.Errorf("repeat returned %+v, want the original unchanged", again)
	}
	if a.listSeq(t, id) != seq {
		t.Error("repeat took a new seq")
	}

	// D-23: someone else's id is a conflict, not a 404.
	bob := a.signUp(t, "bob")
	wantProblem(t, a.call(t, bob, http.MethodPost, "/lists", newListBody(id, "Mine", "a1")),
		http.StatusConflict, ProblemCodeConflict)
}

func TestListListsIsOrderedAndPrivate(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	bob := a.signUp(t, "bob")
	work := a.createList(t, alice, "Work", "b0")
	home := a.createList(t, alice, "Home", "a5")
	a.createList(t, bob, "Bob's", "a1")

	page := decode[ListLists200JSONResponse](t, a.call(t, alice, http.MethodGet, "/lists", ""), http.StatusOK)
	var got []uuid.UUID
	for _, l := range page.Items {
		got = append(got, l.Id)
	}
	// "a0" (inbox) < "a5" < "b0".
	want := []uuid.UUID{alice.inboxID, home.Id, work.Id}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("lists = %v, want %v", got, want)
	}
	if !*page.Items[0].IsInbox || page.Items[0].Name != "Inbox" {
		t.Errorf("first list = %+v, want the inbox", page.Items[0])
	}
}

func TestPatchList(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	list := a.createList(t, alice, "Work", "b0")
	path := "/lists/" + list.Id.String()
	a.clock.Advance(time.Minute)

	patched := decode[List](t, a.call(t, alice, http.MethodPatch, path, `{"name":"Job","color":"#123456"}`), http.StatusOK)
	if patched.Name != "Job" || patched.Position != "b0" || *patched.Version != 2 {
		t.Errorf("patched %+v", patched)
	}
	if !patched.UpdatedAt.Equal(testStart.Add(time.Minute)) {
		t.Errorf("updated_at = %v", patched.UpdatedAt)
	}
	// null clears a nullable field (D-05).
	cleared := decode[List](t, a.call(t, alice, http.MethodPatch, path, `{"color":null}`), http.StatusOK)
	if !cleared.Color.IsNull() || *cleared.Version != 3 {
		t.Errorf("after color null: %+v", cleared)
	}

	// D-41: a patch that changes nothing writes nothing.
	seq := a.listSeq(t, list.Id)
	same := decode[List](t, a.call(t, alice, http.MethodPatch, path, `{"name":"Job","color":null}`), http.StatusOK)
	if *same.Version != 3 || a.listSeq(t, list.Id) != seq {
		t.Errorf("no-op patch wrote: version %d", *same.Version)
	}
}

func TestInboxCannotBeRenamedOrDeleted(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	path := "/lists/" + alice.inboxID.String()

	wantProblem(t, a.call(t, alice, http.MethodPatch, path, `{"name":"Stuff"}`), http.StatusConflict, ProblemCodeConflict)
	wantProblem(t, a.call(t, alice, http.MethodDelete, path, ""), http.StatusConflict, ProblemCodeConflict)

	// Its color and position can change, and sending its current name is
	// not a rename.
	inbox := decode[List](t, a.call(t, alice, http.MethodPatch, path, `{"name":"Inbox","color":"#00FF00","position":"0"}`), http.StatusOK)
	if inbox.Position != "0" {
		t.Errorf("inbox = %+v", inbox)
	}
}

func TestDeleteAndRestoreList(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	list := a.createList(t, alice, "Work", "b0")
	path := "/lists/" + list.Id.String()

	if rec := a.call(t, alice, http.MethodDelete, path, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rec.Code)
	}
	seq := a.listSeq(t, list.Id)
	// D-22: deleting again is a no-op 204; reading or patching is a 404.
	if rec := a.call(t, alice, http.MethodDelete, path, ""); rec.Code != http.StatusNoContent {
		t.Errorf("second delete = %d, want 204", rec.Code)
	}
	if a.listSeq(t, list.Id) != seq {
		t.Error("second delete wrote")
	}
	wantProblem(t, a.call(t, alice, http.MethodGet, path, ""), http.StatusNotFound, ProblemCodeNotFound)
	wantProblem(t, a.call(t, alice, http.MethodPatch, path, `{"name":"x"}`), http.StatusNotFound, ProblemCodeNotFound)

	live := decode[ListLists200JSONResponse](t, a.call(t, alice, http.MethodGet, "/lists", ""), http.StatusOK)
	if len(live.Items) != 1 {
		t.Errorf("live lists = %d, want only the inbox", len(live.Items))
	}
	trash := decode[ListLists200JSONResponse](t, a.call(t, alice, http.MethodGet, "/lists?deleted=true", ""), http.StatusOK)
	if len(trash.Items) != 1 || trash.Items[0].Id != list.Id || trash.Items[0].DeletedAt.IsNull() {
		t.Fatalf("trash = %+v, want the deleted list", trash.Items)
	}

	restored := decode[List](t, a.call(t, alice, http.MethodPost, path+"/restore", ""), http.StatusOK)
	if !restored.DeletedAt.IsNull() || *restored.Version != 3 {
		t.Errorf("restored %+v", restored)
	}
	// Restoring a live list returns it unchanged.
	again := decode[List](t, a.call(t, alice, http.MethodPost, path+"/restore", ""), http.StatusOK)
	if *again.Version != 3 {
		t.Errorf("restoring a live list wrote: version %d", *again.Version)
	}
	decode[List](t, a.call(t, alice, http.MethodGet, path, ""), http.StatusOK)
}

// SPEC section 8: deleted lists can be restored for 30 days, measured with
// the injected clock (D-17).
func TestListRestoreWindow(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	list := a.createList(t, alice, "Work", "b0")
	path := "/lists/" + list.Id.String()
	a.call(t, alice, http.MethodDelete, path, "")

	a.advance(t, 30*24*time.Hour-time.Second, &alice)
	trash := decode[ListLists200JSONResponse](t, a.call(t, alice, http.MethodGet, "/lists?deleted=true", ""), http.StatusOK)
	if len(trash.Items) != 1 {
		t.Fatal("list left the trash before 30 days")
	}

	a.advance(t, time.Second, &alice)
	trash = decode[ListLists200JSONResponse](t, a.call(t, alice, http.MethodGet, "/lists?deleted=true", ""), http.StatusOK)
	if len(trash.Items) != 0 {
		t.Error("list still in the trash after 30 days")
	}
	wantProblem(t, a.call(t, alice, http.MethodPost, path+"/restore", ""), http.StatusConflict, ProblemCodeConflict)
}

func TestListsOfOthersAreNotFound(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	bob := a.signUp(t, "bob")
	list := a.createList(t, alice, "Work", "b0")
	path := "/lists/" + list.Id.String()

	wantProblem(t, a.call(t, bob, http.MethodGet, path, ""), http.StatusNotFound, ProblemCodeNotFound)
	wantProblem(t, a.call(t, bob, http.MethodPatch, path, `{"name":"Mine"}`), http.StatusNotFound, ProblemCodeNotFound)
	wantProblem(t, a.call(t, bob, http.MethodDelete, path, ""), http.StatusNotFound, ProblemCodeNotFound)
	wantProblem(t, a.call(t, bob, http.MethodPost, path+"/restore", ""), http.StatusNotFound, ProblemCodeNotFound)

	unchanged := decode[List](t, a.call(t, alice, http.MethodGet, path, ""), http.StatusOK)
	if unchanged.Name != "Work" || *unchanged.Version != 1 {
		t.Errorf("bob changed alice's list: %+v", unchanged)
	}
}
