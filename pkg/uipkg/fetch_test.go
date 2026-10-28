package uipkg

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// A registry as a package sees it: manifests and blobs by digest, tags by
// name, optionally behind basic auth. Enough of the distribution API for
// a pull.
type fakeRegistry struct {
	t        *testing.T
	mu       sync.Mutex
	blobs    map[string][]byte
	types    map[string]string // media type of a manifest, by digest
	tags     map[string]string
	user     string
	pass     string
	requests atomic.Int64
	srv      *httptest.Server
}

func newRegistry(t *testing.T) *fakeRegistry {
	r := &fakeRegistry{t: t, blobs: map[string][]byte{}, types: map[string]string{}, tags: map[string]string{}}
	r.srv = httptest.NewServer(http.HandlerFunc(r.serve))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *fakeRegistry) host() string { return strings.TrimPrefix(r.srv.URL, "http://") }

func (r *fakeRegistry) serve(w http.ResponseWriter, req *http.Request) {
	r.requests.Add(1)
	if r.user != "" {
		u, p, ok := req.BasicAuth()
		if !ok || u != r.user || p != r.pass {
			w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			http.Error(w, `{"errors":[{"code":"UNAUTHORIZED"}]}`, http.StatusUnauthorized)
			return
		}
	}
	if req.URL.Path == "/v2/" {
		w.WriteHeader(http.StatusOK)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	parts := strings.Split(strings.TrimPrefix(req.URL.Path, "/v2/"), "/")
	kind, ref := parts[len(parts)-2], parts[len(parts)-1]
	switch kind {
	case "manifests":
		d := ref
		if !strings.HasPrefix(ref, "sha256:") {
			d = r.tags[ref]
		}
		b, ok := r.blobs[d]
		if !ok {
			http.Error(w, `{"errors":[{"code":"MANIFEST_UNKNOWN"}]}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", r.types[d])
		w.Header().Set("Docker-Content-Digest", d)
		w.Header().Set("Content-Length", itoa(len(b)))
		if req.Method != http.MethodHead {
			_, _ = w.Write(b)
		}
	case "blobs":
		b, ok := r.blobs[ref]
		if !ok {
			http.Error(w, `{"errors":[{"code":"BLOB_UNKNOWN"}]}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", itoa(len(b)))
		if req.Method != http.MethodHead {
			_, _ = w.Write(b)
		}
	default:
		http.NotFound(w, req)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func jsonInt(n int) string { return strconv.Itoa(n) }

func (r *fakeRegistry) put(b []byte, mediaType string) ocispec.Descriptor {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := digest.FromBytes(b).String()
	r.blobs[d] = b
	if mediaType != "" {
		r.types[d] = mediaType
	}
	return ocispec.Descriptor{MediaType: mediaType, Digest: digest.Digest(d), Size: int64(len(b))}
}

func (r *fakeRegistry) tag(tag string, d ocispec.Descriptor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tags[tag] = d.Digest.String()
}

// tarGz packs files (path → content; a path ending in / is a directory).
func tarGz(files map[string]string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if strings.HasSuffix(name, "/") {
			_ = tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeDir, Mode: 0o755})
			continue
		}
		_ = tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))})
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

// artifact pushes a package the way `oras push dir/` does: one layer, an
// empty config, an artifact type.
func (r *fakeRegistry) artifact(layer []byte) ocispec.Descriptor {
	l := r.put(layer, "application/vnd.oci.image.layer.v1.tar+gzip")
	cfg := r.put([]byte("{}"), ocispec.MediaTypeEmptyJSON)
	m := ocispec.Manifest{Versioned: struct {
		SchemaVersion int `json:"schemaVersion"`
	}{2}, MediaType: ocispec.MediaTypeImageManifest, ArtifactType: "application/vnd.shpyrd.workspace-app.v1", Config: cfg, Layers: []ocispec.Descriptor{l}}
	raw, _ := json.Marshal(m)
	return r.put(raw, ocispec.MediaTypeImageManifest)
}

