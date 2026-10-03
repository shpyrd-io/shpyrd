// Package projectarchive implements bounded-memory, temporary project exports.
// It is deliberately independent of Kubernetes and object-storage catalogs.
package projectarchive

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const Format = "shpyrd-project-v1"
const DefaultLimit int64 = 64 << 30
const metadataLimit int64 = 8 << 20
const maxEntries = 10000

type Entry struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type Manifest struct {
	Format    string    `json:"format"`
	CreatedAt time.Time `json:"createdAt"`
	Entries   []Entry   `json:"entries"`
}

// Bundle owns temporary files, never the user's project files. Call Close.
type Bundle struct {
	dir         string
	limit, size int64
	entries     map[string]Entry
	files       map[string]string
}

func New(parent string, limit int64) (*Bundle, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	dir, err := os.MkdirTemp(parent, "shpyrd-project-")
	if err != nil {
		return nil, err
	}
	return &Bundle{dir: dir, limit: limit, entries: map[string]Entry{}, files: map[string]string{}}, nil
}

func (b *Bundle) Close() error { return os.RemoveAll(b.dir) }
func (b *Bundle) Size() int64  { return b.size }
func validName(name string) bool {
	return name != "" && name != "." && !strings.HasPrefix(name, "/") && !strings.ContainsAny(name, "\\\x00") && path.Clean(name) == name && name != ".." && !strings.HasPrefix(name, "../")
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

// Add spools one entry with a bounded buffer and a SHA-256. Names never become
// filesystem paths: even a valid archive entry is stored under an opaque name.
func (b *Bundle) Add(ctx context.Context, name string, r io.Reader) error {
	if !validName(name) || name == "manifest.json" {
		return fmt.Errorf("invalid archive entry %q", name)
	}
	if _, ok := b.entries[name]; ok {
		return fmt.Errorf("duplicate archive entry %q", name)
	}
	if len(b.entries) >= maxEntries {
		return errors.New("too many archive entries")
	}
	f, err := os.CreateTemp(b.dir, "entry-")
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(f.Name())
		}
	}()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(contextReader{ctx, r}, b.limit-b.size+1))
	if err != nil {
		return err
	}
	if n > b.limit-b.size {
		return errors.New("project archive exceeds the configured size limit")
	}
	if err := f.Close(); err != nil {
		return err
	}
	b.entries[name] = Entry{Name: name, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}
	b.files[name] = f.Name()
	b.size += n
	ok = true
	return nil
}

func (b *Bundle) Open(name string) (*os.File, error) {
	f, ok := b.files[name]
	if !ok {
		return nil, fmt.Errorf("missing archive entry %q", name)
	}
	return os.Open(f)
}

func (b *Bundle) JSON(name string, out any) error {
	f, err := b.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	if b.entries[name].Size > metadataLimit {
		return errors.New("archive metadata exceeds limit")
	}
	d := json.NewDecoder(io.LimitReader(f, metadataLimit+1))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("trailing archive metadata")
	}
	return nil
}

func (b *Bundle) Names() []string {
	names := make([]string, 0, len(b.entries))
	for name := range b.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (b *Bundle) Write(ctx context.Context, w io.Writer) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	man := Manifest{Format: Format, CreatedAt: time.Now().UTC(), Entries: []Entry{}}
	for _, name := range b.Names() {
		e := b.entries[name]
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: e.Size, Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		f, err := b.Open(name)
		if err != nil {
			return err
		}
		_, err = io.Copy(tw, contextReader{ctx, f})
		f.Close()
		if err != nil {
			return err
		}
		man.Entries = append(man.Entries, e)
	}
	data, err := json.Marshal(man)
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0600, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	if _, err := tw.Write(data); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// Read fully validates and spools an untrusted archive before any restore
// side effects. The limit applies to expanded bytes, not compressed input.
func Read(ctx context.Context, parent string, r io.Reader, limit int64) (_ *Bundle, err error) {
	b, err := New(parent, limit)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			b.Close()
		}
	}()
	input := bufio.NewReader(contextReader{ctx, r})
	gz, err := gzip.NewReader(input)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	gz.Multistream(false)
	tr := tar.NewReader(gz)
	var man Manifest
	found := false
	for {
		hdr, nextErr := tr.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return nil, nextErr
		}
		if found {
			return nil, errors.New("entries after manifest")
		}
		if hdr.Typeflag != tar.TypeReg || hdr.Size < 0 {
			return nil, errors.New("archive entries must be regular files")
		}
		if hdr.Name == "manifest.json" {
			if hdr.Size > metadataLimit {
				return nil, errors.New("archive manifest exceeds limit")
			}
			data, readErr := io.ReadAll(io.LimitReader(tr, metadataLimit+1))
			if readErr != nil {
				return nil, readErr
			}
			if err := json.Unmarshal(data, &man); err != nil {
				return nil, err
			}
			found = true
			continue
		}
		if hdr.Size > b.limit-b.size {
			return nil, errors.New("project archive exceeds the configured size limit")
		}
		if err := b.Add(ctx, hdr.Name, tr); err != nil {
			return nil, err
		}
	}
	// Tar EOF alone does not verify the gzip checksum. Permit bounded zero
	// padding, but reject hidden payloads, extra gzip members and truncation.
	if err := finishTar(gz); err != nil {
		return nil, err
	}
	if _, err := input.ReadByte(); err != io.EOF {
		return nil, errors.New("trailing compressed archive data")
	}
	if !found || man.Format != Format || len(man.Entries) != len(b.entries) {
		return nil, errors.New("invalid or incomplete project manifest")
	}
	seen := map[string]bool{}
	for _, e := range man.Entries {
		actual, ok := b.entries[e.Name]
		if !ok || seen[e.Name] || actual != e {
			return nil, fmt.Errorf("archive checksum or inventory mismatch: %q", e.Name)
		}
		seen[e.Name] = true
	}
	if _, ok := b.entries["project.json"]; !ok {
		return nil, errors.New("project.json missing")
	}
	return b, nil
}

func finishTar(r io.Reader) error {
	const maxPadding = 1 << 20
	data, err := io.ReadAll(io.LimitReader(r, maxPadding+1))
	if err != nil {
		return err
	}
	if len(data) > maxPadding {
		return errors.New("excessive tar padding")
	}
	for _, b := range data {
		if b != 0 {
			return errors.New("trailing tar data")
		}
	}
	return nil
}

// File creates a complete tgz before HTTP headers are sent. An export failure
// therefore never downloads a success-looking, truncated archive.
func (b *Bundle) File(ctx context.Context) (string, error) {
	name := filepath.Join(b.dir, "project.tgz")
	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	err = b.Write(ctx, f)
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	return name, closeErr
}
