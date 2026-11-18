package api

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		!reflect.DeepEqual(p.ProcessesAdded, []string{"worker"}) || !reflect.DeepEqual(p.EnvAdded, []string{"FLUXYR_NEW"}) || len(p.EnvKept) != 0 {
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
		"digest is not the catalog's":      {`{"digest":"sha256:stale","inputs":{"NEW_NEEDED":"y"}}`, http.StatusConflict, "preview"},
		"an input the entry does not ask":  {`{"digest":"` + digest + `","inputs":{"NEW_NEEDED":"y","NOPE":"x"}}`, http.StatusBadRequest, "NOPE"},
		"an input the project already has": {`{"digest":"` + digest + `","inputs":{"NEW_NEEDED":"y","OPENROUTER_API_KEY":"x"}}`, http.StatusBadRequest, "already set"},
		"a required input is missing":      {`{"digest":"` + digest + `"}`, http.StatusUnprocessableEntity, "NEW_NEEDED"},
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
		a.Spec.Source.Git.Revision = commitNew
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
	v.setApp(app, func(a *shpyrdv1.App) {
		a.Annotations[shpyrdv1.AnnotationCatalogSource] = "https://elsewhere.test/index.json"
	})
	before = v.snapshot(app)
	if r = v.apply(`{"digest":"` + digest + `"}`); r.code != http.StatusConflict {
		t.Fatalf("not offered: %d %s", r.code, r.body)
	}
	if v.snapshot(app) != before {
		t.Fatal("changed")
	}
}

// settle plays the controller recording release n: the pending config vars
// become the live ones and are deleted, and the live ones are copied to the
// release's snapshot (internal/controller/snapshots.go, snapshotRelease).
func (v *versionedCatalog) settle(app *shpyrdv1.App, n int) {
	v.t.Helper()
	ctx := context.Background()
	a := v.fresh(app)
	sec, pending, err := configvars.Read(ctx, v.cr, a)
	if err != nil {
		v.t.Fatal(err)
	}
	data := map[string][]byte{}
	for k, b := range sec.Data {
		data[k] = append([]byte(nil), b...)
	}
	live := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: a.EnvSecretName(), Namespace: a.Namespace}}
	if err := v.cr.Get(ctx, client.ObjectKeyFromObject(live), live); err == nil {
		live.Data = data
		err = v.cr.Update(ctx, live)
	} else {
		live.Data = data
		err = v.cr.Create(ctx, live)
	}
	if err != nil {
		v.t.Fatal(err)
	}
	if pending {
		if err := v.cr.Delete(ctx, sec); err != nil {
			v.t.Fatal(err)
		}
	}
	snap := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: a.ReleaseSnapshotName(n), Namespace: a.Namespace}, Data: data}
	if err := v.cr.Create(ctx, snap); err != nil {
		v.t.Fatal(err)
	}
}

// rollbackConfig plays the controller restoring release n's config vars:
// the live Secret becomes the snapshot, whatever it held before
// (internal/controller/snapshots.go, restoreRelease).
func (v *versionedCatalog) rollbackConfig(app *shpyrdv1.App, n int) {
	v.t.Helper()
	ctx := context.Background()
	a := v.fresh(app)
	snap := &corev1.Secret{}
	if err := v.cr.Get(ctx, types.NamespacedName{Namespace: a.Namespace, Name: a.ReleaseSnapshotName(n)}, snap); err != nil {
		v.t.Fatal(err)
	}
	live := &corev1.Secret{}
	if err := v.cr.Get(ctx, types.NamespacedName{Namespace: a.Namespace, Name: a.EnvSecretName()}, live); err != nil {
		v.t.Fatal(err)
	}
	live.Data = map[string][]byte{}
	for k, b := range snap.Data {
		live.Data[k] = append([]byte(nil), b...)
	}
	if err := v.cr.Update(ctx, live); err != nil {
		v.t.Fatal(err)
	}
}

