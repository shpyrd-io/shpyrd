package server

import (
	"io/fs"
	"log/slog"
	"os"

	"github.com/shpyrd-io/shpyrd/pkg/ui"
)

// uiFiles are the applications the server serves: the directory named by
// SHPYRD_UI_DIR when it holds an application (the init container wrote
// the server's own there, or an image holding only the applications did,
// RFC-0080), the ones built into the binary otherwise. A directory named
// but empty or without an application is reported and ignored, so a bad
// image never leaves a cluster without its dashboard.
func uiFiles(logger *slog.Logger) fs.FS {
	dir := os.Getenv("SHPYRD_UI_DIR")
	if dir == "" {
		return ui.Dist()
	}
	files := os.DirFS(dir)
	for _, app := range []string{"console", "workspace"} {
		if _, err := fs.Stat(files, app+"/index.html"); err == nil {
			logger.Info("applications served from a directory", "dir", dir)
			return files
		}
	}
	logger.Warn("SHPYRD_UI_DIR holds no application; serving the ones built into the binary", "dir", dir)
	return ui.Dist()
}
