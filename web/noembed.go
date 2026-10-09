//go:build !webui

package web

import "io/fs"

// Files reports that this binary was built without the web client (see
// embed.go).
func Files() (fs.FS, bool) {
	return nil, false
}
