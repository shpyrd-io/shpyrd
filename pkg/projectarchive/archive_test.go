package projectarchive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	b, err := New(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	for name, data := range entries {
		if err := b.Add(context.Background(), name, strings.NewReader(data)); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := b.Write(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestBundleResourceInventories(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries map[string]string
	}{
		{"no volumes or databases", map[string]string{"project.json": `{"release":7}`, "source.tgz": "source"}},
		{"two volumes", map[string]string{"project.json": "{}", "volumes/uploads.tar": "uploads", "volumes/cache.tar": "cache"}},
		{"two databases and volumes", map[string]string{"project.json": "{}", "volumes/a.tar": "a", "volumes/b.tar": "b", "databases/primary.dump": "primary", "databases/events.dump": "events"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := Read(context.Background(), t.TempDir(), bytes.NewReader(testArchive(t, tc.entries)), 0)
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			if len(b.Names()) != len(tc.entries) {
				t.Fatal(b.Names())
			}
			for name, want := range tc.entries {
				f, err := b.Open(name)
				if err != nil {
					t.Fatal(err)
				}
				got, err := io.ReadAll(f)
				f.Close()
				if err != nil || string(got) != want {
					t.Fatalf("%s: %q, %v", name, got, err)
				}
			}
			dir := b.dir
			if err := b.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("temporary data remains: %v", err)
			}
		})
	}
}

func TestReadRejectsDamageAndCleansTemporaryFiles(t *testing.T) {
	valid := testArchive(t, map[string]string{"project.json": "{}", "databases/main.dump": strings.Repeat("data", 100)})
	badCRC := bytes.Clone(valid)
	badCRC[len(badCRC)-8] ^= 0xff
	for name, data := range map[string][]byte{
		"truncated header": valid[:5], "truncated stream": valid[:len(valid)/2], "missing trailer": valid[:len(valid)-8], "bad CRC": badCRC,
		"extra member": append(bytes.Clone(valid), valid...), "extra garbage": append(bytes.Clone(valid), 'x'),
		"missing project": testArchive(t, map[string]string{"source.tgz": "x"}),
	} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			b, err := Read(context.Background(), parent, bytes.NewReader(data), 0)
			if err == nil {
				b.Close()
				t.Fatal("accepted damaged archive")
			}
			entries, err := os.ReadDir(parent)
			if err != nil || len(entries) != 0 {
				t.Fatalf("left temporary files: %v %v", entries, err)
			}
		})
	}
}

func TestBundleLimitsAndCancellation(t *testing.T) {
	b, err := New(t.TempDir(), 5)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.Add(ctx, "project.json", strings.NewReader("{}")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := b.Add(context.Background(), "project.json", strings.NewReader("{}")); err != nil {
		t.Fatal(err)
	}
	if err := b.Add(context.Background(), "large", strings.NewReader("four")); err == nil {
		t.Fatal("size limit ignored")
	}
	if b.Size() != 2 || len(b.Names()) != 1 {
		t.Fatal("failed entry altered inventory")
	}
	for _, name := range []string{"../escape", "/absolute", "a/../b", "a\\b", "manifest.json", "project.json"} {
		if err := b.Add(context.Background(), name, strings.NewReader("x")); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	data := testArchive(t, map[string]string{"project.json": strings.Repeat("x", 100)})
	if got, err := Read(context.Background(), t.TempDir(), bytes.NewReader(data), 50); err == nil {
		got.Close()
		t.Fatal("expanded limit ignored")
	}
}

func TestReadRejectsInventoryTampering(t *testing.T) {
	data := testArchive(t, map[string]string{"project.json": "original"})
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	var altered bytes.Buffer
	zw := gzip.NewWriter(&altered)
	tw := tar.NewWriter(zw)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		if h.Name == "project.json" {
			body = []byte("tampered")
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	zw.Close()
	gz.Close()
	if b, err := Read(context.Background(), t.TempDir(), &altered, 0); err == nil {
		b.Close()
		t.Fatal("accepted wrong checksum")
	}
}

func TestFileCompleteAndPrivate(t *testing.T) {
	b, err := New(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := b.Add(context.Background(), "project.json", strings.NewReader("{}")); err != nil {
		t.Fatal(err)
	}
	name, err := b.File(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 || filepath.Dir(name) != b.dir {
		t.Fatalf("unsafe file %s: %v", name, info.Mode())
	}
	f, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := Read(context.Background(), t.TempDir(), f, 0)
	if err != nil {
		t.Fatal(err)
	}
	got.Close()
}