func TestApplyingAfterARollbackReleasesThePinAndMakesNothingAgain(t *testing.T) {
	v := newVersionedCatalog(t)
	app := v.installedAndRunning()
	v.settle(app, 1)
	v.publishNext(commitNew, fluxyrNext())
	digest := v.currentDigest()
	if r := v.apply(`{"digest":"` + digest + `","inputs":{"NEW_NEEDED":"y"}}`); r.code != http.StatusAccepted {
		t.Fatalf("%d %s", r.code, r.body)
	}
	// The controller records release 2 with its config vars, then a rollback
	// to release 1 restores release 1's: what the update added is gone.
	updatedVars := v.vars(app)
	v.settle(app, 2)
	v.setApp(app, func(a *shpyrdv1.App) {
		ranRelease(a, 2, "r/b@sha256:2", &shpyrdv1.CatalogVersion{Entry: "fluxyr", Digest: digest, Commit: commitNew, Revision: "main"})
		a.Spec.Image = "r/a@sha256:1"
		a.Spec.Bindings = nil // the rollback restores release 1's bindings
		ranRollback(a, 3, "r/a@sha256:1", &shpyrdv1.CatalogVersion{Entry: "fluxyr", Digest: a.Status.Releases[0].Catalog.Digest, Commit: commitOld, Revision: "main"}, 1)
	})
	v.rollbackConfig(app, 1)
	rolled := v.vars(app)
	if rolled["NEW_TOKEN"] != "" || rolled["NEW_NEEDED"] != "" || rolled["OPENROUTER_API_KEY"] == "" {
		t.Fatalf("a rollback leaves release 1's config vars: %v", keysOf(rolled))
	}
	v.want(CatalogUpdateAvailable, commitOld, commitNew)

	// Volumes and the resource are still there, so nothing is made again;
	// the values the project had are taken back, not generated again.
	rec := do(t, v.s, "GET", "/api/projects/fluxyr/catalog/update", "", true)
	var p CatalogUpdatePreview
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Volumes) != 0 || len(p.Resources) != 0 {
		t.Fatalf("nothing is made again: %+v", p)
	}
	actions := map[string]string{}
	for _, in := range p.Inputs {
		actions[in.Name] = in.Action
	}
	if !reflect.DeepEqual(actions, map[string]string{"NEW_TOKEN": "restore", "NEW_MODE": "restore", "NEW_NEEDED": "restore"}) {
		t.Fatalf("inputs %v", actions)
	}
	r := v.apply(`{"digest":"` + digest + `"}`)
	if r.code != http.StatusAccepted {
		t.Fatalf("%d %s", r.code, r.body)
	}
	got := v.fresh(app)
	if got.Spec.Image != "" || got.Spec.Source.Git.Revision != commitNew || !bound(got, "Widget", "cache") {
		t.Fatalf("the pin is not released or the resource not bound again: %q %+v", got.Spec.Image, got.Spec.Bindings)
	}
	after := v.vars(app)
	for _, k := range []string{"NEW_TOKEN", "NEW_MODE", "NEW_NEEDED", "VAULT_ENCRYPTION_KEY", "OPENROUTER_API_KEY"} {
		if after[k] == "" || after[k] != updatedVars[k] {
			t.Fatalf("%s is not what the project had: %q, want %q", k, after[k], updatedVars[k])
		}
	}
	var vols shpyrdv1.VolumeList
	if err := v.cr.List(context.Background(), &vols, client.InNamespace(app.Namespace)); err != nil || len(vols.Items) != 2 {
		t.Fatalf("volumes %d", len(vols.Items))
	}
	v.want(CatalogUpdating, commitOld, commitNew)

	// An input no earlier release held is still made new.
	v.publishNext(commitNew, strings.Replace(fluxyrNext(), "  inputs:\n", "  inputs:\n    - name: BRAND_NEW\n      generate: 4\n", 1))
	v.setApp(app, func(a *shpyrdv1.App) {
		a.Annotations[shpyrdv1.AnnotationCatalogDigest] = "sha256:old"
	})
	if r := v.apply(`{"digest":"` + v.currentDigest() + `"}`); r.code != http.StatusAccepted {
		t.Fatalf("%d %s", r.code, r.body)
	}
	if got := v.vars(app)["BRAND_NEW"]; len(got) != 8 {
		t.Fatalf("a new generated input is generated: %q", got)
	}
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

// ---- what moves a project off the catalog's version, through the real
// endpoints, and what holds it on an older one

const entryGitURL = "https://github.com/shpyrd-io/fluxyr"

func (v *versionedCatalog) post(path, body string) *httptestResult {
	rec := do(v.t, v.s, "POST", path, body, true)
	return &httptestResult{code: rec.Code, body: rec.Body.String()}
}

