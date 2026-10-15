package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopySelfPutsTheBinaryInTheDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := copySelf(dir); err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	want, _ := os.Stat(exe)
	got, err := os.Stat(filepath.Join(dir, "shpyrd-server"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Size() != want.Size() || got.Mode().Perm()&0o111 == 0 {
		t.Errorf("copy = %d bytes mode %v, binary %d bytes", got.Size(), got.Mode(), want.Size())
	}
}
