package projectarchive

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testOperation = "0123456789abcdef0123456789abcdef"

func tarEntries(t *testing.T, headers ...tar.Header) []byte {
	t.Helper()
	var out bytes.Buffer
	tw := tar.NewWriter(&out)
	for _, h := range headers {
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if h.Size > 0 {
			if _, err := io.CopyN(tw, strings.NewReader(strings.Repeat("x", int(h.Size))), h.Size); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func volumeFixture(t *testing.T) []byte {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "manifest.json"), []byte("restored contents"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("root manifest"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../manifest.json", filepath.Join(dir, "nested", "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "nested"), 0750); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1720000000, 0)
	if err := os.Chtimes(filepath.Join(dir, "nested"), stamp, stamp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "nested"), 0700) })
	var out bytes.Buffer
	if err := ExportVolume(context.Background(), dir, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func openTransaction(t *testing.T, dir string) *VolumeTransaction {
	t.Helper()
	v, err := OpenVolumeTransaction(dir, testOperation)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })
	return v
}

func TestVolumeRestoreAndRollback(t *testing.T) {
	data := volumeFixture(t)
	if err := ValidateVolume(context.Background(), bytes.NewReader(data), 0); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "original"), []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}
	v := openTransaction(t, dir)
	if err := v.Stage(context.Background(), bytes.NewReader(data), 0); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "original")); err != nil || string(got) != "keep me" {
		t.Fatal("staging altered original")
	}
	if err := v.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := v.Commit(); err != nil {
		t.Fatalf("commit retry: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "nested", "manifest.json"))
	if err != nil || string(got) != "restored contents" {
		t.Fatalf("restored contents: %q, %v", got, err)
	}
	got, err = os.ReadFile(filepath.Join(dir, "nested", "link"))
	if err != nil || string(got) != "root manifest" {
		t.Fatalf("restored link: %q, %v", got, err)
	}
	info, err := os.Stat(filepath.Join(dir, "nested"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0750 || info.ModTime().Unix() != 1720000000 {
		t.Fatalf("directory metadata: %v %v", info.Mode(), info.ModTime())
	}
	// The helper runs as root; give the unprivileged unit test permission to
	// remove a restored read-only directory when exercising rollback.
	if err := os.Chmod(filepath.Join(dir, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	v.Close()
	v = openTransaction(t, dir)
	if err := v.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := v.Rollback(); err != nil {
		t.Fatalf("rollback retry: %v", err)
	}
	got, err = os.ReadFile(filepath.Join(dir, "original"))
	if err != nil || string(got) != "keep me" {
		t.Fatal("rollback lost original")
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); !os.IsNotExist(err) {
		t.Fatal("rollback left restored files")
	}
	if err := v.Finish(); err != nil {
		t.Fatal(err)
	}
}

func TestVolumeEmptyRestoresEmptyAndCanRollback(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "old"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	v := openTransaction(t, dir)
	if err := v.Stage(context.Background(), bytes.NewReader(tarEntries(t)), 0); err != nil {
		t.Fatal(err)
	}
	if err := v.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old")); !os.IsNotExist(err) {
		t.Fatal("empty restore retained old file")
	}
	if err := v.Rollback(); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "old")); err != nil || string(got) != "old" {
		t.Fatal("empty rollback lost data")
	}
}

func TestVolumeRejectsUnsafeArchives(t *testing.T) {
	reg := func(name string) tar.Header {
		return tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0600, Size: 1}
	}
	for name, headers := range map[string][]tar.Header{
		"traversal": {reg("../outside")}, "absolute": {reg("/outside")}, "recovery": {reg(recoveryDir + "/state.json")},
		"duplicate": {reg("a"), reg("a")}, "privilege": {{Name: "a", Typeflag: tar.TypeReg, Mode: 04755}},
		"device":                      {{Name: "a", Typeflag: tar.TypeChar, Mode: 0600}},
		"hardlink":                    {{Name: "a", Typeflag: tar.TypeLink, Linkname: "../outside", Mode: 0600}},
		"link without target":         {{Name: "a", Typeflag: tar.TypeSymlink, Linkname: "", Mode: 0777}},
		"symlink parent":              {{Name: "a", Typeflag: tar.TypeSymlink, Linkname: "b", Mode: 0777}, reg("a/file")},
		"escaping symlink parent":     {{Name: "a", Typeflag: tar.TypeSymlink, Linkname: "/etc", Mode: 0777}, reg("a/file")},
		"symlink parent of a folder":  {{Name: "a", Typeflag: tar.TypeSymlink, Linkname: ".", Mode: 0777}, {Name: "a/b", Typeflag: tar.TypeDir, Mode: 0700}},
		"symlink parent of a link":    {{Name: "a", Typeflag: tar.TypeSymlink, Linkname: ".", Mode: 0777}, {Name: "a/b", Typeflag: tar.TypeSymlink, Linkname: "c", Mode: 0777}},
		"parent declared after child": {reg("a/file"), {Name: "a", Typeflag: tar.TypeSymlink, Linkname: "b", Mode: 0777}},
		"file parent":                 {reg("a"), reg("a/b")},
	} {
		t.Run(name, func(t *testing.T) {
			data := tarEntries(t, headers...)
			err := ValidateVolume(context.Background(), bytes.NewReader(data), 0)
			if err == nil {
				t.Fatal("accepted unsafe archive")
			}
			// An archive that lays a link and then writes through it is
			// refused for that reason, in words (#129).
			if strings.Contains(name, "symlink parent") && !errors.Is(err, errLinkInPath) {
				t.Fatalf("refused for another reason: %v", err)
			}
			dir := t.TempDir()
			v := openTransaction(t, dir)
			if err := v.Stage(context.Background(), bytes.NewReader(data), 0); err == nil {
				t.Fatal("staged unsafe archive")
			}
			if _, err := os.Lstat(filepath.Join(dir, "a", "file")); !os.IsNotExist(err) {
				t.Fatalf("staging wrote through the link: %v", err)
			}
		})
	}
}

// A volume in production holds links the platform cannot answer for: a
// browser's lock links into a temporary directory, a virtual environment's
// interpreter link to the system's, a link to something gone. They go
// through an export and a restore as links, said one by one on the report,
// and never stop the operation (#129).
func TestVolumeLinksRoundTripAsTheyAre(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	for _, dir := range []string{"profile", "venv/bin", "nested"} {
		if err := os.MkdirAll(filepath.Join(source, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(source, "manifest.json"), []byte("root manifest"), 0600); err != nil {
		t.Fatal(err)
	}
	links := map[string]string{
		"profile/SingletonSocket": "/tmp/org.chromium.Chromium.k2Fw1a/SingletonSocket",
		"venv/bin/python":         "/usr/bin/python3",
		"upwards":                 "../../etc/hosts",
		"broken":                  "missing-file",
		"nested/link":             "../manifest.json",
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(source, name)); err != nil {
			t.Fatal(err)
		}
	}
	var archive, report bytes.Buffer
	if err := ExportVolume(ctx, source, &archive, &report); err != nil {
		t.Fatalf("export stopped on a link: %v", err)
	}
	warnings := strings.Split(strings.TrimSpace(report.String()), "\n")
	if len(warnings) != 4 {
		t.Fatalf("report: %q", report.String())
	}
	for _, name := range []string{"profile/SingletonSocket", "venv/bin/python", "upwards", "broken"} {
		found := false
		for _, line := range warnings {
			if strings.HasPrefix(line, WarningPrefix) && strings.Contains(line, " "+name+" ") && strings.Contains(line, links[name]) {
				found = true
			}
		}
		if !found {
			t.Errorf("report does not name %s and its target: %q", name, report.String())
		}
	}
	if strings.Contains(report.String(), "nested/link") {
		t.Fatalf("report warns about a link inside the volume: %q", report.String())
	}
	if err := ValidateVolume(ctx, bytes.NewReader(archive.Bytes()), 0); err != nil {
		t.Fatalf("validation refused the links: %v", err)
	}
	destination := t.TempDir()
	v := openTransaction(t, destination)
	if err := v.Stage(ctx, bytes.NewReader(archive.Bytes()), 0); err != nil {
		t.Fatalf("staging refused the links: %v", err)
	}
	if err := v.Commit(); err != nil {
		t.Fatal(err)
	}
	for name, target := range links {
		got, err := os.Readlink(filepath.Join(destination, name))
		if err != nil || got != target {
			t.Errorf("%s restored as %q, %v; want the link %q", name, got, err, target)
		}
	}
	if got, err := os.ReadFile(filepath.Join(destination, "nested", "link")); err != nil || string(got) != "root manifest" {
		t.Fatalf("link inside the volume: %q, %v", got, err)
	}
	// Fingerprints compare the links' targets, not what they point at: the
	// destination has no browser profile nor interpreter behind them.
	var before, after bytes.Buffer
	if err := FingerprintVolume(ctx, source, &before); err != nil {
		t.Fatal(err)
	}
	if err := FingerprintVolume(ctx, destination, &after); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before.Bytes(), after.Bytes()) {
		t.Fatal("fingerprints differ over links copied as they are")
	}
	if err := os.Symlink("/usr/bin/python3.12", filepath.Join(destination, "venv", "python-other")); err != nil {
		t.Fatal(err)
	}
	after.Reset()
	if err := FingerprintVolume(ctx, destination, &after); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before.Bytes(), after.Bytes()) {
		t.Fatal("fingerprint does not see a link's target")
	}
}