func (v *versionedCatalog) stateNow() (summary *ProjectCatalog, detail *ProjectCatalog) {
	s, d, _ := v.state()
	return s, d
}

func (v *versionedCatalog) wantEmpty() {
	v.t.Helper()
	s, d := v.stateNow()
	if s == nil || d == nil || s.State != "" || d.State != "" || s.Offered != nil {
		v.t.Fatalf("no offer is made: %+v %+v", s, d)
	}
}

func TestADeployOfAnotherCommitOfTheEntrysRepositoryIsOfferedTheCatalogsVersion(t *testing.T) {
	v := newVersionedCatalog(t)
	app := v.installedAndRunning()
	v.want(CatalogUpToDate, commitOld, "")
	other := "3333333333333333333333333333333333333333"
	for _, revision := range []string{other, "main"} {
		if r := v.post("/api/projects/fluxyr/deploy", `{"git":{"url":"`+entryGitURL+`","revision":"`+revision+`"}}`); r.code != http.StatusAccepted {
			t.Fatalf("deploy: %d %s", r.code, r.body)
		}
		// Before the controller records a release for it, and after.
		v.want(CatalogUpdateAvailable, commitOld, commitOld)
		v.setApp(app, func(a *shpyrdv1.App) { ranRelease(a, len(a.Status.Releases)+1, "r/x@sha256:"+revision[:4], nil) })
		// The new release runs no version of the entry.
		v.want(CatalogUpdateAvailable, "", commitOld)
		// Put it back for the next round.
		v.setApp(app, func(a *shpyrdv1.App) {
			a.Status.Releases = a.Status.Releases[:1]
			a.Spec.Source.Git.Revision = commitOld
		})
	}
	// Apply it: the source is the catalog's commit with the entry's overlay.
	v.post("/api/projects/fluxyr/deploy", `{"git":{"url":"`+entryGitURL+`","revision":"`+other+`"}}`)
	v.setApp(app, func(a *shpyrdv1.App) { ranRelease(a, 2, "r/x@sha256:3333", nil) })
	var p CatalogUpdatePreview
	rec := do(t, v.s, "GET", "/api/projects/fluxyr/catalog/update", "", true)
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || p.State != CatalogUpdateAvailable || commitOf(p.To) != commitOld {
		t.Fatalf("preview %d %s", rec.Code, rec.Body.String())
	}
	r := v.apply(`{"digest":"` + v.currentDigest() + `"}`)
	var res CatalogUpdateResult
	if err := json.Unmarshal([]byte(r.body), &res); err != nil || r.code != http.StatusAccepted || !res.Changed {
		t.Fatalf("apply: %d %s", r.code, r.body)
	}
	got := v.fresh(app)
	if got.Spec.Source.Git.Revision != commitOld || got.Spec.Source.Overlay == nil || got.Spec.Image != "" {
		t.Fatalf("source after apply: %+v %+v", got.Spec.Source.Git, got.Spec.Source.Overlay)
	}
	v.want(CatalogUpdating, "", commitOld)
	r = v.apply(`{"digest":"` + v.currentDigest() + `"}`)
	if err := json.Unmarshal([]byte(r.body), &res); err != nil || res.Changed {
		t.Fatalf("a second apply changes nothing: %d %s", r.code, r.body)
	}
}

func TestAnImageDeployedByHandIsNoLongerOfferedTheCatalogsVersion(t *testing.T) {
	v := newVersionedCatalog(t)
	app := v.installedAndRunning()
	v.publishNext(commitNew, fluxyrNext())
	if r := v.post("/api/projects/fluxyr/deploy", `{"image":"registry.example.test/other/app:1"}`); r.code != http.StatusAccepted {
		t.Fatalf("deploy: %d %s", r.code, r.body)
	}
	v.wantEmpty()
	// After the controller records its release, with no version.
	v.setApp(app, func(a *shpyrdv1.App) { ranRelease(a, 2, "registry.example.test/other/app:1", nil) })
	v.wantEmpty()
	before := v.snapshot(app)
	if r := v.apply(`{"digest":"` + v.currentDigest() + `","inputs":{"NEW_NEEDED":"y"}}`); r.code != http.StatusConflict {
		t.Fatalf("an update is not applied over an image: %d %s", r.code, r.body)
	}
	if v.snapshot(app) != before {
		t.Fatal("changed")
	}
}

