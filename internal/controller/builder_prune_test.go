package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// fakeRegistry is a registry over TLS. It keeps tags per repository and
// records every request. DELETE of a tag removes the tag only and answers
// 202, an unknown tag 404; DELETE of a digest removes the manifest and its
// tags.
type fakeRegistry struct {
	mu    sync.Mutex
	srv   *httptest.Server
	tags  map[string]map[string]string // repo -> tag -> digest
	extra map[string]map[string]bool   // repo -> digests with no tag
	log   []string                     // "METHOD /path"
	gone  []string                     // "repo tag digest-at-delete" of removed tags
	// fail answers a status for a request; 0 serves it.
	fail func(method, path string) int
	// hang makes a request wait until the test ends.
	hang func(method, path string) bool
	done chan struct{}
}

func newFakeRegistry(t *testing.T) *fakeRegistry {
	t.Helper()
	f := &fakeRegistry{tags: map[string]map[string]string{}, extra: map[string]map[string]bool{}, done: make(chan struct{})}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(func() { close(f.done); f.srv.Close() })
	return f
}

func (f *fakeRegistry) host() string { return strings.TrimPrefix(f.srv.URL, "https://") }

func (f *fakeRegistry) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.log = append(f.log, r.Method+" "+r.URL.Path)
	fail, hang := f.fail, f.hang
	f.mu.Unlock()
	if hang != nil && hang(r.Method, r.URL.Path) {
		<-f.done
		return
	}
	if fail != nil {
		if code := fail(r.Method, r.URL.Path); code != 0 {
			w.WriteHeader(code)
			return
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	p := strings.TrimPrefix(r.URL.Path, "/v2/")
	if repo, ok := strings.CutSuffix(p, "/tags/list"); ok {
		var out []string
		for t := range f.tags[repo] {
			out = append(out, t)
		}
		sort.Strings(out)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": repo, "tags": out})
		return
	}
	i := strings.LastIndex(p, "/manifests/")
	if i < 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	repo, ref := p[:i], p[i+len("/manifests/"):]
	byDigest := strings.HasPrefix(ref, "sha256:")
	digest := f.tags[repo][ref]
	if byDigest {
		digest = ""
		if f.extra[repo][ref] {
			digest = ref
		}
		for _, d := range f.tags[repo] {
			if d == ref {
				digest = ref
			}
		}
	}
	switch r.Method {
	case http.MethodHead, http.MethodGet:
		if digest == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Docker-Content-Digest", digest)
	case http.MethodDelete:
		if digest == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if byDigest {
			for t, d := range f.tags[repo] {
				if d == ref {
					delete(f.tags[repo], t)
				}
			}
			delete(f.extra[repo], ref)
		} else {
			f.gone = append(f.gone, repo+" "+ref+" "+digest)
			delete(f.tags[repo], ref)
		}
		w.WriteHeader(http.StatusAccepted)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (f *fakeRegistry) set(repo, tag, digest string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tags[repo] == nil {
		f.tags[repo] = map[string]string{}
	}
	f.tags[repo][tag] = digest
}

func (f *fakeRegistry) has(repo, tag string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.tags[repo][tag]
	return ok
}

func (f *fakeRegistry) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.log...)
}

// count says how many recorded requests start with prefix.
func (f *fakeRegistry) count(prefix string) int {
	n := 0
	for _, l := range f.requests() {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

func (f *fakeRegistry) listings() int { return f.countSuffix("/tags/list") }

func (f *fakeRegistry) countSuffix(s string) int {
	n := 0
	for _, l := range f.requests() {
		if strings.HasSuffix(l, s) {
			n++
		}
	}
	return n
}

func (f *fakeRegistry) deletes() []string {
	var out []string
	for _, l := range f.requests() {
		if strings.HasPrefix(l, "DELETE ") {
			out = append(out, l)
		}
	}
	return out
}

func dg(s string) string {
	h := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(h[:])
}

// pruneEnv is a project that composes its build, with its builder repository
// in a fake registry and a clock the test moves.
type pruneEnv struct {
	t     *testing.T
	r     *AppReconciler
	c     client.Client
	reg   *fakeRegistry
	app   *shpyrdv1.App
	clock time.Time
	repo  string
	named string // the builder's named tag
}

var pruneBase = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

func newPruneEnv(t *testing.T, composes bool, objs ...client.Object) *pruneEnv {
	t.Helper()
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "app-web", Generation: 1},
		Spec: shpyrdv1.AppSpec{
			Image:     "ghcr.io/example/app@" + dg("pinned"),
			Processes: map[string]shpyrdv1.Process{"web": {}},
		},
	}
	if composes {
		app.Spec.Build = &shpyrdv1.Build{Buildpacks: []string{"go"}}
	}
	r, c := newTestReconciler(t, append([]client.Object{app}, objs...)...)
	reg := newFakeRegistry(t)
	e := &pruneEnv{t: t, r: r, c: c, reg: reg, app: app, clock: pruneBase}
	r.Config.RegistryHost = reg.host()
	r.Config.RegistryDeletes = true
	r.Config.SystemNamespace = "shpyrd-system"
	r.RegistryHTTP = reg.srv.Client()
	r.Now = func() time.Time { return e.clock }
	e.repo = "apps/default/web/builder"
	e.named = "builder-app-web-web-builder"
	return e
}

// ts is the time tag of the moment d after the base time.
func ts(d time.Duration) string { return pruneBase.Add(d).Format("20060102150405") }

func (e *pruneEnv) image(digest string) string { return e.reg.host() + "/" + e.repo + "@" + digest }

// builder makes the Builder with the status kpack writes, and the named tags
// the registry holds for it.
func (e *pruneEnv) builder(current, run string, ready bool) {
	e.t.Helper()
	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(BuilderGVK)
	b.SetName(builderName(e.app))
	b.SetNamespace(e.app.Namespace)
	status := "True"
	if !ready {
		status = "False"
	}
	b.Object["status"] = map[string]interface{}{
		"conditions":  []interface{}{map[string]interface{}{"type": "Ready", "status": status}},
		"latestImage": e.image(current),
		"stack":       map[string]interface{}{"runImage": e.image(run)},
	}
	ctx := context.Background()
	old := &unstructured.Unstructured{}
	old.SetGroupVersionKind(BuilderGVK)
	if err := e.c.Get(ctx, client.ObjectKeyFromObject(b), old); err == nil {
		b.SetResourceVersion(old.GetResourceVersion())
		if err := e.c.Update(ctx, b); err != nil {
			e.t.Fatal(err)
		}
	} else if err := e.c.Create(ctx, b); err != nil {
		e.t.Fatal(err)
	}
	e.reg.set(e.repo, e.named, current)
	e.reg.set(e.repo, e.named+"-run-image", run)
}

// build makes a kpack Build with the given images and Succeeded status
// ("" for none).
func (e *pruneEnv) build(name, builderDigest, runDigest, succeeded string) {
	e.t.Helper()
	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(KpackBuildGVK)
	b.SetName(name)
	b.SetNamespace(e.app.Namespace)
	b.Object["spec"] = map[string]interface{}{
		"builder":  map[string]interface{}{"image": e.image(builderDigest)},
		"runImage": map[string]interface{}{"image": e.image(runDigest)},
	}
	if succeeded != "" {
		b.Object["status"] = map[string]interface{}{"conditions": []interface{}{map[string]interface{}{"type": "Succeeded", "status": succeeded}}}
	}
	ctx := context.Background()
	old := &unstructured.Unstructured{}
	old.SetGroupVersionKind(KpackBuildGVK)
	if err := e.c.Get(ctx, client.ObjectKeyFromObject(b), old); err == nil {
		b.SetResourceVersion(old.GetResourceVersion())
		if err := e.c.Update(ctx, b); err != nil {
			e.t.Fatal(err)
		}
	} else if err := e.c.Create(ctx, b); err != nil {
		e.t.Fatal(err)
	}
}

func (e *pruneEnv) reconcile() ctrl.Result {
	e.t.Helper()
	res, err := e.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: e.app.Namespace, Name: e.app.Name}})
	if err != nil {
		e.t.Fatalf("reconcile: %v", err)
	}
	return res
}

