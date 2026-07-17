package web

import (
	"embed"
	"io/fs"
)

//go:embed all:static
var staticRoot embed.FS

// Static is the web UI filesystem rooted at static/ contents.
func Static() fs.FS {
	sub, err := fs.Sub(staticRoot, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