func TestAnUploadOrAnotherRepositoryIsNotOfferedAnUpdateEither(t *testing.T) {
	for name, body := range map[string]string{
		"another repository": `{"git":{"url":"https://github.com/someone/else","revision":"main"}}`,
		"an upload":          `{"blob":{"url":"http://elsewhere.test/blob","sha256":"abc"}}`,
	} {
		v := newVersionedCatalog(t)
		v.installedAndRunning()
		v.publishNext(commitNew, fluxyrNext())
		if r := v.post("/api/projects/fluxyr/deploy", body); r.code != http.StatusAccepted {
			t.Logf("%s: deploy answered %d %s", name, r.code, r.body)
			continue
		}
		s, d := v.stateNow()
		if s == nil || d == nil || s.State != "" || d.State != "" {
			t.Errorf("%s: %+v %+v", name, s, d)
		}
	}
}

func TestARollbackThroughTheEndpointHoldsTheProjectOnTheOlderVersion(t *testing.T) {
	v := newVersionedCatalog(t)
	app := v.installedAndRunning()
	v.publishNext(commitNew, fluxyrNext())
	digestNew := v.currentDigest()
	if r := v.apply(`{"digest":"` + digestNew + `","inputs":{"NEW_NEEDED":"y"}}`); r.code != http.StatusAccepted {
		t.Fatalf("%d %s", r.code, r.body)
	}
	v.setApp(app, func(a *shpyrdv1.App) {
		ranRelease(a, 2, "r/b@sha256:2", &shpyrdv1.CatalogVersion{Entry: "fluxyr", Digest: digestNew, Commit: commitNew, Revision: "main"})
	})
	v.want(CatalogUpToDate, commitNew, "")
	if r := v.post("/api/projects/fluxyr/rollback", `{"release":1,"force":true}`); r.code != http.StatusOK {
		t.Fatalf("rollback: %d %s", r.code, r.body)
	}
	// Before the controller records the rollback release, and after.
	v.want(CatalogUpdateAvailable, commitNew, commitNew)
	// The controller records the rollback and consumes the request.
	v.setApp(app, func(a *shpyrdv1.App) {
		delete(a.Annotations, shpyrdv1.AnnotationReleaseNote)
		delete(a.Annotations, shpyrdv1.AnnotationRollbackTo)
		ranRollback(a, 3, "r/a@sha256:1", a.Status.Releases[0].Catalog, 1)
	})
	v.want(CatalogUpdateAvailable, commitOld, commitNew)
	if r := v.apply(`{"digest":"` + digestNew + `"}`); r.code != http.StatusAccepted {
		t.Fatalf("apply after a rollback: %d %s", r.code, r.body)
	}
	if got := v.fresh(app); got.Spec.Image != "" {
		t.Fatalf("the pin is released: %q", got.Spec.Image)
	}
}

func TestAnUpdateKeepsAPlainVariableTheProjectHas(t *testing.T) {
	v := newVersionedCatalog(t)
	app := v.installedAndRunning()
	v.setApp(app, func(a *shpyrdv1.App) {
		for i := range a.Spec.Env {
			if a.Spec.Env[i].Name == "FLUXYR_HOST" {
				a.Spec.Env[i].Value = "127.0.0.1"
			}
		}
	})
	// A version that declares the same value and adds a variable.
	v.publishNext(commitNew, fluxyrNext())
	var p CatalogUpdatePreview
	rec := do(t, v.s, "GET", "/api/projects/fluxyr/catalog/update", "", true)
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.EnvAdded, []string{"FLUXYR_NEW"}) || !reflect.DeepEqual(p.EnvKept, []string{"FLUXYR_HOST"}) {
		t.Fatalf("preview %+v", p)
	}
	if r := v.apply(`{"digest":"` + v.currentDigest() + `","inputs":{"NEW_NEEDED":"y"}}`); r.code != http.StatusAccepted {
		t.Fatalf("%d %s", r.code, r.body)
	}
	got := v.fresh(app)
	if !hasEnvValue(got.Spec.Env, "FLUXYR_HOST", "127.0.0.1") || !hasEnvValue(got.Spec.Env, "FLUXYR_NEW", "enabled") {
		t.Fatalf("env %+v", got.Spec.Env)
	}
	// Another version changes the value it declares: the project's stays.
	v.setApp(app, func(a *shpyrdv1.App) {
		a.Annotations[shpyrdv1.AnnotationCatalogDigest] = "sha256:old"
		for i := range a.Spec.Env {
			if a.Spec.Env[i].Name == "FLUXYR_HOST" {
				a.Spec.Env[i].Value = "0.0.0.0"
			}
		}
	})
	v.publishNext("4444444444444444444444444444444444444444", strings.Replace(fluxyrNext(), "FLUXYR_HOST: 0.0.0.0", "FLUXYR_HOST: 0.0.0.0:9000", 1))
	rec = do(t, v.s, "GET", "/api/projects/fluxyr/catalog/update", "", true)
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || !reflect.DeepEqual(p.EnvKept, []string{"FLUXYR_HOST"}) || len(p.EnvAdded) != 0 {
		t.Fatalf("preview %v %+v", err, p)
	}
	if r := v.apply(`{"digest":"` + v.currentDigest() + `"}`); r.code != http.StatusAccepted {
		t.Fatalf("%d %s", r.code, r.body)
	}
	if got := v.fresh(app); !hasEnvValue(got.Spec.Env, "FLUXYR_HOST", "0.0.0.0") {
		t.Fatalf("env %+v", got.Spec.Env)
	}
}

