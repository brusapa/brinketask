package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestMux() *http.ServeMux {
	mux := http.NewServeMux()
	Register(mux, Server{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return mux
}

// decodeProblem checks the status, content type and required fields of a
// problem response (schema Problem in the contract) and returns it.
func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int) Problem {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, wantStatus, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); got != problemContentType {
		t.Errorf("Content-Type = %q, want %q", got, problemContentType)
	}
	var problem Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("body is not a problem: %v", err)
	}
	if problem.Status != wantStatus {
		t.Errorf("problem.status = %d, want %d", problem.Status, wantStatus)
	}
	if problem.Type == "" || problem.Title == "" || problem.Code == "" {
		t.Errorf("problem misses required fields: %+v", problem)
	}
	return problem
}

func TestOperationsAnswerNotImplemented(t *testing.T) {
	mux := newTestMux()
	const id = "0192f1a0-0000-7000-8000-000000000001"
	tests := []struct {
		method, path, body string
	}{
		{http.MethodGet, "/api/v1/me", ""},
		{http.MethodPatch, "/api/v1/me", `{"timezone":"Europe/Madrid"}`},
		{http.MethodGet, "/api/v1/lists", ""},
		{http.MethodDelete, "/api/v1/tasks/" + id, ""},
		{http.MethodPost, "/api/v1/tasks/" + id + "/complete", `{"completion_id":"` + id + `"}`},
		{http.MethodGet, "/api/v1/sync/changes", ""},
		{http.MethodPost, "/api/v1/push/subscriptions/" + id + "/test", ""},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			problem := decodeProblem(t, rec, http.StatusNotImplemented)
			if problem.Code != codeNotImplemented {
				t.Errorf("code = %q, want %q", problem.Code, codeNotImplemented)
			}
		})
	}
}

func TestUnknownAPIPathIsNotFoundProblem(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestMux().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/no-such-thing", nil))
	problem := decodeProblem(t, rec, http.StatusNotFound)
	if problem.Code != codeNotFound {
		t.Errorf("code = %q, want %q", problem.Code, codeNotFound)
	}
}

func TestInvalidRequestsAreValidationProblems(t *testing.T) {
	tests := []struct {
		name, method, path, body string
	}{
		{"path id is not a UUID", http.MethodGet, "/api/v1/lists/not-a-uuid", ""},
		{"body is not JSON", http.MethodPost, "/api/v1/lists", "{"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, strings.NewReader(tt.body))
			newTestMux().ServeHTTP(rec, req)
			problem := decodeProblem(t, rec, http.StatusBadRequest)
			if problem.Code != codeValidationFailed {
				t.Errorf("code = %q, want %q", problem.Code, codeValidationFailed)
			}
		})
	}
}

// failingServer reuses every method of Server (Go "embedding": the methods of
// the embedded field are promoted to the outer type) and overrides GetMe.
type failingServer struct {
	Server
}

func (failingServer) GetMe(context.Context, GetMeRequestObject) (GetMeResponseObject, error) {
	return nil, errors.New("database exploded")
}

func TestHandlerErrorIsGenericInternalProblem(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, failingServer{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/me", nil))

	problem := decodeProblem(t, rec, http.StatusInternalServerError)
	if problem.Code != codeInternal {
		t.Errorf("code = %q, want %q", problem.Code, codeInternal)
	}
	// Internal error details must stay in the server log.
	if strings.Contains(rec.Body.String(), "exploded") {
		t.Errorf("internal error leaked to the client: %s", rec.Body)
	}
}
