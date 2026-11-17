package web

import (
	"embed"
	"io/fs"
)

// The Product owns this embedded frontend input. The HTTP server accepts the
// same fs.FS boundary for templates and static assets.
//
// all: preserves underscore- and dot-leading hashed chunks.
//
//go:embed all:html
var content embed.FS

// EmbeddedFrontend returns the Product's bundled Classic/Nexus frontend.
func EmbeddedFrontend() fs.FS {
	bundle, err := fs.Sub(content, "html")
	if err != nil {
		panic(err) // html is fixed by go:embed at build time.
	}
	return bundle
}
