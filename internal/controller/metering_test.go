package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ids"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/prom"
	"github.com/shpyrd-io/shpyrd/pkg/sizes"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// fakeProm answers instant queries by matching a substring of the PromQL.
func fakeProm(t *testing.T, answers map[string][]map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		var result []map[string]any
		for needle, series := range answers {
			if strings.Contains(q, needle) {
				result = series
				break
			}
		}
		if result == nil {
			result = []map[string]any{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data":   map[string]any{"resultType": "vector", "result": result},
		})
	}))
}

func sample(labels map[string]string, v string) map[string]any {
	return map[string]any{"metric": labels, "value": []any{1.0, v}}
}

// TestMeteringWritesBucketsBySlug pins the two regressions that made the
// first metering loop a silent no-op: the namespace mapping must come from
// the Kubernetes API (not kube_namespace_labels, which KSM does not export
// by default), and buckets carry the workspace slug that the store resolves.
func TestMeteringWritesBucketsBySlug(t *testing.T) {
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   "app-shop",
		Labels: map[string]string{shpyrdv1.LabelWorkspace: store.DefaultWorkspace, shpyrdv1.LabelProject: "shop"},
	}}
	unrelated := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ns, unrelated).Build()

	srv := fakeProm(t, map[string][]map[string]any{
		"container_cpu_usage_seconds_total": {
			sample(map[string]string{"namespace": "app-shop", "label_shpyrd_io_process": "web"}, "12.5"),
			sample(map[string]string{"namespace": "app-shop", "label_cnpg_io_cluster": "db"}, "3"), // the database's pod
			sample(map[string]string{"namespace": "app-shop", "label_kpack_io_build": "shop-build-1"}, "2"),
			sample(map[string]string{"namespace": "app-shop"}, "1"), // an unlabelled pod
			sample(map[string]string{"namespace": "app-other", "label_shpyrd_io_process": "web"}, "99"),
		},
		"kube_pod_container_resource_requests": {
			sample(map[string]string{"namespace": "app-shop", "label_shpyrd_io_process": "web"}, "0.25"),
		},
		"kube_persistentvolume_capacity_bytes": {
			sample(map[string]string{"namespace": "app-shop", "persistentvolumeclaim": "shop-cache"}, "50"),
			sample(map[string]string{"namespace": "app-shop", "persistentvolumeclaim": "db-1"}, "20"),
			sample(map[string]string{"namespace": "app-shop", "persistentvolumeclaim": "uploads"}, "5"),
		},
		"kube_persistentvolumeclaim_labels": {
			sample(map[string]string{"namespace": "app-shop", "persistentvolumeclaim": "db-1", "label_cnpg_io_cluster": "db"}, "1"),
			sample(map[string]string{"namespace": "app-shop", "persistentvolumeclaim": "uploads", "label_shpyrd_io_volume": "uploads"}, "1"),
		},
		"nginx_ingress_controller_response_size_sum": {
			sample(map[string]string{"exported_namespace": "app-shop"}, "4096"),
		},
	})
	defer srv.Close()

	st := store.NewMemory()
	m := &MeteringLoop{Store: st, Prom: prom.NewClient(srv.URL), Client: c}

	end := time.Now().UTC().Truncate(bucketInterval)
	start := end.Add(-bucketInterval)
	n, err := m.writeBuckets(context.Background(), start, end)
	if err != nil {
		t.Fatal(err)
	}
	// cpu: web, postgres/db, build, other (4); memory: web (1);
	// storage: build-cache, postgres/db, volume/uploads (3); egress (1).
	if n != 9 {
		t.Fatalf("buckets written = %d, want 9", n)
	}

	got, err := st.QueryBuckets(context.Background(), store.DefaultWorkspace, "shop", start.Add(-time.Second), end.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]float64{}
	for _, b := range got {
		if b.Quantity != nil {
			byKey[b.Component+"/"+b.Metric] = *b.Quantity
		}
	}
	if byKey["web/"+store.MetricCPUUsed] != 12.5 {
		t.Errorf("web cpu = %v, want 12.5 (%v)", byKey["web/"+store.MetricCPUUsed], byKey)
	}
	want := map[string]float64{
		"postgres/db/" + store.MetricCPUUsed:    3,
		"build/" + store.MetricCPUUsed:          2,
		"other/" + store.MetricCPUUsed:          1,
		"web/" + store.MetricEgressHTTP:         4096,
		"build-cache/" + store.MetricStorage:    50,
		"postgres/db/" + store.MetricStorage:    20,
		"volume/uploads/" + store.MetricStorage: 5,
	}
	for k, v := range want {
		if byKey[k] != v {
			t.Errorf("%s = %v, want %v (all: %v)", k, byKey[k], v, byKey)
		}
	}
	// Nothing for the unmapped namespace.
	other, _ := st.QueryBuckets(context.Background(), store.DefaultWorkspace, "other", start.Add(-time.Second), end.Add(time.Second))
	if len(other) != 0 {
		t.Errorf("unmapped namespace produced buckets: %+v", other)
	}
}

