package catalog

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const fluxyr = `catalog:
  name: Fluxyr
  description: A Python framework for agents, skills and durable execution.
  category: agents
  tags: [agent, python]
  icon: bot
  color: teal
  publisher: shpyrd
  license: MPL-2.0
  homepage: https://github.com/shpyrd-io/fluxyr
  source:
    git: https://github.com/shpyrd-io/fluxyr
    revision: main
  inputs:
    - name: FLUXYR_MODEL
      label: Model
      default: minimax/minimax-m3
    - name: OPENROUTER_API_KEY
      label: OpenRouter API key
      secret: true
      required: true
    - name: VAULT_ENCRYPTION_KEY
      generate: 32
  volumes:
    - name: data
      size: 5Gi
processes:
  web:
    port: 5050
    volumes:
      - name: data
        path: /var/lib/fluxyr
build:
  strategy: dockerfile
env:
  FLUXYR_HOST: 0.0.0.0
`

func writeEntry(t *testing.T, dir, name, yaml string, files map[string]string) string {
	t.Helper()
	folder := filepath.Join(dir, name)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, EntryFile), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	for n, c := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(folder, n)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(folder, n), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return folder
}

func TestAnEntryIsReadFromItsFolderAndValidated(t *testing.T) {
	dir := t.TempDir()
	folder := writeEntry(t, dir, "fluxyr", fluxyr, map[string]string{"README.md": "# Fluxyr", "icon.svg": "<svg/>"})
	f, err := ReadEntry(folder)
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "fluxyr" || f.Catalog.Name != "Fluxyr" || f.Revision() != "main" {
		t.Fatalf("read %+v", f.IndexEntry)
	}
	if !strings.HasPrefix(f.Digest, "sha256:") {
		t.Fatalf("digest %q", f.Digest)
	}
	if len(f.OverlayFiles()) != 0 {
		t.Fatalf("the readme and the icon are not the overlay: %v", f.OverlayFiles())
	}
	procs, build, env, err := f.Spec()
	if err != nil {
		t.Fatal(err)
	}
	if *procs["web"].Port != 5050 || procs["web"].Volumes[0].Path != "/var/lib/fluxyr" || build.Strategy != "dockerfile" || env[0].Name != "FLUXYR_HOST" {
		t.Fatalf("spec %+v %+v %+v", procs, build, env)
	}
}

func TestTheDigestFollowsTheFolderContent(t *testing.T) {
	dir := t.TempDir()
	a, _ := ReadEntry(writeEntry(t, dir, "a", fluxyr, map[string]string{"Dockerfile": "FROM x"}))
	b, _ := ReadEntry(writeEntry(t, dir, "b", fluxyr, map[string]string{"Dockerfile": "FROM x"}))
	c, _ := ReadEntry(writeEntry(t, dir, "c", fluxyr, map[string]string{"Dockerfile": "FROM y"}))
	if a.Digest != b.Digest {
		t.Fatal("the same content has the same digest")
	}
	if a.Digest == c.Digest {
		t.Fatal("a changed file changes the digest")
	}
}

func TestWhatAnEntryMayNotSay(t *testing.T) {
	cases := []struct{ name, yaml, want string }{
		{"no name", strings.Replace(fluxyr, "name: Fluxyr", "name: ''", 1), "catalog.name is missing"},
		{"no source", strings.Replace(fluxyr, "git: https://github.com/shpyrd-io/fluxyr", "git: ''", 1), "catalog.source.git is missing"},
		{"a colour nobody has", strings.Replace(fluxyr, "color: teal", "color: mauve", 1), "catalog.color"},
		{"an input with a bad name", strings.Replace(fluxyr, "name: FLUXYR_MODEL", "name: 1model", 1), "catalog.inputs[0]"},
		{"required with a default", strings.Replace(fluxyr, "default: minimax/minimax-m3", "default: x\n      required: true", 1), "required and default together"},
		{"a volume nobody mounts", strings.Replace(fluxyr, "path: /var/lib/fluxyr", "path: /var/lib/fluxyr\n      - name: other\n        path: /other", 1), "which catalog.volumes does not declare"},
		{"a strategy nobody has", strings.Replace(fluxyr, "strategy: dockerfile", "strategy: nix", 1), "build.strategy must be"},
		{"a key the yaml does not have", fluxyr + "colour: red\n", "colour"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, err := Parse([]byte(c.yaml))
			if err == nil {
				err = e.Validate("fluxyr", nil)
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want %q", err, c.want)
			}
		})
	}
	e, _ := Parse([]byte(fluxyr))
	if err := e.Validate("Fluxyr!", nil); err == nil || !strings.Contains(err.Error(), "the folder") {
		t.Fatalf("the folder names the entry: %v", err)
	}
}

