package controller

import (
	"context"
	"testing"

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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

// newScalingTestReconciler is a reconciler on a cluster with KEDA's kinds
// and, when metrics is set, the Metrics Server's.
func newScalingTestReconciler(t *testing.T, metrics bool, objs ...client.Object) (*AppReconciler, client.Client) {
	t.Helper()
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	mapper := meta.NewDefaultRESTMapper(nil)
	kinds := []schema.GroupVersionKind{InterceptorRouteGVK, ScaledObjectGVK}
	if metrics {
		kinds = append(kinds, PodMetricsGVK)
	}
	for _, gvk := range kinds {
		scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
		scheme.AddKnownTypeWithName(gvk.GroupVersion().WithKind(gvk.Kind+"List"), &unstructured.UnstructuredList{})
		mapper.Add(gvk, meta.RESTScopeNamespace)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithRESTMapper(mapper).WithObjects(objs...).
		WithStatusSubresource(&shpyrdv1.App{}, &shpyrdv1.Volume{}).Build()
	r := &AppReconciler{
		ProcessTypes: func(context.Context, string) []string { return nil },
		Client:       c, APIReader: c, Scheme: scheme, Recorder: record.NewFakeRecorder(100),
		Config: Config{Domain: "example.test", HTTPSPort: "8443", RegistryHost: "10.96.0.50:5000", RegistryInsecure: true, SystemNamespace: "shpyrd-system", SleepAllowed: always}.Defaults(),
	}
	return r, c
}

func autoscaledApp(bounds *shpyrdv1.AutoscaleSpec, sleep *shpyrdv1.SleepSpec) *shpyrdv1.App {
	return &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop", Generation: 1},
		Spec: shpyrdv1.AppSpec{
			Image:     "ghcr.io/acme/shop:1",
			Access:    shpyrdv1.AccessPublic,
			Processes: map[string]shpyrdv1.Process{"web": {Port: ptr.To[int32](8080), Autoscale: bounds, Sleep: sleep}},
		},
	}
}

func getScaledObject(t *testing.T, c client.Client, ns, name string) (*unstructured.Unstructured, bool) {
	t.Helper()
	so := &unstructured.Unstructured{}
	so.SetGroupVersionKind(ScaledObjectGVK)
	err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, so)
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatal(err)
	}
	return so, err == nil
}

func ingressBackend(t *testing.T, c client.Client, ns, name string) string {
	t.Helper()
	ing := &networkingv1.Ingress{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, ing); err != nil {
		t.Fatal(err)
	}
	return ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name
}

func triggerTypes(so *unstructured.Unstructured) []string {
	triggers, _, _ := unstructured.NestedSlice(so.Object, "spec", "triggers")
	var out []string
	for _, tr := range triggers {
		m, _ := tr.(map[string]interface{})
		typ, _ := m["type"].(string)
		out = append(out, typ)
	}
	return out
}

