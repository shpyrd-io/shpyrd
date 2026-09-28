package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"k8s.io/utils/ptr"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// HTTP sleep (RFC-0075): when a web process has a sleep policy the controller
// renders three objects:
//  1. ExternalName Service "web-sleep" → the KEDA HTTP add-on interceptor.
//  2. InterceptorRoute (http.keda.sh/v1beta1) with the cold-start config.
//  3. ScaledObject (keda.sh/v1alpha1) driving the Deployment 0↔N.
// The Ingress backend switches to "web-sleep" only once these objects
// exist (reconcileSleep reports it; see mutateIngress). On a cluster
// without the keda-http extension the app keeps routing to its own
// Service and the process status says what is missing — sleep never takes
// an app down.

var (
	InterceptorRouteGVK = schema.GroupVersionKind{Group: "http.keda.sh", Version: "v1beta1", Kind: "InterceptorRoute"}
	ScaledObjectGVK     = schema.GroupVersionKind{Group: "keda.sh", Version: "v1alpha1", Kind: "ScaledObject"}
)

const (
	interceptorProxySvc = "keda-add-ons-http-interceptor-proxy.keda.svc.cluster.local"
	webSleepServiceName = "web-sleep"
)

// sleepEnabled reports whether the web process has a valid sleep policy,
// its own or the workspace's default.
func (r *AppReconciler) sleepEnabled(app *shpyrdv1.App) bool {
	spec := r.webSleepSpec(app)
	return spec != nil && parseSleepDuration(spec.After) > 0
}

// webSleepSpec returns the effective SleepSpec of the web process: the
// process's own when set (an explicit "off" is a policy too — it opts the
// project out of the workspace default), else the workspace's plan default,
// else nil.
func (r *AppReconciler) webSleepSpec(app *shpyrdv1.App) *shpyrdv1.SleepSpec {
	for _, p := range processes(app) {
		if p.Name == "web" && p.Sleep != nil {
			return p.Sleep
		}
	}
	if r.Config.WorkspaceSleepDefault == nil {
		return nil
	}
	after, resuming := r.Config.WorkspaceSleepDefault(app.Labels[shpyrdv1.LabelWorkspace])
	if parseSleepDuration(after) == 0 {
		return nil
	}
	return &shpyrdv1.SleepSpec{After: after, Resuming: resuming}
}

// sleepSource says where the effective policy comes from, for the status.
func (r *AppReconciler) sleepSource(app *shpyrdv1.App) string {
	for _, p := range processes(app) {
		if p.Name == "web" && p.Sleep != nil {
			return "project"
		}
	}
	return "plan"
}

// parseSleepDuration parses the after value; returns 0 when disabled.
func parseSleepDuration(after string) time.Duration {
	after = strings.TrimSpace(strings.ToLower(after))
	if after == "" || after == "off" || after == "false" {
		return 0
	}
	d, err := time.ParseDuration(after)
	if err != nil || d < 5*time.Minute {
		return 0
	}
	if d > 24*time.Hour {
		return 24 * time.Hour
	}
	return d
}

// sleepUnavailableMessage is what the process status says when the policy
// is set but the cluster lacks the KEDA HTTP add-on.
const sleepUnavailableMessage = "sleep needs the sleep extension (shpyrd-ctl extensions enable sleep)"

// scaledObjectGrace is how long a ScaledObject may stay not Ready before
// sleep is torn down for the app. KEDA treats a trigger it cannot evaluate
// as inactive and scales the Deployment to zero at once, so a broken
// scaler is an outage, not a no-op: the controller notices, removes the
// objects (KEDA then restores the replica count and the Ingress goes back
// to the app's Service) and pauses sleep for this spec generation.
const scaledObjectGrace = 3 * time.Minute

// sleepPause is why sleep is paused for an app, and for which generation.
type sleepPause struct {
	generation int64
	reason     string
}

// sleepPause reports whether sleep is paused for the app's current spec.
func (r *AppReconciler) sleepPause(app *shpyrdv1.App) (sleepPause, bool) {
	v, ok := r.sleepPaused.Load(app.UID)
	if !ok {
		return sleepPause{}, false
	}
	p := v.(sleepPause)
	if p.generation != app.Generation {
		r.sleepPaused.Delete(app.UID) // a new policy retries
		return sleepPause{}, false
	}
	return p, true
}