// TestMeteringStorageZeroFill: a project without any Bound claim gets an
// explicit zero storage bucket, so "no storage" is a number, not a gap.
func TestMeteringStorageZeroFill(t *testing.T) {
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   "app-blog",
		Labels: map[string]string{shpyrdv1.LabelWorkspace: store.DefaultWorkspace, shpyrdv1.LabelProject: "blog"},
	}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ns).Build()
	srv := fakeProm(t, nil)
	defer srv.Close()
	st := store.NewMemory()
	m := &MeteringLoop{Store: st, Prom: prom.NewClient(srv.URL), Client: c}
	end := time.Now().UTC().Truncate(bucketInterval)
	if _, err := m.writeBuckets(context.Background(), end.Add(-bucketInterval), end); err != nil {
		t.Fatal(err)
	}
	got, _ := st.QueryBuckets(context.Background(), store.DefaultWorkspace, "blog", end.Add(-bucketInterval-time.Second), end.Add(time.Second))
	var storage *store.UsageBucket
	for i := range got {
		if got[i].Metric == store.MetricStorage {
			storage = &got[i]
		}
	}
	if storage == nil || storage.Quantity == nil || *storage.Quantity != 0 || storage.Quality != store.QualityComplete {
		t.Fatalf("zero storage bucket: %+v", storage)
	}
}

// TestSleepWorkspaceDefault: a project without its own policy inherits the
// workspace plan's default; an explicit "off" opts out.
func TestSleepWorkspaceDefault(t *testing.T) {
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-acme-shop", Labels: map[string]string{shpyrdv1.LabelWorkspace: "acme"}},
		Spec:       shpyrdv1.AppSpec{Image: "ghcr.io/acme/shop:1", Processes: map[string]shpyrdv1.Process{"web": {Port: ptr.To[int32](8080)}}},
	}
	r, _ := newTestReconciler(t, app)
	r.Config.SleepAllowed = always
	r.Config.WorkspaceSleepDefault = func(slug string) (string, string) {
		if slug == "acme" {
			return "15m", "page"
		}
		return "", ""
	}
	if !r.sleepEnabled(app) {
		t.Fatal("the workspace default should enable sleep")
	}
	if sp := r.webSleepSpec(app); sp == nil || sp.After != "15m" || sp.Resuming != "page" || r.sleepSource(app) != "workspace" {
		t.Errorf("effective spec = %+v source=%s", sp, r.sleepSource(app))
	}
	// Explicit off wins.
	p := app.Spec.Processes["web"]
	p.Sleep = &shpyrdv1.SleepSpec{After: "off"}
	app.Spec.Processes["web"] = p
	if r.sleepEnabled(app) {
		t.Error("explicit off must opt out of the plan default")
	}
	// Another workspace without a plan default: no sleep.
	app.Labels[shpyrdv1.LabelWorkspace] = "other"
	p.Sleep = nil
	app.Spec.Processes["web"] = p
	if r.sleepEnabled(app) {
		t.Error("no default, no policy: no sleep")
	}
}

// An App named by its ID (RFC-0076, every project since v0.9.43) has its
// web Deployment and Service called "web": the sleep objects must point
// at those names, or KEDA scales nothing and the interceptor routes to a
// Service that does not exist. (Found on the first kind cluster to enable
// sleep after RFC-0076: "deployments.apps <id>-web not found".)
func TestSleepObjectsFollowIDNamedWorkloads(t *testing.T) {
	short := ids.Short(testProjectID)
	ns := project.IDNamespace(testProjectID)
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: short, Namespace: ns, Generation: 1},
		Spec: shpyrdv1.AppSpec{
			ID: testProjectID, Slug: "shop", Image: "ghcr.io/acme/shop:1", Access: shpyrdv1.AccessPublic,
			Processes: map[string]shpyrdv1.Process{"web": {
				Port:  ptr.To[int32](8080),
				Sleep: &shpyrdv1.SleepSpec{After: "5m", Resuming: "page"},
			}},
		},
	}
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	mapper := meta.NewDefaultRESTMapper(nil)
	for _, gvk := range []schema.GroupVersionKind{InterceptorRouteGVK, ScaledObjectGVK} {
		scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
		scheme.AddKnownTypeWithName(gvk.GroupVersion().WithKind(gvk.Kind+"List"), &unstructured.UnstructuredList{})
		mapper.Add(gvk, meta.RESTScopeNamespace)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithRESTMapper(mapper).WithObjects(app,
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns, Labels: map[string]string{shpyrdv1.LabelManagedBy: "shpyrd"}}}).
		WithStatusSubresource(&shpyrdv1.App{}).Build()
	r := &AppReconciler{
		ProcessTypes: func(context.Context, string) []string { return nil },
		Client:       c, APIReader: c, Scheme: scheme, Recorder: record.NewFakeRecorder(100),
		Config: Config{Domain: "example.test", HTTPSPort: "8443", RegistryHost: "10.96.0.50:5000", RegistryInsecure: true, SystemNamespace: "shpyrd-system", SleepAllowed: always}.Defaults(),
	}
	runReconcile(t, r, app)

	dep := &appsv1.Deployment{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "web"}, dep); err != nil {
		t.Fatalf("the web Deployment is named web: %v", err)
	}
	so := &unstructured.Unstructured{}
	so.SetGroupVersionKind(ScaledObjectGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: short + "-sleep"}, so); err != nil {
		t.Fatalf("scaledobject: %v", err)
	}
	if target, _, _ := unstructured.NestedString(so.Object, "spec", "scaleTargetRef", "name"); target != "web" {
		t.Errorf("scaleTargetRef.name = %q, want web", target)
	}
	ir := &unstructured.Unstructured{}
	ir.SetGroupVersionKind(InterceptorRouteGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: short}, ir); err != nil {
		t.Fatalf("interceptorroute: %v", err)
	}
	if target, _, _ := unstructured.NestedString(ir.Object, "spec", "target", "service"); target != "web" {
		t.Errorf("interceptor target = %q, want web", target)
	}
	ing := &networkingv1.Ingress{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "app"}, ing); err != nil {
		t.Fatal(err)
	}
	if got := ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name; got != webSleepServiceName {
		t.Errorf("backend = %q, want %s", got, webSleepServiceName)
	}
}