// RFC-0047: bounds on the web process render one ScaledObject, named like
// the Deployment, with two triggers — the requests in flight the
// interceptor counts, and CPU as the Metrics Server measures it, 70% of the
// size's CPU by default (350m of shared-s's 0.5). The Ingress routes through
// the interceptor; nothing is asleep (no idle count), and the status reads
// the bounds back.
func TestAutoscaleRendersOneScaledObjectWithBothTriggers(t *testing.T) {
	app := autoscaledApp(&shpyrdv1.AutoscaleSpec{Min: 2, Max: 10}, nil)
	r, c := newScalingTestReconciler(t, true, app)
	got := runReconcile(t, r, app)

	so, ok := getScaledObject(t, c, "app-shop", "shop-web")
	if !ok {
		t.Fatal("no ScaledObject named like the web Deployment")
	}
	if minR, _, _ := unstructured.NestedInt64(so.Object, "spec", "minReplicaCount"); minR != 2 {
		t.Errorf("minReplicaCount = %d, want 2", minR)
	}
	if maxR, _, _ := unstructured.NestedInt64(so.Object, "spec", "maxReplicaCount"); maxR != 10 {
		t.Errorf("maxReplicaCount = %d, want 10", maxR)
	}
	if _, found, _ := unstructured.NestedInt64(so.Object, "spec", "idleReplicaCount"); found {
		t.Error("an awake process has no idle count")
	}
	if types := triggerTypes(so); len(types) != 2 || types[0] != "external-push" || types[1] != "cpu" {
		t.Errorf("triggers = %v, want the interceptor's then cpu", types)
	}
	triggers, _, _ := unstructured.NestedSlice(so.Object, "spec", "triggers")
	cpu := triggers[1].(map[string]interface{})
	if cpu["metricType"] != "AverageValue" || cpu["metadata"].(map[string]interface{})["value"] != "350m" {
		t.Errorf("cpu trigger = %v, want AverageValue 350m (70%% of shared-s's 0.5)", cpu)
	}
	if restore, _, _ := unstructured.NestedBool(so.Object, "spec", "advanced", "restoreToOriginalReplicaCount"); restore {
		t.Error("the controller writes the count back itself: KEDA must not restore it")
	}
	if win, _, _ := unstructured.NestedInt64(so.Object, "spec", "advanced", "horizontalPodAutoscalerConfig", "behavior", "scaleDown", "stabilizationWindowSeconds"); win != 300 {
		t.Errorf("scale-down stabilisation = %d, want 300", win)
	}
	ir := &unstructured.Unstructured{}
	ir.SetGroupVersionKind(InterceptorRouteGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop"}, ir); err != nil {
		t.Fatal(err)
	}
	if target, _, _ := unstructured.NestedInt64(ir.Object, "spec", "scalingMetric", "concurrency", "targetValue"); target != 20 {
		t.Errorf("requests in flight per instance = %d, want the default 20", target)
	}
	if _, found, _ := unstructured.NestedMap(ir.Object, "spec", "scalingMetric", "requestRate"); found {
		t.Error("no request rate in wait mode: concurrency is the signal")
	}
	if b := ingressBackend(t, c, "app-shop", "shop"); b != webInterceptorService {
		t.Errorf("backend = %s, want the interceptor", b)
	}
	st := got.Status.Processes["web"].Autoscale
	if st == nil || st.Min != 2 || st.Max != 10 || st.CPU != 70 || st.Concurrency != 20 || st.State != "active" || st.Message != "" {
		t.Errorf("status = %+v, want the bounds and targets, active, nothing missing", st)
	}
}

// Without the Metrics Server the CPU half has nothing to read: the object
// carries the interceptor's trigger alone and the status says so, while
// the count still follows the requests in flight.
func TestAutoscaleWithoutMetricsServerFollowsRequestsOnly(t *testing.T) {
	app := autoscaledApp(&shpyrdv1.AutoscaleSpec{Min: 1, Max: 4, CPU: ptr.To[int32](50), Concurrency: ptr.To[int32](8)}, nil)
	r, c := newScalingTestReconciler(t, false, app)
	got := runReconcile(t, r, app)
	so, ok := getScaledObject(t, c, "app-shop", "shop-web")
	if !ok {
		t.Fatal("no ScaledObject")
	}
	if types := triggerTypes(so); len(types) != 1 || types[0] != "external-push" {
		t.Errorf("triggers = %v, want the interceptor's alone", types)
	}
	st := got.Status.Processes["web"].Autoscale
	if st == nil || st.State != "active" || st.Message != autoscaleNoCPUMessage || st.CPU != 50 || st.Concurrency != 8 {
		t.Errorf("status = %+v, want active with the CPU gap said", st)
	}
}

