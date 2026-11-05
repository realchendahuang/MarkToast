// Package frontend embeds MarkToast's dependency-free web UI so the CLI and
// desktop shell remain standalone binaries.
package frontend

import (
	"embed"
	"io/fs"
)

//go:embed index.html app.js i18n.js theme-init.js style.css logo.svg fonts/*
var assets embed.FS

// FS returns the embedded web UI rooted at the frontend directory.
func FS() fs.FS {
	sub, err := fs.Sub(assets, ".")
	if err != nil {
		panic(err)
	}
	return sub
}
