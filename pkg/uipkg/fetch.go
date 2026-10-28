package uipkg

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/errcode"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// The limits of a package (RFC-0084): what is extracted, not what the
// registry says.
const (
	MaxBytes       = 64 << 20
	MaxFiles       = 4000
	maxManifest    = 4 << 20
	maxDescription = 64 << 10 // shpyrd-app.json
	// ManifestFile is the optional description at the package's root.
	ManifestFile = "shpyrd-app.json"
	// Unused is how long a cached package stays after its last use.
	Unused = 30 * 24 * time.Hour
)

// Manifest is what shpyrd-app.json says about a package: a name and a
// version for people, and the origins beyond the server its pages load
// scripts from, connect to and show images from.
type Manifest struct {
	Name    string   `json:"name,omitempty"`
	Version string   `json:"version,omitempty"`
	Origins []string `json:"origins,omitempty"`
}

// Fetcher brings packages to directories.
type Fetcher struct {
	// CacheDir is where pulled packages live, one directory per digest.
	CacheDir string
	// PlainHTTP says a registry host is reached without TLS: a registry
	// on this machine while developing. Nil means never.
	PlainHTTP func(host string) bool
	// Client makes the registry calls; nil means a client that retries.
	Client *http.Client
}

// Resolve settles a reference: an OCI tag becomes the digest it points at
// now, so what is stored never moves. A directory is checked to exist.
func (f *Fetcher) Resolve(ctx context.Context, ref Ref, cred Credential) (Ref, error) {
	switch ref.Kind {
	case KindDir:
		if st, err := os.Stat(ref.Dir); err != nil || !st.IsDir() {
			return ref, fmt.Errorf("%s is not a directory", ref.Dir)
		}
		return ref, nil
	case KindOCI:
		if ref.Digest != "" {
			return ref, nil
		}
		repo, err := f.repository(ref, cred)
		if err != nil {
			return ref, err
		}
		desc, err := repo.Resolve(ctx, ref.Tag)
		if err != nil {
			return ref, registryError(ref, err)
		}
		ref.Digest = desc.Digest.String()
		return ref, nil
	}
	return ref, errors.New("unknown reference")
}

// Fetch is the directory holding the package: the directory itself for
// dir:, the cache's for oci:, pulled when it is not there yet. The
// manifest is what the package says about itself, nil without one.
func (f *Fetcher) Fetch(ctx context.Context, ref Ref, cred Credential) (string, *Manifest, error) {
	var dir string
	switch ref.Kind {
	case KindDir:
		if st, err := os.Stat(ref.Dir); err != nil || !st.IsDir() {
			return "", nil, fmt.Errorf("%s is not a directory", ref.Dir)
		}
		dir = ref.Dir
	case KindOCI:
		if ref.Digest == "" {
			return "", nil, errors.New("an OCI reference is pulled by digest; resolve it first")
		}
		if f.CacheDir == "" {
			return "", nil, errors.New("no cache directory for packages")
		}
		dir = filepath.Join(f.CacheDir, strings.TrimPrefix(ref.Digest, "sha256:"))
		if st, err := os.Stat(filepath.Join(dir, "index.html")); err == nil && !st.IsDir() {
			now := time.Now()
			_ = os.Chtimes(dir, now, now)
		} else if err := f.pull(ctx, ref, cred, dir); err != nil {
			return "", nil, err
		}
	default:
		return "", nil, errors.New("unknown reference")
	}
	if st, err := os.Stat(filepath.Join(dir, "index.html")); err != nil || st.IsDir() {
		return "", nil, errors.New("the package has no index.html at its root")
	}
	m, err := ReadManifest(dir)
	if err != nil {
		return "", nil, err
	}
	return dir, m, nil
}

// Prune removes cached packages unused for longer than Unused.
func (f *Fetcher) Prune(olderThan time.Duration) {
	entries, err := os.ReadDir(f.CacheDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !e.IsDir() {
			continue
		}
		if time.Since(info.ModTime()) > olderThan {
			_ = os.RemoveAll(filepath.Join(f.CacheDir, e.Name()))
		}
	}
}

