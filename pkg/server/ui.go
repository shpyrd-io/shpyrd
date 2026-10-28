package server

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

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

// uiCacheDir is where the applications workspaces chose are kept by
// digest (RFC-0084): SHPYRD_UI_CACHE_DIR; else beside the exported
// applications when the server reads them from a directory; else under
// the data directory.
func uiCacheDir(dataDir string) string {
	if d := os.Getenv("SHPYRD_UI_CACHE_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("SHPYRD_UI_DIR"); d != "" {
		return filepath.Join(d, ".cache")
	}
	return filepath.Join(dataDir, "ui")
}
