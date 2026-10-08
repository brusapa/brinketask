package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// aliceSnapshot fingerprints every row of alice's that a write would
// touch: a write changes a version and a seq.
func (a *testApp) snapshot(t *testing.T, u user) string {
	t.Helper()
	var s string
	err := a.pool.QueryRow(context.Background(), `
		SELECT string_agg(x, ',' ORDER BY x) FROM (
			SELECT l.id || ':' || l.version || ':' || l.seq AS x FROM lists l
			JOIN list_members m ON m.list_id = l.id WHERE m.user_id = $1
			UNION ALL
			SELECT t.id || ':' || t.version || ':' || t.seq FROM tasks t
			JOIN list_members m ON m.list_id = t.list_id WHERE m.user_id = $1
			UNION ALL
			SELECT c.id || ':' || c.version || ':' || c.seq FROM checklist_items c
			JOIN tasks t ON t.id = c.task_id JOIN list_members m ON m.list_id = t.list_id WHERE m.user_id = $1
			UNION ALL
			SELECT id || ':' || version || ':' || seq FROM tags WHERE owner_id = $1
			UNION ALL
			SELECT id || ':' || coalesce(undone_at::text, '') FROM task_completions c
			WHERE c.task_id IN (SELECT t.id FROM tasks t JOIN list_members m ON m.list_id = t.list_id WHERE m.user_id = $1)
		) rows`, u.id).Scan(&s)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// SPEC section 11, "Authorization": a user cannot read or modify another
// user's resources, and gets 404 for them (CLAUDE.md). Every phase 2
// operation is tried by bob on alice's resources.
func TestAuthorizationMatrix(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	bob := a.signUp(t, "bob")

	list := a.createList(t, alice, "Alice's", "b0")
	tag := a.createTag(t, alice, "alice")
	task := a.taskWith(t, alice, list.Id, "secret", "a", fmt.Sprintf(`,"tag_ids":[%q]`, tag.Id))
	item := a.createItem(t, alice, task, "item", "a")
	completed := a.createTask(t, alice, alice.inboxID, "done")
	completion := newID(t)
	a.complete(t, alice, completed.Id, completion, "")
	deletedList := a.createList(t, alice, "Deleted", "c0")
	a.call(t, alice, http.MethodDelete, "/lists/"+deletedList.Id.String(), "")
	before := a.snapshot(t, alice)

	day := "completed_from=" + url.QueryEscape(testStart.Add(-time.Hour).Format(time.RFC3339)) +
		"&completed_to=" + url.QueryEscape(testStart.Add(time.Hour).Format(time.RFC3339))
	taskPath := "/tasks/" + task.String()
	notFound := []struct{ method, path, body string }{
		{http.MethodGet, "/lists/" + list.Id.String(), ""},
		{http.MethodPatch, "/lists/" + list.Id.String(), `{"name":"x"}`},
		{http.MethodDelete, "/lists/" + list.Id.String(), ""},
		{http.MethodPost, "/lists/" + deletedList.Id.String() + "/restore", ""},
		{http.MethodGet, taskPath, ""},
		{http.MethodPatch, taskPath, `{"title":"x"}`},
		{http.MethodDelete, taskPath, ""},
		{http.MethodPost, taskPath + "/restore", ""},
		{http.MethodPost, taskPath + "/complete", fmt.Sprintf(`{"completion_id":%q}`, newID(t))},
		{http.MethodPost, "/tasks/" + completed.Id.String() + "/uncomplete", fmt.Sprintf(`{"completion_id":%q}`, completion)},
		{http.MethodGet, "/tasks/" + completed.Id.String() + "/completions", ""},
		{http.MethodPost, taskPath + "/checklist-items", newItemBody(newID(t), "x", "a")},
		{http.MethodPatch, "/checklist-items/" + item.Id.String(), `{"is_done":true}`},
		{http.MethodDelete, "/checklist-items/" + item.Id.String(), ""},
		{http.MethodPatch, "/tags/" + tag.Id.String(), `{"name":"x"}`},
		{http.MethodDelete, "/tags/" + tag.Id.String(), ""},
		{http.MethodGet, "/tasks?list_id=" + list.Id.String(), ""},
		{http.MethodGet, "/tasks?tag_id=" + tag.Id.String(), ""},
		{http.MethodGet, "/completions?" + day + "&list_id=" + alice.inboxID.String(), ""},
		{http.MethodGet, "/completions?" + day + "&tag_id=" + tag.Id.String(), ""},
	}
	for _, op := range notFound {
		t.Run(op.method+" "+op.path, func(t *testing.T) {
			wantProblem(t, a.call(t, bob, op.method, op.path, op.body), http.StatusNotFound, ProblemCodeNotFound)
		})
	}

	// Pointing his own data at alice's is invalid (D-27, D-35), and taking
	// an id of hers is a conflict (D-23).
	bobsTask := a.createTask(t, bob, bob.inboxID, "bob's")
	wantProblem(t, a.call(t, bob, http.MethodPost, "/tasks", newTaskBody(newID(t), list.Id, "x")),
		http.StatusUnprocessableEntity, ProblemCodeValidationFailed)
	wantProblem(t, a.call(t, bob, http.MethodPatch, "/tasks/"+bobsTask.Id.String(), fmt.Sprintf(`{"list_id":%q}`, list.Id)),
		http.StatusUnprocessableEntity, ProblemCodeValidationFailed)
	wantProblem(t, a.call(t, bob, http.MethodPatch, "/tasks/"+bobsTask.Id.String(), fmt.Sprintf(`{"tag_ids":[%q]}`, tag.Id)),
		http.StatusUnprocessableEntity, ProblemCodeValidationFailed)
	wantProblem(t, a.call(t, bob, http.MethodPost, "/tasks", newTaskBody(task, bob.inboxID, "x")),
		http.StatusConflict, ProblemCodeConflict)

	if after := a.snapshot(t, alice); after != before {
		t.Errorf("bob changed alice's data:\nbefore %s\nafter  %s", before, after)
	}

	// None of bob's listings mentions anything of alice's.
	secrets := []string{list.Id.String(), tag.Id.String(), task.String(), item.Id.String(),
		completed.Id.String(), alice.inboxID.String(), "secret"}
	for _, path := range []string{
		"/lists", "/lists?deleted=true", "/tags", "/tasks", "/tasks?deleted=true", "/tasks?status=done",
		"/tasks?q=secret", "/completions?" + day, "/sync/changes",
	} {
		body := a.call(t, bob, http.MethodGet, path, "").Body.String()
		for _, secret := range secrets {
			if strings.Contains(body, secret) {
				t.Errorf("GET %s shows bob %q", path, secret)
			}
		}
	}
}
