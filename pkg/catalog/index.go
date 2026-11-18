package catalog

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

// IndexFile is the file the catalog is published as, at its source's
// address; the entries' files lie beside it, under entries/<name>/.
const IndexFile = "index.json"

// IndexVersion is the shape of the index this code writes and reads.
const IndexVersion = 1

// Index is the catalog as published: every entry, whole, so one request
// lists the catalog and shows an entry; the readme, the icon and the
// overlay are files beside it, by path.
type Index struct {
	Version     int          `json:"version"`
	GeneratedAt time.Time    `json:"generatedAt"`
	Entries     []IndexEntry `json:"entries"`
}

// IndexEntry is one entry of the index: the entry's shpyrd.yaml, its
// name (its folder), its digest (the content of the folder, hashed), the
// commit of the code (together they are a version of the entry), and
// where its files are, as paths from the index.
type IndexEntry struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
	// Commit is the full commit the entry's revision named when the
	// catalog was published. An index written before versions have none.
	Commit string `json:"commit,omitempty"`
	Entry
	Readme  string   `json:"readme,omitempty"`
	Icon    string   `json:"iconFile,omitempty"`
	Overlay *Overlay `json:"overlay,omitempty"`
}

// Overlay is the archive of the files beside the entry's shpyrd.yaml,
// laid over the checkout before a Dockerfile build.
type Overlay struct {
	Path   string   `json:"path"`
	SHA256 string   `json:"sha256"`
	Files  []string `json:"files"`
}

// The files of an entry's folder that are not the overlay.
const (
	readmeFile = "README.md"
)

var iconFiles = []string{"icon.svg", "icon.png", "icon.webp"}

// Find is the entry named name, or nil.
func (ix *Index) Find(name string) *IndexEntry {
	for i := range ix.Entries {
		if ix.Entries[i].Name == name {
			return &ix.Entries[i]
		}
	}
	return nil
}

// Categories are the categories the entries use, each once, in order.
func (ix *Index) Categories() []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range ix.Entries {
		if c := e.Catalog.Category; !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out
}

// ParseIndex reads a published index.
func ParseIndex(b []byte) (*Index, error) {
	var ix Index
	if err := json.Unmarshal(b, &ix); err != nil {
		return nil, fmt.Errorf("%s: %w", IndexFile, err)
	}
	if ix.Version != IndexVersion {
		return nil, fmt.Errorf("%s: version %d; this server reads version %d", IndexFile, ix.Version, IndexVersion)
	}
	for i := range ix.Entries {
		e := &ix.Entries[i]
		if err := e.Validate(e.Name, e.overlayFiles()); err != nil {
			return nil, err
		}
		if e.Commit != "" && !IsFullCommit(e.Commit) {
			return nil, fmt.Errorf("%s: entry %s: commit %q is not a full commit name (40 or 64 lowercase hexadecimal digits)", IndexFile, e.Name, e.Commit)
		}
	}
	return &ix, nil
}

func (e *IndexEntry) overlayFiles() []string {
	if e.Overlay == nil {
		return nil
	}
	return e.Overlay.Files
}

// RefLister lists the branches and tags of the git repository at url, as
// `git ls-remote` does: ref name (refs/heads/main, refs/tags/v1) to commit.
// PublishResolved takes one so that tests, and a catalog that wants no
// network, can say what a repository holds.
type RefLister func(ctx context.Context, url string) (map[string]string, error)

// Resolved is what publishing found for one entry: the revision it names,
// the commit that is, and whether the revision is a branch (a branch
// moves; the version stays the commit it had at publish).
type Resolved struct {
	Name     string
	Revision string
	Commit   string
	Branch   bool
}

