package httpapi

import (
	"net/http"
	"testing"
)

func TestGetMe(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	bob := a.signUp(t, "bob")

	// Each session sees its own user, never the other one.
	for _, tt := range []struct {
		u     user
		email string
	}{{alice, "alice@example.com"}, {bob, "bob@example.com"}} {
		me := decode[User](t, a.call(t, tt.u, http.MethodGet, "/me", ""), http.StatusOK)
		if me.Id != tt.u.id || me.InboxListId != tt.u.inboxID {
			t.Errorf("user = %v inbox %v, want %v inbox %v", me.Id, me.InboxListId, tt.u.id, tt.u.inboxID)
		}
		if email, err := me.Email.Get(); err != nil || email != tt.email {
			t.Errorf("email = %q (%v), want %q", email, err, tt.email)
		}
		if !me.DisplayName.IsNull() {
			t.Errorf("display_name = %v, want null", me.DisplayName)
		}
		if me.Timezone != "Europe/Madrid" || me.AllDayReminderTime != "09:00" {
			t.Errorf("settings = %q, %q", me.Timezone, me.AllDayReminderTime)
		}
	}

	anonymous := user{cookie: "none"}
	wantProblem(t, a.call(t, anonymous, http.MethodGet, "/me", ""), http.StatusUnauthorized, ProblemCodeUnauthenticated)
}

func TestPatchMe(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	bob := a.signUp(t, "bob")

	me := decode[User](t, a.call(t, alice, http.MethodPatch, "/me", `{"timezone":"Atlantic/Canary"}`), http.StatusOK)
	if me.Id != alice.id || me.Timezone != "Atlantic/Canary" || me.AllDayReminderTime != "09:00" {
		t.Errorf("after patch: %+v", me)
	}
	me = decode[User](t, a.call(t, alice, http.MethodPatch, "/me", `{"all_day_reminder_time":"08:15"}`), http.StatusOK)
	if me.Timezone != "Atlantic/Canary" || me.AllDayReminderTime != "08:15" {
		t.Errorf("second patch touched other fields: %+v", me)
	}
	// Repeating a patch is harmless.
	me = decode[User](t, a.call(t, alice, http.MethodPatch, "/me", `{"all_day_reminder_time":"08:15"}`), http.StatusOK)
	if me.AllDayReminderTime != "08:15" {
		t.Errorf("repeated patch: %+v", me)
	}
	decode[User](t, a.call(t, alice, http.MethodPatch, "/me", `{}`), http.StatusOK)

	// Alice's patches did not touch Bob.
	other := decode[User](t, a.call(t, bob, http.MethodGet, "/me", ""), http.StatusOK)
	if other.Timezone != "Europe/Madrid" || other.AllDayReminderTime != "09:00" {
		t.Errorf("bob's settings changed: %+v", other)
	}
}

func TestPatchMeValidation(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")

	tests := []struct {
		name, body, field string
	}{
		// Passes the schema, fails the rule that the zone exists.
		{"unknown zone", `{"timezone":"Mars/Olympus_Mons"}`, "/timezone"},
		{"server-local pseudo zone", `{"timezone":"Local"}`, "/timezone"},
		{"hour out of range", `{"all_day_reminder_time":"24:00"}`, "/all_day_reminder_time"},
		{"wrong format", `{"all_day_reminder_time":"9:00"}`, "/all_day_reminder_time"},
		{"null", `{"timezone":null}`, "/timezone"},
		{"unknown field", `{"locale":"es"}`, "/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problem := wantProblem(t, a.call(t, alice, http.MethodPatch, "/me", tt.body),
				http.StatusUnprocessableEntity, ProblemCodeValidationFailed)
			if problem.Errors == nil || len(*problem.Errors) == 0 || (*problem.Errors)[0].Field != tt.field {
				t.Errorf("errors = %+v, want one for %q", problem.Errors, tt.field)
			}
		})
	}

	me := decode[User](t, a.call(t, alice, http.MethodGet, "/me", ""), http.StatusOK)
	if me.Timezone != "Europe/Madrid" || me.AllDayReminderTime != "09:00" {
		t.Errorf("a rejected patch changed the profile: %+v", me)
	}
}

func TestPatchMeCrossOrigin(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	req := newRequest(t, http.MethodPatch, testOrigin+"/api/v1/me", mergePatchType, `{"timezone":"Asia/Tokyo"}`)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	addCookie(req, "brinketask_session", alice.cookie)
	wantProblem(t, serve(t, a.mux, req), http.StatusForbidden, ProblemCodeForbidden)

	me := decode[User](t, a.call(t, alice, http.MethodGet, "/me", ""), http.StatusOK)
	if me.Timezone != "Europe/Madrid" {
		t.Errorf("cross-origin patch applied: %+v", me)
	}
}
