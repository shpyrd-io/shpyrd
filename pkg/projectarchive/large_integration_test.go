package projectarchive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type repeatedBlock struct {
	data   []byte
	offset int
}

func (r *repeatedBlock) Read(p []byte) (int, error) {
	n := copy(p, r.data[r.offset:])
	r.offset = (r.offset + n) % len(r.data)
	return n, nil
}

// Round-trips a genuinely >1 GiB file with incompressible content through the
// same volume tar, project tgz, validation and transaction paths as the API.
func TestLargeProjectVolumeRoundTrip(t *testing.T) {
	if os.Getenv("SHPYRD_PROJECT_ARCHIVE_LARGE_TEST") != "1" {
		t.Skip("set SHPYRD_PROJECT_ARCHIVE_LARGE_TEST=1 for the 1.125 GiB filesystem round-trip")
	}
	ctx := context.Background()
	started := time.Now()
	const size int64 = 1152 << 20
	var peak atomic.Uint64
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				if m.HeapAlloc > peak.Load() {
					peak.Store(m.HeapAlloc)
				}
			}
		}
	}()
	defer func() { close(done); <-stopped }()
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	block := make([]byte, 1<<20)
	_, _ = rand.New(rand.NewSource(46)).Read(block)
	hash := sha256.New()
	if _, err = io.CopyN(io.MultiWriter(f, hash), &repeatedBlock{data: block}, size); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := New(t.TempDir(), size+(8<<20))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err = b.Add(ctx, "project.json", strings.NewReader("{}")); err != nil {
		t.Fatal(err)
	}
	r, w := io.Pipe()
	exported := make(chan error, 1)
	go func() { err := ExportVolume(ctx, dir, w); w.CloseWithError(err); exported <- err }()
	err = b.Add(ctx, "volumes/data.tar", r)
	r.CloseWithError(err)
	sourceErr := <-exported
	if err != nil || sourceErr != nil {
		t.Fatalf("export: %v %v", err, sourceErr)
	}
	archive, err := b.File(ctx)
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	read, err := Read(ctx, t.TempDir(), input, size+(8<<20))
	input.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	vol, err := read.Open("volumes/data.tar")
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	v := openTransaction(t, target)
	err = v.Stage(ctx, vol, size)
	vol.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err = v.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := os.Open(filepath.Join(target, "large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	restored := sha256.New()
	n, err := io.Copy(restored, got)
	got.Close()
	if err != nil || n != size || !bytes.Equal(hash.Sum(nil), restored.Sum(nil)) {
		t.Fatalf("checksum mismatch: %d %v", n, err)
	}
	if err = v.Finish(); err != nil {
		t.Fatal(err)
	}
	if peak.Load() > 256<<20 {
		t.Fatalf("large file buffered in RAM: peak heap %d MiB", peak.Load()>>20)
	}
	t.Logf("file=%.3f GiB elapsed=%s peak-Go-heap=%.1f MiB sha256=%s", float64(size)/(1<<30), time.Since(started).Round(time.Millisecond), float64(peak.Load())/(1<<20), fmt.Sprintf("%x", hash.Sum(nil)))
}
