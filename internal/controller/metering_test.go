package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
			sample(map[string]string{"namespace": "app-shop"}, "3"), // a pod without the label: component default
			sample(map[string]string{"namespace": "app-other", "label_shpyrd_io_process": "web"}, "99"),
		},
		"container_memory_working_set_bytes": {
			sample(map[string]string{"namespace": "app-shop", "label_shpyrd_io_process": "web"}, "0.25"),
		},
		"kube_persistentvolumeclaim_resource_requests_storage_bytes": {},
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
	// cpu web + cpu default + memory web + storage zero + egress = 5
	if n != 5 {
		t.Fatalf("buckets written = %d, want 5", n)
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
	if byKey["default/"+store.MetricCPUUsed] != 3 {
		t.Errorf("default cpu = %v, want 3", byKey["default/"+store.MetricCPUUsed])
	}
	if byKey["web/"+store.MetricEgressHTTP] != 4096 {
		t.Errorf("egress = %v, want 4096", byKey["web/"+store.MetricEgressHTTP])
	}
	if q, ok := byKey["default/"+store.MetricStorage]; !ok || q != 0 {
		t.Errorf("storage zero bucket missing or non-zero: %v %v", q, ok)
	}
	// Nothing for the unmapped namespace.
	other, _ := st.QueryBuckets(context.Background(), store.DefaultWorkspace, "other", start.Add(-time.Second), end.Add(time.Second))
	if len(other) != 0 {
		t.Errorf("unmapped namespace produced buckets: %+v", other)
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
		if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop"}, ir); !apierrors.IsNotFound(err) {
			t.Errorf("interceptorroute still there: %v", err)
		}
	})
}
