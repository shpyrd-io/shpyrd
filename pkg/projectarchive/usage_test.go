package projectarchive

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMeasureVolumeCountsSparseFilesWithoutFollowingLinks(t *testing.T) {
	directory := t.TempDir()
	file, err := os.Create(filepath.Join(directory, "large"))
	if err != nil {
		t.Fatal(err)
	}
	const size = 2 << 30
	if err := file.Truncate(size); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if err := os.Symlink("/", filepath.Join(directory, "outside")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, recoveryDir), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, recoveryDir, "old"), []byte("ignored"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := MeasureVolume(context.Background(), directory, &out); err != nil {
		t.Fatal(err)
	}
	var result struct{ Bytes int64 }
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Bytes != size {
		t.Fatalf("got %d bytes, want %d", result.Bytes, size)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := MeasureVolume(ctx, directory, &out); err == nil {
		t.Fatal("ignored cancellation")
	}
}
