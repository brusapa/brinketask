package health

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/brusapa/brinketask/internal/storage"
	"github.com/brusapa/brinketask/internal/testdb"
)

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// pingFunc adapts a plain function to the Pinger interface.
type pingFunc func(ctx context.Context) error

func (f pingFunc) Ping(ctx context.Context) error { return f(ctx) }

func get(t *testing.T, handler http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, Path, nil))
	return rec
}

func TestHandler(t *testing.T) {
	tests := []struct {
		name       string
		ping       pingFunc
		wantStatus int
		wantBody   string
	}{
		{
			name:       "database answers",
			ping:       func(context.Context) error { return nil },
			wantStatus: http.StatusOK,
			wantBody:   "ok\n",
		},
		{
			name:       "database fails",
			ping:       func(context.Context) error { return errors.New("connection refused") },
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "unavailable\n",
		},
		{
			// A database that never answers: the ping only returns when the
			// handler's timeout cancels its context.
			name: "database hangs",
			ping: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "unavailable\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := get(t, Handler(tt.ping, 20*time.Millisecond, discardLogger))
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if rec.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.wantBody)
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", got)
			}
		})
	}
}

func TestRegisterOnlyAnswersGetAndHead(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, pingFunc(func(context.Context) error { return nil }), discardLogger)

	for method, want := range map[string]int{
		http.MethodGet:  http.StatusOK,
		http.MethodHead: http.StatusOK,
		http.MethodPost: http.StatusMethodNotAllowed,
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, Path, nil))
		if rec.Code != want {
			t.Errorf("%s %s: status = %d, want %d", method, Path, rec.Code, want)
		}
	}
}

// Against a real PostgreSQL: 200 while it answers, 503 once the pool is gone.
func TestHandlerWithRealDatabase(t *testing.T) {
	pool, err := storage.Open(context.Background(), testdb.Start(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	handler := Handler(pool, Timeout, discardLogger)

	if rec := get(t, handler); rec.Code != http.StatusOK {
		t.Fatalf("status with database up = %d, want 200", rec.Code)
	}

	pool.Close()
	if rec := get(t, handler); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status with database down = %d, want 503", rec.Code)
	}
}