// TestMeteringNoProjects: with no project namespaces the loop writes
// nothing and reports zero (not an error).
func TestMeteringNoProjects(t *testing.T) {
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	srv := fakeProm(t, nil)
	defer srv.Close()
	m := &MeteringLoop{Store: store.NewMemory(), Prom: prom.NewClient(srv.URL), Client: c}
	n, err := m.writeBuckets(context.Background(), time.Now().Add(-bucketInterval), time.Now())
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

// TestSleepNeverBreaksRouting: a sleep policy on a cluster without the KEDA
// HTTP add-on leaves the Ingress on the app's own Service; with the add-on,
// the backend moves to the interceptor and the auth cache shortens.
func TestSleepNeverBreaksRouting(t *testing.T) {
	newApp := func() *shpyrdv1.App {
		return &shpyrdv1.App{
			ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"},
			Spec: shpyrdv1.AppSpec{
				Image:  "ghcr.io/acme/shop:1",
				Access: shpyrdv1.AccessAuthenticated,
				Processes: map[string]shpyrdv1.Process{"web": {
					Port:  ptr.To[int32](8080),
					Sleep: &shpyrdv1.SleepSpec{After: "15m", Resuming: "page"},
				}},
			},
		}
	}
	ingressOf := func(t *testing.T, c client.Client) *networkingv1.Ingress {
		ing := &networkingv1.Ingress{}
		if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop"}, ing); err != nil {
			t.Fatal(err)
		}
		return ing
	}

	t.Run("without keda-http", func(t *testing.T) {
		app := newApp()
		r, c := newTestReconciler(t, app)
		r.Config.SystemNamespace = "shpyrd-system"
		r.Config.SleepAllowed = always
		runReconcile(t, r, app)
		ing := ingressOf(t, c)
		if got := ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name; got != "shop-web" {
			t.Errorf("backend = %q, want the app's own Service", got)
		}
		if got := ing.Annotations["nginx.ingress.kubernetes.io/auth-cache-duration"]; got != "200 20s, 401 5s, 403 5s" {
			t.Errorf("auth cache = %q", got)
		}
		got := &shpyrdv1.App{}
		if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop"}, got); err != nil {
			t.Fatal(err)
		}
		if st := got.Status.Processes["web"]; st.Sleep == nil || st.Sleep.State != "unavailable" || st.Sleep.Message == "" {
			t.Errorf("web sleep status = %+v, want unavailable with a message", st.Sleep)
		}
	})

	t.Run("with keda-http", func(t *testing.T) {
		app := newApp()
		scheme, err := kube.Scheme()
		if err != nil {
			t.Fatal(err)
		}
		// The fake client's default REST mapper is empty; give it one that
		// knows the KEDA kinds, which is what the reconciler asks.
		mapper := meta.NewDefaultRESTMapper(nil)
		for _, gvk := range []schema.GroupVersionKind{InterceptorRouteGVK, ScaledObjectGVK} {
			scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
			scheme.AddKnownTypeWithName(gvk.GroupVersion().WithKind(gvk.Kind+"List"), &unstructured.UnstructuredList{})
			mapper.Add(gvk, meta.RESTScopeNamespace)
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithRESTMapper(mapper).WithObjects(app).
			WithStatusSubresource(&shpyrdv1.App{}).Build()
		r := &AppReconciler{
			ProcessTypes: func(context.Context, string) []string { return nil },
			Client:       c, APIReader: c, Scheme: scheme, Recorder: record.NewFakeRecorder(100),
			Config: Config{Domain: "example.test", HTTPSPort: "8443", RegistryHost: "10.96.0.50:5000", RegistryInsecure: true, SystemNamespace: "shpyrd-system", SleepAllowed: always}.Defaults(),
		}
		runReconcile(t, r, app)
		ing := ingressOf(t, c)
		if got := ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name; got != webSleepServiceName {
			t.Errorf("backend = %q, want %s", got, webSleepServiceName)
		}
		if got := ing.Annotations["nginx.ingress.kubernetes.io/auth-cache-duration"]; got != "200 5s, 401 5s, 403 5s" {
			t.Errorf("auth cache = %q", got)
		}
		ir := &unstructured.Unstructured{}
		ir.SetGroupVersionKind(InterceptorRouteGVK)
		if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop"}, ir); err != nil {
			t.Fatalf("interceptorroute: %v", err)
		}
		target, _, _ := unstructured.NestedString(ir.Object, "spec", "target", "service")
		if target != "shop-web" {
			t.Errorf("interceptor target = %q", target)
		}
		so := &unstructured.Unstructured{}
		so.SetGroupVersionKind(ScaledObjectGVK)
		if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop-sleep"}, so); err != nil {
			t.Fatalf("scaledobject: %v", err)
		}
		if cd, _, _ := unstructured.NestedInt64(so.Object, "spec", "cooldownPeriod"); cd != 900 {
			t.Errorf("cooldown = %d, want 900", cd)
		}
		cm := &corev1.ConfigMap{}
		if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: sleepPageCMName(app)}, cm); err != nil {
			t.Errorf("resuming page configmap: %v", err)
		}

		// KEDA scales the web Deployment to zero: the next reconcile must
		// leave it there (not fight the scaler) and report the process as
		// sleeping rather than failing.
		dep := &appsv1.Deployment{}
		if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop-web"}, dep); err != nil {
			t.Fatal(err)
		}
		dep.Spec.Replicas = ptr.To[int32](0)
		if err := c.Update(context.Background(), dep); err != nil {
			t.Fatal(err)
		}
		got := runReconcile(t, r, app)
		if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop-web"}, dep); err != nil {
			t.Fatal(err)
		}
		if dep.Spec.Replicas == nil || *dep.Spec.Replicas != 0 {
			t.Errorf("reconcile reset replicas to %v; KEDA owns them while sleep is active", dep.Spec.Replicas)
		}
		if st := got.Status.Processes["web"]; st.Sleep == nil || st.Sleep.State != "sleeping" || st.Desired != 0 {
			t.Errorf("web status = %+v, want sleeping with desired 0", st)
		}

		// KEDA never gets the trigger working: past the grace period the
		// controller tears sleep down, gives the instances back, routes to
		// the app's Service again and pauses sleep for this generation.
		so.Object["status"] = map[string]interface{}{"conditions": []interface{}{
			map[string]interface{}{"type": "Ready", "status": "False", "message": "no metric specs returned from scalers"},
		}}
		if err := c.Status().Update(context.Background(), so); err != nil {
			// The fake client has no status subresource for unstructured kinds; a plain update will do.
			if err := c.Update(context.Background(), so); err != nil {
				t.Fatal(err)
			}
		}
		r.Now = func() time.Time { return time.Now().Add(scaledObjectGrace + time.Minute) }
		got = runReconcile(t, r, app)
		if got := ingressOf(t, c).Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name; got != "shop-web" {
			t.Errorf("backend after pause = %q, want the app's own Service", got)
		}
		if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop-web"}, dep); err != nil {
			t.Fatal(err)
		}
		if dep.Spec.Replicas == nil || *dep.Spec.Replicas != 1 {
			t.Errorf("replicas after pause = %v, want 1", dep.Spec.Replicas)
		}
		if st := got.Status.Processes["web"]; st.Sleep == nil || st.Sleep.State != "unavailable" || !strings.Contains(st.Sleep.Message, "paused") {
			t.Errorf("web status after pause = %+v", st.Sleep)
		}
		if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop-sleep"}, so); !apierrors.IsNotFound(err) {
			t.Errorf("scaledobject still there after pause: %v", err)
		}
		// Reconciling again does not recreate it (same generation).
		runReconcile(t, r, app)
		if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop-sleep"}, so); !apierrors.IsNotFound(err) {
			t.Errorf("scaledobject recreated while paused: %v", err)
		}
		r.Now = nil

		// Turning the policy off removes the objects and restores the backend.
		if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop"}, app); err != nil {
			t.Fatal(err)
		}
		p := app.Spec.Processes["web"]
		p.Sleep = nil
		app.Spec.Processes["web"] = p
		if err := c.Update(context.Background(), app); err != nil {
			t.Fatal(err)
		}
		runReconcile(t, r, app)
		if got := ingressOf(t, c).Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name; got != "shop-web" {
			t.Errorf("backend after off = %q", got)
		}
		// Without the policy the controller owns replicas again.
		if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop-web"}, dep); err != nil {
			t.Fatal(err)
		}
		if dep.Spec.Replicas == nil || *dep.Spec.Replicas != 1 {
			t.Errorf("replicas after off = %v, want 1", dep.Spec.Replicas)
		}
		if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop"}, ir); !apierrors.IsNotFound(err) {
			t.Errorf("interceptorroute still there: %v", err)
		}
	})
}