// passes reconciles, a minute apart, until a reconcile sends the registry
// nothing; it returns the number of reconciles.
func (e *pruneEnv) passes() int {
	e.t.Helper()
	for i := 1; i <= 30; i++ {
		before := len(e.reg.requests())
		e.reconcile()
		e.clock = e.clock.Add(time.Minute)
		if len(e.reg.requests()) == before {
			return i
		}
	}
	e.t.Fatal("the passes did not end")
	return 0
}

// retries reconciles n times, a minute apart.
func (e *pruneEnv) retries(n int) {
	for i := 0; i < n; i++ {
		e.reconcile()
		e.clock = e.clock.Add(time.Minute)
	}
}

// later moves the clock past the hour a finished repository is left alone.
func (e *pruneEnv) later() { e.clock = e.clock.Add(builderPruneIdle + time.Minute) }

func (e *pruneEnv) remaining() []string {
	e.reg.mu.Lock()
	defer e.reg.mu.Unlock()
	var out []string
	for t := range e.reg.tags[e.repo] {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func (e *pruneEnv) wantTags(want ...string) {
	e.t.Helper()
	sort.Strings(want)
	if got := e.remaining(); !reflect.DeepEqual(got, want) {
		e.t.Errorf("tags = %v, want %v", got, want)
	}
}

func (e *pruneEnv) noManifestDeletes() {
	e.t.Helper()
	for _, l := range e.reg.deletes() {
		if i := strings.LastIndex(l, "/manifests/"); i < 0 || strings.HasPrefix(l[i+len("/manifests/"):], "sha256:") {
			e.t.Errorf("a manifest was deleted: %s", l)
		}
	}
}

// oldTag adds a time tag a week old (n distinguishes them) for digest.
func (e *pruneEnv) oldTag(n int, digest string) string {
	tag := ts(-7*24*time.Hour + time.Duration(n)*time.Second)
	e.reg.set(e.repo, tag, digest)
	return tag
}

func TestPruneKeepsWhatIsInUse(t *testing.T) { // C2
	e := newPruneEnv(t, true)
	cur, run := dg("builder"), dg("run")
	e.builder(cur, run, true)
	var kept []string
	n := 0
	for i := 0; i < 3; i++ {
		n++
		kept = append(kept, e.oldTag(n, cur))
	}
	for i := 0; i < 2; i++ {
		n++
		kept = append(kept, e.oldTag(n, run))
	}
	for i := 0; i < 5; i++ {
		n++
		e.oldTag(n, dg(fmt.Sprint("other", i)))
	}
	e.passes()
	e.wantTags(append(kept, e.named, e.named+"-run-image")...)
	if got := len(e.reg.deletes()); got != 5 {
		t.Errorf("deletes = %d, want 5", got)
	}
	e.noManifestDeletes()
}

func TestPruneKeepsTheImagesOfBuildsNotFinished(t *testing.T) { // C3
	for _, which := range []string{"builder", "run"} {
		for _, state := range []string{"", "Unknown", "True", "False"} {
			t.Run(which+"/"+state, func(t *testing.T) {
				e := newPruneEnv(t, true)
				e.builder(dg("cur"), dg("curRun"), true)
				used := dg("used")
				tag := e.oldTag(1, used)
				b, r := dg("cur"), dg("curRun")
				if which == "builder" {
					b = used
				} else {
					r = used
				}
				succeeded := state
				if state == "Unknown" {
					succeeded = "Unknown"
				}
				e.build("web-build-1", b, r, succeeded)
				finished := state == "True" || state == "False"
				e.passes()
				if got := !e.reg.has(e.repo, tag); got != finished {
					t.Fatalf("tag removed = %v with the build %q, want %v", got, state, finished)
				}
				if !finished {
					// The build finishes: a pass after it removes the tag.
					e.build("web-build-1", b, r, "True")
					e.later()
					e.passes()
					if e.reg.has(e.repo, tag) {
						t.Error("the tag stayed after the build finished")
					}
				}
			})
		}
	}
}

func TestPruneIsBoundedAndThrottled(t *testing.T) { // C4
	e := newPruneEnv(t, true)
	e.builder(dg("cur"), dg("curRun"), true)
	for i := 0; i < 120; i++ {
		e.oldTag(i, dg(fmt.Sprint("o", i)))
	}
	res := e.reconcile()
	if got := len(e.reg.deletes()); got != 50 {
		t.Fatalf("first reconcile deleted %d tags, want 50", got)
	}
	if res.RequeueAfter <= 0 || res.RequeueAfter > time.Minute {
		t.Fatalf("requeue = %v, want within a minute", res.RequeueAfter)
	}
	// A reconcile before the minute is up sends nothing.
	before := len(e.reg.requests())
	e.clock = e.clock.Add(10 * time.Second)
	if res := e.reconcile(); res.RequeueAfter <= 0 || res.RequeueAfter > time.Minute {
		t.Errorf("requeue in the wait = %v", res.RequeueAfter)
	}
	if len(e.reg.requests()) != before {
		t.Error("a request was sent before the retry was due")
	}
	e.clock = e.clock.Add(time.Minute)
	e.reconcile()
	if got := len(e.reg.deletes()); got != 100 {
		t.Fatalf("second pass: %d deleted, want 100", got)
	}
	e.clock = e.clock.Add(time.Minute)
	e.reconcile()
	if got := len(e.reg.deletes()); got != 120 {
		t.Fatalf("third pass: %d deleted, want 120", got)
	}
	// Nothing remains: no listing within the hour.
	lists := e.reg.listings()
	// A tag that passes the margin after the last listing.
	late := ts(-23 * time.Hour)
	e.reg.set(e.repo, late, dg("late"))
	for _, d := range []time.Duration{time.Minute, 30 * time.Minute, 58 * time.Minute} {
		e.clock = pruneBase.Add(3*time.Minute + d)
		e.reconcile()
	}
	if e.reg.listings() != lists {
		t.Error("a tag listing was sent within the hour")
	}
	e.clock = pruneBase.Add(3*time.Minute + 61*time.Minute)
	e.reconcile()
	if e.reg.listings() != lists+1 || e.reg.has(e.repo, late) {
		t.Errorf("after the hour: listings %d (was %d), late tag present %v", e.reg.listings(), lists, e.reg.has(e.repo, late))
	}
}

func TestPruneSendsNoDelete(t *testing.T) { // C5
	cur, run := dg("cur"), dg("curRun")
	cases := []struct {
		name  string
		setup func(e *pruneEnv)
	}{
		{"builder not ready", func(e *pruneEnv) { e.builder(cur, run, false) }},
		{"no current image", func(e *pruneEnv) {
			e.builder(cur, run, true)
			b := &unstructured.Unstructured{}
			b.SetGroupVersionKind(BuilderGVK)
			_ = e.c.Get(context.Background(), types.NamespacedName{Namespace: "app-web", Name: "web-builder"}, b)
			unstructured.RemoveNestedField(b.Object, "status", "latestImage")
			if err := e.c.Update(context.Background(), b); err != nil {
				t.Fatal(err)
			}
		}},
		{"no run image digest", func(e *pruneEnv) {
			e.builder(cur, run, true)
			b := &unstructured.Unstructured{}
			b.SetGroupVersionKind(BuilderGVK)
			_ = e.c.Get(context.Background(), types.NamespacedName{Namespace: "app-web", Name: "web-builder"}, b)
			_ = unstructured.SetNestedField(b.Object, e.reg.host()+"/"+e.repo, "status", "stack", "runImage")
			if err := e.c.Update(context.Background(), b); err != nil {
				t.Fatal(err)
			}
		}},
		{"image in another repository", func(e *pruneEnv) {
			e.builder(cur, run, true)
			b := &unstructured.Unstructured{}
			b.SetGroupVersionKind(BuilderGVK)
			_ = e.c.Get(context.Background(), types.NamespacedName{Namespace: "app-web", Name: "web-builder"}, b)
			_ = unstructured.SetNestedField(b.Object, e.reg.host()+"/apps/default/other/builder@"+cur, "status", "latestImage")
			if err := e.c.Update(context.Background(), b); err != nil {
				t.Fatal(err)
			}
		}},
		{"image in another host", func(e *pruneEnv) {
			e.builder(cur, run, true)
			b := &unstructured.Unstructured{}
			b.SetGroupVersionKind(BuilderGVK)
			_ = e.c.Get(context.Background(), types.NamespacedName{Namespace: "app-web", Name: "web-builder"}, b)
			_ = unstructured.SetNestedField(b.Object, "elsewhere.example/"+e.repo+"@"+cur, "status", "latestImage")
			if err := e.c.Update(context.Background(), b); err != nil {
				t.Fatal(err)
			}
		}},
		{"builder named tag differs", func(e *pruneEnv) {
			e.builder(cur, run, true)
			e.reg.set(e.repo, e.named, dg("another"))
		}},
		{"run image named tag differs", func(e *pruneEnv) {
			e.builder(cur, run, true)
			e.reg.set(e.repo, e.named+"-run-image", dg("another"))
		}},
		{"named tag missing", func(e *pruneEnv) {
			e.builder(cur, run, true)
			e.reg.mu.Lock()
			delete(e.reg.tags[e.repo], e.named+"-run-image")
			e.reg.mu.Unlock()
		}},
		{"registry in maintenance", func(e *pruneEnv) {
			e.builder(cur, run, true)
			dep := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: registryDeploymentName, Namespace: "shpyrd-system", Annotations: map[string]string{shpyrdv1.AnnotationRegistryReadOnlySince: e.clock.Format(time.RFC3339)}},
				Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Volumes: []corev1.Volume{
					{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: registryReadOnlyConfig}}}},
				}}}},
			}
			if err := e.c.Create(context.Background(), dep); err != nil {
				t.Fatal(err)
			}
		}},
		{"deletes not allowed", func(e *pruneEnv) {
			e.builder(cur, run, true)
			e.r.Config.RegistryDeletes = false
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newPruneEnv(t, true)
			e.oldTag(1, dg("other"))
			tc.setup(e)
			e.retries(3)
			if d := e.reg.deletes(); len(d) != 0 {
				t.Errorf("deletes sent: %v", d)
			}
			if tc.name == "deletes not allowed" && len(e.reg.requests()) != 0 {
				t.Errorf("requests sent: %v", e.reg.requests())
			}
		})
	}

	t.Run("app being deleted", func(t *testing.T) {
		e := newPruneEnv(t, true)
		e.builder(cur, run, true)
		e.oldTag(1, dg("other"))
		now := metav1.NewTime(e.clock)
		gone := e.app.DeepCopy()
		gone.DeletionTimestamp = &now
		if d := e.r.pruneBuilderTags(context.Background(), gone); d != 0 {
			t.Errorf("requeue = %v", d)
		}
		if len(e.reg.requests()) != 0 {
			t.Errorf("requests sent: %v", e.reg.requests())
		}
	})
}