// ---- an image deployed by hand is not a rollback, however it came to be an
// image the project ran before; and what attaches a resource needs the
// permission to

// ranDeploy is the release the controller records for a deploy of image.
func ranDeploy(app *shpyrdv1.App, n int, image string) {
	app.Status.Releases = append(app.Status.Releases, shpyrdv1.Release{Number: n, Image: image, Source: "x", Description: "Deploy"})
}

func TestAnImageDeployedAgainAfterAnotherIsStillNoRollback(t *testing.T) {
	v := newVersionedCatalog(t)
	app := v.installedAndRunning()
	v.publishNext(commitNew, fluxyrNext())
	for n, image := range []string{"registry.example.test/app:1", "registry.example.test/app:2", "registry.example.test/app:1"} {
		if r := v.post("/api/projects/fluxyr/deploy", `{"image":"`+image+`"}`); r.code != http.StatusAccepted {
			t.Fatalf("deploy %s: %d %s", image, r.code, r.body)
		}
		v.wantEmpty()
		v.setApp(app, func(a *shpyrdv1.App) { ranDeploy(a, n+2, image) })
		v.wantEmpty()
	}
	before := v.snapshot(app)
	if r := v.apply(`{"digest":"` + v.currentDigest() + `","inputs":{"NEW_NEEDED":"y"}}`); r.code != http.StatusConflict {
		t.Fatalf("an update is not applied over an image: %d %s", r.code, r.body)
	}
	if v.snapshot(app) != before {
		t.Fatal("changed")
	}
}

// The CLI, with a kubeconfig, writes the App itself (internal/cli
// appcmds.go): the same marks as the API leaves.
func TestARollbackWrittenByTheCLIHoldsTheProjectOnTheOlderVersion(t *testing.T) {
	v := newVersionedCatalog(t)
	app := v.installedAndRunning()
	v.publishNext(commitNew, fluxyrNext())
	digestNew := v.currentDigest()
	if r := v.apply(`{"digest":"` + digestNew + `","inputs":{"NEW_NEEDED":"y"}}`); r.code != http.StatusAccepted {
		t.Fatalf("%d %s", r.code, r.body)
	}
	v.setApp(app, func(a *shpyrdv1.App) {
		ranRelease(a, 2, "r/b@sha256:2", &shpyrdv1.CatalogVersion{Entry: "fluxyr", Digest: digestNew, Commit: commitNew, Revision: "main"})
	})
	v.setApp(app, func(a *shpyrdv1.App) {
		a.Spec.Image = "r/a@sha256:1"
		a.Annotations[shpyrdv1.AnnotationReleaseNote] = "Rollback to v1"
		a.Annotations[shpyrdv1.AnnotationRollbackTo] = "1"
	})
	// The request, not yet recorded.
	v.want(CatalogUpdateAvailable, commitNew, commitNew)
	// The controller records it and consumes the request.
	v.setApp(app, func(a *shpyrdv1.App) {
		delete(a.Annotations, shpyrdv1.AnnotationReleaseNote)
		delete(a.Annotations, shpyrdv1.AnnotationRollbackTo)
		ranRollback(a, 3, "r/a@sha256:1", a.Status.Releases[0].Catalog, 1)
	})
	v.want(CatalogUpdateAvailable, commitOld, commitNew)
	// Releases of config vars after it keep it held.
	v.setApp(app, func(a *shpyrdv1.App) { ranRelease(a, 4, "r/a@sha256:1", a.Status.Releases[0].Catalog) })
	v.want(CatalogUpdateAvailable, commitOld, commitNew)
	if r := v.apply(`{"digest":"` + digestNew + `"}`); r.code != http.StatusAccepted {
		t.Fatalf("apply: %d %s", r.code, r.body)
	}
}