// scaledObjectBroken reports whether the ScaledObject has been not Ready
// for longer than the grace period, and KEDA's message.
func (r *AppReconciler) scaledObjectBroken(so *unstructured.Unstructured) (bool, string) {
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}
	if now.Sub(so.GetCreationTimestamp().Time) < scaledObjectGrace {
		return false, ""
	}
	conditions, _, _ := unstructured.NestedSlice(so.Object, "status", "conditions")
	for _, c := range conditions {
		m, _ := c.(map[string]interface{})
		if m["type"] == "Ready" && m["status"] == "False" {
			msg, _ := m["message"].(string)
			return true, msg
		}
	}
	return false, ""
}

// reconcileSleep ensures or removes the KEDA sleep objects for the app and
// reports whether the Ingress should route through the interceptor. It is
// false when the policy is off or when KEDA's CRDs are not installed.
func (r *AppReconciler) reconcileSleep(ctx context.Context, app *shpyrdv1.App) (bool, error) {
	if !r.sleepEnabled(app) {
		return false, r.deleteSleepObjects(ctx, app)
	}
	if !r.kedaHTTPAvailable() {
		log.FromContext(ctx).V(1).Info("sleep policy set but keda-http is not installed; routing stays on the app's Service", "app", app.Name)
		return false, r.deleteSleepObjects(ctx, app)
	}
	if _, paused := r.sleepPause(app); paused {
		return false, r.deleteSleepObjects(ctx, app)
	}
	sp := r.webSleepSpec(app)
	cooldown := int64(parseSleepDuration(sp.After).Seconds())

	// Discover the web Service name (convention: app.Name + "-web" or app.Name).
	webSvc := app.Name + "-web"
	if _, err := r.webServiceName(ctx, app); err == nil {
		webSvc, _ = r.webServiceName(ctx, app)
	}

	// Compute max replicas from the web process.
	maxR := int64(1)
	for _, p := range processes(app) {
		if p.Name == "web" && p.Replicas != nil && *p.Replicas > 0 {
			maxR = int64(*p.Replicas)
		}
	}

	// 1. ExternalName Service bridging to the KEDA interceptor.
	extSvc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: webSleepServiceName, Namespace: app.Namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, extSvc, func() error {
		extSvc.Labels = mergeMaps(extSvc.Labels, map[string]string{shpyrdv1.LabelApp: app.Name, shpyrdv1.LabelManagedBy: "shpyrd"})
		extSvc.Spec = corev1.ServiceSpec{
			Type:         corev1.ServiceTypeExternalName,
			ExternalName: interceptorProxySvc,
			Ports:        []corev1.ServicePort{{Port: 8080, Name: "http"}},
		}
		return controllerutil.SetControllerReference(app, extSvc, r.Scheme)
	}); err != nil {
		return false, fmt.Errorf("sleep externalname svc: %w", err)
	}

	// 2. InterceptorRoute.
	ir := makeUnstructured(InterceptorRouteGVK, app.Name, app.Namespace)
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, ir, func() error {
		ir.SetLabels(mergeMaps(ir.GetLabels(), map[string]string{shpyrdv1.LabelApp: app.Name, shpyrdv1.LabelManagedBy: "shpyrd"}))
		hosts := r.Config.domains(app)
		rules := make([]interface{}, 0, len(hosts))
		for _, h := range hosts {
			rules = append(rules, map[string]interface{}{"hosts": []interface{}{h}})
		}
		// What wakes the app. In wait mode requests are held in the
		// interceptor and the scaler acts on that concurrency. In page mode
		// the placeholder is answered at once, so concurrency is back to
		// zero before the scaler looks: the route scales on request rate
		// instead (every placeholder-served request counts for a minute).
		// The target values are high on purpose — a sleeping app wakes to
		// its own instance count and RFC-0047 autoscaling is a separate
		// feature — so they only ever decide 0 versus awake.
		scalingMetric := map[string]interface{}{
			"concurrency": map[string]interface{}{"targetValue": int64(1000)},
		}
		coldStart := map[string]interface{}{"maxPendingRequests": int64(100), "overflow": "Reject"}
		if sp.Resuming == "page" {
			scalingMetric = map[string]interface{}{
				"requestRate": map[string]interface{}{"targetValue": int64(1000), "window": "1m", "granularity": "1s"},
			}
			if cmErr := r.ensureSleepPage(ctx, app); cmErr != nil {
				return cmErr
			}
			coldStart["placeholder"] = map[string]interface{}{
				"response": map[string]interface{}{
					"statusCode": int64(503),
					"headers": map[string]interface{}{
						"Content-Type": "text/html; charset=utf-8",
						"Retry-After":  "2",
					},
					"bodyFromConfigMap": map[string]interface{}{
						"name": sleepPageCMName(app), "key": "index.html",
					},
				},
			}
		}
		ir.Object["spec"] = map[string]interface{}{
			"target":        map[string]interface{}{"service": webSvc, "port": int64(80)},
			"rules":         rules,
			"scalingMetric": scalingMetric,
			"staticRoutes": []interface{}{
				map[string]interface{}{
					"rules": []interface{}{
						map[string]interface{}{
							"paths": []interface{}{map[string]interface{}{"value": "/.shpyrd/"}},
						},
					},
					"response": map[string]interface{}{"statusCode": int64(200), "body": ""},
				},
			},
			"coldStart": coldStart,
			"timeouts":  map[string]interface{}{"readiness": "120s"},
		}
		return controllerutil.SetControllerReference(app, ir, r.Scheme)
	}); err != nil {
		if isNoMatchKind(err) {
			log.FromContext(ctx).V(1).Info("sleep policy set but keda-http is not installed", "app", app.Name)
			return false, nil
		}
		return false, fmt.Errorf("interceptorroute: %w", err)
	}

	// 3. ScaledObject.
	so := makeUnstructured(ScaledObjectGVK, app.Name+"-sleep", app.Namespace)
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, so, func() error {
		so.SetLabels(mergeMaps(so.GetLabels(), map[string]string{shpyrdv1.LabelApp: app.Name, shpyrdv1.LabelManagedBy: "shpyrd"}))
		so.Object["spec"] = map[string]interface{}{
			"scaleTargetRef": map[string]interface{}{
				"apiVersion": "apps/v1", "kind": "Deployment", "name": app.Name + "-web",
			},
			"minReplicaCount": int64(0),
			"maxReplicaCount": maxR,
			"cooldownPeriod":  cooldown,
			// A trigger that has never been active has no cooldown to wait
			// for; without this KEDA scales a freshly enabled app to zero
			// at once instead of after the quiet period.
			"initialCooldownPeriod": cooldown,
			// The add-on's external scaler reads the InterceptorRoute named
			// here (v0.16 contract; hostnames/service/port were the
			// deprecated HTTPScaledObject path) and derives the metric spec
			// from its scalingMetric.
			"triggers": []interface{}{
				map[string]interface{}{
					"type": "external-push",
					"metadata": map[string]interface{}{
						"scalerAddress":    "keda-add-ons-http-external-scaler.keda:9090",
						"interceptorRoute": app.Name,
					},
				},
			},
			// Waking restores the process's own instance count.
			"advanced": map[string]interface{}{"restoreToOriginalReplicaCount": true},
		}
		return controllerutil.SetControllerReference(app, so, r.Scheme)
	}); err != nil {
		if isNoMatchKind(err) {
			log.FromContext(ctx).V(1).Info("sleep policy set but keda is not installed", "app", app.Name)
			return false, nil
		}
		return false, fmt.Errorf("scaledobject: %w", err)
	}
	if broken, msg := r.scaledObjectBroken(so); broken {
		reason := "sleep paused: KEDA could not scale this app (" + firstNonEmpty(msg, "ScaledObject not Ready") + "); set the policy again to retry"
		r.sleepPaused.Store(app.UID, sleepPause{generation: app.Generation, reason: reason})
		r.Recorder.Event(app, corev1.EventTypeWarning, "SleepPaused", reason)
		log.FromContext(ctx).Info("sleep paused", "app", app.Name, "reason", reason)
		if err := r.deleteSleepObjects(ctx, app); err != nil {
			return false, err
		}
		// Give the web process its instances back now rather than on the
		// next event: KEDA may have scaled it to zero.
		dep := &appsv1.Deployment{}
		if err := r.Client.Get(ctx, types.NamespacedName{Name: app.Name + "-web", Namespace: app.Namespace}, dep); err == nil {
			if dep.Spec.Replicas == nil || *dep.Spec.Replicas != int32(maxR) {
				dep.Spec.Replicas = ptr.To(int32(maxR))
				if err := r.Client.Update(ctx, dep); err != nil {
					return false, fmt.Errorf("restore replicas after sleep pause: %w", err)
				}
			}
		}
		return false, nil
	}
	return true, nil
}