// TestNodePools: with two pools (RFC-0077) app processes select the apps
// pool, kpack builds too, databases select the platform pool; with a single
// pool nothing carries a selector.
func TestNodePools(t *testing.T) {
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"},
		Spec:       shpyrdv1.AppSpec{Image: "ghcr.io/acme/shop:1", Processes: map[string]shpyrdv1.Process{"web": {Port: ptr.To[int32](8080)}}},
	}
	r, c := newTestReconciler(t, app)
	r.Config.AppsPool, r.Config.PlatformPool = "apps", "platform"
	runReconcile(t, r, app)
	dep := &appsv1.Deployment{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop-web"}, dep); err != nil {
		t.Fatal(err)
	}
	if got := dep.Spec.Template.Spec.NodeSelector[PoolLabel]; got != "apps" {
		t.Errorf("web pod nodeSelector = %v", dep.Spec.Template.Spec.NodeSelector)
	}

	// Databases go to the platform pool.
	storage := resource.MustParse("10Gi")
	pg := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "app-shop"}, Spec: shpyrdv1.PostgresSpec{Storage: &storage}}
	cl := desiredCNPGCluster(pg, storage, sizes.Defaults().Postgres.Sizes[0], corev1.ResourceRequirements{}, "", "platform")
	sel, _, _ := unstructured.NestedMap(cl.Object, "spec", "affinity", "nodeSelector")
	if sel[PoolLabel] != "platform" {
		t.Errorf("cnpg nodeSelector = %v", sel)
	}
	// Single pool: no selectors anywhere.
	cl = desiredCNPGCluster(pg, storage, sizes.Defaults().Postgres.Sizes[0], corev1.ResourceRequirements{}, "", "")
	if _, found, _ := unstructured.NestedMap(cl.Object, "spec", "affinity"); found {
		t.Errorf("single pool must not set affinity")
	}
	r.Config.AppsPool = ""
	runReconcile(t, r, app)
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop-web"}, dep)
	if dep.Spec.Template.Spec.NodeSelector != nil {
		t.Errorf("single pool web nodeSelector = %v", dep.Spec.Template.Spec.NodeSelector)
	}
}