// IsFullCommit says s is a full commit name: 40 (SHA-1) or 64 (SHA-256)
// lowercase hexadecimal digits.
func IsFullCommit(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// Publish reads the entry folders under dir (one per entry, named by it)
// and writes the index and the entries' files to out: index.json, and
// entries/<name>/{README.md,icon.*,overlay.tgz}. The entries are
// validated; the first one that is not stops the build. It returns the
// index written. The entries carry no commit: PublishResolved adds it.
func Publish(dir, out string, now time.Time) (*Index, error) {
	ix, _, err := PublishResolved(context.Background(), dir, out, now, nil)
	return ix, err
}

// PublishResolved is Publish, and also resolves each entry's revision to
// the commit it names now, with list, and writes that commit in the
// index. A revision that is a full commit needs no listing. When a
// revision cannot be found, or a repository cannot be listed, nothing is
// written, and the error names every such entry and its revision. With a
// nil list, entries are published without a commit.
func PublishResolved(ctx context.Context, dir, out string, now time.Time, list RefLister) (*Index, []Resolved, error) {
	folders, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	ix := &Index{Version: IndexVersion, GeneratedAt: now.UTC().Truncate(time.Second)}
	var read []*Folder
	var resolved []Resolved
	var problems []string
	listings := map[string]map[string]string{}
	listErrs := map[string]error{}
	for _, f := range folders {
		if !f.IsDir() || strings.HasPrefix(f.Name(), ".") {
			continue
		}
		e, err := ReadEntry(filepath.Join(dir, f.Name()))
		if err != nil {
			return nil, nil, err
		}
		if list != nil {
			r, err := resolve(ctx, e, list, listings, listErrs)
			if err != nil {
				problems = append(problems, err.Error())
			} else {
				e.Commit = r.Commit
				resolved = append(resolved, r)
			}
		}
		read = append(read, e)
	}
	if len(problems) > 0 {
		plural := "ies"
		if len(problems) == 1 {
			plural = "y"
		}
		return nil, nil, fmt.Errorf("no index written; the revision of %d entr%s cannot be resolved:\n  - %s", len(problems), plural, strings.Join(problems, "\n  - "))
	}
	for _, e := range read {
		if err := e.write(filepath.Join(dir, e.Name), out); err != nil {
			return nil, nil, err
		}
		ix.Entries = append(ix.Entries, e.IndexEntry)
	}
	if len(ix.Entries) == 0 {
		return nil, nil, fmt.Errorf("%s holds no entry: an entry is a folder with a %s", dir, EntryFile)
	}
	sort.Slice(ix.Entries, func(i, j int) bool { return ix.Entries[i].Name < ix.Entries[j].Name })
	sort.Slice(resolved, func(i, j int) bool { return resolved[i].Name < resolved[j].Name })
	b, err := json.MarshalIndent(ix, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, nil, err
	}
	if err := os.WriteFile(filepath.Join(out, IndexFile), append(b, '\n'), 0o644); err != nil {
		return nil, nil, err
	}
	return ix, resolved, nil
}

// resolve finds the commit e's revision names. A full commit is taken as
// it is; anything else is looked up among the repository's refs, listed
// once per repository.
func resolve(ctx context.Context, e *Folder, list RefLister, listings map[string]map[string]string, listErrs map[string]error) (Resolved, error) {
	rev := e.Revision()
	r := Resolved{Name: e.Name, Revision: rev}
	if lower := strings.ToLower(rev); IsFullCommit(lower) {
		r.Commit = lower
		return r, nil
	}
	repo := e.Catalog.Source.Git
	refs, ok := listings[repo]
	if !ok {
		if err, failed := listErrs[repo]; failed {
			return r, fmt.Errorf("entry %s, revision %q: %w", e.Name, rev, err)
		}
		var err error
		refs, err = list(ctx, repo)
		if err != nil {
			err = fmt.Errorf("cannot read the repository %s: %w", repo, err)
			listErrs[repo] = err
			return r, fmt.Errorf("entry %s, revision %q: %w", e.Name, rev, err)
		}
		listings[repo] = refs
	}
	commit, found := gitauth.ResolveBranch(refs, rev)
	if !found {
		return r, fmt.Errorf("entry %s, revision %q: no branch, tag or full commit of that name in %s", e.Name, rev, repo)
	}
	r.Commit = commit
	_, r.Branch = refs["refs/heads/"+rev]
	if strings.HasPrefix(rev, "refs/heads/") {
		r.Branch = true
	}
	return r, nil
}

// Folder is an entry read from its folder: what the index says of it,
// and the files of its overlay, in order.
type Folder struct {
	IndexEntry
	overlay []string
	readme  string
	icon    string
}

// ReadEntry reads and validates the entry in folder dir, named by the
// folder.
func ReadEntry(dir string) (*Folder, error) {
	name := filepath.Base(filepath.Clean(dir))
	raw, err := os.ReadFile(filepath.Join(dir, EntryFile))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("entry %s: no %s in %s", name, EntryFile, dir)
		}
		return nil, err
	}
	e, err := Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("entry %s: %w", name, err)
	}
	f := &Folder{IndexEntry: IndexEntry{Name: name, Entry: *e}}
	h := sha256.New()
	hash := func(rel string, b []byte) {
		fmt.Fprintf(h, "%s\x00%d\x00", rel, len(b))
		h.Write(b)
	}
	hash(EntryFile, raw)
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("entry %s: %s is a link; the files of an entry are plain files", name, rel)
		}
		switch {
		case rel == EntryFile:
			return nil
		case rel == readmeFile:
			f.readme = rel
		case isIcon(rel):
			if f.icon != "" {
				return fmt.Errorf("entry %s: two icons, %s and %s; keep one", name, f.icon, rel)
			}
			f.icon = rel
		default:
			f.overlay = append(f.overlay, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(f.overlay)
	for _, rel := range append(append([]string{}, f.overlay...), f.readme, f.icon) {
		if rel == "" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			return nil, err
		}
		hash(rel, b)
	}
	f.Digest = "sha256:" + hex.EncodeToString(h.Sum(nil))
	if err := f.Validate(name, f.overlay); err != nil {
		return nil, err
	}
	return f, nil
}