func TestApplyingAnUpdateThatAttachesAResourceNeedsTheResourcePermission(t *testing.T) {
	v := newVersionedCatalog(t)
	app := v.installedAndRunning()
	v.publishNext(commitNew, fluxyrNext())
	digest := v.currentDigest()
	// The first update makes the resource and binds it; a rollback restores
	// the older bindings and leaves the resource: it exists, unbound.
	if r := v.apply(`{"digest":"` + digest + `","inputs":{"NEW_NEEDED":"y"}}`); r.code != http.StatusAccepted {
		t.Fatalf("%d %s", r.code, r.body)
	}
	v.setApp(app, func(a *shpyrdv1.App) {
		ranRelease(a, 2, "r/b@sha256:2", &shpyrdv1.CatalogVersion{Entry: "fluxyr", Digest: digest, Commit: commitNew, Revision: "main"})
		a.Spec.Image = "r/a@sha256:1"
		a.Spec.Bindings = nil
		ranRollback(a, 3, "r/a@sha256:1", a.Status.Releases[0].Catalog, 1)
	})
	if !v.widget(app, "cache") || bound(v.fresh(app), "Widget", "cache") {
		t.Fatal("the resource exists and is not bound")
	}
	// Whoever may deploy and change config, but not manage resources, may not
	// attach it by hand, nor by an update.
	key := projectGrantKey(v.fresh(app))
	sid, csrf, _ := signInPerson(t, v.s, "dev@example.test", store.WorkspaceRoleMember)
	if _, err := v.s.store.AddGrant(context.Background(), store.DefaultWorkspace, store.Grant{Project: key, Role: shpyrdv1.RoleDeveloper, User: "dev@example.test"}); err != nil {
		t.Fatal(err)
	}
	v.s.authz.Invalidate()
	body := `{"digest":"` + digest + `"}`
	preview := doCookie(t, v.s, "GET", "/api/projects/fluxyr/catalog/update", "", sid, "")
	var p CatalogUpdatePreview
	if err := json.Unmarshal(preview.Body.Bytes(), &p); err != nil || !reflect.DeepEqual(p.Binds, []string{"widget cache"}) || len(p.Resources) != 0 {
		t.Fatalf("the preview names what it attaches: %v %+v", err, p)
	}
	before := v.snapshot(app)
	if r := doCookie(t, v.s, "POST", "/api/projects/fluxyr/catalog/update", body, sid, csrf); r.Code != http.StatusForbidden || !strings.Contains(r.Body.String(), "manage resources") {
		t.Fatalf("a developer attaches by an update: %d %s", r.Code, r.Body.String())
	}
	if v.snapshot(app) != before || bound(v.fresh(app), "Widget", "cache") {
		t.Fatal("a refusal changes nothing")
	}
	// An administrator of the project may.
	adminSID, adminCSRF, _ := signInPerson(t, v.s, "admin@example.test", store.WorkspaceRoleMember)
	if _, err := v.s.store.AddGrant(context.Background(), store.DefaultWorkspace, store.Grant{Project: key, Role: shpyrdv1.RoleAdmin, User: "admin@example.test"}); err != nil {
		t.Fatal(err)
	}
	v.s.authz.Invalidate()
	if r := doCookie(t, v.s, "POST", "/api/projects/fluxyr/catalog/update", body, adminSID, adminCSRF); r.Code != http.StatusAccepted {
		t.Fatalf("an admin applies: %d %s", r.Code, r.Body.String())
	}
	if !bound(v.fresh(app), "Widget", "cache") {
		t.Fatal("bound again")
	}
}