func TestPruneFailureLeavesTheProjectAlone(t *testing.T) { // C6
	for _, failing := range []string{"list", "resolve", "delete", "timeout"} {
		t.Run(failing, func(t *testing.T) {
			e := newPruneEnv(t, true)
			e.builder(dg("cur"), dg("curRun"), true)
			tag := e.oldTag(1, dg("other"))
			// What the project looks like with a healthy registry.
			healthy := e.reconcileStatus()

			e2 := newPruneEnv(t, true)
			e2.builder(dg("cur"), dg("curRun"), true)
			tag = e2.oldTag(1, dg("other"))
			var broken atomic.Bool
			e2.reg.mu.Lock()
			broken.Store(true)
			e2.reg.fail = func(method, path string) int {
				switch {
				case !broken.Load():
					return 0
				case failing == "list" && strings.HasSuffix(path, "/tags/list"):
					return 503
				case failing == "resolve" && method == http.MethodHead && strings.HasSuffix(path, "/"+tag):
					return 503
				case failing == "delete" && method == http.MethodDelete:
					return 503
				}
				return 0
			}
			if failing == "timeout" {
				e2.reg.hang = func(method, path string) bool { return broken.Load() && strings.HasSuffix(path, "/tags/list") }
				old := builderPruneBudget
				builderPruneBudget = 100 * time.Millisecond
				t.Cleanup(func() { builderPruneBudget = old })
			}
			e2.reg.mu.Unlock()

			got := e2.reconcileStatus()
			if !reflect.DeepEqual(got, healthy) {
				t.Errorf("status changed by a failing registry:\n got %+v\nwant %+v", got, healthy)
			}
			if !e2.reg.has(e2.repo, tag) {
				t.Fatal("the tag went while the registry failed")
			}
			// The registry answers again: a later pass removes the tag.
			e2.reg.mu.Lock()
			broken.Store(false)
			e2.reg.hang = nil
			e2.reg.mu.Unlock()
			e2.clock = e2.clock.Add(time.Minute)
			e2.reconcile()
			if e2.reg.has(e2.repo, tag) {
				t.Error("the tag stayed after the registry recovered")
			}
		})
	}
}

