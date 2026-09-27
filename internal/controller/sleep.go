package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// HTTP sleep (RFC-0075): when a web process has a sleep policy the controller
// renders three objects:
//  1. ExternalName Service "web-sleep" → the KEDA HTTP add-on interceptor.
//  2. InterceptorRoute (http.keda.sh/v1beta1) with the cold-start config.
//  3. ScaledObject (keda.sh/v1alpha1) driving the Deployment 0↔N.
// The Ingress backend switches to "web-sleep" via edgeAnnotations while
// sleep is active (see desired.go); nothing changes in the edge's auth flow.

var (
	InterceptorRouteGVK = schema.GroupVersionKind{Group: "http.keda.sh", Version: "v1beta1", Kind: "InterceptorRoute"}
	ScaledObjectGVK     = schema.GroupVersionKind{Group: "keda.sh", Version: "v1alpha1", Kind: "ScaledObject"}
)

const (
	interceptorProxySvc = "keda-add-ons-http-interceptor-proxy.keda.svc.cluster.local"
	webSleepServiceName = "web-sleep"
)

// sleepEnabled reports whether the web process has a valid sleep policy.
func sleepEnabled(app *shpyrdv1.App) bool {
	spec := webSleepSpec(app)
	return spec != nil && parseSleepDuration(spec.After) > 0
}

// webSleepSpec returns the SleepSpec of the web process, or nil.
func webSleepSpec(app *shpyrdv1.App) *shpyrdv1.SleepSpec {
	for _, p := range processes(app) {
		if p.Name == "web" && p.Sleep != nil {
			return p.Sleep
		}
	}
	return nil
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

// reconcileSleep ensures or removes the KEDA sleep objects for the app.
func (r *AppReconciler) reconcileSleep(ctx context.Context, app *shpyrdv1.App) error {
	if !sleepEnabled(app) {
		return r.deleteSleepObjects(ctx, app)
	}
	sp := webSleepSpec(app)
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
		return fmt.Errorf("sleep externalname svc: %w", err)
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
		coldStart := map[string]interface{}{"maxPendingRequests": int64(100), "overflow": "Reject"}
		if sp.Resuming == "page" {
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
			"target": map[string]interface{}{"service": webSvc, "port": int64(80)},
			"rules":  rules,
			"scalingMetric": map[string]interface{}{
				"concurrency": map[string]interface{}{"targetValue": int64(1000)},
			},
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
		return fmt.Errorf("interceptorroute: %w", err)
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
			"triggers": []interface{}{
				map[string]interface{}{
					"type": "external-push",
					"metadata": map[string]interface{}{
						"scalerAddress": "keda-add-ons-http-external-scaler.keda:9090",
						"hostnames":     strings.Join(r.Config.domains(app), ","),
						"service":       webSvc, "port": "80",
					},
				},
			},
		}
		return controllerutil.SetControllerReference(app, so, r.Scheme)
	}); err != nil {
		// ScaledObject creation fails gracefully when KEDA is not installed.
		if !apierrors.IsNotFound(err) && !isNoMatchKind(err) {
			return fmt.Errorf("scaledobject: %w", err)
		}
	}
	return nil
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