// A shared database's size, read from its server's exporter, becomes its
// storage line and a Cap condition on the plan's ladder.
func TestSharedCapsAreMeasuredAgainstTheCap(t *testing.T) {
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	cap := resource.MustParse("1Gi")
	near := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "near", Namespace: "p-shop", UID: "uid-near", Generation: 2}, Spec: shpyrdv1.PostgresSpec{Type: shpyrdv1.PostgresTypeShared, Storage: &cap}, Status: shpyrdv1.ResourceStatus{Phase: shpyrdv1.ResourceReady}}
	over := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "over", Namespace: "p-shop", UID: "uid-over"}, Spec: shpyrdv1.PostgresSpec{Type: shpyrdv1.PostgresTypeShared, Storage: &cap}, Status: shpyrdv1.ResourceStatus{Phase: shpyrdv1.ResourceReady}}
	quiet := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "quiet", Namespace: "p-shop", UID: "uid-quiet"}, Spec: shpyrdv1.PostgresSpec{Type: shpyrdv1.PostgresTypeShared, Storage: &cap}, Status: shpyrdv1.ResourceStatus{Phase: shpyrdv1.ResourceReady}}
	dedicated := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "own", Namespace: "p-shop", UID: "uid-own"}, Status: shpyrdv1.ResourceStatus{Phase: shpyrdv1.ResourceReady}}
	srv := fakeProm(t, map[string][]map[string]any{
		"cnpg_pg_database_size_bytes": {
			sample(map[string]string{"datname": sharedDatabaseName(tenantHash(near.UID))}, fmt.Sprint(900*1024*1024)),
			sample(map[string]string{"datname": sharedDatabaseName(tenantHash(over.UID))}, fmt.Sprint(1700*1024*1024)),
		},
	})
	defer srv.Close()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(near, over, quiet, dedicated).WithStatusSubresource(near, over, quiet, dedicated).Build()
	m := &MeteringLoop{Store: store.NewMemory(), Prom: prom.NewClient(srv.URL), Client: c}
	if err := m.sharedCaps(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	get := func(name string) *shpyrdv1.Postgres {
		pg := &shpyrdv1.Postgres{}
		if err := c.Get(context.Background(), types.NamespacedName{Namespace: "p-shop", Name: name}, pg); err != nil {
			t.Fatal(err)
		}
		return pg
	}
	capOf := func(pg *shpyrdv1.Postgres) *metav1.Condition {
		for i := range pg.Status.Conditions {
			if pg.Status.Conditions[i].Type == ConditionCap {
				return &pg.Status.Conditions[i]
			}
		}
		return nil
	}
	n := get("near")
	if n.Status.Storage != "900Mi of 1Gi" || capOf(n) == nil || capOf(n).Reason != CapNear || !strings.Contains(capOf(n).Message, "88%") {
		t.Errorf("near = %q %+v", n.Status.Storage, capOf(n))
	}
	o := get("over")
	if capOf(o) == nil || capOf(o).Reason != CapFar || capOf(o).Status != metav1.ConditionFalse {
		t.Errorf("over 150%% = %+v", capOf(o))
	}
	if q := get("quiet"); q.Status.Storage != "" || capOf(q) != nil {
		t.Errorf("a tenant not scraped yet keeps its status: %q %+v", q.Status.Storage, capOf(q))
	}
	if d := get("own"); d.Status.Storage != "" || capOf(d) != nil {
		t.Errorf("a dedicated database is not measured here: %q %+v", d.Status.Storage, capOf(d))
	}
}