func isIcon(rel string) bool {
	for _, i := range iconFiles {
		if rel == i {
			return true
		}
	}
	return false
}

// OverlayFiles are the files beside the entry's shpyrd.yaml that the
// overlay carries.
func (f *Folder) OverlayFiles() []string { return f.overlay }

// write copies the entry's files to out/entries/<name>/ and fills in the
// index entry's paths.
func (f *Folder) write(dir, out string) error {
	rel := path.Join("entries", f.Name)
	dst := filepath.Join(out, filepath.FromSlash(rel))
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	copyFile := func(name string) error {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, name), b, 0o644)
	}
	if f.readme != "" {
		if err := copyFile(f.readme); err != nil {
			return err
		}
		f.Readme = path.Join(rel, f.readme)
	}
	if f.icon != "" {
		if err := copyFile(f.icon); err != nil {
			return err
		}
		f.Icon = path.Join(rel, f.icon)
	}
	if len(f.overlay) > 0 {
		b, err := overlayArchive(dir, f.overlay)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dst, "overlay.tgz"), b, 0o644); err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		f.Overlay = &Overlay{Path: path.Join(rel, "overlay.tgz"), SHA256: hex.EncodeToString(sum[:]), Files: f.overlay}
	}
	return nil
}

// overlayArchive is the overlay as a gzipped tar, the same bytes for the
// same files: no times, no owners, so the digest says whether it changed.
func overlayArchive(dir string, files []string) ([]byte, error) {
	var buf bytes.Buffer
	gz, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	tw := tar.NewWriter(gz)
	for _, rel := range files {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		mode := int64(0o644)
		if info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		hdr := &tar.Header{Name: rel, Mode: mode, Size: int64(len(b)), Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0)}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write(b); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ReadOverlay lists the files of an overlay archive and checks it is one:
// plain files with relative paths, no links, no escape.
func ReadOverlay(b []byte) ([]string, error) {
	gz, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("the overlay is not a gzipped tar: %w", err)
	}
	tr := tar.NewReader(gz)
	var files []string
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("the overlay is not a gzipped tar: %w", err)
		}
		clean := path.Clean(hdr.Name)
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		if hdr.Typeflag != tar.TypeReg || path.IsAbs(clean) || clean == "." || strings.HasPrefix(clean, "../") {
			return nil, fmt.Errorf("the overlay holds %q, which is not a plain file inside the checkout", hdr.Name)
		}
		files = append(files, clean)
	}
	return files, nil
}