// Sleep and bounds on one process are one ScaledObject (KEDA refuses two on
// a Deployment): zero while asleep, the minimum on a wake, the bounds under
// load, with the quiet period as the cooldown. In page mode the request
// rate wakes the app and the concurrency target scales it.
func TestAutoscaleAndSleepShareTheScaledObject(t *testing.T) {
	app := autoscaledApp(&shpyrdv1.AutoscaleSpec{Min: 2, Max: 6}, &shpyrdv1.SleepSpec{After: "15m", Resuming: "page"})
	r, c := newScalingTestReconciler(t, true, app)
	got := runReconcile(t, r, app)
	so, ok := getScaledObject(t, c, "app-shop", "shop-web")
	if !ok {
		t.Fatal("no ScaledObject")
	}
	if idle, found, _ := unstructured.NestedInt64(so.Object, "spec", "idleReplicaCount"); !found || idle != 0 {
		t.Errorf("idleReplicaCount = %d (found %v), want 0", idle, found)
	}
	if minR, _, _ := unstructured.NestedInt64(so.Object, "spec", "minReplicaCount"); minR != 2 {
		t.Errorf("minReplicaCount = %d, want the minimum", minR)
	}
	if cd, _, _ := unstructured.NestedInt64(so.Object, "spec", "cooldownPeriod"); cd != 900 {
		t.Errorf("cooldownPeriod = %d, want the quiet period", cd)
	}
	if types := triggerTypes(so); len(types) != 2 {
		t.Errorf("triggers = %v, want both", types)
	}
	ir := &unstructured.Unstructured{}
	ir.SetGroupVersionKind(InterceptorRouteGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop"}, ir); err != nil {
		t.Fatal(err)
	}
	if target, _, _ := unstructured.NestedInt64(ir.Object, "spec", "scalingMetric", "concurrency", "targetValue"); target != 20 {
		t.Errorf("concurrency target = %d, want 20", target)
	}
	if rate, _, _ := unstructured.NestedInt64(ir.Object, "spec", "scalingMetric", "requestRate", "targetValue"); rate != wakeOnlyTarget {
		t.Errorf("request rate target = %d, want the wake-only %d", rate, wakeOnlyTarget)
	}
	ps := got.Status.Processes["web"]
	if ps.Sleep == nil || ps.Sleep.State != "awake" || ps.Autoscale == nil || ps.Autoscale.State != "active" {
		t.Errorf("status = sleep %+v autoscale %+v, want both read back", ps.Sleep, ps.Autoscale)
	}

	// The scaler puts the app to sleep: the status says so, with the bounds.
	web := &appsv1.Deployment{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop-web"}, web); err != nil {
		t.Fatal(err)
	}
	web.Spec.Replicas = ptr.To[int32](0)
	if err := c.Update(context.Background(), web); err != nil {
		t.Fatal(err)
	}
	got = runReconcile(t, r, app)
	ps = got.Status.Processes["web"]
	if ps.Sleep.State != "sleeping" || ps.Desired != 0 {
		t.Errorf("asleep: state %s desired %d, want sleeping and 0", ps.Sleep.State, ps.Desired)
	}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: "shop-web"}, web); err != nil {
		t.Fatal(err)
	}
	if *web.Spec.Replicas != 0 {
		t.Errorf("a reconcile woke the app: replicas = %d", *web.Spec.Replicas)
	}
}

// While the scaler runs the show, the status counts what it asks for: a
// Deployment at 5 of bounds 2–10 reads as 5 desired. Bounds taken away put
// the declared count back and the Ingress on the app's own Service.
func TestAutoscaleFollowsTheScalerAndOffRestoresTheCount(t *testing.T) {
	app := autoscaledApp(&shpyrdv1.AutoscaleSpec{Min: 2, Max: 10}, nil)
	app.Spec.Processes["web"] = shpyrdv1.Process{Port: ptr.To[int32](8080), Replicas: ptr.To[int32](3), Autoscale: app.Spec.Processes["web"].Autoscale}
	r, c := newScalingTestReconciler(t, true, app)
	runReconcile(t, r, app)
	web := &appsv1.Deployment{}
	key := types.NamespacedName{Namespace: "app-shop", Name: "shop-web"}
	if err := c.Get(context.Background(), key, web); err != nil {
		t.Fatal(err)
	}
	web.Spec.Replicas = ptr.To[int32](5) // the HPA's decision
	if err := c.Update(context.Background(), web); err != nil {
		t.Fatal(err)
	}
	got := runReconcile(t, r, app)
	if d := got.Status.Processes["web"].Desired; d != 5 {
		t.Errorf("desired = %d, want the scaler's 5", d)
	}
	if err := c.Get(context.Background(), key, web); err != nil {
		t.Fatal(err)
	}
	if *web.Spec.Replicas != 5 {
		t.Errorf("a reconcile overwrote the scaler's count: %d", *web.Spec.Replicas)
	}

	cur := &shpyrdv1.App{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(app), cur); err != nil {
		t.Fatal(err)
	}
	p := cur.Spec.Processes["web"]
	p.Autoscale = nil
	cur.Spec.Processes["web"] = p
	if err := c.Update(context.Background(), cur); err != nil {
		t.Fatal(err)
	}
	got = runReconcile(t, r, app)
	if _, ok := getScaledObject(t, c, "app-shop", "shop-web"); ok {
		t.Error("bounds taken away must leave no ScaledObject")
	}
	if err := c.Get(context.Background(), key, web); err != nil {
		t.Fatal(err)
	}
	if *web.Spec.Replicas != 3 {
		t.Errorf("replicas = %d, want the declared 3 back", *web.Spec.Replicas)
	}
	if b := ingressBackend(t, c, "app-shop", "shop"); b != "shop-web" {
		t.Errorf("backend = %s, want the app's own Service", b)
	}
	if got.Status.Processes["web"].Autoscale != nil || got.Status.Processes["web"].Desired != 3 {
		t.Errorf("status = %+v, want no bounds and 3 desired", got.Status.Processes["web"])
	}
}

