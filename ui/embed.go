// Package ui embeds the web page that `sil serve` serves: plain HTML, CSS and JavaScript, no build
// step, no external reference.
package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:web
var webFS embed.FS

// FS is the page file system, rooted at the web directory (index.html at its top).
func FS() fs.FS {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err) // the directory is embedded above; this cannot happen
	}
	return sub
}
