//go:build unix

package projectarchive

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStagingTreeRefusesWritingThroughLinks(t *testing.T) { checkStagingTreeRefusals(t) }

// A link already in the staging tree, wherever it points, is never written
// through: not into a directory outside the tree, not into one inside it.
// The archive's own lexical check cannot see such a link, so the handles do.
func checkStagingTreeRefusals(t *testing.T) {
	t.Helper()
	parent := t.TempDir()
	outside := filepath.Join(parent, "outside")
	dir := filepath.Join(parent, "volume")
	for _, d := range []string{outside, filepath.Join(dir, "real")} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"escape": outside, "inside": "real", "deep/escape": "../../outside"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	tree, err := openStagingTree(root)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	refused := map[string]func() error{
		"file through escaping link":   func() error { f, err := tree.create("escape/x", 0600); closeFile(f); return err },
		"file through inside link":     func() error { f, err := tree.create("inside/x", 0600); closeFile(f); return err },
		"file through deep link":       func() error { f, err := tree.create("deep/escape/x", 0600); closeFile(f); return err },
		"folder through escaping link": func() error { return tree.mkdirAll("escape/sub", 0700) },
		"folder through inside link":   func() error { return tree.mkdirAll("inside/sub/deeper", 0700) },
		"folder through a file":        func() error { return tree.mkdirAll("file/sub", 0700) },
		"link through escaping link":   func() error { return tree.symlink("anywhere", "escape/l") },
		"file over a link":             func() error { f, err := tree.create("escape", 0600); closeFile(f); return err },
	}
	for name, attempt := range refused {
		err := attempt()
		if err == nil {
			t.Errorf("%s: written", name)
		} else if name != "file over a link" && !errors.Is(err, errLinkInPath) {
			t.Errorf("%s: refused for another reason: %v", name, err)
		}
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("outside directory touched: %v %v", entries, err)
	}
	if entries, err := os.ReadDir(filepath.Join(dir, "real")); err != nil || len(entries) != 0 {
		t.Fatalf("directory behind the inside link touched: %v %v", entries, err)
	}
	// The same calls, with no link in the path, do their work.
	if err := tree.mkdirAll("real/sub/deeper", 0700); err != nil {
		t.Fatal(err)
	}
	f, err := tree.create("real/sub/deeper/ok", 0640)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("ok"); err != nil {
		t.Fatal(err)
	}
	closeFile(f)
	if err := tree.symlink("/usr/bin/python3", "real/python"); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "real", "sub", "deeper", "ok")); err != nil || string(got) != "ok" {
		t.Fatalf("file below the root: %q %v", got, err)
	}
	if got, err := os.Readlink(filepath.Join(dir, "real", "python")); err != nil || got != "/usr/bin/python3" {
		t.Fatalf("link below the root: %q %v", got, err)
	}
}

func closeFile(f *os.File) {
	if f != nil {
		f.Close()
	}
}