// A deploy may give any release note, "Rollback to v1" included: it is no
// rollback, which only the controller records (Release.RollbackTo).
func TestADeployWithTheNoteOfARollbackIsNoRollback(t *testing.T) {
	v := newVersionedCatalog(t)
	app := v.installedAndRunning()
	v.publishNext(commitNew, fluxyrNext())
	v.setApp(app, func(a *shpyrdv1.App) { ranDeploy(a, 2, "registry.example.test/app:1") })
	if r := v.post("/api/projects/fluxyr/deploy", `{"image":"registry.example.test/app:2","note":"Rollback to v1"}`); r.code != http.StatusAccepted {
		t.Fatalf("deploy: %d %s", r.code, r.body)
	}
	v.wantEmpty()
	// The controller records it with the note a deploy gave, and no rollback.
	v.setApp(app, func(a *shpyrdv1.App) {
		a.Status.Releases = append(a.Status.Releases, shpyrdv1.Release{Number: 3, Image: "registry.example.test/app:2", Source: "x", Description: "Rollback to v1"})
		delete(a.Annotations, shpyrdv1.AnnotationReleaseNote)
	})
	v.wantEmpty()
	// The same note on an image deployed again after another.
	v.post("/api/projects/fluxyr/deploy", `{"image":"registry.example.test/app:1","note":"Rollback to v1"}`)
	v.setApp(app, func(a *shpyrdv1.App) {
		a.Status.Releases = append(a.Status.Releases, shpyrdv1.Release{Number: 4, Image: "registry.example.test/app:1", Source: "x", Description: "Rollback to v1"})
	})
	v.wantEmpty()
}

// The limit of the offer after a rollback: the release the controller
// recorded for it must be in the history of 20 releases.
func TestTheOfferAfterARollbackLastsWhileItsReleaseIsInTheHistory(t *testing.T) {
	for name, further := range map[string]int{"19 further releases keep the rollback release": 19, "20 push it out": 20} {
		v := newVersionedCatalog(t)
		app := v.installedAndRunning()
		v.publishNext(commitNew, fluxyrNext())
		digestNew := v.currentDigest()
		if r := v.apply(`{"digest":"` + digestNew + `","inputs":{"NEW_NEEDED":"y"}}`); r.code != http.StatusAccepted {
			t.Fatalf("%s: %d %s", name, r.code, r.body)
		}
		v.setApp(app, func(a *shpyrdv1.App) {
			versionA := a.Status.Releases[0].Catalog
			// The history as the controller keeps it: the last 20 releases.
			a.Status.Releases = nil
			a.Spec.Image = "r/a@sha256:1"
			n := 3
			if further < 20 {
				ranRollback(a, n, "r/a@sha256:1", versionA, 1)
				for i := 0; i < further; i++ {
					n++
					ranRelease(a, n, "r/a@sha256:1", versionA)
				}
			} else {
				for i := 0; i < further; i++ {
					ranRelease(a, n+i+1, "r/a@sha256:1", versionA)
				}
			}
			if len(a.Status.Releases) > 20 {
				t.Fatalf("the history holds 20: %d", len(a.Status.Releases))
			}
		})
		if further < 20 {
			v.want(CatalogUpdateAvailable, commitOld, commitNew)
			if r := v.apply(`{"digest":"` + digestNew + `"}`); r.code != http.StatusAccepted {
				t.Fatalf("%s: apply %d %s", name, r.code, r.body)
			}
		} else {
			s, d := v.stateNow()
			if s == nil || d == nil || s.State != "" || commitOf(s.Running) != commitOld {
				t.Fatalf("%s: the version shown, no offer: %+v %+v", name, s, d)
			}
			if r := v.apply(`{"digest":"` + digestNew + `"}`); r.code != http.StatusConflict || !strings.Contains(r.body, "the catalog offers this project no update") {
				t.Fatalf("%s: apply %d %s", name, r.code, r.body)
			}
		}
	}
}

