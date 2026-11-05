package api

import (
	"context"
	"encoding/json"
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
	t.Helper()
	dir, out := t.TempDir(), t.TempDir()
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
	write("fluxyr", fluxyrEntry, map[string]string{"README.md": "# Fluxyr\n\nAn agent.", "Dockerfile": "FROM python:3.12-slim"})
	write("crm", crmEntry, map[string]string{})
	if _, err := catalog.Publish(dir, out, time.Now()); err != nil {
		t.Fatal(err)
	}
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