// ensureSleepPage creates or updates the ConfigMap that serves the branded
// "waking up" HTML when resuming: page is set.
func (r *AppReconciler) ensureSleepPage(ctx context.Context, app *shpyrdv1.App) error {
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: sleepPageCMName(app), Namespace: app.Namespace,
		Labels: map[string]string{
			shpyrdv1.LabelApp:            app.Name,
			shpyrdv1.LabelManagedBy:      "shpyrd",
			"http.keda.sh/response-body": "true",
		},
	}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		cm.Data = map[string]string{"index.html": buildSleepPageHTML(app.Name)}
		return controllerutil.SetControllerReference(app, cm, r.Scheme)
	})
	return err
}

func sleepPageCMName(app *shpyrdv1.App) string { return app.Name + "-sleep-page" }

func buildSleepPageHTML(name string) string {
	return `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<meta http-equiv="refresh" content="3"><title>` + name + ` — waking up</title>` +
		`<style>body{margin:0;font:16px/1.5 system-ui,sans-serif;background:#0f172a;color:#e2e8f0;display:flex;min-height:100vh;align-items:center;justify-content:center}` +
		`main{max-width:28rem;padding:2.5rem;background:#1e293b;border-radius:12px;border-top:4px solid #ff4f00;text-align:center}` +
		`h1{font-size:1.2rem;margin:0 0 .75rem}p{color:#94a3b8;margin:0}</style></head>` +
		`<body><main><h1>Waking up — one moment</h1><p>` + name + ` is starting. This page refreshes itself.</p></main></body></html>`
}

