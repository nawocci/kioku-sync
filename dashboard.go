// Package main embeds the compiled dashboard so the server is a single
// binary. The dashboard is built from web/ by the Dockerfile or by
// `npm run build` during development.
package main

import (
	"embed"
	"io/fs"
)

//go:embed all:web/build
var dashboardBuild embed.FS

// dashboard returns the build with web/build as its root, so the index
// sits at / instead of at /web/build/.
func dashboard() (fs.FS, error) {
	return fs.Sub(dashboardBuild, "web/build")
}