// A shared database is metered by its cap, from the object: cap GiB times
// the window, one bucket per database; a dedicated one is not.
func TestSharedDatabasesAreMeteredByTheirCap(t *testing.T) {
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "p-shop", Labels: map[string]string{shpyrdv1.LabelWorkspace: store.DefaultWorkspace, shpyrdv1.LabelProject: "shop"}}}
	cap := resource.MustParse("5Gi")
	shared := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "p-shop"}, Spec: shpyrdv1.PostgresSpec{Type: shpyrdv1.PostgresTypeShared, Storage: &cap}}
	dedicated := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "own", Namespace: "p-shop"}, Spec: shpyrdv1.PostgresSpec{Storage: &cap}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ns, shared, dedicated).Build()
	srv := fakeProm(t, nil)
	defer srv.Close()
	st := store.NewMemory()
	m := &MeteringLoop{Store: st, Prom: prom.NewClient(srv.URL), Client: c}
	end := time.Now().UTC().Truncate(bucketInterval)
	if _, err := m.writeBuckets(context.Background(), end.Add(-bucketInterval), end); err != nil {
		t.Fatal(err)
	}
	got, _ := st.QueryBuckets(context.Background(), store.DefaultWorkspace, "shop", end.Add(-bucketInterval-time.Second), end.Add(time.Second))
	var buckets []store.UsageBucket
	for _, b := range got {
		if b.Metric == store.MetricPostgresShared {
			buckets = append(buckets, b)
		}
	}
	if len(buckets) != 1 || buckets[0].Component != "postgres/db" || buckets[0].Quantity == nil || *buckets[0].Quantity != 5*bucketInterval.Seconds() || buckets[0].Unit != store.UnitGiBSeconds || buckets[0].Quality != store.QualityComplete {
		t.Fatalf("shared buckets = %+v", buckets)
	}
}