// reconcileStatus reconciles and returns the project's phase, message and
// conditions (without times).
func (e *pruneEnv) reconcileStatus() map[string]string {
	e.t.Helper()
	e.reconcile()
	app := &shpyrdv1.App{}
	if err := e.c.Get(context.Background(), client.ObjectKeyFromObject(e.app), app); err != nil {
		e.t.Fatal(err)
	}
	out := map[string]string{"phase": string(app.Status.Phase), "message": app.Status.Message}
	for _, c := range app.Status.Conditions {
		out[c.Type] = string(c.Status) + "/" + c.Reason + "/" + c.Message
	}
	return out
}

func TestPruneNeverTouchesOtherRepositories(t *testing.T) { // C7
	e := newPruneEnv(t, true)
	cur, run := dg("cur"), dg("curRun")
	e.builder(cur, run, true)
	for i := 0; i < 8; i++ {
		e.oldTag(i, dg(fmt.Sprint("o", i)))
	}
	e.oldTag(20, cur)
	e.oldTag(21, run)
	var releases []string
	for i := 1; i <= 19; i++ {
		d := dg(fmt.Sprint("release", i))
		releases = append(releases, d)
		e.reg.set("apps/default/web", fmt.Sprintf("b%d", i), d)
		e.app.Status.Releases = append(e.app.Status.Releases, shpyrdv1.Release{Number: i, Image: e.reg.host() + "/apps/default/web@" + d})
	}
	if err := e.c.Get(context.Background(), client.ObjectKeyFromObject(e.app), e.app); err != nil {
		t.Fatal(err)
	}
	e.app.Status.Releases = nil
	for i, d := range releases {
		e.app.Status.Releases = append(e.app.Status.Releases, shpyrdv1.Release{Number: i + 1, Image: e.reg.host() + "/apps/default/web@" + d})
	}
	if err := e.c.Status().Update(context.Background(), e.app); err != nil {
		t.Fatal(err)
	}
	e.reg.mu.Lock()
	e.reg.log = nil
	e.reg.mu.Unlock()
	e.passes()
	for _, l := range e.reg.requests() {
		method, path, _ := strings.Cut(l, " ")
		if method == http.MethodGet || method == http.MethodHead {
			continue
		}
		if method != http.MethodDelete || !strings.HasPrefix(path, "/v2/"+e.repo+"/manifests/") {
			t.Errorf("a write outside the builder repository: %s", l)
		}
	}
	e.noManifestDeletes()
	if !e.reg.has(e.repo, e.named+"-run-image") {
		t.Error("the run image's named tag went")
	}
	got := &shpyrdv1.App{}
	if err := e.c.Get(context.Background(), client.ObjectKeyFromObject(e.app), got); err != nil {
		t.Fatal(err)
	}
	if len(got.Status.Releases) != 20 || len(got.Status.StaleImages) != 0 {
		t.Fatalf("history = %d releases, %d stale images; want 20 and none", len(got.Status.Releases), len(got.Status.StaleImages))
	}
	for i, d := range releases {
		resp, err := e.reg.srv.Client().Head(e.reg.srv.URL + "/v2/apps/default/web/manifests/" + d)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Errorf("release %d does not resolve by digest: %v %v", i+1, err, resp)
		}
	}
}

