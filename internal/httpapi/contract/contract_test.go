package contract_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brusapa/brinketask/internal/httpapi"
	"github.com/brusapa/brinketask/internal/httpapi/contract"
)

func load(t *testing.T) *contract.Contract {
	t.Helper()
	c, err := httpapi.LoadContract()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func jsonHeader() http.Header {
	return http.Header{"Content-Type": []string{"application/json"}}
}

// The response check must be able to fail; otherwise the contract tests of
// every other package prove nothing.
func TestValidateResponse(t *testing.T) {
	c := load(t)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/me", nil)

	valid := `{"id":"0192f1a0-0000-7000-8000-000000000001","timezone":"Europe/Madrid",` +
		`"all_day_reminder_time":"09:00","inbox_list_id":"0192f1a0-0000-7000-8000-000000000002"}`
	if err := c.ValidateResponse(req, http.StatusOK, jsonHeader(), []byte(valid)); err != nil {
		t.Errorf("valid user rejected: %v", err)
	}

	missingID := `{"timezone":"Europe/Madrid","all_day_reminder_time":"09:00",` +
		`"inbox_list_id":"0192f1a0-0000-7000-8000-000000000002"}`
	if err := c.ValidateResponse(req, http.StatusOK, jsonHeader(), []byte(missingID)); err == nil {
		t.Error("user without id accepted")
	}

	badTime := `{"id":"0192f1a0-0000-7000-8000-000000000001","timezone":"Europe/Madrid",` +
		`"all_day_reminder_time":"9:00:00","inbox_list_id":"0192f1a0-0000-7000-8000-000000000002"}`
	if err := c.ValidateResponse(req, http.StatusOK, jsonHeader(), []byte(badTime)); err == nil {
		t.Error("user with a malformed time accepted")
	}

	// A problem must carry a code from the enum (D-28).
	problem := http.Header{"Content-Type": []string{"application/problem+json"}}
	unknownCode := `{"type":"about:blank","title":"Teapot","status":418,"code":"teapot"}`
	if err := c.ValidateResponse(req, http.StatusTeapot, problem, []byte(unknownCode)); err == nil {
		t.Error("problem with an unknown code accepted")
	}
}

func TestMatch(t *testing.T) {
	c := load(t)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost,
		"/api/v1/tasks/0192f1a0-0000-7000-8000-000000000001/complete", nil)
	route, ok := c.Match(req)
	if !ok {
		t.Fatal("no route for POST /tasks/{id}/complete")
	}
	if route.Method != http.MethodPost || route.Path != "/tasks/{id}/complete" {
		t.Errorf("route = %s %s, want POST /tasks/{id}/complete", route.Method, route.Path)
	}

	if _, ok := c.Match(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/auth/login", nil)); ok {
		t.Error("/auth/login matched an API operation")
	}
}
