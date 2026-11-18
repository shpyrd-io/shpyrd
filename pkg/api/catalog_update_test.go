package api

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/configvars"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// fluxyr as the catalog publishes it in its next version: a new process,
// a plain variable, a volume, a resource, and three new inputs.
func fluxyrNext() string {
	y := fluxyrEntry
	y = strings.Replace(y, "  inputs:\n", "  inputs:\n    - name: NEW_TOKEN\n      generate: 8\n    - name: NEW_MODE\n      label: Mode\n      default: fast\n    - name: NEW_NEEDED\n      label: Needed\n      required: true\n", 1)
	y = strings.Replace(y, "  volumes:\n", "  resources:\n    - kind: widget\n      name: cache\n  volumes:\n    - name: extra\n      size: 1Gi\n", 1)
	y = strings.Replace(y, "processes:\n  web:", "processes:\n  worker:\n    command: [run-worker]\n    volumes: [{name: extra, path: /extra}]\n  web:", 1)
	y = strings.Replace(y, "  FLUXYR_HOST: 0.0.0.0\n", "  FLUXYR_HOST: 0.0.0.0\n  FLUXYR_NEW: enabled\n", 1)
	return y
}

// publishNext republishes with the given fluxyr yaml and main at commit.
func (v *versionedCatalog) publishNext(commit, yaml string) {
	v.t.Helper()
	v.refs["main"] = commit
	publishEntries(v.t, v.dir, v.out, v.list, yaml)
	if _, err := v.s.appCatalog.Refresh(context.Background()); err != nil {
		v.t.Fatal(err)
	}
}

// installedAndRunning installs fluxyr and plays the controller: release 1
// built from the installed version.
func (v *versionedCatalog) installedAndRunning() *shpyrdv1.App {
	v.t.Helper()
	app := v.install()
	return v.setApp(app, func(a *shpyrdv1.App) {
		ranRelease(a, 1, "r/a@sha256:1", &shpyrdv1.CatalogVersion{Entry: "fluxyr", Digest: a.Annotations[shpyrdv1.AnnotationCatalogDigest], Commit: commitOld, Revision: "main"})
	})
}

func (v *versionedCatalog) currentDigest() string {
	ix, err := v.s.appCatalog.Index(context.Background())
	if err != nil {
		v.t.Fatal(err)
	}
	return ix.Find("fluxyr").Digest
}

func (v *versionedCatalog) fresh(app *shpyrdv1.App) *shpyrdv1.App {
	v.t.Helper()
	got := &shpyrdv1.App{}
	if err := v.cr.Get(context.Background(), client.ObjectKeyFromObject(app), got); err != nil {
		v.t.Fatal(err)
	}
	return got
}

func (v *versionedCatalog) apply(body string) *httptestResult {
	rec := do(v.t, v.s, "POST", "/api/projects/fluxyr/catalog/update", body, true)
	return &httptestResult{code: rec.Code, body: rec.Body.String()}
}

type httptestResult struct {
	code int
	body string
}

func (v *versionedCatalog) snapshot(app *shpyrdv1.App) string {
	a := v.fresh(app)
	b, _ := json.Marshal(struct {
		Spec shpyrdv1.AppSpec
		Ann  map[string]string
	}{a.Spec, a.Annotations})
	var vols shpyrdv1.VolumeList
	_ = v.cr.List(context.Background(), &vols, client.InNamespace(app.Namespace))
	names := []string{}
	for _, x := range vols.Items {
		names = append(names, x.Name)
	}
	return string(b) + strings.Join(names, ",")
}

func (v *versionedCatalog) widget(app *shpyrdv1.App, name string) bool {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion("test.shpyrd.io/v1")
	u.SetKind("Widget")
	err := v.cr.Get(context.Background(), types.NamespacedName{Namespace: app.Namespace, Name: name}, u)
	if err != nil && !errors.IsNotFound(err) {
		v.t.Fatal(err)
	}
	return err == nil
}

func (v *versionedCatalog) vars(app *shpyrdv1.App) map[string]string {
	sec, _, err := configvars.Read(context.Background(), v.cr, v.fresh(app))
	if err != nil {
		v.t.Fatal(err)
	}
	out := map[string]string{}
	for k, b := range sec.Data {
		out[k] = string(b)
	}
	return out
}

