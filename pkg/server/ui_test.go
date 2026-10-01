package server

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestTheApplicationsComeFromTheDirectoryWhenItHoldsThem(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "console"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "console", "index.html"), []byte("<html>from the directory</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHPYRD_UI_DIR", dir)
	got, err := fs.ReadFile(uiFiles(slog.Default()), "console/index.html")
	if err != nil || string(got) != "<html>from the directory</html>" {
		t.Errorf("console/index.html = %q, %v", got, err)
	}
}

func TestAnEmptyOrMissingDirectoryFallsBackToTheBinary(t *testing.T) {
	for _, dir := range []string{"", t.TempDir(), filepath.Join(t.TempDir(), "missing")} {
		t.Setenv("SHPYRD_UI_DIR", dir)
		files := uiFiles(slog.Default())
		// The binary under test embeds no application: dist holds .gitkeep.
		if _, err := fs.Stat(files, ".gitkeep"); err != nil {
			t.Errorf("SHPYRD_UI_DIR=%q: not the embedded files: %v", dir, err)
		}
	}
}
