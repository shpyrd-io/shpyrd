package ui

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestExportWritesTheApplicationsAsTheyAre(t *testing.T) {
	src := fstest.MapFS{
		"console/index.html":   {Data: []byte("<html>console</html>")},
		"console/_next/a/b.js": {Data: []byte("chunk")},
		"workspace/index.html": {Data: []byte("<html>workspace</html>")},
		".gitkeep":             {Data: nil},
	}
	dir := filepath.Join(t.TempDir(), "ui")
	if err := export(src, dir); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]string{"console/index.html": "<html>console</html>", "console/_next/a/b.js": "chunk", "workspace/index.html": "<html>workspace</html>"} {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v", p, got, err)
		}
	}
	// Running it again over the same directory is fine (a pod restart).
	if err := export(src, dir); err != nil {
		t.Errorf("second export: %v", err)
	}
}