func (f *Fetcher) repository(ref Ref, cred Credential) (*remote.Repository, error) {
	repo, err := remote.NewRepository(ref.Host + "/" + ref.Repository)
	if err != nil {
		return nil, err
	}
	client := f.Client
	if client == nil {
		client = retry.DefaultClient
	}
	ac := &auth.Client{Client: client, Cache: auth.NewCache()}
	ac.SetUserAgent("shpyrd")
	if cred.Token != "" {
		ac.Credential = auth.StaticCredential(ref.Host, auth.Credential{Username: cred.Username, Password: cred.Token})
	}
	repo.Client = ac
	repo.PlainHTTP = f.PlainHTTP != nil && f.PlainHTTP(ref.Host)
	return repo, nil
}

// pull brings the package of a digest into dir: the manifest, the image's
// when the digest names an index, then each layer applied in order into
// a directory beside dir, moved into place only when whole.
func (f *Fetcher) pull(ctx context.Context, ref Ref, cred Credential, dir string) error {
	repo, err := f.repository(ref, cred)
	if err != nil {
		return err
	}
	desc, err := repo.Resolve(ctx, ref.Digest)
	if err != nil {
		return registryError(ref, err)
	}
	manifest, err := f.manifestOf(ctx, repo, desc)
	if err != nil {
		return err
	}
	if len(manifest.Layers) == 0 {
		return errors.New("the package has no layers")
	}
	if err := os.MkdirAll(f.CacheDir, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(f.CacheDir, ".pull-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	lim := &limits{}
	for _, layer := range manifest.Layers {
		if err := f.applyLayer(ctx, repo, layer, tmp, lim); err != nil {
			return err
		}
	}
	if st, err := os.Stat(filepath.Join(tmp, "index.html")); err != nil || st.IsDir() {
		return errors.New("the package has no index.html at its root")
	}
	_ = os.RemoveAll(dir)
	return os.Rename(tmp, dir)
}

// manifestOf reads the manifest a descriptor names; for an index, the
// manifest of the image for linux/amd64, else the first that is a real
// platform (an attestation says "unknown"), else the first.
func (f *Fetcher) manifestOf(ctx context.Context, repo *remote.Repository, desc ocispec.Descriptor) (*ocispec.Manifest, error) {
	for range 2 {
		if desc.Size > maxManifest {
			return nil, fmt.Errorf("the manifest is %d bytes; at most %d", desc.Size, maxManifest)
		}
		raw, err := content.FetchAll(ctx, repo.Manifests(), desc)
		if err != nil {
			return nil, fmt.Errorf("reading the manifest: %w", err)
		}
		switch desc.MediaType {
		case ocispec.MediaTypeImageIndex, "application/vnd.docker.distribution.manifest.list.v2+json":
			var index ocispec.Index
			if err := json.Unmarshal(raw, &index); err != nil {
				return nil, fmt.Errorf("the index does not parse: %w", err)
			}
			chosen, ok := pickFromIndex(index)
			if !ok {
				return nil, errors.New("the index names no image")
			}
			desc = chosen
			continue
		default:
			var m ocispec.Manifest
			if err := json.Unmarshal(raw, &m); err != nil {
				return nil, fmt.Errorf("the manifest does not parse: %w", err)
			}
			return &m, nil
		}
	}
	return nil, errors.New("the index names another index")
}

func pickFromIndex(index ocispec.Index) (ocispec.Descriptor, bool) {
	var first, real *ocispec.Descriptor
	for i := range index.Manifests {
		d := &index.Manifests[i]
		if first == nil {
			first = d
		}
		if p := d.Platform; p != nil {
			if p.OS == "linux" && p.Architecture == "amd64" {
				return *d, true
			}
			if real == nil && p.OS != "unknown" && p.Architecture != "unknown" {
				real = d
			}
		}
	}
	if real != nil {
		return *real, true
	}
	if first != nil {
		return *first, true
	}
	return ocispec.Descriptor{}, false
}

type limits struct {
	bytes int64
	files int
}

// applyLayer pulls one layer and writes its files into dir, as a runtime
// would lay an image out: directories, regular files, whiteouts; nothing
// else (links are skipped). The bytes are verified against the digest
// as they are read.
func (f *Fetcher) applyLayer(ctx context.Context, repo *remote.Repository, layer ocispec.Descriptor, dir string, lim *limits) error {
	if layer.Size > MaxBytes*4 {
		return fmt.Errorf("a layer is %d bytes; the package is at most %d extracted", layer.Size, MaxBytes)
	}
	rc, err := repo.Blobs().Fetch(ctx, layer)
	if err != nil {
		return fmt.Errorf("reading a layer: %w", err)
	}
	defer rc.Close()
	vr := content.NewVerifyReader(rc, layer)
	var r io.Reader = vr
	switch {
	case strings.HasSuffix(layer.MediaType, "+gzip"), strings.HasSuffix(layer.MediaType, ".gzip"):
		gz, err := gzip.NewReader(vr)
		if err != nil {
			return fmt.Errorf("a layer is not gzip: %w", err)
		}
		defer gz.Close()
		r = gz
	case strings.HasSuffix(layer.MediaType, "+zstd"), strings.HasSuffix(layer.MediaType, ".zstd"):
		zr, err := zstd.NewReader(vr)
		if err != nil {
			return fmt.Errorf("a layer is not zstd: %w", err)
		}
		defer zr.Close()
		r = zr
	}
	if err := untar(r, dir, lim); err != nil {
		return err
	}
	// Drain what the tar reader did not consume, so the whole blob is
	// checked against its digest.
	if _, err := io.Copy(io.Discard, vr); err != nil {
		return err
	}
	if err := vr.Verify(); err != nil {
		return fmt.Errorf("a layer does not match its digest: %w", err)
	}
	return nil
}

func untar(r io.Reader, dir string, lim *limits) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("a layer is not a tar: %w", err)
		}
		for _, seg := range strings.Split(hdr.Name, "/") {
			if seg == ".." {
				return fmt.Errorf("a layer names %q, outside the package", hdr.Name)
			}
		}
		name := path.Clean("/" + hdr.Name)
		if name == "/" {
			continue
		}
		name = strings.TrimPrefix(name, "/")
		base := path.Base(name)
		target := filepath.Join(dir, filepath.FromSlash(name))
		// Whiteouts (OCI image layout): .wh..wh..opq empties the directory
		// as earlier layers left it; .wh.<name> removes <name>.
		if base == ".wh..wh..opq" {
			parent := filepath.Dir(target)
			entries, _ := os.ReadDir(parent)
			for _, e := range entries {
				_ = os.RemoveAll(filepath.Join(parent, e.Name()))
			}
			continue
		}
		if rest, ok := strings.CutPrefix(base, ".wh."); ok {
			_ = os.RemoveAll(filepath.Join(filepath.Dir(target), rest))
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			lim.files++
			if lim.files > MaxFiles {
				return fmt.Errorf("the package has more than %d files", MaxFiles)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			w, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return err
			}
			n, err := io.Copy(w, io.LimitReader(tr, MaxBytes-lim.bytes+1))
			w.Close()
			if err != nil {
				return err
			}
			lim.bytes += n
			if lim.bytes > MaxBytes {
				return fmt.Errorf("the package is over %d bytes", MaxBytes)
			}
		default:
			// Links, devices, and the rest have no place in a folder of pages.
		}
	}
}