// A shared server's disk grows by half when its fullest instance passes
// 70 %, once the previous step has landed; below that, or while a step is
// in flight, nothing moves.
func TestSharedServerDisksGrowAtSeventyPercent(t *testing.T) {
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	server := func(name, size string) *unstructured.Unstructured {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(CNPGClusterGVK)
		u.SetNamespace(SharedPostgresNamespace)
		u.SetName(name)
		u.SetLabels(map[string]string{sharedServerLabel: "server"})
		_ = unstructured.SetNestedField(u.Object, size, "spec", "storage", "size")
		return u
	}
	gi := func(n float64) string { return fmt.Sprint(int64(n * (1 << 30))) }
	answers := map[string][]map[string]any{
		"kubelet_volume_stats_used_bytes": {
			sample(map[string]string{"persistentvolumeclaim": "pg-shared-1-1"}, gi(38)),
			sample(map[string]string{"persistentvolumeclaim": "pg-shared-1-2"}, gi(20)),
			sample(map[string]string{"persistentvolumeclaim": "pg-shared-10-1"}, gi(10)),
		},
		"kubelet_volume_stats_capacity_bytes": {
			sample(map[string]string{"persistentvolumeclaim": "pg-shared-1-1"}, gi(50)),
			sample(map[string]string{"persistentvolumeclaim": "pg-shared-1-2"}, gi(50)),
			sample(map[string]string{"persistentvolumeclaim": "pg-shared-10-1"}, gi(50)),
		},
	}
	srv := fakeProm(t, answers)
	defer srv.Close()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(server("pg-shared-1", "50Gi"), server("pg-shared-10", "50Gi")).Build()
	m := &MeteringLoop{Store: store.NewMemory(), Prom: prom.NewClient(srv.URL), Client: c}
	sizeOf := func(name string) string {
		u := getU(t, c, "Cluster", SharedPostgresNamespace, name)
		size, _, _ := unstructured.NestedString(u.Object, "spec", "storage", "size")
		return size
	}
	if err := m.sharedServerDisks(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if sizeOf("pg-shared-1") != "75Gi" || sizeOf("pg-shared-10") != "50Gi" {
		t.Errorf("after 76%% on one instance: %s and %s", sizeOf("pg-shared-1"), sizeOf("pg-shared-10"))
	}
	// The capacity still says 50Gi: the step is in flight, no second step.
	if err := m.sharedServerDisks(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if sizeOf("pg-shared-1") != "75Gi" {
		t.Errorf("a step in flight is waited for: %s", sizeOf("pg-shared-1"))
	}
	// Landed at 75Gi with 38Gi used (51 %): nothing more.
	answers["kubelet_volume_stats_capacity_bytes"][0] = sample(map[string]string{"persistentvolumeclaim": "pg-shared-1-1"}, gi(75))
	answers["kubelet_volume_stats_capacity_bytes"][1] = sample(map[string]string{"persistentvolumeclaim": "pg-shared-1-2"}, gi(75))
	if err := m.sharedServerDisks(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if sizeOf("pg-shared-1") != "75Gi" {
		t.Errorf("under the threshold: %s", sizeOf("pg-shared-1"))
	}
}

func TestHumanBytesReadsLikeAPerson(t *testing.T) {
	for n, want := range map[int64]string{7829171: "7.5Mi", 900 * 1024 * 1024: "900Mi", 1700 * 1024 * 1024: "1.7Gi", 1 << 30: "1Gi", 512: "512", 4 * 1024: "4Ki"} {
		if got := humanBytes(n); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

// A project's buckets are metered by what they hold; the bucket a
// database's backups go to is the platform's and is not.
func TestBucketsAreMeteredByWhatTheyHold(t *testing.T) {
	ctx := context.Background()
	scheme, _ := kube.Scheme()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "p-shop", Labels: project.NamespaceLabels("acme", "shop")}}
	uploads := &shpyrdv1.ObjectBucket{ObjectMeta: metav1.ObjectMeta{Name: "uploads", Namespace: "p-shop", Labels: map[string]string{shpyrdv1.LabelProject: "shop"}}, Status: shpyrdv1.ObjectBucketStatus{UsedBytes: 2 << 30}}
	backups := &shpyrdv1.ObjectBucket{ObjectMeta: metav1.ObjectMeta{Name: "db-backups", Namespace: "p-shop", Labels: map[string]string{"shpyrd.io/postgres": "db"}}, Status: shpyrdv1.ObjectBucketStatus{UsedBytes: 5 << 30}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ns, uploads, backups).Build()
	m := &MeteringLoop{Store: store.NewMemory(), Client: c}
	nsMap, err := m.nsMapping(ctx)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	out := m.bucketBuckets(ctx, start, start.Add(5*time.Minute), nsMap)
	if len(out) != 1 {
		t.Fatalf("buckets = %+v, want the project's own only", out)
	}
	if b := out[0]; b.Component != "bucket/uploads" || b.Metric != store.MetricObjectStorage || b.Unit != store.UnitGiBSeconds || *b.Quantity != 2*300 {
		t.Errorf("bucket = %+v", b)
	}
}

// Every Ready dedicated database is stamped with what its data weighs,
// from the exporter's size by pod, so a conversion can be judged against
// the cap before it starts.
func TestDedicatedDatabasesAreStampedWithTheirSize(t *testing.T) {
	ctx := context.Background()
	scheme, _ := kube.Scheme()
	db := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "p-shop"}, Spec: shpyrdv1.PostgresSpec{Type: shpyrdv1.PostgresTypeDedicated}, Status: shpyrdv1.ResourceStatus{Phase: shpyrdv1.ResourceReady}}
	dbtwo := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "db-two", Namespace: "p-shop"}, Spec: shpyrdv1.PostgresSpec{Type: shpyrdv1.PostgresTypeDedicated}, Status: shpyrdv1.ResourceStatus{Phase: shpyrdv1.ResourceReady}}
	shared := &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: "tenant", Namespace: "p-shop"}, Spec: shpyrdv1.PostgresSpec{Type: shpyrdv1.PostgresTypeShared}, Status: shpyrdv1.ResourceStatus{Phase: shpyrdv1.ResourceReady}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(db, dbtwo, shared).Build()
	srv := fakeProm(t, map[string][]map[string]any{
		"cnpg_pg_database_size_bytes": {
			sample(map[string]string{"namespace": "p-shop", "pod": "db-1"}, "3221225472"),
			sample(map[string]string{"namespace": "p-shop", "pod": "db-2"}, "3221225400"),
			sample(map[string]string{"namespace": "p-shop", "pod": "db-two-1"}, "1024"),
		},
	})
	defer srv.Close()
	m := &MeteringLoop{Store: store.NewMemory(), Prom: prom.NewClient(srv.URL), Client: c}
	if err := m.dedicatedSizes(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	got := &shpyrdv1.Postgres{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "p-shop", Name: "db"}, got); err != nil {
		t.Fatal(err)
	}
	if DataBytes(got) != 3221225472 {
		t.Errorf("db weighs %d, want the primary's 3221225472 (the pods' largest)", DataBytes(got))
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "p-shop", Name: "db-two"}, got); err != nil {
		t.Fatal(err)
	}
	if DataBytes(got) != 1024 {
		t.Errorf("db-two weighs %d, want 1024 (its own pod, not db's prefix)", DataBytes(got))
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "p-shop", Name: "tenant"}, got); err != nil {
		t.Fatal(err)
	}
	if _, stamped := got.Annotations[AnnotationDataBytes]; stamped {
		t.Error("a shared database was stamped; its cap is measured elsewhere")
	}
}

