// Package web embeds the built Mini App.
package web

import (
	"embed"
	"io/fs"
)

// The dist directory is produced by `npm run build` in web/miniapp.
// Only .gitkeep is committed, so the Go build works without Node.
//
//go:embed all:miniapp/dist
var dist embed.FS

// MiniApp returns the built Mini App, or nil if it was not built.
func MiniApp() fs.FS {
	sub, err := fs.Sub(dist, "miniapp/dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}
