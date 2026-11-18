package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/catalog"
	"github.com/shpyrd-io/shpyrd/pkg/configvars"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

const fluxyrEntry = `catalog:
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
  inputs:
    - name: FLUXYR_MODEL
      label: Model
      default: minimax/minimax-m3
    - name: OPENROUTER_API_KEY
      label: OpenRouter API key
      help: From openrouter.ai/keys.
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

const crmEntry = `catalog:
  name: Simple CRM
  description: Contacts and deals for a small team.
  category: crm
  source:
    git: https://git.example.test/acme/crm
    revision: v2
    subPath: apps/crm
  resources:
    - kind: widget
      name: main
processes:
  web: {}
`

const fluxyrIcon = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/></svg>`

// widgetExt owns a resource kind with no rules of its own, so an install
// can make one without a sizes catalog.
type widgetExt struct{}

func (widgetExt) Name() string                          { return "widgets" }
func (widgetExt) Description() string                   { return "test" }
func (widgetExt) Components() []ext.ComponentRef        { return nil }
func (widgetExt) Register(ctrl.Manager, ext.Deps) error { return nil }
func (widgetExt) CLI(ext.CLIGlobals) []*cobra.Command   { return nil }
func (widgetExt) Routes(ext.Router, ext.Deps) error     { return nil }
func (widgetExt) Types() []ext.ResourceType {
	return []ext.ResourceType{{Kind: "Widget", Group: "test.shpyrd.io", Version: "v1", Resource: "widgets", Bindable: true}}
}

// A published catalog on a test server, and a server reading it.
func catalogServer(t *testing.T) (*Server, client.Client) {
	return catalogServerWith(t, nil)
}

// catalogServerWith publishes the catalog resolving each entry's revision
// with list, as the catalog's CI does; with nil, entries carry no commit.
func catalogServerWith(t *testing.T, list catalog.RefLister) (*Server, client.Client) {
	t.Helper()
	dir, out := t.TempDir(), t.TempDir()
	publishEntries(t, dir, out, list, fluxyrEntry)
	return catalogServerAt(t, out)
}