// TestActivitySignalFollowsTheWorkspaceDefault: the loop keeps the activity
// check for a database sleeping by the workspace's default, not only for one
// with a policy of its own (storage plan step 6); before, such a database
// waited for ever for a check nobody wrote. An explicit off, a shared
// database and a high-availability one are left alone, and without auto
// sleep the default does not apply.
func TestActivitySignalFollowsTheWorkspaceDefault(t *testing.T) {
	ctx := context.Background()
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	two := int32(2)
	fresh := func() client.Client {
		mk := func(name string, spec shpyrdv1.PostgresSpec) *shpyrdv1.Postgres {
			return &shpyrdv1.Postgres{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "app-shop"}, Spec: spec}
		}
		return fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			mk("own", shpyrdv1.PostgresSpec{Sleep: &shpyrdv1.PostgresSleepSpec{After: "30m"}}),
			mk("bydefault", shpyrdv1.PostgresSpec{}),
			mk("off", shpyrdv1.PostgresSpec{Sleep: &shpyrdv1.PostgresSleepSpec{After: "off"}}),
			mk("shared", shpyrdv1.PostgresSpec{Type: shpyrdv1.PostgresTypeShared}),
			mk("ha", shpyrdv1.PostgresSpec{Instances: &two}),
		).WithStatusSubresource(&shpyrdv1.Postgres{}).Build()
	}
	var scraped []map[string]any
	for _, n := range []string{"own", "bydefault", "off", "shared", "ha"} {
		scraped = append(scraped, sample(map[string]string{"job": "app-shop/" + n}, "1"))
	}
	srv := fakeProm(t, map[string][]map[string]any{
		"cnpg_backends_total[5m]":     scraped, // every database is scraped
		"cnpg_backends_total{usename": {},      // nobody is connected
	})
	defer srv.Close()
	checked := func(c client.Client, name string) bool {
		t.Helper()
		pg := &shpyrdv1.Postgres{}
		if err := c.Get(ctx, types.NamespacedName{Namespace: "app-shop", Name: name}, pg); err != nil {
			t.Fatal(err)
		}
		return pg.Status.Sleep != nil && pg.Status.Sleep.ActivityCheckedAt != nil
	}
	run := func(name string, m *MeteringLoop, c client.Client, want map[string]bool) {
		t.Helper()
		m.Client = c
		if err := m.postgresActivity(ctx, time.Now()); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for db, w := range want {
			if got := checked(c, db); got != w {
				t.Errorf("%s: %s checked = %v, want %v", name, db, got, w)
			}
		}
	}
	none := func(context.Context, string) string { return "" }
	hour := func(context.Context, string) string { return "1h" }
	base := func() *MeteringLoop { return &MeteringLoop{Store: store.NewMemory(), Prom: prom.NewClient(srv.URL)} }

	m := base()
	m.SleepAllowed, m.WorkspaceSleepDefault = always, none
	run("no default", m, fresh(), map[string]bool{"own": true, "bydefault": false, "off": false, "shared": false, "ha": false})

	m = base()
	m.SleepAllowed, m.WorkspaceSleepDefault = always, hour
	run("workspace default", m, fresh(), map[string]bool{"own": true, "bydefault": true, "off": false, "shared": false, "ha": false})

	m = base()
	m.SleepAllowed, m.WorkspaceSleepDefault = func() bool { return false }, hour
	run("auto sleep off", m, fresh(), map[string]bool{"own": true, "bydefault": false})

	m = base() // the loop of a platform without the fields: as before
	run("unwired", m, fresh(), map[string]bool{"own": true, "bydefault": false})
}