func TestPruneProjectThatNoLongerComposes(t *testing.T) { // C8
	t.Run("builder absent", func(t *testing.T) {
		e := newPruneEnv(t, false)
		e.reg.set(e.repo, e.named, dg("x"))
		e.reg.set(e.repo, e.named+"-run-image", dg("y"))
		keepB, keepR := dg("inflight"), dg("inflightRun")
		kb := e.oldTag(1, keepB)
		kr := e.oldTag(2, keepR)
		for i := 3; i < 8; i++ {
			e.oldTag(i, dg(fmt.Sprint("o", i)))
		}
		e.build("web-build-1", keepB, keepR, "")
		e.passes()
		e.wantTags(kb, kr, e.named, e.named+"-run-image")
	})
	t.Run("builder cannot be read", func(t *testing.T) {
		e := newPruneEnv(t, false)
		e.oldTag(1, dg("o"))
		e.r.APIReader = failingReader{Client: e.c}
		e.passes()
		if d := e.reg.deletes(); len(d) != 0 {
			t.Errorf("deletes: %v", d)
		}
	})
	t.Run("builds cannot be listed", func(t *testing.T) {
		e := newPruneEnv(t, false)
		e.oldTag(1, dg("o"))
		e.r.APIReader = failingReader{Client: e.c, list: true}
		e.reconcile()
		if d := e.reg.deletes(); len(d) != 0 {
			t.Errorf("deletes: %v", d)
		}
	})
	t.Run("builder still there", func(t *testing.T) {
		e := newPruneEnv(t, false)
		e.builder(dg("cur"), dg("curRun"), true)
		e.oldTag(1, dg("o"))
		e.reconcile() // reconcile of a pinned image does not remove the Builder
		if d := e.reg.deletes(); len(d) != 0 {
			t.Errorf("deletes: %v", d)
		}
	})
	t.Run("composing, builder absent", func(t *testing.T) {
		e := newPruneEnv(t, true)
		e.oldTag(1, dg("o"))
		e.passes()
		if d := e.reg.deletes(); len(d) != 0 {
			t.Errorf("deletes: %v", d)
		}
	})
}

