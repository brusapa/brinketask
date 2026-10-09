package metrics_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brusapa/brinketask/internal/metrics"
	"github.com/brusapa/brinketask/internal/purge"
)

// scrape returns what Prometheus would read.
func scrape(t *testing.T, m *metrics.Metrics) string {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func wantLines(t *testing.T, got string, lines ...string) {
	t.Helper()
	for _, line := range lines {
		if !strings.Contains(got, line+"\n") {
			t.Errorf("missing %q", line)
		}
	}
}

// D-72: requests are labelled by the route pattern, never by the path,
// which would carry ids.
func TestHTTPRequestsByRoute(t *testing.T) {
	m := metrics.New(nil)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/tasks/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("POST /api/v1/tasks", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "{}")
	})
	handler := m.Middleware(mux)
	for _, r := range []*http.Request{
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/tasks/0199c0de-0000-7000-8000-000000000001", nil),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/tasks/0199c0de-0000-7000-8000-000000000002", nil),
		httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/tasks", nil),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nowhere/secret-title", nil),
	} {
		handler.ServeHTTP(httptest.NewRecorder(), r)
	}

	got := scrape(t, m)
	wantLines(t, got,
		`brinketask_http_requests_total{code="404",method="GET",route="GET /api/v1/tasks/{id}"} 2`,
		`brinketask_http_requests_total{code="200",method="POST",route="POST /api/v1/tasks"} 1`,
		`brinketask_http_requests_total{code="404",method="GET",route="unmatched"} 1`,
		`brinketask_http_request_duration_seconds_count{method="POST",route="POST /api/v1/tasks"} 1`,
	)
	for _, leak := range []string{"0199c0de", "secret-title"} {
		if strings.Contains(got, leak) {
			t.Errorf("metrics contain %q", leak)
		}
	}
}

func TestSchedulerAndPurgeMetrics(t *testing.T) {
	m := metrics.New(nil)
	m.ReminderFired(20 * time.Second)
	m.Delivery("sent")
	m.Delivery("sent")
	m.Delivery("gone")
	m.PurgeResult(purge.Result{Tasks: 3, Sessions: 1}, time.Unix(1_800_000_000, 0))
	m.PurgeResult(purge.Result{Skipped: true, Tasks: 99}, time.Unix(1_900_000_000, 0))

	wantLines(t, scrape(t, m),
		`brinketask_reminders_fired_total 1`,
		`brinketask_reminder_fire_delay_seconds_bucket{le="30"} 1`,
		`brinketask_reminder_fire_delay_seconds_bucket{le="15"} 0`,
		`brinketask_deliveries_total{outcome="sent"} 2`,
		`brinketask_deliveries_total{outcome="gone"} 1`,
		`brinketask_purged_rows_total{kind="tasks"} 3`,
		`brinketask_purged_rows_total{kind="sessions"} 1`,
		`brinketask_last_purge_timestamp_seconds 1.8e+09`,
	)
}

// The Go runtime and process collectors are registered.
func TestRuntimeMetrics(t *testing.T) {
	got := scrape(t, metrics.New(nil))
	for _, name := range []string{"go_goroutines ", "process_start_time_seconds "} {
		if !strings.Contains(got, name) {
			t.Errorf("missing %s", name)
		}
	}
}
