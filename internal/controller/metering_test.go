package controller

import (
	"context"
	"encoding/json"
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
	"github.com/shpyrd-io/shpyrd/pkg/prom"
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
		"container_memory_working_set_bytes": {
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
	r.Config.WorkspaceSleepDefault = func(slug string) (string, string) {
		if slug == "acme" {
			return "15m", "page"
		}
		return "", ""
	}
	if !r.sleepEnabled(app) {
		t.Fatal("plan default should enable sleep")
	}
	if sp := r.webSleepSpec(app); sp == nil || sp.After != "15m" || sp.Resuming != "page" || r.sleepSource(app) != "plan" {
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
			Config: Config{Domain: "example.test", HTTPSPort: "8443", RegistryHost: "10.96.0.50:5000", RegistryInsecure: true, SystemNamespace: "shpyrd-system"}.Defaults(),
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