func TestAnOverlayGoesWithADockerfileBuild(t *testing.T) {
	e, _ := Parse([]byte(strings.Replace(fluxyr, "strategy: dockerfile", "strategy: buildpacks", 1)))
	err := e.Validate("fluxyr", []string{"Dockerfile"})
	var ve *ValidationError
	if !errors.As(err, &ve) || !strings.Contains(err.Error(), "laid over the checkout") {
		t.Fatalf("got %v", err)
	}
	e, _ = Parse([]byte(fluxyr))
	if err := e.Validate("fluxyr", []string{"Dockerfile"}); err != nil {
		t.Fatal(err)
	}
}

func TestPublishWritesTheIndexAndTheEntriesFiles(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	writeEntry(t, dir, "fluxyr", fluxyr, map[string]string{"README.md": "# Fluxyr", "icon.svg": "<svg/>", "Dockerfile": "FROM python", "conf/app.toml": "x = 1"})
	writeEntry(t, dir, "crm", strings.Replace(strings.Replace(fluxyr, "category: agents", "category: crm", 1), "strategy: dockerfile", "strategy: buildpacks", 1), nil)
	ix, err := Publish(dir, out, time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(ix.Entries) != 2 || ix.Entries[0].Name != "crm" || ix.Entries[1].Name != "fluxyr" {
		t.Fatalf("entries %+v", ix.Entries)
	}
	f := ix.Find("fluxyr")
	if f.Readme != "entries/fluxyr/README.md" || f.Icon != "entries/fluxyr/icon.svg" || f.Overlay == nil || f.Overlay.Path != "entries/fluxyr/overlay.tgz" {
		t.Fatalf("files %+v", f)
	}
	if strings.Join(f.Overlay.Files, ",") != "Dockerfile,conf/app.toml" {
		t.Fatalf("overlay files %v", f.Overlay.Files)
	}
	if got := ix.Categories(); strings.Join(got, ",") != "agents,crm" {
		t.Fatalf("categories %v", got)
	}
	raw, err := os.ReadFile(filepath.Join(out, IndexFile))
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseIndex(raw)
	if err != nil {
		t.Fatal(err)
	}
	if back.Find("fluxyr").Overlay.SHA256 != f.Overlay.SHA256 {
		t.Fatal("the index read back is the index written")
	}
	tgz, err := os.ReadFile(filepath.Join(out, "entries", "fluxyr", "overlay.tgz"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := ReadOverlay(tgz)
	if err != nil || strings.Join(files, ",") != "Dockerfile,conf/app.toml" {
		t.Fatalf("overlay %v %v", files, err)
	}
	// Published twice, the overlay is the same bytes: the digest means
	// something.
	out2 := t.TempDir()
	if _, err := Publish(dir, out2, time.Now()); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(filepath.Join(out2, "entries", "fluxyr", "overlay.tgz"))
	if string(again) != string(tgz) {
		t.Fatal("the overlay archive is not reproducible")
	}
}

func TestPublishStopsAtAnEntryThatIsWrong(t *testing.T) {
	dir := t.TempDir()
	writeEntry(t, dir, "fluxyr", fluxyr, nil)
	writeEntry(t, dir, "broken", strings.Replace(fluxyr, "category: agents", "category: ''", 1), nil)
	_, err := Publish(dir, t.TempDir(), time.Now())
	if err == nil || !strings.Contains(err.Error(), "entry broken") {
		t.Fatalf("got %v", err)
	}
	if _, err := Publish(t.TempDir(), t.TempDir(), time.Now()); err == nil || !strings.Contains(err.Error(), "holds no entry") {
		t.Fatalf("got %v", err)
	}
}

// A published catalog on a test server.
func serve(t *testing.T, out string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	fs := http.FileServer(http.Dir(out))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, IndexFile) {
			hits.Add(1)
			w.Header().Set("ETag", `"v1"`)
			if r.Header.Get("If-None-Match") == `"v1"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		fs.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestTheFetcherReadsTheIndexOnceAnHourAndTheFilesByPath(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	writeEntry(t, dir, "fluxyr", fluxyr, map[string]string{"README.md": "# Fluxyr", "Dockerfile": "FROM python"})
	if _, err := Publish(dir, out, time.Now()); err != nil {
		t.Fatal(err)
	}
	srv, hits := serve(t, out)
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	f := &Fetcher{URL: srv.URL + "/" + IndexFile, now: func() time.Time { return now }}
	ctx := context.Background()
	ix, err := f.Index(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Index(ctx); err != nil || hits.Load() != 1 {
		t.Fatalf("a fresh index is served from memory: %d reads, %v", hits.Load(), err)
	}
	now = now.Add(2 * time.Hour)
	if _, err := f.Index(ctx); err != nil || hits.Load() != 2 {
		t.Fatalf("a stale index is asked again: %d reads, %v", hits.Load(), err)
	}
	if st := f.Status(); st.Entries != 1 || st.Error != "" || st.FetchedAt != now {
		t.Fatalf("status %+v", st)
	}
	e := ix.Find("fluxyr")
	readme, err := f.Readme(ctx, e)
	if err != nil || readme != "# Fluxyr" {
		t.Fatalf("readme %q %v", readme, err)
	}
	tgz, err := f.Overlay(ctx, e)
	if err != nil || len(tgz) == 0 {
		t.Fatalf("overlay %v", err)
	}
	if _, err := f.Resolve("../secret"); err == nil {
		t.Fatal("a path above the catalog is refused")
	}
}

func TestTheFetcherServesTheLastIndexWhileTheSourceIsDown(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	writeEntry(t, dir, "fluxyr", fluxyr, nil)
	if _, err := Publish(dir, out, time.Now()); err != nil {
		t.Fatal(err)
	}
	srv, _ := serve(t, out)
	now := time.Now()
	f := &Fetcher{URL: srv.URL + "/" + IndexFile, now: func() time.Time { return now }}
	if _, err := f.Index(context.Background()); err != nil {
		t.Fatal(err)
	}
	srv.Close()
	now = now.Add(2 * time.Hour)
	ix, err := f.Index(context.Background())
	if err != nil || ix == nil || len(ix.Entries) != 1 {
		t.Fatalf("the last index is served: %v %v", ix, err)
	}
	if st := f.Status(); st.Error == "" {
		t.Fatal("the error is kept for the operator")
	}
	// Nothing read yet and nothing to read: an error.
	cold := &Fetcher{URL: srv.URL + "/" + IndexFile}
	if _, err := cold.Index(context.Background()); err == nil || !strings.Contains(err.Error(), "could not be read") {
		t.Fatalf("got %v", err)
	}
	var none *Fetcher
	if _, err := none.Index(context.Background()); !errors.Is(err, ErrNoCatalog) {
		t.Fatalf("got %v", err)
	}
	if New("off") != nil || New("").URL != DefaultURL {
		t.Fatal("off is no catalog, empty the default")
	}
}

func TestAnOverlayThatIsNotTheOneTheIndexNamesIsRefused(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	writeEntry(t, dir, "fluxyr", fluxyr, map[string]string{"Dockerfile": "FROM python"})
	if _, err := Publish(dir, out, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "entries", "fluxyr", "overlay.tgz"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, _ := serve(t, out)
	f := &Fetcher{URL: srv.URL + "/" + IndexFile}
	ix, err := f.Index(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Overlay(context.Background(), ix.Find("fluxyr")); err == nil || !strings.Contains(err.Error(), "not the one the index names") {
		t.Fatalf("got %v", err)
	}
}
