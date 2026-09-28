// Package web embeds the built single-page UI (web/dist) into the hub binary.
package web

import (
	"embed"
	"io/fs"
)

// Dist contains the Vite build output. It is populated by `make web`
// (or `npm run build` in web/). A checkout without a build contains only
// .gitkeep, in which case Built() reports false.
//
//go:embed all:dist
var Dist embed.FS

// FS returns the dist subtree.
func FS() (fs.FS, error) {
	return fs.Sub(Dist, "dist")
}

// Built reports whether a UI build is embedded.
func Built() bool {
	f, err := Dist.Open("dist/index.html")
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}
