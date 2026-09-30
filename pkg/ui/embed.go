// Package ui embeds the applications the server serves: a folder per
// application in dist/, console/ and workspace/, each what `next build`
// exported for it (apps/<app>/out). `make ui` fills dist before `make
// server`; without it dist holds a .gitkeep alone and the server answers
// with a placeholder page.
package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist is the built applications, rooted at their folders.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