func TestVolumePartialStageRetryAndCancellation(t *testing.T) {
	data := tarEntries(t, tar.Header{Name: "file", Typeflag: tar.TypeReg, Mode: 0600, Size: 2000})
	v := openTransaction(t, t.TempDir())
	if err := v.Stage(context.Background(), bytes.NewReader(data[:1000]), 0); err == nil {
		t.Fatal("accepted truncation")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := v.Stage(ctx, bytes.NewReader(data), 0); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := v.Stage(context.Background(), bytes.NewReader(data), 100); err == nil {
		t.Fatal("accepted oversized volume")
	}
	if err := v.Stage(context.Background(), bytes.NewReader(data), 0); err != nil {
		t.Fatalf("could not retry failed stage: %v", err)
	}
}

func TestVolumeCrashBetweenRenames(t *testing.T) {
	for _, phase := range []string{"staged", "installing"} {
		t.Run(phase, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range []string{"first", "second"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("old-"+name), 0600); err != nil {
					t.Fatal(err)
				}
			}
			v := openTransaction(t, dir)
			data := tarEntries(t, tar.Header{Name: "first", Typeflag: tar.TypeReg, Mode: 0600, Size: 1})
			if err := v.Stage(context.Background(), bytes.NewReader(data), 0); err != nil {
				t.Fatal(err)
			}
			s, err := v.readState()
			if err != nil {
				t.Fatal(err)
			}
			if err := v.moveIfPresent("first", v.base+"/previous/first"); err != nil {
				t.Fatal(err)
			}
			if phase == "installing" {
				if err := v.moveIfPresent("second", v.base+"/previous/second"); err != nil {
					t.Fatal(err)
				}
				s.Phase = phase
				if err := v.save(s); err != nil {
					t.Fatal(err)
				}
				if err := v.moveIfPresent(v.base+"/incoming/first", "first"); err != nil {
					t.Fatal(err)
				}
			}
			v.Close()
			v = openTransaction(t, dir)
			if err := v.Rollback(); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"first", "second"} {
				got, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil || string(got) != "old-"+name {
					t.Fatalf("lost %s: %q %v", name, got, err)
				}
			}
		})
	}
}