// failingReader fails the reads of Builders (and, with list, of Build lists)
// with an error that is not NotFound.
type failingReader struct {
	client.Client
	list bool
}

func (f failingReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if f.list {
		return f.Client.Get(ctx, key, obj, opts...)
	}
	if u, ok := obj.(*unstructured.Unstructured); ok && u.GetKind() == "Builder" {
		return fmt.Errorf("the API is unavailable")
	}
	return f.Client.Get(ctx, key, obj, opts...)
}

func (f failingReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if u, ok := list.(*unstructured.UnstructuredList); ok && f.list && strings.HasPrefix(u.GetKind(), "Build") {
		return fmt.Errorf("the API is unavailable")
	}
	return f.Client.List(ctx, list, opts...)
}

func TestPruneNeverDeletesWhatIsNotATime(t *testing.T) { // C11
	e := newPruneEnv(t, true)
	e.builder(dg("cur"), dg("curRun"), true)
	notTimes := []string{
		"latest", "v1.2.3", "builder-other",
		"2026111012000",   // 13 digits
		"202611101200000", // 15 digits
		"20261345250000",  // 14 digits, not a date
		"20230230120000",  // 30 February
		"20261110126000",  // minute 60
		"2026111012000a",  // not digits
		"٢٠٢٦١١١٠١٢٠٠٠٠",  // other digits
	}
	for i, tag := range notTimes {
		e.reg.set(e.repo, tag, dg(fmt.Sprint("n", i)))
	}
	e.oldTag(1, dg("o")) // one to delete, so that a pass runs
	e.passes()
	for _, tag := range notTimes {
		if !e.reg.has(e.repo, tag) {
			t.Errorf("tag %q was deleted", tag)
		}
	}
	if len(e.reg.deletes()) != 1 {
		t.Errorf("deletes = %v", e.reg.deletes())
	}
}