// ReadManifest reads shpyrd-app.json from a package directory; nil when
// there is none. The origins are checked: a scheme and a host, nothing
// after.
func ReadManifest(dir string) (*Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(raw) > maxDescription {
		return nil, fmt.Errorf("%s is over %d bytes", ManifestFile, maxDescription)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s does not parse: %w", ManifestFile, err)
	}
	for i, o := range m.Origins {
		u, err := url.Parse(o)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "wss") || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return nil, fmt.Errorf("%s: origin %q is not a scheme and a host (https://cdn.example.com)", ManifestFile, o)
		}
		m.Origins[i] = u.Scheme + "://" + u.Host
	}
	return &m, nil
}

// registryError says what the registry answered, in words a person who
// set the reference can act on.
func registryError(ref Ref, err error) error {
	status := 0
	var er *errcode.ErrorResponse
	if errors.As(err, &er) {
		status = er.StatusCode
	}
	switch {
	case status == http.StatusUnauthorized, errors.Is(err, auth.ErrBasicCredentialNotFound):
		return fmt.Errorf("%s refused the pull as unauthorized: give the workspace a credential for %s, or make the repository public", ref.Host, ref.Host)
	case status == http.StatusForbidden:
		return fmt.Errorf("%s refused the pull of %s: the credential has no read access to it", ref.Host, ref.Repository)
	case status == http.StatusNotFound, errors.Is(err, errdef.ErrNotFound):
		return fmt.Errorf("%s has no %s at %s", ref.Host, ref.Repository, firstNonEmpty(ref.Digest, ref.Tag))
	}
	return fmt.Errorf("%s: %s", ref.Host, err.Error())
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Digest of bytes, for callers that build packages (tests, tools).
func Digest(b []byte) string { return digest.FromBytes(b).String() }