// publishEntries writes the test entries (fluxyr with the given yaml, and
// crm) under dir and publishes them to out.
func publishEntries(t *testing.T, dir, out string, list catalog.RefLister, fluxyrYAML string) {
	t.Helper()
	write := func(name, yaml string, files map[string]string) {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
		files[catalog.EntryFile] = yaml
		for n, c := range files {
			if err := os.WriteFile(filepath.Join(dir, name, n), []byte(c), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	write("fluxyr", fluxyrYAML, map[string]string{"README.md": "# Fluxyr\n\nAn agent.", "Dockerfile": "FROM python:3.12-slim", "icon.svg": fluxyrIcon})
	write("crm", crmEntry, map[string]string{})
	if _, _, err := catalog.PublishResolved(context.Background(), dir, out, time.Now(), list); err != nil {
		t.Fatal(err)
	}
}

// catalogServerAt is a server reading the catalog published in out.
func catalogServerAt(t *testing.T, out string) (*Server, client.Client) {
	t.Helper()
	srv := httptest.NewServer(http.FileServer(http.Dir(out)))
	t.Cleanup(srv.Close)

	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	cr := crfake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&shpyrdv1.App{}, &shpyrdv1.Volume{}).Build()
	k := &kube.Client{Kube: kubefake.NewSimpleClientset(), Namespace: "shpyrd-system"}
	s, err := newServer(k, Options{
		Token: testToken, Store: principalFixtureStore(), Apps: cr, Public: PublicConfig{Domain: "example.test"},
		Extensions: []ext.Extension{widgetExt{}},
		AppCatalog: &catalog.Fetcher{URL: srv.URL + "/" + catalog.IndexFile},
		Sources:    &SourceStore{Dir: t.TempDir(), BaseURL: "http://shpyrd-server.shpyrd-system.svc"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.sleepAvailable = func() bool { return true }
	s.sleepGate = func() bool { return true }
	return s, cr
}

func appBySlug(t *testing.T, cr client.Client, slug string) *shpyrdv1.App {
	t.Helper()
	var list shpyrdv1.AppList
	if err := cr.List(context.Background(), &list); err != nil {
		t.Fatal(err)
	}
	for i := range list.Items {
		if list.Items[i].Spec.Slug == slug {
			return &list.Items[i]
		}
	}
	return nil
}

func TestTheCatalogIsListedWithItsEntriesAndCategories(t *testing.T) {
	s, _ := catalogServer(t)
	rec := do(t, s, "GET", "/api/config", "", false)
	if !strings.Contains(rec.Body.String(), `"catalog":true`) {
		t.Fatalf("config says there is a catalog: %s", rec.Body.String())
	}
	rec = do(t, s, "GET", "/api/catalog", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var view CatalogView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if strings.Join(view.Categories, ",") != "agents,crm" || len(view.Entries) != 2 {
		t.Fatalf("catalog %+v", view)
	}
	f := view.Entries[1]
	if f.Name != "fluxyr" || f.DisplayName != "Fluxyr" || f.Build != "dockerfile" || strings.Join(f.Overlay, ",") != "Dockerfile" || strings.Join(f.Processes, ",") != "web" || f.Source.Revision != "main" || f.Readme != "" {
		t.Fatalf("entry %+v", f)
	}
	if len(f.Inputs) != 3 || f.Inputs[1].Secret != true || f.Volumes[0].Size != "5Gi" {
		t.Fatalf("entry %+v", f)
	}
	// The image the entry ships, served from this origin (the pages'
	// policy allows no other) and what it is, so the card draws an SVG as
	// a symbol in the entry's colour.
	if !strings.HasPrefix(f.IconURL, "/api/catalog/entries/fluxyr/icon?v=") || f.IconType != "image/svg+xml" {
		t.Fatalf("icon %q %q", f.IconURL, f.IconType)
	}
	if view.Entries[0].IconURL != "" || view.Entries[0].IconType != "" {
		t.Fatalf("an entry without an image points at none: %+v", view.Entries[0])
	}
	rec = do(t, s, "GET", f.IconURL, "", true)
	if rec.Code != http.StatusOK || rec.Body.String() != fluxyrIcon || rec.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("the image: %d %q %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "private, max-age=86400" || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("headers %v", rec.Header())
	}
	if rec = do(t, s, "GET", "/api/catalog/entries/"+view.Entries[0].Name+"/icon", "", true); rec.Code != http.StatusNotFound {
		t.Fatalf("no image, no answer: %d", rec.Code)
	}
	rec = do(t, s, "GET", "/api/catalog/entries/fluxyr", "", true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"readme":"# Fluxyr\n\nAn agent."`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "GET", "/api/catalog/entries/nothing", "", true); rec.Code != http.StatusNotFound {
		t.Fatalf("an entry that is not there: %d", rec.Code)
	}
}

func TestWithoutACatalogTheRoutesSaySo(t *testing.T) {
	s, _ := newTestServer(t, nil, nil)
	if rec := do(t, s, "GET", "/api/config", "", false); !strings.Contains(rec.Body.String(), `"catalog":false`) {
		t.Fatalf("config: %s", rec.Body.String())
	}
	if rec := do(t, s, "GET", "/api/catalog", "", true); rec.Code != http.StatusNotFound {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, "POST", "/api/catalog/entries/fluxyr/install", `{}`, true); rec.Code != http.StatusNotFound {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestAnInstallNeedsTheRequiredInputsBeforeAnythingIsMade(t *testing.T) {
	s, cr := catalogServer(t)
	rec := do(t, s, "POST", "/api/catalog/entries/fluxyr/install", `{}`, true)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "the install needs OpenRouter API key (OPENROUTER_API_KEY)") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if appBySlug(t, cr, "fluxyr") != nil {
		t.Fatal("nothing is made before the inputs are complete")
	}
	rec = do(t, s, "POST", "/api/catalog/entries/fluxyr/install", `{"inputs":{"OPENROUTER_API_KEY":"k","COLOUR":"red"}}`, true)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `asks no input`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestAnInstallMakesTheProjectTheEntryDescribes(t *testing.T) {
	s, cr := catalogServer(t)
	rec := do(t, s, "POST", "/api/catalog/entries/fluxyr/install", `{"inputs":{"OPENROUTER_API_KEY":"sk-or-123"}}`, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var resp CatalogInstallResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Project.Slug != "fluxyr" || resp.Project.DisplayName != "Fluxyr" || resp.Project.CatalogEntry != "fluxyr" || resp.Project.Icon != "bot" || resp.Project.IconColor != "teal" {
		t.Fatalf("project %+v", resp.Project)
	}
	app := appBySlug(t, cr, "fluxyr")
	if app == nil {
		t.Fatal("no project")
	}
	ctx := context.Background()
	// The source: upstream, on its branch, with the overlay kept by the
	// source store and named by its digest.
	src := app.Spec.Source
	if src == nil || src.Git == nil || src.Git.URL != "https://github.com/shpyrd-io/fluxyr" || src.Git.Revision != "main" {
		t.Fatalf("source %+v", src)
	}
	if src.Overlay == nil || !strings.HasPrefix(src.Overlay.URL, "http://shpyrd-server.shpyrd-system.svc/api/sources/") || len(src.Overlay.SHA256) != 64 {
		t.Fatalf("overlay %+v", src.Overlay)
	}
	if _, err := os.Stat(filepath.Join(s.sources.Dir, src.Overlay.SHA256+".tgz")); err != nil {
		t.Fatalf("the overlay is in the source store: %v", err)
	}
	// What the entry said of the project.
	web := app.Spec.Processes["web"]
	if web.Port == nil || *web.Port != 5050 || len(web.Volumes) != 1 || web.Volumes[0].Path != "/var/lib/fluxyr" {
		t.Fatalf("web %+v", web)
	}
	if app.Spec.Build == nil || app.Spec.Build.Strategy != "dockerfile" || len(app.Spec.Env) != 1 || app.Spec.Env[0].Name != "FLUXYR_HOST" {
		t.Fatalf("build %+v env %+v", app.Spec.Build, app.Spec.Env)
	}
	if app.Annotations[shpyrdv1.AnnotationCatalogEntry] != "fluxyr" || !strings.HasPrefix(app.Annotations[shpyrdv1.AnnotationCatalogDigest], "sha256:") || !strings.HasSuffix(app.Annotations[shpyrdv1.AnnotationCatalogSource], "/index.json") {
		t.Fatalf("marks %v", app.Annotations)
	}
	if app.Annotations[shpyrdv1.AnnotationReleaseNote] != "Install Fluxyr from the catalog" || app.Annotations[shpyrdv1.AnnotationDeployedClient] == "" {
		t.Fatalf("the first deploy is noted and attributed: %v", app.Annotations)
	}
	// The image the entry ships is the project's own icon, so the card
	// looks the same on the launcher as in the catalog.
	if v := app.Annotations[shpyrdv1.AnnotationIconFile]; !strings.HasSuffix(v, ".svg") {
		t.Fatalf("icon file %q", v)
	}
	if data, typ, err := s.store.ProjectIcon(ctx, app.Spec.ID); err != nil || string(data) != fluxyrIcon || typ != "image/svg+xml" {
		t.Fatalf("stored icon: %v %q %s", err, typ, data)
	}
	if resp.Project.IconURL == "" || !strings.Contains(resp.Project.IconURL, "/api/projects/fluxyr/icon?v=") {
		t.Fatalf("the answer points at the image: %q", resp.Project.IconURL)
	}
	// The volume.
	var vol shpyrdv1.Volume
	if err := cr.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: "data"}, &vol); err != nil {
		t.Fatalf("volume: %v", err)
	}
	if vol.Spec.Size.String() != "5Gi" {
		t.Fatalf("volume %+v", vol.Spec)
	}
	// The config vars, pending until the first deploy: the key as a
	// secret, the model as a plain variable, the vault key generated.
	var sec corev1.Secret
	if err := cr.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: configvars.PendingName(app)}, &sec); err != nil {
		t.Fatalf("pending config: %v", err)
	}
	if string(sec.Data["OPENROUTER_API_KEY"]) != "sk-or-123" || configvars.Plain(&sec, "OPENROUTER_API_KEY") {
		t.Fatalf("the key is a secret: %v", sec.Data)
	}
	if string(sec.Data["FLUXYR_MODEL"]) != "minimax/minimax-m3" || !configvars.Plain(&sec, "FLUXYR_MODEL") {
		t.Fatalf("the model is the default, plain: %v", sec.Data)
	}
	if key := string(sec.Data["VAULT_ENCRYPTION_KEY"]); len(key) != 64 || configvars.Plain(&sec, "VAULT_ENCRYPTION_KEY") {
		t.Fatalf("the vault key is generated, 32 bytes as hex, secret: %q", key)
	}
	// Twice is a conflict on the slug, as making any project twice is.
	if rec := do(t, s, "POST", "/api/catalog/entries/fluxyr/install", `{"inputs":{"OPENROUTER_API_KEY":"k"}}`, true); rec.Code != http.StatusConflict {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestAnInstallTakesItsOwnSlugRevisionAndResources(t *testing.T) {
	s, cr := catalogServer(t)
	rec := do(t, s, "POST", "/api/catalog/entries/crm/install", `{"slug":"sales","name":"Sales","revision":"v3","access":"public"}`, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	app := appBySlug(t, cr, "sales")
	if app == nil || app.Spec.Access != "public" {
		t.Fatalf("project %+v", app)
	}
	src := app.Spec.Source
	if src.Git.Revision != "v3" || src.SubPath != "apps/crm" || src.Overlay != nil {
		t.Fatalf("source %+v", src)
	}
	if app.Spec.Build != nil {
		t.Fatalf("buildpacks: no build settings: %+v", app.Spec.Build)
	}
	w := &unstructured.Unstructured{}
	w.SetGroupVersionKind(schema.GroupVersionKind{Group: "test.shpyrd.io", Version: "v1", Kind: "Widget"})
	if err := cr.Get(context.Background(), types.NamespacedName{Namespace: app.Namespace, Name: "main"}, w); err != nil {
		t.Fatalf("the widget is made in the project: %v", err)
	}
	if len(app.Spec.Bindings) != 1 || app.Spec.Bindings[0].Kind != "Widget" || app.Spec.Bindings[0].Name != "main" {
		t.Fatalf("bindings %+v", app.Spec.Bindings)
	}
	if rec := do(t, s, "GET", "/api/projects/sales", "", true); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"catalogEntry":"crm"`) {
		t.Fatalf("the project says where it came from: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAnInstallOfAKindTheClusterLacksIsRefusedBeforeAnythingIsMade(t *testing.T) {
	s, cr := catalogServer(t)
	s.opts.Extensions = nil
	rec := do(t, s, "POST", "/api/catalog/entries/crm/install", `{}`, true)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "the entry needs a widget") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if appBySlug(t, cr, "crm") != nil {
		t.Fatal("nothing is made")
	}
}

func TestTheCatalogNeedsSomeoneWhoMayMakeAProject(t *testing.T) {
	s, _ := catalogServer(t)
	if rec := do(t, s, "GET", "/api/catalog", "", false); rec.Code != http.StatusUnauthorized {
		t.Fatalf("%d", rec.Code)
	}
	_ = gin.Mode()
}

const publishedCommit = "0123456789abcdef0123456789abcdef01234567"

func publishedRefs(context.Context, string) (map[string]string, error) {
	return map[string]string{"refs/heads/main": publishedCommit, "refs/tags/v2": "89abcdef0123456789abcdef0123456789abcdef", "refs/tags/v3": "89abcdef0123456789abcdef0123456789abcdef"}, nil
}

func TestAnInstallOfAnEntryWithACommitPinsItAndRecordsTheVersion(t *testing.T) {
	s, cr := catalogServerWith(t, publishedRefs)
	rec := do(t, s, "POST", "/api/catalog/entries/fluxyr/install", `{"inputs":{"OPENROUTER_API_KEY":"sk-or-123"}}`, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	app := appBySlug(t, cr, "fluxyr")
	src := app.Spec.Source
	if src == nil || src.Git == nil || src.Git.URL != "https://github.com/shpyrd-io/fluxyr" || src.Git.Revision != publishedCommit || src.Overlay == nil {
		t.Fatalf("source %+v", src)
	}
	a := app.Annotations
	if a[shpyrdv1.AnnotationCatalogEntry] != "fluxyr" || !strings.HasPrefix(a[shpyrdv1.AnnotationCatalogDigest], "sha256:") || a[shpyrdv1.AnnotationCatalogSource] == "" ||
		a[shpyrdv1.AnnotationCatalogCommit] != publishedCommit || a[shpyrdv1.AnnotationCatalogRevision] != "main" {
		t.Fatalf("annotations %v", a)
	}
}

func TestAnInstallWithARevisionOrWithoutACommitIsAsItWas(t *testing.T) {
	s, cr := catalogServerWith(t, publishedRefs)
	rec := do(t, s, "POST", "/api/catalog/entries/crm/install", `{"revision":"v3"}`, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	app := appBySlug(t, cr, "crm")
	if app.Spec.Source.Git.Revision != "v3" || app.Annotations[shpyrdv1.AnnotationCatalogCommit] != "" {
		t.Fatalf("--revision: %+v %v", app.Spec.Source.Git, app.Annotations)
	}
	s, cr = catalogServer(t)
	if rec := do(t, s, "POST", "/api/catalog/entries/crm/install", `{}`, true); rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	app = appBySlug(t, cr, "crm")
	if app.Spec.Source.Git.Revision != "v2" || app.Annotations[shpyrdv1.AnnotationCatalogCommit] != "" {
		t.Fatalf("no commit: %+v %v", app.Spec.Source.Git, app.Annotations)
	}
}

// --- what a project says it runs -------------------------------------

const (
	commitOld = "1111111111111111111111111111111111111111"
	commitNew = "2222222222222222222222222222222222222222"
	fluxyrGit = "https://github.com/shpyrd-io/fluxyr"
)

// A catalog that can publish a new version of fluxyr, and a project
// installed from the first.
type versionedCatalog struct {
	t    *testing.T
	s    *Server
	cr   client.Client
	dir  string
	out  string
	refs map[string]string
}

func (v *versionedCatalog) list(context.Context, string) (map[string]string, error) {
	return map[string]string{"refs/heads/main": v.refs["main"], "refs/tags/v2": v.refs["main"]}, nil
}

// publish republishes with fluxyr's main at commit, and has the server
// read it now.
func (v *versionedCatalog) publish(commit, extraEnv string) {
	v.t.Helper()
	v.refs["main"] = commit
	yaml := fluxyrEntry
	if extraEnv != "" {
		yaml += extraEnv
	}
	publishEntries(v.t, v.dir, v.out, v.list, yaml)
	if _, err := v.s.appCatalog.Refresh(context.Background()); err != nil {
		v.t.Fatal(err)
	}
}

func newVersionedCatalog(t *testing.T) *versionedCatalog {
	t.Helper()
	v := &versionedCatalog{t: t, dir: t.TempDir(), out: t.TempDir(), refs: map[string]string{"main": commitOld}}
	publishEntries(t, v.dir, v.out, v.list, fluxyrEntry)
	v.s, v.cr = catalogServerAt(t, v.out)
	return v
}

func (v *versionedCatalog) install() *shpyrdv1.App {
	v.t.Helper()
	rec := do(v.t, v.s, "POST", "/api/catalog/entries/fluxyr/install", `{"inputs":{"OPENROUTER_API_KEY":"k"}}`, true)
	if rec.Code != http.StatusCreated {
		v.t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	return appBySlug(v.t, v.cr, "fluxyr")
}

// setReleases plays the controller: the project's release history, and the
// image the App is pinned to.
func (v *versionedCatalog) setApp(app *shpyrdv1.App, mutate func(*shpyrdv1.App)) *shpyrdv1.App {
	v.t.Helper()
	if err := v.cr.Get(context.Background(), client.ObjectKeyFromObject(app), app); err != nil {
		v.t.Fatal(err)
	}
	mutate(app)
	status := app.Status
	if err := v.cr.Update(context.Background(), app); err != nil {
		v.t.Fatal(err)
	}
	app.Status = status
	if err := v.cr.Status().Update(context.Background(), app); err != nil {
		v.t.Fatal(err)
	}
	return app
}

func (v *versionedCatalog) state() (summary, detailView *ProjectCatalog, d AppDetail) {
	v.t.Helper()
	rec := do(v.t, v.s, "GET", "/api/projects", "", true)
	if rec.Code != http.StatusOK {
		v.t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var list []AppSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		v.t.Fatal(err)
	}
	for _, a := range list {
		if a.Slug == "fluxyr" {
			summary = a.Catalog
		}
	}
	rec = do(v.t, v.s, "GET", "/api/projects/fluxyr", "", true)
	if rec.Code != http.StatusOK {
		v.t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		v.t.Fatal(err)
	}
	return summary, d.Catalog, d
}

func (v *versionedCatalog) want(state, running, offered string) {
	v.t.Helper()
	sum, det, _ := v.state()
	for name, pc := range map[string]*ProjectCatalog{"summary": sum, "detail": det} {
		if pc == nil || pc.Entry != "fluxyr" || pc.State != state {
			v.t.Fatalf("%s: %+v, want state %q", name, pc, state)
		}
		if got := commitOf(pc.Running); got != running {
			v.t.Fatalf("%s: running %q, want %q", name, got, running)
		}
		if got := commitOf(pc.Offered); got != offered {
			v.t.Fatalf("%s: offered %q, want %q", name, got, offered)
		}
	}
}

func commitOf(v *CatalogVersionView) string {
	if v == nil {
		return ""
	}
	return v.Commit
}

func ranRelease(app *shpyrdv1.App, n int, image string, ver *shpyrdv1.CatalogVersion) {
	app.Status.Releases = append(app.Status.Releases, shpyrdv1.Release{Number: n, Image: image, Source: "x", Catalog: ver})
}

// ranRollback is the release the controller records for a rollback to
// release to: its note is the one both rollback paths write.
func ranRollback(app *shpyrdv1.App, n int, image string, ver *shpyrdv1.CatalogVersion, to int) {
	app.Status.Releases = append(app.Status.Releases, shpyrdv1.Release{Number: n, Image: image, Source: "x", Catalog: ver, Description: fmt.Sprintf("Rollback to v%d", to), RollbackTo: to})
}

func TestAProjectSaysWhatItRunsAndWhenTheCatalogOffersAnotherVersion(t *testing.T) {
	v := newVersionedCatalog(t)
	app := v.install()
	// Set to the commit it installed, no release yet: installing it.
	v.want(CatalogUpdating, "", commitOld)

	ver := func(commit, digest string) *shpyrdv1.CatalogVersion {
		return &shpyrdv1.CatalogVersion{Entry: "fluxyr", Digest: digest, Commit: commit, Revision: "main"}
	}
	var digestOld string
	v.setApp(app, func(a *shpyrdv1.App) {
		digestOld = a.Annotations[shpyrdv1.AnnotationCatalogDigest]
		ranRelease(a, 1, "r/a@sha256:1", ver(commitOld, digestOld))
	})
	v.want(CatalogUpToDate, commitOld, "")

	// The catalog publishes a different commit: an update is available.
	v.publish(commitNew, "")
	v.want(CatalogUpdateAvailable, commitOld, commitNew)

	// The project is set to it (the update applied): rolling out, no second offer.
	var digestNew string
	v.setApp(app, func(a *shpyrdv1.App) {
		ix, _ := v.s.appCatalog.Index(context.Background())
		digestNew = ix.Find("fluxyr").Digest
		a.Annotations[shpyrdv1.AnnotationCatalogDigest] = digestNew
		a.Annotations[shpyrdv1.AnnotationCatalogCommit] = commitNew
		a.Spec.Source.Git.Revision = commitNew
	})
	v.want(CatalogUpdating, commitOld, commitNew)

	// The controller records the release built from it.
	v.setApp(app, func(a *shpyrdv1.App) { ranRelease(a, 2, "r/b@sha256:2", ver(commitNew, digestNew)) })
	v.want(CatalogUpToDate, commitNew, "")
	_, _, d := v.state()
	if r := d.Status.Releases; len(r) != 2 || commitOf(r[0].Catalog) != commitOld || commitOf(r[1].Catalog) != commitNew || r[1].Catalog.ShortCommit != commitNew[:12] {
		t.Fatalf("release history %+v", r)
	}

	// Rolled back to release 1: pinned image, still set to the new
	// version, a release recorded with the old one. It reads as running the
	// old version with an update available, across reads and a config change.
	v.setApp(app, func(a *shpyrdv1.App) {
		a.Spec.Image = "r/a@sha256:1"
		ranRollback(a, 3, "r/a@sha256:1", ver(commitOld, digestOld), 1)
	})
	v.want(CatalogUpdateAvailable, commitOld, commitNew)
	v.want(CatalogUpdateAvailable, commitOld, commitNew)
	v.setApp(app, func(a *shpyrdv1.App) { ranRelease(a, 4, "r/a@sha256:1", ver(commitOld, digestOld)) })
	v.want(CatalogUpdateAvailable, commitOld, commitNew)
	_, _, d = v.state()
	if len(d.Status.Releases) != 4 || commitOf(d.Status.Releases[3].Catalog) != commitOld {
		t.Fatalf("history after rollback %+v", d.Status.Releases)
	}
}

func TestAChangedDigestWithTheSameCommitIsAnotherVersion(t *testing.T) {
	v := newVersionedCatalog(t)
	app := v.install()
	v.setApp(app, func(a *shpyrdv1.App) {
		ranRelease(a, 1, "r/a@sha256:1", &shpyrdv1.CatalogVersion{Entry: "fluxyr", Digest: a.Annotations[shpyrdv1.AnnotationCatalogDigest], Commit: commitOld})
	})
	v.want(CatalogUpToDate, commitOld, "")
	v.publish(commitOld, "  # a note\n")
	if sum, _, _ := v.state(); sum.State != CatalogUpdateAvailable {
		t.Fatalf("the entry changed and the code did not: %+v", sum)
	}
}

func TestAProjectInstalledBeforeVersionsIsOfferedTheFirstOne(t *testing.T) {
	v := newVersionedCatalog(t)
	app := v.install()
	v.setApp(app, func(a *shpyrdv1.App) {
		// As installed on a branch before the catalog published commits.
		delete(a.Annotations, shpyrdv1.AnnotationCatalogCommit)
		delete(a.Annotations, shpyrdv1.AnnotationCatalogRevision)
		a.Annotations[shpyrdv1.AnnotationCatalogDigest] = "sha256:old"
		a.Spec.Source.Git.Revision = "main"
		ranRelease(a, 1, "r/a@sha256:1", nil)
	})
	v.want(CatalogUpdateAvailable, "", commitOld)
}

func TestNoOfferIsMadeWhenTheCatalogCannotOfferOne(t *testing.T) {
	cases := map[string]func(v *versionedCatalog, a *shpyrdv1.App){
		"another catalog": func(v *versionedCatalog, a *shpyrdv1.App) {
			a.Annotations[shpyrdv1.AnnotationCatalogSource] = "https://elsewhere.test/index.json"
		},
		"source moved to another repository": func(v *versionedCatalog, a *shpyrdv1.App) {
			a.Spec.Source.Git.URL = "https://github.com/someone/else"
		},
		"source is an image": func(v *versionedCatalog, a *shpyrdv1.App) { a.Spec.Source = nil },
		"entry gone": func(v *versionedCatalog, a *shpyrdv1.App) {
			a.Annotations[shpyrdv1.AnnotationCatalogEntry] = "vanished"
		},
	}
	for name, mutate := range cases {
		v := newVersionedCatalog(t)
		app := v.install()
		v.publish(commitNew, "")
		v.setApp(app, func(a *shpyrdv1.App) { mutate(v, a) })
		sum, det, _ := v.state()
		if sum == nil || det == nil || sum.State != "" || sum.Offered != nil || det.State != "" {
			t.Errorf("%s: %+v %+v", name, sum, det)
		}
	}
}

func TestNoOfferWhenTheServerReadsNoCatalogOrTheEntryHasNoCommit(t *testing.T) {
	v := newVersionedCatalog(t)
	app := v.install()
	v.s.appCatalog = nil
	if sum, _, _ := v.state(); sum == nil || sum.State != "" {
		t.Fatalf("no catalog: %+v", sum)
	}
	// A catalog that publishes no commits.
	s, cr := catalogServer(t)
	if rec := do(t, s, "POST", "/api/catalog/entries/fluxyr/install", `{"inputs":{"OPENROUTER_API_KEY":"k"}}`, true); rec.Code != http.StatusCreated {
		t.Fatalf("%d", rec.Code)
	}
	rec := do(t, s, "GET", "/api/projects/fluxyr", "", true)
	var d AppDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil || d.Catalog == nil || d.Catalog.State != "" {
		t.Fatalf("an entry with no commit: %v %+v", err, d.Catalog)
	}
	_ = cr
	_ = app
}

func TestACatalogThatCannotBeReadDoesNotFailTheRead(t *testing.T) {
	v := newVersionedCatalog(t)
	v.install()
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	v.s.appCatalog = &catalog.Fetcher{URL: dead.URL + "/index.json"}
	sum, det, _ := v.state()
	if sum == nil || det == nil || sum.State != "" || det.State != "" {
		t.Fatalf("%+v %+v", sum, det)
	}
}

func TestAProjectNotFromTheCatalogReadsAsBefore(t *testing.T) {
	s, _ := catalogServer(t)
	if rec := do(t, s, "POST", "/api/projects", `{"name":"plain"}`, true); rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	rec := do(t, s, "GET", "/api/projects/plain", "", true)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"catalog"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestTheEntryViewCarriesTheCommitTheCatalogPublished(t *testing.T) {
	s, _ := catalogServerWith(t, publishedRefs)
	rec := do(t, s, "GET", "/api/catalog/entries/fluxyr", "", true)
	var f CatalogEntryView
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &f) != nil || f.Commit != publishedCommit {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, s, "GET", "/api/catalog", "", true)
	var view CatalogView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	for _, e := range view.Entries {
		if e.Commit == "" {
			t.Fatalf("%s has no commit in the list", e.Name)
		}
	}
	s, _ = catalogServer(t)
	if rec := do(t, s, "GET", "/api/catalog/entries/fluxyr", "", true); strings.Contains(rec.Body.String(), `"commit"`) {
		t.Fatalf("an entry without a commit says none: %s", rec.Body.String())
	}
}
