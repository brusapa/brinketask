// Package webui serves the web client's static files: the single-page app
// on every path the browser may open, and its assets.
package webui

import (
	"bytes"
	"errors"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

// ContentSecurityPolicy allows only our own origin (SPEC section 10).
// style-src without 'unsafe-inline' still lets React set element styles,
// because it sets them through the DOM, not through style attributes in
// HTML.
const ContentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; " +
	"img-src 'self' data:; font-src 'self'; connect-src 'self'; manifest-src 'self'; " +
	"worker-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; " +
	"frame-ancestors 'none'"

// Cache-Control values. Vite puts a hash of the content in every file name
// under assets/, so those never change and can be cached for a year;
// everything else, index.html first, is revalidated on each load so a new
// release is picked up.
const (
	cacheImmutable  = "public, max-age=31536000, immutable"
	cacheRevalidate = "no-cache"
)

func init() {
	// Go's table does not know the manifest of an installable web app;
	// browsers want this type for it.
	if err := mime.AddExtensionType(".webmanifest", "application/manifest+json"); err != nil {
		panic(err)
	}
}

// assetsDir is where Vite writes the hashed files.
const assetsDir = "assets/"

// Register serves the client on "/", the pattern that matches every path no
// other route claims. files is nil when the binary was built without the
// client; then every path answers 404 and a warning is logged once.
func Register(mux *http.ServeMux, files fs.FS, logger *slog.Logger) error {
	if files == nil {
		logger.Warn("built without the web client (build tag webui); only the API is served")
		mux.Handle("/", http.NotFoundHandler())
		return nil
	}
	handler, err := Handler(files)
	if err != nil {
		return err
	}
	mux.Handle("/", handler)
	return nil
}

// Handler returns the handler of the client's files. It fails if files has
// no index.html.
func Handler(files fs.FS) (http.Handler, error) {
	index, err := fs.ReadFile(files, "index.html")
	if err != nil {
		return nil, errors.New("webui: the web client has no index.html")
	}
	return &handler{files: files, index: index}, nil
}

type handler struct {
	files fs.FS
	index []byte
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	header := w.Header()
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "no-referrer")

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		header.Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Paths of the API and of the login routes that no route matched are
	// mistakes, not pages of the app: answering index.html would hide them.
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") {
		http.NotFound(w, r)
		return
	}

	// path.Clean resolves "..", so a request cannot leave the embedded
	// tree; fs.FS rejects such names anyway.
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name != "" && name != "index.html" {
		if h.serveFile(w, r, name) {
			return
		}
		// A missing asset is a 404: answering index.html would hand the
		// browser HTML where it expects a script or a font.
		if name+"/" == assetsDir || strings.HasPrefix(name, assetsDir) {
			http.NotFound(w, r)
			return
		}
	}

	// Any other path is a route of the single-page app (/today,
	// /lists/<id>…): the app reads the URL and renders the right screen.
	header.Set("Content-Security-Policy", ContentSecurityPolicy)
	header.Set("Cache-Control", cacheRevalidate)
	header.Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(h.index))
}

// serveFile writes the named file if it exists and is a regular file.
func (h *handler) serveFile(w http.ResponseWriter, r *http.Request, name string) bool {
	info, err := fs.Stat(h.files, name)
	if err != nil || info.IsDir() {
		return false
	}
	if strings.HasPrefix(name, assetsDir) {
		w.Header().Set("Cache-Control", cacheImmutable)
	} else {
		w.Header().Set("Cache-Control", cacheRevalidate)
	}
	// ServeFileFS sets Content-Type from the extension and handles HEAD and
	// Range requests. Embedded files have no modification time, so no
	// Last-Modified header is sent.
	http.ServeFileFS(w, r, h.files, name) //nolint:gosec // G703: name is cleaned and h.files is the embedded tree, which rejects ".."
	return true
}