// deleteSleepObjects removes all KEDA sleep objects for the app.
func (r *AppReconciler) deleteSleepObjects(ctx context.Context, app *shpyrdv1.App) error {
	for _, o := range []client.Object{
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: webSleepServiceName, Namespace: app.Namespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: sleepPageCMName(app), Namespace: app.Namespace}},
	} {
		if err := r.deleteIfExists(ctx, o); err != nil {
			return err
		}
	}
	for _, gvk := range []schema.GroupVersionKind{InterceptorRouteGVK, ScaledObjectGVK} {
		u := makeUnstructured(gvk, app.Name, app.Namespace)
		if err := r.Client.Delete(ctx, u); err != nil && !apierrors.IsNotFound(err) && !isNoMatchKind(err) {
			return fmt.Errorf("delete %s: %w", gvk.Kind, err)
		}
		u2 := makeUnstructured(gvk, app.Name+"-sleep", app.Namespace)
		if err := r.Client.Delete(ctx, u2); err != nil && !apierrors.IsNotFound(err) && !isNoMatchKind(err) {
			return fmt.Errorf("delete %s-sleep: %w", gvk.Kind, err)
		}
	}
	return nil
}

// webServiceName discovers the web Service's name.
func (r *AppReconciler) webServiceName(ctx context.Context, app *shpyrdv1.App) (string, error) {
	candidate := app.Name + "-web"
	svc := &corev1.Service{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: candidate, Namespace: app.Namespace}, svc); err == nil {
		return candidate, nil
	}
	return app.Name, nil
}

// kedaHTTPAvailable asks the REST mapper (kept current by discovery) whether
// both KEDA kinds the sleep objects need are installed.
func (r *AppReconciler) kedaHTTPAvailable() bool {
	m := r.Client.RESTMapper()
	if m == nil {
		return false
	}
	for _, gvk := range []schema.GroupVersionKind{InterceptorRouteGVK, ScaledObjectGVK} {
		if _, err := m.RESTMapping(gvk.GroupKind(), gvk.Version); err != nil {
			return false
		}
	}
	return true
}

// isNoMatchKind returns true when KEDA's CRDs are not installed.
func isNoMatchKind(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "no matches for kind") ||
		strings.Contains(err.Error(), "no kind is registered"))
}

// makeUnstructured creates an empty unstructured object.
func makeUnstructured(gvk schema.GroupVersionKind, name, namespace string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)
	u.SetName(name)
	u.SetNamespace(namespace)
	return u
}