func TestPruneHonoursTheMargin(t *testing.T) { // C12
	e := newPruneEnv(t, true)
	e.builder(dg("cur"), dg("curRun"), true)
	young := pruneBase.Add(-23 * time.Hour).Format("20060102150405")
	exact := pruneBase.Add(-24 * time.Hour).Format("20060102150405")
	future := pruneBase.Add(48 * time.Hour).Format("20060102150405")
	over := pruneBase.Add(-24*time.Hour - time.Second).Format("20060102150405")
	for _, tag := range []string{young, exact, future, over} {
		e.reg.set(e.repo, tag, dg("o"+tag))
	}
	e.passes()
	if e.reg.has(e.repo, over) || !e.reg.has(e.repo, young) || !e.reg.has(e.repo, exact) || !e.reg.has(e.repo, future) {
		t.Fatalf("tags = %v", e.remaining())
	}
	e.clock = pruneBase.Add(3 * time.Hour)
	e.passes()
	if !e.reg.has(e.repo, future) {
		t.Error("a tag with a time in the future was deleted")
	}
	if e.reg.has(e.repo, young) || e.reg.has(e.repo, exact) {
		t.Errorf("tags older than 24 hours stayed: %v", e.remaining())
	}
	e.clock = pruneBase.Add(73 * time.Hour)
	e.passes()
	if e.reg.has(e.repo, future) {
		t.Error("the tag was not deleted once older than 24 hours")
	}
}