// image pushes a package the way `docker build` of `FROM scratch` + COPY
// does: Docker media types, one layer per COPY, behind an index with an
// attestation.
func (r *fakeRegistry) image(layers ...[]byte) ocispec.Descriptor {
	var ls []ocispec.Descriptor
	for _, l := range layers {
		ls = append(ls, r.put(l, "application/vnd.docker.image.rootfs.diff.tar.gzip"))
	}
	cfg := r.put([]byte(`{"architecture":"amd64","os":"linux"}`), "application/vnd.docker.container.image.v1+json")
	m := ocispec.Manifest{Versioned: struct {
		SchemaVersion int `json:"schemaVersion"`
	}{2}, MediaType: "application/vnd.docker.distribution.manifest.v2+json", Config: cfg, Layers: ls}
	raw, _ := json.Marshal(m)
	md := r.put(raw, "application/vnd.docker.distribution.manifest.v2+json")
	md.Platform = &ocispec.Platform{OS: "linux", Architecture: "amd64"}
	att := r.put([]byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{},"layers":[]}`), ocispec.MediaTypeImageManifest)
	att.Platform = &ocispec.Platform{OS: "unknown", Architecture: "unknown"}
	index := ocispec.Index{Versioned: struct {
		SchemaVersion int `json:"schemaVersion"`
	}{2}, MediaType: ocispec.MediaTypeImageIndex, Manifests: []ocispec.Descriptor{att, md}}
	raw, _ = json.Marshal(index)
	return r.put(raw, ocispec.MediaTypeImageIndex)
}

func fetcher(t *testing.T) *Fetcher {
	return &Fetcher{CacheDir: filepath.Join(t.TempDir(), "cache"), PlainHTTP: func(string) bool { return true }}
}

func ociRef(t *testing.T, reg *fakeRegistry, repo string, d ocispec.Descriptor) Ref {
	t.Helper()
	ref, err := Parse("oci://" + reg.host() + "/" + repo + "@" + d.Digest.String())
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return string(b)
}

func TestAnArtifactIsPulledOnceAndServedFromTheCache(t *testing.T) {
	reg := newRegistry(t)
	d := reg.artifact(tarGz(map[string]string{
		"index.html":               "<html>acme</html>",
		"_next/static/a.js":        "js",
		"shpyrd-app.json":          `{"name":"Acme","version":"2.3.1","origins":["https://cdn.acme.test/"]}`,
		"_next/":                   "",
		"_next/static/chunks/b.js": "chunk",
	}))
	f := fetcher(t)
	dir, m, err := f.Fetch(context.Background(), ociRef(t, reg, "acme/workspace-ui", d), Credential{})
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, "index.html"); got != "<html>acme</html>" {
		t.Errorf("index.html = %q", got)
	}
	if got := read(t, dir, "_next/static/chunks/b.js"); got != "chunk" {
		t.Errorf("chunk = %q", got)
	}
	if m == nil || m.Name != "Acme" || m.Version != "2.3.1" || len(m.Origins) != 1 || m.Origins[0] != "https://cdn.acme.test" {
		t.Errorf("manifest = %+v", m)
	}
	if !strings.HasPrefix(filepath.Base(dir), strings.TrimPrefix(d.Digest.String(), "sha256:")[:8]) {
		t.Errorf("cache dir %s is not named by the digest", dir)
	}
	before := reg.requests.Load()
	if _, _, err := f.Fetch(context.Background(), ociRef(t, reg, "acme/workspace-ui", d), Credential{}); err != nil {
		t.Fatal(err)
	}
	if reg.requests.Load() != before {
		t.Errorf("the second fetch asked the registry %d times", reg.requests.Load()-before)
	}
}

func TestAnImageBehindAnIndexIsLaidOutLayerByLayer(t *testing.T) {
	reg := newRegistry(t)
	d := reg.image(
		tarGz(map[string]string{"index.html": "v1", "old.js": "gone later", "keep.js": "kept"}),
		tarGz(map[string]string{"index.html": "v2", ".wh.old.js": ""}),
	)
	dir, _, err := fetcher(t).Fetch(context.Background(), ociRef(t, reg, "acme/ui", d), Credential{})
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, "index.html"); got != "v2" {
		t.Errorf("index.html = %q, want the later layer's", got)
	}
	if got := read(t, dir, "keep.js"); got != "kept" {
		t.Errorf("keep.js = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "old.js")); err == nil {
		t.Error("old.js survived its whiteout")
	}
}

func TestATagIsResolvedToTheDigestItPointsAtNow(t *testing.T) {
	reg := newRegistry(t)
	d := reg.artifact(tarGz(map[string]string{"index.html": "x"}))
	reg.tag("stable", d)
	ref, err := Parse("oci://" + reg.host() + "/acme/ui:stable")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := fetcher(t).Fetch(context.Background(), ref, Credential{}); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Errorf("fetch by tag = %v, want a refusal", err)
	}
	got, err := fetcher(t).Resolve(context.Background(), ref, Credential{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest != d.Digest.String() || got.String() != "oci://"+reg.host()+"/acme/ui@"+d.Digest.String() {
		t.Errorf("resolved = %s", got)
	}
	if _, err := fetcher(t).Resolve(context.Background(), Ref{Kind: KindOCI, Host: reg.host(), Repository: "acme/ui", Tag: "missing"}, Credential{}); err == nil || !strings.Contains(err.Error(), "has no acme/ui at missing") {
		t.Errorf("missing tag = %v", err)
	}
}

func TestAPrivateRepositoryNeedsTheWorkspacesCredential(t *testing.T) {
	reg := newRegistry(t)
	reg.user, reg.pass = "acme", "ghp_secret"
	d := reg.artifact(tarGz(map[string]string{"index.html": "x"}))
	ref := ociRef(t, reg, "acme/ui", d)
	_, _, err := fetcher(t).Fetch(context.Background(), ref, Credential{})
	if err == nil || !strings.Contains(err.Error(), "unauthorized") || !strings.Contains(err.Error(), "credential") {
		t.Errorf("anonymous = %v", err)
	}
	if _, _, err := fetcher(t).Fetch(context.Background(), ref, Credential{Username: "acme", Token: "ghp_secret"}); err != nil {
		t.Errorf("with the credential: %v", err)
	}
}

func TestAPackageIsRefusedWhenItIsNotOne(t *testing.T) {
	reg := newRegistry(t)
	cases := map[string]struct {
		layer []byte
		want  string
	}{
		"no index.html":       {tarGz(map[string]string{"readme.md": "x"}), "no index.html"},
		"a path outside":      {tarGz(map[string]string{"index.html": "x", "../etc/passwd": "root"}), "outside the package"},
		"too many files":      {manyFiles(MaxFiles + 1), "more than"},
		"over the size limit": {tarGz(map[string]string{"index.html": "x", "big.bin": strings.Repeat("0", MaxBytes+1)}), "over"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			d := reg.artifact(tc.layer)
			_, _, err := fetcher(t).Fetch(context.Background(), ociRef(t, reg, "acme/ui", d), Credential{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
	t.Run("a bad manifest file", func(t *testing.T) {
		d := reg.artifact(tarGz(map[string]string{"index.html": "x", ManifestFile: `{"origins":["https://cdn.acme.test/path"]}`}))
		_, _, err := fetcher(t).Fetch(context.Background(), ociRef(t, reg, "acme/ui", d), Credential{})
		if err == nil || !strings.Contains(err.Error(), "origin") {
			t.Errorf("err = %v", err)
		}
	})
}

func manyFiles(n int) []byte {
	files := map[string]string{"index.html": "x"}
	for i := 0; i < n; i++ {
		files["f/"+jsonInt(i)+".txt"] = "."
	}
	return tarGz(files)
}

func TestADirectoryIsServedAsItIs(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := fetcher(t).Fetch(context.Background(), Ref{Kind: KindDir, Dir: dir}, Credential{}); err == nil {
		t.Error("an empty directory passed")
	}
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte("x"), 0o644)
	got, m, err := fetcher(t).Fetch(context.Background(), Ref{Kind: KindDir, Dir: dir}, Credential{})
	if err != nil || got != dir || m != nil {
		t.Errorf("= %s %v %v", got, m, err)
	}
	if _, err := fetcher(t).Resolve(context.Background(), Ref{Kind: KindDir, Dir: filepath.Join(dir, "missing")}, Credential{}); err == nil {
		t.Error("a missing directory resolved")
	}
}

func TestUnusedPackagesArePruned(t *testing.T) {
	f := fetcher(t)
	old := filepath.Join(f.CacheDir, "old")
	fresh := filepath.Join(f.CacheDir, "fresh")
	_ = os.MkdirAll(old, 0o755)
	_ = os.MkdirAll(fresh, 0o755)
	_ = os.Chtimes(old, time.Now().Add(-2*Unused), time.Now().Add(-2*Unused))
	f.Prune(Unused)
	if _, err := os.Stat(old); err == nil {
		t.Error("the old package stayed")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("the fresh package went")
	}
}

func TestReferencesAreParsedAndWrittenBack(t *testing.T) {
	good := map[string]string{
		"dir:/srv/acme/out/":                               "dir:/srv/acme/out",
		"oci://ghcr.io/acme/workspace-ui:2.3.1":            "oci://ghcr.io/acme/workspace-ui:2.3.1",
		"oci://ghcr.io/acme/workspace-ui":                  "oci://ghcr.io/acme/workspace-ui:latest",
		"oci://localhost:5001/acme/ui@sha256:" + zeros(64): "oci://localhost:5001/acme/ui@sha256:" + zeros(64),
		"oci://ghcr.io/acme/ui:2.3.1@sha256:" + zeros(64):  "oci://ghcr.io/acme/ui@sha256:" + zeros(64),
		"  oci://registry-1.docker.io/acme/ui:v1  ":        "oci://registry-1.docker.io/acme/ui:v1",
	}
	for in, want := range good {
		r, err := Parse(in)
		if err != nil || r.String() != want {
			t.Errorf("Parse(%q) = %q, %v; want %q", in, r.String(), err, want)
		}
	}
	bad := []string{"", "out/", "dir:relative/out", "oci://", "oci://ghcr.io", "oci://ghcr.io/Acme/UI", "oci://ghcr.io/acme/ui@sha256:short", "http://x/y", "oci://ghcr.io/acme/ui:bad tag"}
	for _, in := range bad {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) passed", in)
		}
	}
}

func zeros(n int) string { return strings.Repeat("0", n) }

func TestTheSourcesSettingSaysWhatMayBeSet(t *testing.T) {
	s, err := ParseSources("")
	if err != nil || !s.Allows(KindOCI) || s.Allows(KindDir) || s.String() != "oci" {
		t.Errorf("default = %s %v", s, err)
	}
	s, _ = ParseSources("oci, dir")
	if !s.Allows(KindDir) || s.String() != "oci,dir" {
		t.Errorf("oci,dir = %s", s)
	}
	s, _ = ParseSources("off")
	if !s.Off || s.Allows(KindOCI) || s.String() != "off" {
		t.Errorf("off = %s", s)
	}
	if _, err := ParseSources("ftp"); err == nil {
		t.Error("ftp passed")
	}
}

func TestTheSecretNameTellsHostsApart(t *testing.T) {
	a, b := SecretName("acme", "ghcr.io"), SecretName("acme", "ghcr-io")
	if a == b || !strings.HasPrefix(a, "shpyrd-registry-token-acme-ghcr-io-") {
		t.Errorf("%s vs %s", a, b)
	}
}
