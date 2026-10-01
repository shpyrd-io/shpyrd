// Package ui embeds the applications the server serves: a folder per
// application in dist/, console/ and workspace/, each what `next build`
// exported for it (apps/<app>/out). `make ui` fills dist before `make
// server`; without it dist holds a .gitkeep alone and the server answers
// with a placeholder page.
package ui

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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

// Export writes the embedded applications to a directory, as they are in
// dist/: `shpyrd-server ui-export <dir>`, which an init container runs so
// the server reads its applications from a directory in every install,
// and an image holding only the applications can take the server's place
// there (SHPYRD_UI_IMAGE).
func Export(dir string) error {
	return export(Dist(), dir)
}

func export(src fs.FS, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		body, err := fs.ReadFile(src, p)
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		return os.WriteFile(target, body, 0o644)
	})
}
