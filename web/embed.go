package web

import (
	"embed"
	"io/fs"
)

//go:embed static/*
var content embed.FS

// StaticFS returns the static file system rooted at "static/".
var StaticFS, _ = fs.Sub(content, "static")