func TestThePreviewSaysWhatAnUpdateDoesAndChangesNothing(t *testing.T) {
	v := newVersionedCatalog(t)
	app := v.installedAndRunning()
	v.publishNext(commitNew, fluxyrNext())
	before := v.snapshot(app)

	rec := do(t, v.s, "GET", "/api/projects/fluxyr/catalog/update", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var p CatalogUpdatePreview
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.State != CatalogUpdateAvailable || commitOf(p.From) != commitOld || commitOf(p.To) != commitNew || p.Repository != fluxyrGit {
		t.Fatalf("versions %+v", p)
	}
	actions := map[string]string{}
	for _, in := range p.Inputs {
		actions[in.Name] = in.Action
	}
	want := map[string]string{"NEW_TOKEN": "generate", "NEW_MODE": "default", "NEW_NEEDED": "ask"}
	if !reflect.DeepEqual(actions, want) {
		t.Fatalf("inputs %v, want %v (inputs the project has are not listed)", actions, want)
	}
	if !reflect.DeepEqual(p.Volumes, []string{"extra"}) || !reflect.DeepEqual(p.Resources, []string{"widget cache"}) ||
		!reflect.DeepEqual(p.ProcessesAdded, []string{"worker"}) || !reflect.DeepEqual(p.EnvChanged, []string{"FLUXYR_NEW"}) {
		t.Fatalf("preview %+v", p)
	}
	if strings.Contains(rec.Body.String(), "sk-or") {
		t.Fatal("no value is echoed")
	}
	if v.snapshot(app) != before {
		t.Fatal("a preview changes nothing")
	}
}

func TestApplyingAnUpdateAddsAndKeepsWhatAPersonSet(t *testing.T) {
	v := newVersionedCatalog(t)
	app := v.installedAndRunning()
	// A person changes the project after the install.
	v.setApp(app, func(a *shpyrdv1.App) {
		web := a.Spec.Processes["web"]
		three := int32(3)
		web.Replicas, web.Size = &three, "shared-m"
		a.Spec.Processes["web"] = web
		a.Spec.Processes["legacy"] = shpyrdv1.Process{Command: []string{"old"}}
		a.Annotations[shpyrdv1.AnnotationDisplayName] = "My agent"
	})
	rec := do(t, v.s, "PUT", "/api/projects/fluxyr/secrets", `{"set":{"OPENROUTER_API_KEY":"mine"}}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	generated := v.vars(app)["VAULT_ENCRYPTION_KEY"]
	if generated == "" {
		t.Fatal("the install generated a key")
	}
	v.publishNext(commitNew, fluxyrNext())
	before := v.fresh(app)

	// A required input missing: refused, nothing changed.
	snap := v.snapshot(app)
	if r := v.apply(`{"digest":"` + v.currentDigest() + `"}`); r.code != http.StatusUnprocessableEntity || !strings.Contains(r.body, "NEW_NEEDED") {
		t.Fatalf("%d %s", r.code, r.body)
	}
	if v.snapshot(app) != snap || v.widget(app, "cache") {
		t.Fatal("a refusal changes nothing")
	}

	r := v.apply(`{"digest":"` + v.currentDigest() + `","inputs":{"NEW_NEEDED":"yes"}}`)
	if r.code != http.StatusAccepted {
		t.Fatalf("%d %s", r.code, r.body)
	}
	got := v.fresh(app)
	// The version the project is set to, and the deploy.
	if got.Spec.Source.Git.Revision != commitNew || got.Spec.Source.Git.URL != fluxyrGit || got.Spec.Image != "" ||
		got.Annotations[shpyrdv1.AnnotationCatalogCommit] != commitNew || got.Annotations[shpyrdv1.AnnotationCatalogDigest] != v.currentDigest() ||
		!strings.Contains(got.Annotations[shpyrdv1.AnnotationReleaseNote], "Update Fluxyr") || got.Annotations[configvars.AnnotationDeployRequested] == before.Annotations[configvars.AnnotationDeployRequested] {
		t.Fatalf("not set to the new version: %+v %v", got.Spec.Source.Git, got.Annotations)
	}
	if got.Annotations[shpyrdv1.AnnotationDeployedClient] == "" {
		t.Fatalf("the deploy is attributed: %v", got.Annotations)
	}
	// Kept: what a person set.
	web := got.Spec.Processes["web"]
	if web.Replicas == nil || *web.Replicas != 3 || web.Size != "shared-m" || got.Spec.Processes["legacy"].Command[0] != "old" {
		t.Fatalf("processes %+v", got.Spec.Processes)
	}
	vars := v.vars(app)
	if vars["OPENROUTER_API_KEY"] != "mine" || vars["VAULT_ENCRYPTION_KEY"] != generated {
		t.Fatalf("config vars were changed: %v", keysOf(vars))
	}
	if got.Annotations[shpyrdv1.AnnotationDisplayName] != "My agent" || got.Annotations[shpyrdv1.AnnotationCatalogEntry] != "fluxyr" {
		t.Fatalf("the project's name changed: %v", got.Annotations)
	}
	// Added: the new things.
	if _, ok := got.Spec.Processes["worker"]; !ok {
		t.Fatalf("worker not added: %+v", got.Spec.Processes)
	}
	if !hasEnvValue(got.Spec.Env, "FLUXYR_NEW", "enabled") || !hasEnvValue(got.Spec.Env, "FLUXYR_HOST", "0.0.0.0") {
		t.Fatalf("env %+v", got.Spec.Env)
	}
	if vars["NEW_TOKEN"] == "" || len(vars["NEW_TOKEN"]) != 16 || vars["NEW_MODE"] != "fast" || vars["NEW_NEEDED"] != "yes" {
		t.Fatalf("new inputs: %v", keysOf(vars))
	}
	if !v.widget(app, "cache") || !bound(got, "Widget", "cache") {
		t.Fatal("the resource is made and bound")
	}
	var vols shpyrdv1.VolumeList
	if err := v.cr.List(context.Background(), &vols, client.InNamespace(app.Namespace)); err != nil || len(vols.Items) != 2 {
		t.Fatalf("volumes %v %v", vols.Items, err)
	}
	// No secret is echoed or put in the audit.
	if strings.Contains(r.body, vars["NEW_TOKEN"]) || strings.Contains(r.body, generated) {
		t.Fatalf("the answer carries a secret: %s", r.body)
	}
	rec = do(t, v.s, "GET", "/api/projects/fluxyr/audit", "", true)
	if !strings.Contains(rec.Body.String(), "catalog.update") || strings.Contains(rec.Body.String(), vars["NEW_TOKEN"]) {
		t.Fatalf("audit %s", rec.Body.String())
	}
	// The project then reads as updating to the new version.
	v.want(CatalogUpdating, commitOld, commitNew)
}

func keysOf(m map[string]string) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestAnUpdateIsRefusedWithNothingChanged(t *testing.T) {
	v := newVersionedCatalog(t)
	app := v.installedAndRunning()
	v.publishNext(commitNew, fluxyrNext())
	digest := v.currentDigest()
	for name, tc := range map[string]struct {
		body   string
		code   int
		inBody string
	}{
		"digest is not the catalog's": {`{"digest":"sha256:stale","inputs":{"NEW_NEEDED":"y"}}`, http.StatusConflict, "preview"},
		"an input the entry does not ask": {`{"digest":"` + digest + `","inputs":{"NEW_NEEDED":"y","NOPE":"x"}}`, http.StatusBadRequest, "NOPE"},
		"an input the project already has": {`{"digest":"` + digest + `","inputs":{"NEW_NEEDED":"y","OPENROUTER_API_KEY":"x"}}`, http.StatusBadRequest, "already set"},
		"a required input is missing":       {`{"digest":"` + digest + `"}`, http.StatusUnprocessableEntity, "NEW_NEEDED"},
	} {
		before := v.snapshot(app)
		r := v.apply(tc.body)
		if r.code != tc.code || !strings.Contains(r.body, tc.inBody) {
			t.Errorf("%s: %d %s", name, r.code, r.body)
		}
		if v.snapshot(app) != before || v.widget(app, "cache") {
			t.Errorf("%s: changed the project", name)
		}
	}
	// A resource kind the cluster lacks.
	v.publishNext(commitNew, strings.Replace(fluxyrNext(), "kind: widget", "kind: gadget", 1))
	before := v.snapshot(app)
	if r := v.apply(`{"digest":"` + v.currentDigest() + `","inputs":{"NEW_NEEDED":"y"}}`); r.code != http.StatusBadRequest || !strings.Contains(r.body, "gadget") {
		t.Errorf("unavailable kind: %d %s", r.code, r.body)
	}
	if v.snapshot(app) != before {
		t.Error("an unavailable kind changed the project")
	}
}

func TestNothingIsAppliedWhenThereIsNothingToApply(t *testing.T) {
	v := newVersionedCatalog(t)
	app := v.installedAndRunning()
	digest := v.currentDigest()
	// Up to date.
	before := v.snapshot(app)
	r := v.apply(`{"digest":"` + digest + `"}`)
	var res CatalogUpdateResult
	if err := json.Unmarshal([]byte(r.body), &res); err != nil || r.code != http.StatusOK || res.Changed || res.Reason == "" {
		t.Fatalf("up to date: %d %s", r.code, r.body)
	}
	if v.snapshot(app) != before {
		t.Fatal("changed")
	}
	// Rolling out already.
	v.publishNext(commitNew, fluxyrNext())
	digest = v.currentDigest()
	v.setApp(app, func(a *shpyrdv1.App) {
		a.Annotations[shpyrdv1.AnnotationCatalogCommit] = commitNew
		a.Annotations[shpyrdv1.AnnotationCatalogDigest] = digest
	})
	before = v.snapshot(app)
	r = v.apply(`{"digest":"` + digest + `","inputs":{"NEW_NEEDED":"y"}}`)
	if err := json.Unmarshal([]byte(r.body), &res); err != nil || r.code != http.StatusOK || res.Changed || !strings.Contains(res.Reason, "rolling out") {
		t.Fatalf("rolling out: %d %s", r.code, r.body)
	}
	if v.snapshot(app) != before {
		t.Fatal("changed")
	}
	// Not offered: another catalog.
	v.setApp(app, func(a *shpyrdv1.App) { a.Annotations[shpyrdv1.AnnotationCatalogSource] = "https://elsewhere.test/index.json" })
	before = v.snapshot(app)
	if r = v.apply(`{"digest":"` + digest + `"}`); r.code != http.StatusConflict {
		t.Fatalf("not offered: %d %s", r.code, r.body)
	}
	if v.snapshot(app) != before {
		t.Fatal("changed")
	}
}

func TestApplyingAfterARollbackReleasesThePinAndMakesNothingAgain(t *testing.T) {
	v := newVersionedCatalog(t)
	app := v.installedAndRunning()
	v.publishNext(commitNew, fluxyrNext())
	digest := v.currentDigest()
	if r := v.apply(`{"digest":"` + digest + `","inputs":{"NEW_NEEDED":"y"}}`); r.code != http.StatusAccepted {
		t.Fatalf("%d %s", r.code, r.body)
	}
	vars := v.vars(app)
	// The controller records the release; then a rollback to release 1.
	v.setApp(app, func(a *shpyrdv1.App) {
		ranRelease(a, 2, "r/b@sha256:2", &shpyrdv1.CatalogVersion{Entry: "fluxyr", Digest: digest, Commit: commitNew, Revision: "main"})
		a.Spec.Image = "r/a@sha256:1"
		ranRelease(a, 3, "r/a@sha256:1", &shpyrdv1.CatalogVersion{Entry: "fluxyr", Digest: a.Status.Releases[0].Catalog.Digest, Commit: commitOld, Revision: "main"})
	})
	v.want(CatalogUpdateAvailable, commitOld, commitNew)
	// Everything the first update made is still there: nothing to make.
	rec := do(t, v.s, "GET", "/api/projects/fluxyr/catalog/update", "", true)
	var p CatalogUpdatePreview
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Volumes) != 0 || len(p.Resources) != 0 || len(p.Inputs) != 0 {
		t.Fatalf("nothing is made again: %+v", p)
	}
	r := v.apply(`{"digest":"` + digest + `"}`)
	if r.code != http.StatusAccepted {
		t.Fatalf("%d %s", r.code, r.body)
	}
	got := v.fresh(app)
	if got.Spec.Image != "" || got.Spec.Source.Git.Revision != commitNew || got.Annotations[configvars.AnnotationDeployRequested] == "" {
		t.Fatalf("the pin is not released: %q %+v", got.Spec.Image, got.Spec.Source.Git)
	}
	if after := v.vars(app); !reflect.DeepEqual(after, vars) {
		t.Fatal("config vars were made or changed again")
	}
	var vols shpyrdv1.VolumeList
	if err := v.cr.List(context.Background(), &vols, client.InNamespace(app.Namespace)); err != nil || len(vols.Items) != 2 {
		t.Fatalf("volumes %d", len(vols.Items))
	}
	// Now it is rolling out to the new version.
	v.want(CatalogUpdating, commitOld, commitNew)
}

func TestWhoMayPreviewAndApplyAnUpdate(t *testing.T) {
	v := newVersionedCatalog(t)
	app := v.installedAndRunning()
	v.publishNext(commitNew, fluxyrNext())
	digest := v.currentDigest()
	ctx := context.Background()
	key := projectGrantKey(v.fresh(app))
	as := func(email, role string) (string, string) {
		sid, csrf, _ := signInPerson(t, v.s, email, store.WorkspaceRoleMember)
		if role != "" {
			if _, err := v.s.store.AddGrant(ctx, store.DefaultWorkspace, store.Grant{Project: key, Role: role, User: email}); err != nil {
				t.Fatal(err)
			}
			v.s.authz.Invalidate()
		}
		return sid, csrf
	}
	viewerSID, viewerCSRF := as("viewer@example.test", shpyrdv1.RoleViewer)
	devSID, devCSRF := as("dev@example.test", shpyrdv1.RoleDeveloper)
	strangerSID, strangerCSRF := as("stranger@example.test", "")
	url := "/api/projects/fluxyr/catalog/update"
	body := `{"digest":"` + digest + `","inputs":{"NEW_NEEDED":"y"}}`

	if r := doCookie(t, v.s, "GET", url, "", viewerSID, ""); r.Code != http.StatusOK {
		t.Errorf("a viewer previews: %d %s", r.Code, r.Body.String())
	}
	if r := doCookie(t, v.s, "GET", url, "", strangerSID, ""); r.Code != http.StatusForbidden && r.Code != http.StatusNotFound {
		t.Errorf("someone with no access previews: %d", r.Code)
	}
	before := v.snapshot(app)
	if r := doCookie(t, v.s, "POST", url, body, viewerSID, viewerCSRF); r.Code != http.StatusForbidden {
		t.Errorf("a viewer applies: %d %s", r.Code, r.Body.String())
	}
	if r := doCookie(t, v.s, "POST", url, body, strangerSID, strangerCSRF); r.Code != http.StatusForbidden && r.Code != http.StatusNotFound {
		t.Errorf("a stranger applies: %d", r.Code)
	}
	// A developer may deploy and change config, but not make a resource.
	if r := doCookie(t, v.s, "POST", url, body, devSID, devCSRF); r.Code != http.StatusForbidden || !strings.Contains(r.Body.String(), "manage resources") {
		t.Errorf("a developer applies an update that makes a resource: %d %s", r.Code, r.Body.String())
	}
	if v.snapshot(app) != before {
		t.Fatal("a refusal changes nothing")
	}
	// An update that makes nothing to manage is the developer's.
	noResources := strings.NewReplacer("  resources:\n    - kind: widget\n      name: cache\n  volumes:\n    - name: extra\n      size: 1Gi\n", "  volumes:\n", "    volumes: [{name: extra, path: /extra}]\n", "")
	v.publishNext(commitNew, noResources.Replace(fluxyrNext()))
	body = `{"digest":"` + v.currentDigest() + `","inputs":{"NEW_NEEDED":"y"}}`
	if r := doCookie(t, v.s, "POST", url, body, devSID, devCSRF); r.Code != http.StatusAccepted {
		t.Errorf("a developer applies: %d %s", r.Code, r.Body.String())
	}
}

func TestAProjectNotFromTheCatalogHasNoUpdate(t *testing.T) {
	s, _ := catalogServer(t)
	if rec := do(t, s, "POST", "/api/projects", `{"name":"plain"}`, true); rec.Code != http.StatusCreated {
		t.Fatalf("%d", rec.Code)
	}
	if rec := do(t, s, "GET", "/api/projects/plain/catalog/update", "", true); rec.Code != http.StatusNotFound {
		t.Errorf("preview: %d", rec.Code)
	}
	if rec := do(t, s, "POST", "/api/projects/plain/catalog/update", `{"digest":"x"}`, true); rec.Code != http.StatusNotFound {
		t.Errorf("apply: %d", rec.Code)
	}
}

