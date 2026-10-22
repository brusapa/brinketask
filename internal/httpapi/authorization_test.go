package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
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
			SELECT r.id || ':' || r.version || ':' || r.seq FROM reminders r
			JOIN tasks t ON t.id = r.task_id JOIN list_members m ON m.list_id = t.list_id WHERE m.user_id = $1
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
	a.call(t, alice, http.MethodPatch, "/tasks/"+task.String(), `{"due_date":"2026-10-10"}`)
	reminder := a.addReminder(t, alice, task, relativeBody(t, 0), http.StatusCreated)
	completed := a.createTask(t, alice, alice.inboxID, "done")
	completion := newID(t)
	a.complete(t, alice, completed.Id, completion, "")
	deletedList := a.createList(t, alice, "Deleted", "c0")
	a.call(t, alice, http.MethodDelete, "/lists/"+deletedList.Id.String(), "")
	// A deleted task with its children, and a deleted tag: for their
	// owner a repeated DELETE answers 204 (D-22); for bob they are as
	// unknown as live ones.
	deletedTask := a.taskWith(t, alice, list.Id, "secret too", "b", `,"due_date":"2026-10-10"`)
	deletedItem := a.createItem(t, alice, deletedTask, "item", "a")
	deletedReminder := a.addReminder(t, alice, deletedTask, relativeBody(t, 0), http.StatusCreated)
	a.call(t, alice, http.MethodDelete, "/tasks/"+deletedTask.String(), "")
	deletedTag := a.createTag(t, alice, "gone")
	a.call(t, alice, http.MethodDelete, "/tags/"+deletedTag.Id.String(), "")
	device := a.register(t, alice, newID(t), a.push.NewDevice(t, "alice's phone"), http.StatusCreated)
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
		{http.MethodPost, taskPath + "/skip", fmt.Sprintf(`{"completion_id":%q}`, newID(t))},
		{http.MethodPost, "/tasks/" + completed.Id.String() + "/uncomplete", fmt.Sprintf(`{"completion_id":%q}`, completion)},
		{http.MethodGet, "/tasks/" + completed.Id.String() + "/completions", ""},
		{http.MethodPost, taskPath + "/checklist-items", newItemBody(newID(t), "x", "a")},
		{http.MethodPatch, "/checklist-items/" + item.Id.String(), `{"is_done":true}`},
		{http.MethodDelete, "/checklist-items/" + item.Id.String(), ""},
		{http.MethodPost, taskPath + "/reminders", relativeBody(t, 5)},
		{http.MethodPatch, "/reminders/" + reminder.Id.String(), `{"offset_minutes":5}`},
		{http.MethodDelete, "/reminders/" + reminder.Id.String(), ""},
		{http.MethodPost, taskPath + "/snooze", fmt.Sprintf(`{"reminder_id":%q,"until":"2026-10-09T10:00:00Z"}`, newID(t))},
		{http.MethodPatch, "/tags/" + tag.Id.String(), `{"name":"x"}`},
		{http.MethodDelete, "/tags/" + tag.Id.String(), ""},
		{http.MethodGet, "/tasks?list_id=" + list.Id.String(), ""},
		{http.MethodGet, "/tasks?tag_id=" + tag.Id.String(), ""},
		{http.MethodGet, "/completions?" + day + "&list_id=" + alice.inboxID.String(), ""},
		{http.MethodGet, "/completions?" + day + "&tag_id=" + tag.Id.String(), ""},

		{http.MethodGet, "/lists/" + deletedList.Id.String(), ""},
		{http.MethodPatch, "/lists/" + deletedList.Id.String(), `{"name":"x"}`},
		{http.MethodDelete, "/lists/" + deletedList.Id.String(), ""},
		{http.MethodGet, "/tasks/" + deletedTask.String(), ""},
		{http.MethodPatch, "/tasks/" + deletedTask.String(), `{"title":"x"}`},
		{http.MethodDelete, "/tasks/" + deletedTask.String(), ""},
		{http.MethodPost, "/tasks/" + deletedTask.String() + "/restore", ""},
		{http.MethodGet, "/tasks/" + deletedTask.String() + "/completions", ""},
		{http.MethodPatch, "/checklist-items/" + deletedItem.Id.String(), `{"is_done":true}`},
		{http.MethodDelete, "/checklist-items/" + deletedItem.Id.String(), ""},
		{http.MethodPatch, "/reminders/" + deletedReminder.Id.String(), `{"offset_minutes":5}`},
		{http.MethodDelete, "/reminders/" + deletedReminder.Id.String(), ""},
		{http.MethodPatch, "/tags/" + deletedTag.Id.String(), `{"name":"x"}`},
		{http.MethodDelete, "/tags/" + deletedTag.Id.String(), ""},
		{http.MethodGet, "/tasks?tag_id=" + deletedTag.Id.String(), ""},

		{http.MethodDelete, "/push/subscriptions/" + device.Id.String(), ""},
		{http.MethodPost, "/push/subscriptions/" + device.Id.String() + "/test", ""},
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
	// A completion_id is a client id like any other (D-23): completing his
	// own task with one of alice's is a conflict, and changes neither.
	wantProblem(t, a.call(t, bob, http.MethodPost, "/tasks/"+bobsTask.Id.String()+"/complete", fmt.Sprintf(`{"completion_id":%q}`, completion)),
		http.StatusConflict, ProblemCodeConflict)
	// Undoing it from his task is a no-op, as for an unknown id.
	if got := a.uncomplete(t, bob, bobsTask.Id, completion); got.Applied {
		t.Errorf("bob undid alice's completion: %+v", got)
	}

	if after := a.snapshot(t, alice); after != before {
		t.Errorf("bob changed alice's data:\nbefore %s\nafter  %s", before, after)
	}
	// Devices are not syncable rows; alice's is still hers.
	devices := decode[ListPushSubscriptions200JSONResponse](t, a.call(t, alice, http.MethodGet, "/push/subscriptions", ""), http.StatusOK)
	if len(devices.Items) != 1 || devices.Items[0].Id != device.Id {
		t.Errorf("alice's devices = %+v", devices.Items)
	}
	a.devices.Wait()
	if got := len(a.push.Messages()); got != 0 {
		t.Errorf("bob's test request reached alice's device: %d message(s)", got)
	}

	// None of bob's listings mentions anything of alice's. Each answers
	// 200 and, where bob has data, shows it: an error response mentions
	// nobody's data and would pass the check below on its own.
	bobsTag := a.createTag(t, bob, "bob")
	bobsDevice := a.register(t, bob, newID(t), a.push.NewDevice(t, "bob's phone"), http.StatusCreated)
	secrets := []string{list.Id.String(), tag.Id.String(), task.String(), item.Id.String(),
		completed.Id.String(), alice.inboxID.String(), deletedTask.String(), deletedItem.Id.String(),
		deletedReminder.Id.String(), deletedTag.Id.String(), device.Id.String(), "secret"}
	for path, own := range map[string][]uuid.UUID{
		"/lists":              {bob.inboxID},
		"/lists?deleted=true": nil,
		"/tags":               {bobsTag.Id},
		"/push/subscriptions": {bobsDevice.Id},
		"/tasks":              {bobsTask.Id},
		"/tasks?deleted=true": nil,
		"/tasks?status=done":  nil,
		"/tasks?q=secret":     nil,
		"/completions?" + day: nil,
		"/sync/changes":       {bob.inboxID, bobsTask.Id, bobsTag.Id},
	} {
		rec := a.call(t, bob, http.MethodGet, path, "")
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
		}
		body := rec.Body.String()
		for _, id := range own {
			if !strings.Contains(body, id.String()) {
				t.Errorf("GET %s does not show bob's %s", path, id)
			}
		}
		for _, secret := range secrets {
			if strings.Contains(body, secret) {
				t.Errorf("GET %s shows bob %q", path, secret)
			}
		}
	}
}