func TestVolumeStateCannotNameArbitrarySubtrees(t *testing.T) {
	v := openTransaction(t, t.TempDir())
	if err := v.Stage(context.Background(), bytes.NewReader(tarEntries(t)), 0); err != nil {
		t.Fatal(err)
	}
	if err := v.save(volumeState{Phase: "committed", Incoming: []string{"../outside"}}); err != nil {
		t.Fatal(err)
	}
	if err := v.Rollback(); err == nil {
		t.Fatal("accepted corrupt recovery state")
	}
}

func TestVolumeRootMetadataAndFingerprintSurviveRestore(t *testing.T) {
	ctx := context.Background()
	source, destination := t.TempDir(), t.TempDir()
	if err := os.Chmod(source, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(destination, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "value"), []byte("contents"), 0640); err != nil {
		t.Fatal(err)
	}
	var archive, before, after bytes.Buffer
	if err := ExportVolume(ctx, source, &archive, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := FingerprintVolume(ctx, source, &before); err != nil {
		t.Fatal(err)
	}
	transaction := openTransaction(t, destination)
	if err := transaction.Stage(ctx, &archive, DefaultLimit); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := FingerprintVolume(ctx, destination, &after); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before.Bytes(), after.Bytes()) {
		t.Fatal("restored tree fingerprint differs")
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Fatal("rollback did not restore root permissions")
	}
}