func TestPruneWhenTheBuilderMovesAndBack(t *testing.T) { // C13
	e := newPruneEnv(t, true)
	a, b, run := dg("A"), dg("B"), dg("run")
	// B is current; A's time tags were pruned meanwhile.
	e.builder(b, run, true)
	e.oldTag(1, a)
	e.oldTag(2, b)
	e.passes()
	if e.reg.has(e.repo, ts(-7*24*time.Hour+time.Second)) || !e.reg.has(e.repo, ts(-7*24*time.Hour+2*time.Second)) {
		t.Fatalf("tags = %v", e.remaining())
	}
	deletedBefore := len(e.reg.gone)

	// The Builder moves to A; the registry's named tag still says B.
	e.later()
	e.builder(a, run, true)
	e.reg.set(e.repo, e.named, b)
	e.oldTag(3, b)
	e.retries(3)
	if len(e.reg.gone) != deletedBefore {
		t.Errorf("a pass deleted while the registry and the Builder disagreed: %v", e.reg.gone[deletedBefore:])
	}
	// kpack catches up: named tag A, a new time tag for A.
	e.reg.set(e.repo, e.named, a)
	e.oldTag(4, a)
	e.clock = e.clock.Add(time.Minute)
	e.passes()
	for _, g := range e.reg.gone[deletedBefore:] {
		if f := strings.Fields(g); f[2] == a || strings.HasPrefix(f[1], "builder-") {
			t.Errorf("deleted %s while A was current", g)
		}
	}
	if !e.reg.has(e.repo, e.named) || !e.reg.has(e.repo, e.named+"-run-image") {
		t.Error("a named tag went")
	}
	// And back to B.
	deletedBefore = len(e.reg.gone)
	e.later()
	e.builder(b, run, true)
	e.oldTag(5, b)
	e.passes()
	for _, g := range e.reg.gone[deletedBefore:] {
		if f := strings.Fields(g); f[2] == b || strings.HasPrefix(f[1], "builder-") {
			t.Errorf("deleted %s while B was current", g)
		}
	}
	if !e.reg.has(e.repo, e.named) || !e.reg.has(e.repo, e.named+"-run-image") || !e.reg.has(e.repo, ts(-7*24*time.Hour+5*time.Second)) {
		t.Errorf("tags = %v", e.remaining())
	}
}

func TestPruneGoesOnAfterTheTagsItAlreadyLookedAt(t *testing.T) {
	// More kept tags than one pass can look at must not hide the tags
	// behind them.
	e := newPruneEnv(t, true)
	cur := dg("cur")
	e.builder(cur, dg("curRun"), true)
	for i := 0; i < 30; i++ {
		e.oldTag(i, cur)
	}
	last := e.oldTag(99, dg("other"))
	// Each pass may look at one tag only before its budget is gone.
	e.reg.mu.Lock()
	var seen atomic.Int32
	e.reg.hang = func(method, path string) bool {
		if method == http.MethodHead && strings.Contains(path, "/manifests/2026") {
			return seen.Add(1)%10 == 0
		}
		return false
	}
	e.reg.mu.Unlock()
	old := builderPruneBudget
	builderPruneBudget = 50 * time.Millisecond
	t.Cleanup(func() { builderPruneBudget = old })
	for i := 0; i < 20 && e.reg.has(e.repo, last); i++ {
		e.clock = e.clock.Add(time.Minute)
		e.reconcile()
	}
	if e.reg.has(e.repo, last) {
		t.Error("the tag behind the kept ones was never reached")
	}
}
