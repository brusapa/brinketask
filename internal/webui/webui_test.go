package webui

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

var testFiles = fstest.MapFS{
	"index.html":              {Data: []byte("<!doctype html><title>app</title>")},
	"assets/index-abc123.js":  {Data: []byte("console.log(1)")},
	"assets/font-def456.woff": {Data: []byte("font")},
	"robots.txt":              {Data: []byte("User-agent: *")},
}

func serve(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, target, nil))
	return rec
}

func newHandler(t *testing.T) http.Handler {
	t.Helper()
	h, err := Handler(testFiles)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// Every route of the single-page app gets index.html with the CSP and no
// long-term caching.
func TestAppRoutesServeIndex(t *testing.T) {
	h := newHandler(t)
	for _, target := range []string{"/", "/index.html", "/today", "/lists/0192f5b0-0000-7000-8000-000000000001", "/search?q=x", "/a/b/c"} {
		t.Run(target, func(t *testing.T) {
			rec := serve(t, h, http.MethodGet, target)
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>app</title>") {
				t.Fatalf("status %d, body %q", rec.Code, rec.Body.String())
			}
			header := rec.Header()
			if header.Get("Content-Type") != "text/html; charset=utf-8" {
				t.Errorf("Content-Type = %q", header.Get("Content-Type"))
			}
			if header.Get("Content-Security-Policy") != ContentSecurityPolicy {
				t.Errorf("CSP = %q", header.Get("Content-Security-Policy"))
			}
			if header.Get("Cache-Control") != cacheRevalidate {
				t.Errorf("Cache-Control = %q", header.Get("Cache-Control"))
			}
			if header.Get("X-Content-Type-Options") != "nosniff" {
				t.Error("nosniff missing")
			}
		})
	}
}

func TestAssets(t *testing.T) {
	h := newHandler(t)

	rec := serve(t, h, http.MethodGet, "/assets/index-abc123.js")
	if rec.Code != http.StatusOK || rec.Body.String() != "console.log(1)" {
		t.Fatalf("asset: status %d, body %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != cacheImmutable {
		t.Errorf("asset Cache-Control = %q", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/javascript") {
		t.Errorf("asset Content-Type = %q", got)
	}

	// Files outside assets/ are not hashed, so they are revalidated.
	rec = serve(t, h, http.MethodGet, "/robots.txt")
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != cacheRevalidate {
		t.Errorf("robots.txt: status %d, Cache-Control %q", rec.Code, rec.Header().Get("Cache-Control"))
	}

	// A missing asset is a 404, never index.html.
	for _, target := range []string{"/assets/index-old.js", "/assets/", "/assets"} {
		if rec := serve(t, h, http.MethodGet, target); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", target, rec.Code)
		}
	}

	rec = serve(t, h, http.MethodHead, "/assets/index-abc123.js")
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Errorf("HEAD: status %d, %d body bytes", rec.Code, rec.Body.Len())
	}
}

// Unmatched API and login paths are errors, not app routes.
func TestAPIAndAuthPathsAreNotTheApp(t *testing.T) {
	h := newHandler(t)
	for _, target := range []string{"/api/v2/tasks", "/api/", "/auth/unknown"} {
		rec := serve(t, h, http.MethodGet, target)
		if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "<title>") {
			t.Errorf("%s: status %d, body %q", target, rec.Code, rec.Body.String())
		}
	}
}

func TestOnlyGetAndHead(t *testing.T) {
	h := newHandler(t)
	rec := serve(t, h, http.MethodPost, "/today")
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("POST: status %d, Allow %q", rec.Code, rec.Header().Get("Allow"))
	}
}

func TestPathTraversalStaysInside(t *testing.T) {
	h := newHandler(t)
	rec := serve(t, h, http.MethodGet, "/assets/../../../../etc/passwd")
	if strings.Contains(rec.Body.String(), "root:") {
		t.Fatal("served a file outside the client")
	}
}

// The real mux: registered routes keep their handlers, everything else
// reaches the client.
func TestRegisterLeavesOtherRoutesAlone(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") })
	mux.HandleFunc("/api/v1/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	if err := Register(mux, testFiles, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	if rec := serve(t, mux, http.MethodGet, "/healthz"); rec.Body.String() != "ok" {
		t.Errorf("/healthz = %q", rec.Body.String())
	}
	if rec := serve(t, mux, http.MethodGet, "/api/v1/tasks"); rec.Code != http.StatusTeapot {
		t.Errorf("/api/v1/tasks = %d", rec.Code)
	}
	if rec := serve(t, mux, http.MethodGet, "/trash"); !strings.Contains(rec.Body.String(), "<title>app</title>") {
		t.Errorf("/trash = %q", rec.Body.String())
	}
}

func TestWithoutTheClient(t *testing.T) {
	mux := http.NewServeMux()
	if err := Register(mux, nil, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	if rec := serve(t, mux, http.MethodGet, "/"); rec.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404", rec.Code)
	}
	if _, err := Handler(fstest.MapFS{}); err == nil {
		t.Error("Handler accepted files without index.html")
	}
}
