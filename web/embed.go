//go:build webui

// Package web holds the web client. Its Go side only exposes the built
// files (web/dist) to the server binary, which serves them from the same
// origin as the API (D-18).
//
// The files are embedded only when the build tag "webui" is set, which
// `make build` and the Dockerfile do after building the client. Without
// the tag (go test, go vet, the linter) the package compiles without
// web/dist, so the Go side does not need Node.
package web

import (
	"embed"
	"io/fs"
)

// The "all:" prefix also embeds files whose names start with "." or "_".
//
//go:embed all:dist
var dist embed.FS

// Files returns the built client, rooted at web/dist, and true.
func Files() (fs.FS, bool) {
	files, err := fs.Sub(dist, "dist")
	if err != nil {
		// fs.Sub fails only for an invalid path, and "dist" is valid.
		panic(err)
	}
	return files, true
}
