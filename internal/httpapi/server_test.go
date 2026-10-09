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

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func newTestMux(t *testing.T, server StrictServerInterface) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	if err := Register(mux, server, discardLogger); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return mux
}

// serve sends req through handler and checks that the response matches the
// contract (SPEC section 11, "Contract"): its status is declared for the
// operation and its body has the declared schema.
func serve(t *testing.T, handler http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	checkContract(t, req, rec)
	return rec
}

func checkContract(t *testing.T, req *http.Request, rec *httptest.ResponseRecorder) {
	t.Helper()
	c, err := LoadContract()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Match(req); !ok {
		// Unknown paths have no operation to check against.
		return
	}
	if err := c.ValidateResponse(req, rec.Code, rec.Header(), rec.Body.Bytes()); err != nil {
		t.Errorf("response violates the contract: %v\nbody: %s", err, rec.Body)
	}
}

func newRequest(t *testing.T, method, path, contentType, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req
}

// decodeProblem checks the status, content type and required fields of a
// problem response (schema Problem in the contract) and returns it.
func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode ProblemCode) Problem {
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
	if problem.Code != wantCode {
		t.Errorf("problem.code = %q, want %q", problem.Code, wantCode)
	}
	if problem.Type == "" || problem.Title == "" {
		t.Errorf("problem misses required fields: %+v", problem)
	}
	return problem
}

const (
	jsonType       = "application/json"
	mergePatchType = "application/merge-patch+json"
	someID         = "0192f1a0-0000-7000-8000-000000000001"
)

func TestOperationsAnswerNotImplemented(t *testing.T) {
	mux := newTestMux(t, Server{})
	tests := []struct {
		method, path, contentType, body string
	}{
		// Operations of later phases (reminders, recurrence, push).
		{http.MethodGet, "/api/v1/push/subscriptions", "", ""},
		{http.MethodDelete, "/api/v1/reminders/" + someID, "", ""},
		{http.MethodPost, "/api/v1/tasks/" + someID + "/skip", jsonType, `{"completion_id":"` + someID + `"}`},
		{http.MethodPost, "/api/v1/push/subscriptions/" + someID + "/test", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			rec := serve(t, mux, newRequest(t, tt.method, tt.path, tt.contentType, tt.body))
			decodeProblem(t, rec, http.StatusNotImplemented, ProblemCodeNotImplemented)
		})
	}
}

func TestUnknownAPIPathIsNotFoundProblem(t *testing.T) {
	rec := serve(t, newTestMux(t, Server{}), newRequest(t, http.MethodGet, "/api/v1/no-such-thing", "", ""))
	decodeProblem(t, rec, http.StatusNotFound, ProblemCodeNotFound)
}

// A request that cannot be understood at all is a 400.
func TestMalformedRequests(t *testing.T) {
	tests := []struct {
		name, method, path, contentType, body string
	}{
		{"path id is not a UUID", http.MethodGet, "/api/v1/lists/not-a-uuid", "", ""},
		{"body is not JSON", http.MethodPost, "/api/v1/lists", jsonType, "{"},
		{"body is missing", http.MethodPatch, "/api/v1/me", mergePatchType, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, newTestMux(t, Server{}), newRequest(t, tt.method, tt.path, tt.contentType, tt.body))
			decodeProblem(t, rec, http.StatusBadRequest, ProblemCodeMalformedRequest)
		})
	}
}

// A well-formed request that the schema forbids is a 422 that names each
// offending field (D-05: a merge patch may only carry declared fields).
func TestSchemaViolationsAreValidationProblems(t *testing.T) {
	tests := []struct {
		name, method, path, contentType, body string
		wantFields                            []string
	}{
		{"wrong type", http.MethodPatch, "/api/v1/me", mergePatchType, `{"timezone":5}`, []string{"/timezone"}},
		{"pattern", http.MethodPatch, "/api/v1/me", mergePatchType, `{"all_day_reminder_time":"9:00"}`, []string{"/all_day_reminder_time"}},
		{"too long", http.MethodPatch, "/api/v1/me", mergePatchType, `{"timezone":"` + strings.Repeat("A", 65) + `"}`, []string{"/timezone"}},
		{"null in a non-nullable field", http.MethodPatch, "/api/v1/me", mergePatchType, `{"timezone":null}`, []string{"/timezone"}},
		{"unknown field", http.MethodPatch, "/api/v1/me", mergePatchType, `{"locale":"es"}`, []string{"/"}},
		{
			"every error is reported", http.MethodPatch, "/api/v1/me", mergePatchType,
			`{"timezone":5,"all_day_reminder_time":"25:00"}`, []string{"/timezone", "/all_day_reminder_time"},
		},
		{"query parameter out of range", http.MethodGet, "/api/v1/tasks?limit=0", "", "", []string{"limit"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, newTestMux(t, Server{}), newRequest(t, tt.method, tt.path, tt.contentType, tt.body))
			problem := decodeProblem(t, rec, http.StatusUnprocessableEntity, ProblemCodeValidationFailed)
			if problem.Errors == nil {
				t.Fatal("problem has no errors list")
			}
			got := map[string]bool{}
			for _, fieldErr := range *problem.Errors {
				got[fieldErr.Field] = true
				if fieldErr.Message == "" {
					t.Errorf("error for %q has no message", fieldErr.Field)
				}
			}
			for _, field := range tt.wantFields {
				if !got[field] {
					t.Errorf("errors = %+v, want one for %q", *problem.Errors, field)
				}
			}
		})
	}
}