// A single-instance volume pins the process to one instance: the bounds
// do not apply, no object is made, and the status says why.
func TestAutoscaleOnAPinnedProcessDoesNotApply(t *testing.T) {
	vol := testVolume("data", "5Gi", "")
	app := autoscaledApp(&shpyrdv1.AutoscaleSpec{Min: 2, Max: 10}, nil)
	app.Name, app.Namespace = "demo", "app-demo"
	p := app.Spec.Processes["web"]
	p.Volumes = []shpyrdv1.VolumeMount{{Name: "data", Path: "/data"}}
	app.Spec.Processes["web"] = p
	r, c := newScalingTestReconciler(t, true, app, vol)
	got := runReconcile(t, r, app)
	if _, ok := getScaledObject(t, c, "app-demo", "demo-web"); ok {
		t.Error("a pinned process must have no ScaledObject")
	}
	st := got.Status.Processes["web"].Autoscale
	if st == nil || st.State != "unavailable" || st.Message != autoscalePinnedMessage {
		t.Errorf("status = %+v, want unavailable, pinned", st)
	}
	if b := ingressBackend(t, c, "app-demo", "demo"); b == webInterceptorService {
		t.Error("a pinned process routes to its own Service")
	}
}

// Before autoscaling shared the objects, sleep's ScaledObject was
// "<app>-sleep" and its bridge "web-sleep". KEDA refuses a second
// ScaledObject on the Deployment, so the old one goes first — without
// restoring the count it found, so a sleeping app stays asleep — and the
// reconcile looks again shortly; the new object and bridge take over, and
// the old bridge goes once the Ingress is off it.
func TestLegacySleepObjectsAreReplaced(t *testing.T) {
	app := autoscaledApp(nil, &shpyrdv1.SleepSpec{After: "15m"})
	legacy := makeUnstructured(ScaledObjectGVK, "shop-sleep", "app-shop")
	legacy.Object["spec"] = map[string]interface{}{
		"scaleTargetRef": map[string]interface{}{"name": "shop-web"},
		"advanced":       map[string]interface{}{"restoreToOriginalReplicaCount": true},
	}
	bridge := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: legacyWebSleepService, Namespace: "app-shop"}, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeExternalName, ExternalName: interceptorProxySvc}}
	r, c := newScalingTestReconciler(t, false, app, legacy, bridge)

	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(app)}
	res, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.RequeueAfter != scalingRetry {
		t.Errorf("first pass requeues after %s, want %s", res.RequeueAfter, scalingRetry)
	}
	if _, ok := getScaledObject(t, c, "app-shop", "shop-sleep"); ok {
		t.Error("the legacy ScaledObject must go first")
	}
	if _, ok := getScaledObject(t, c, "app-shop", "shop-web"); ok {
		t.Error("the new ScaledObject waits for the old one to be gone")
	}
	if b := ingressBackend(t, c, "app-shop", "shop"); b != webInterceptorService {
		t.Errorf("backend = %s, want the new bridge at once", b)
	}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: legacyWebSleepService}, &corev1.Service{}); !apierrors.IsNotFound(err) {
		t.Errorf("the old bridge goes once the Ingress is off it: %v", err)
	}

	res, err = r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.RequeueAfter == scalingRetry {
		t.Error("second pass has nothing to wait for")
	}
	so, ok := getScaledObject(t, c, "app-shop", "shop-web")
	if !ok {
		t.Fatal("the new ScaledObject is made on the second pass")
	}
	if minR, _, _ := unstructured.NestedInt64(so.Object, "spec", "minReplicaCount"); minR != 1 {
		t.Errorf("minReplicaCount = %d, want the declared count", minR)
	}
	if idle, found, _ := unstructured.NestedInt64(so.Object, "spec", "idleReplicaCount"); !found || idle != 0 {
		t.Error("sleep alone still sleeps: idle count 0")
	}
}