// B: a rollback to a release that came from a deploy of an image holds the
// project on that image, which is not the entry's code.
func TestARollbackToAnImageDeployedByHandIsNoOffer(t *testing.T) {
	v := newVersionedCatalog(t)
	app := v.installedAndRunning()
	v.publishNext(commitNew, fluxyrNext())
	for n, image := range []string{"registry.example.test/app:1", "registry.example.test/app:2"} {
		if r := v.post("/api/projects/fluxyr/deploy", `{"image":"`+image+`"}`); r.code != http.StatusAccepted {
			t.Fatalf("deploy: %d %s", r.code, r.body)
		}
		v.setApp(app, func(a *shpyrdv1.App) { ranDeploy(a, n+2, image) })
	}
	v.wantEmpty()
	if r := v.post("/api/projects/fluxyr/rollback", `{"release":2,"force":true}`); r.code != http.StatusOK {
		t.Fatalf("rollback: %d %s", r.code, r.body)
	}
	// While the request is pending, and once the controller has recorded it
	// (release 4, which ran no version of the entry, carries none).
	v.wantEmpty()
	v.setApp(app, func(a *shpyrdv1.App) {
		delete(a.Annotations, shpyrdv1.AnnotationReleaseNote)
		delete(a.Annotations, shpyrdv1.AnnotationRollbackTo)
		ranRollback(a, 4, "registry.example.test/app:1", nil, 2)
	})
	v.wantEmpty()
	before := v.snapshot(app)
	if r := v.apply(`{"digest":"` + v.currentDigest() + `","inputs":{"NEW_NEEDED":"y"}}`); r.code != http.StatusConflict {
		t.Fatalf("an update is not applied over an image: %d %s", r.code, r.body)
	}
	if v.snapshot(app) != before {
		t.Fatal("changed")
	}
}

// A: an image deployed while a rollback request is still pending is not the
// rollback's, before and after the controller records it.
func TestAnImageDeployedWhileARollbackIsPendingIsNoOffer(t *testing.T) {
	for name, rollbackTo := range map[string]int{"the controller records no rollback": 0, "an older controller records the pending one": 1} {
		v := newVersionedCatalog(t)
		app := v.installedAndRunning()
		v.publishNext(commitNew, fluxyrNext())
		v.setApp(app, func(a *shpyrdv1.App) { ranDeploy(a, 2, "r/b@sha256:2") })
		if r := v.post("/api/projects/fluxyr/rollback", `{"release":1,"force":true}`); r.code != http.StatusOK {
			t.Fatalf("%s: rollback: %d %s", name, r.code, r.body)
		}
		v.want(CatalogUpdateAvailable, commitOld, commitNew)
		if r := v.post("/api/projects/fluxyr/deploy", `{"image":"registry.example.test/app:7"}`); r.code != http.StatusAccepted {
			t.Fatalf("%s: deploy: %d %s", name, r.code, r.body)
		}
		v.wantEmpty()
		v.setApp(app, func(a *shpyrdv1.App) {
			delete(a.Annotations, shpyrdv1.AnnotationReleaseNote)
			delete(a.Annotations, shpyrdv1.AnnotationRollbackTo)
			a.Status.Releases = append(a.Status.Releases, shpyrdv1.Release{Number: 3, Image: "registry.example.test/app:7", Source: "x", RollbackTo: rollbackTo})
		})
		v.wantEmpty()
		if r := v.apply(`{"digest":"` + v.currentDigest() + `","inputs":{"NEW_NEEDED":"y"}}`); r.code != http.StatusConflict {
			t.Fatalf("%s: apply %d %s", name, r.code, r.body)
		}
	}
}

// A rollback to a release that ran a version of the entry is still offered
// (the C23 cases above); so is one after a hand-deployed image was left.
func TestARollbackToTheEntrysBuildAfterAnImageWasDeployedIsOffered(t *testing.T) {
	v := newVersionedCatalog(t)
	app := v.installedAndRunning()
	v.publishNext(commitNew, fluxyrNext())
	v.post("/api/projects/fluxyr/deploy", `{"image":"registry.example.test/app:1"}`)
	v.setApp(app, func(a *shpyrdv1.App) { ranDeploy(a, 2, "registry.example.test/app:1") })
	v.wantEmpty()
	if r := v.post("/api/projects/fluxyr/rollback", `{"release":1,"force":true}`); r.code != http.StatusOK {
		t.Fatalf("rollback: %d %s", r.code, r.body)
	}
	v.want(CatalogUpdateAvailable, commitOld, commitNew)
	v.setApp(app, func(a *shpyrdv1.App) {
		delete(a.Annotations, shpyrdv1.AnnotationReleaseNote)
		delete(a.Annotations, shpyrdv1.AnnotationRollbackTo)
		ranRollback(a, 3, "r/a@sha256:1", a.Status.Releases[0].Catalog, 1)
	})
	v.want(CatalogUpdateAvailable, commitOld, commitNew)
}