func TestWrongContentTypeIsUnsupported(t *testing.T) {
	// PATCH bodies are merge patches (D-05), not plain JSON.
	rec := serve(t, newTestMux(t, Server{}), newRequest(t, http.MethodPatch, "/api/v1/me", jsonType, `{"timezone":"Europe/Madrid"}`))
	decodeProblem(t, rec, http.StatusUnsupportedMediaType, ProblemCodeUnsupportedMediaType)
}

func TestBodyLimit(t *testing.T) {
	mux := newTestMux(t, Server{})

	// One byte over the limit (D-29) is rejected before any decoding.
	tooLarge := `{"name":"` + strings.Repeat("a", MaxBodyBytes) + `"}`
	rec := serve(t, mux, newRequest(t, http.MethodPost, "/api/v1/lists", jsonType, tooLarge))
	decodeProblem(t, rec, http.StatusRequestEntityTooLarge, ProblemCodePayloadTooLarge)

	// Exactly at the limit, the body reaches validation (and fails it,
	// because the list misses required fields).
	padding := MaxBodyBytes - len(`{"name":""}`)
	atLimit := `{"name":"` + strings.Repeat("a", padding) + `"}`
	rec = serve(t, mux, newRequest(t, http.MethodPost, "/api/v1/lists", jsonType, atLimit))
	decodeProblem(t, rec, http.StatusUnprocessableEntity, ProblemCodeValidationFailed)
}

// recordingServer reuses every method of Server (Go "embedding": the methods
// of the embedded field are promoted to the outer type) and overrides
// PatchMe to record what reached it.
type recordingServer struct {
	Server
	got *PatchMeRequestObject
}

func (s recordingServer) PatchMe(_ context.Context, request PatchMeRequestObject) (PatchMeResponseObject, error) {
	*s.got = request
	return PatchMedefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

// Validation reads the body; the handler must still receive all of it.
func TestValidRequestReachesTheHandler(t *testing.T) {
	var got PatchMeRequestObject
	mux := newTestMux(t, recordingServer{got: &got})
	rec := serve(t, mux, newRequest(t, http.MethodPatch, "/api/v1/me", mergePatchType, `{"timezone":"Atlantic/Canary"}`))
	decodeProblem(t, rec, http.StatusNotImplemented, ProblemCodeNotImplemented)
	if got.Body == nil || got.Body.Timezone == nil || *got.Body.Timezone != "Atlantic/Canary" {
		t.Errorf("handler received %+v, want timezone Atlantic/Canary", got.Body)
	}
}

// Middlewares passed to Register run in the order given, before validation.
func TestMiddlewareOrder(t *testing.T) {
	var order []string
	record := func(name string) MiddlewareFunc {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}
	mux := http.NewServeMux()
	if err := Register(mux, Server{}, discardLogger, record("first"), record("second")); err != nil {
		t.Fatal(err)
	}
	// An invalid body shows that both middlewares ran before validation
	// rejected it.
	serve(t, mux, newRequest(t, http.MethodPatch, "/api/v1/me", mergePatchType, `{"timezone":5}`))
	if strings.Join(order, ",") != "first,second" {
		t.Errorf("middleware order = %v, want [first second]", order)
	}
}

// failingServer overrides GetMe to fail like a broken database would.
type failingServer struct {
	Server
}

func (failingServer) GetMe(context.Context, GetMeRequestObject) (GetMeResponseObject, error) {
	return nil, errors.New("database exploded")
}

func TestHandlerErrorIsGenericInternalProblem(t *testing.T) {
	rec := serve(t, newTestMux(t, failingServer{}), newRequest(t, http.MethodGet, "/api/v1/me", "", ""))
	decodeProblem(t, rec, http.StatusInternalServerError, ProblemCodeInternal)
	// Internal error details must stay in the server log.
	if strings.Contains(rec.Body.String(), "exploded") {
		t.Errorf("internal error leaked to the client: %s", rec.Body)
	}
}
