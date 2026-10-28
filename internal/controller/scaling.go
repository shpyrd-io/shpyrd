package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// Scaling the web process with KEDA: sleep (RFC-0075) and autoscaling
// (RFC-0047). Both render the same three objects, so one function owns
// them, and a process may have both at once:
//  1. ExternalName Service "web-interceptor" → the KEDA HTTP add-on's
//     interceptor, which counts the requests in flight and wakes a
//     sleeping app.
//  2. InterceptorRoute (http.keda.sh/v1beta1): the hosts, the requests an
//     instance may have in flight, the cold-start behaviour.
//  3. ScaledObject (keda.sh/v1alpha1), named like the Deployment, driving
//     it: 0 while asleep; the declared count, or the bounds, otherwise —
//     on the requests in flight and, when the Metrics Server is installed,
//     on CPU, whichever asks for more instances.
//
// KEDA refuses a second ScaledObject on a Deployment, which is why one
// object carries both. The Ingress points at the interceptor only once the
// objects exist (reconcileWebScaling reports it; see mutateIngress). On a
// cluster without KEDA's kinds the app keeps routing to its own Service
// and the process status says what is missing — neither feature ever takes
// an app down.

var (
	InterceptorRouteGVK = schema.GroupVersionKind{Group: "http.keda.sh", Version: "v1beta1", Kind: "InterceptorRoute"}
	ScaledObjectGVK     = schema.GroupVersionKind{Group: "keda.sh", Version: "v1alpha1", Kind: "ScaledObject"}
	// PodMetricsGVK is the Metrics Server's kind: present, CPU is measured.
	PodMetricsGVK = schema.GroupVersionKind{Group: "metrics.k8s.io", Version: "v1beta1", Kind: "PodMetrics"}
)

const (
	interceptorProxySvc = "keda-add-ons-http-interceptor-proxy.keda.svc.cluster.local"
	// webInterceptorService bridges the Ingress to the interceptor.
	webInterceptorService = "web-interceptor"
	// legacyWebSleepService is the bridge's name, and "<app>-sleep" the
	// ScaledObject's, from before autoscaling shared the objects; a
	// reconcile replaces them (legacyScaledObjectName).
	legacyWebSleepService = "web-sleep"
	// wakeOnlyTarget is a request target so high that the interceptor's
	// metric only ever decides asleep versus awake: a sleeping app wakes to
	// its own count, a process without bounds keeps it.
	wakeOnlyTarget = int64(1000)
	// scaleDownStabilization is how long the HPA holds a lower count before
	// acting on it: the RFC's five minutes (Kubernetes' default too).
	scaleDownStabilization = int64(300)
	// scalingRetry is how soon to look again while a legacy object is on
	// its way out.
	scalingRetry = 5 * time.Second
	// sleepWakeWindow is how long a wake may take: the interceptor holds a
	// request that long for the app to be ready, and a waking process has
	// at least that long to start before its startup probe gives up.
	sleepWakeWindow = 120 * time.Second
)

// What the process status says when the bounds are set but cannot be
// followed, or only in part.
const (
	autoscaleUnavailableMessage = "autoscaling needs the autoscale extension (shpyrd-ctl extensions enable autoscale)"
	autoscaleNoCPUMessage       = "the count follows the requests in flight only: CPU is not measured without the Metrics Server (shpyrd-ctl extensions enable autoscale installs it)"
	autoscalePinnedMessage      = "pinned to one instance by a single-instance volume: the bounds do not apply"
)

// webScaling is what KEDA does for the web process once a reconcile has
// run: whether the Ingress routes through the interceptor (sleep or
// autoscaling is on), which of the two is on, and whether CPU is measured.
type webScaling struct {
	interceptor bool
	sleep       bool
	autoscale   bool
	cpu         bool
	// retry asks for another look soon: a legacy object is on its way out.
	retry time.Duration
}

// webAutoscale returns the web process's bounds when they are set, valid
// and can apply: not during maintenance, and not on a process a
// single-instance volume pins to one instance.
func webAutoscale(app *shpyrdv1.App, mounts []resolvedMount) *shpyrdv1.AutoscaleSpec {
	if maintenance(app) || hasDisk(mounts) {
		return nil
	}
	for _, p := range processes(app) {
		if p.Name == "web" && p.Autoscale.Valid() {
			return p.Autoscale
		}
	}
	return nil
}

// declaredWebReplicas is the count the web process asks for in its spec,
// at least one: what a wake restores and what a process without bounds
// keeps.
func declaredWebReplicas(app *shpyrdv1.App) int64 {
	for _, p := range processes(app) {
		if p.Name == "web" && p.Replicas != nil && *p.Replicas > 0 {
			return int64(*p.Replicas)
		}
	}
	return 1
}

// cpuTarget is the CPU an instance may use before another is added, as the
// quantity KEDA compares the instances' average with: the target percent
// of the size's CPU. The size's CPU is its limit; a shared size requests a
// share of it, which is why the trigger is an average value and not a
// utilisation of the request.
func cpuTarget(res corev1.ResourceRequirements, percent int32) string {
	cpu := res.Limits.Cpu()
	if cpu.IsZero() {
		cpu = res.Requests.Cpu()
	}
	milli := cpu.MilliValue() * int64(percent) / 100
	if milli < 1 {
		milli = 1
	}
	return fmt.Sprintf("%dm", milli)
}

func legacyScaledObjectName(app *shpyrdv1.App) string { return app.Name + "-sleep" }

// webAutoscaleStatus is the read-back of the web process's bounds: the
// bounds and targets as set, and whether the cluster follows them, decided
// after the objects were reconciled. Nil when no bounds are set.
func (r *AppReconciler) webAutoscaleStatus(app *shpyrdv1.App, mounts []resolvedMount, ws webScaling) *shpyrdv1.AutoscaleStatus {
	var as *shpyrdv1.AutoscaleSpec
	for _, p := range processes(app) {
		if p.Name == "web" && p.Autoscale.Valid() {
			as = p.Autoscale
		}
	}
	if as == nil {
		return nil
	}
	st := &shpyrdv1.AutoscaleStatus{Min: as.Min, Max: as.Max, CPU: as.TargetCPU(), Concurrency: as.TargetConcurrency(), State: "active"}
	switch pause, paused := r.sleepPause(app); {
	case paused:
		st.State, st.Message = "unavailable", pause.reason
	case hasDisk(mounts):
		st.State, st.Message = "unavailable", autoscalePinnedMessage
	case !ws.autoscale:
		st.State, st.Message = "unavailable", autoscaleUnavailableMessage
	case !ws.cpu:
		st.Message = autoscaleNoCPUMessage
	}
	return st
}

// reconcileWebScaling ensures or removes the KEDA objects of the web
// process and reports what they do. webRes is the resources the web
// process runs with (the CPU target is a part of its size's CPU).
func (r *AppReconciler) reconcileWebScaling(ctx context.Context, app *shpyrdv1.App, mounts []resolvedMount, webRes corev1.ResourceRequirements) (webScaling, error) {
	var ws webScaling
	sleepOn := r.sleepEnabled(app)
	as := webAutoscale(app, mounts)
	if _, paused := r.sleepPause(app); paused {
		sleepOn, as = false, nil
	}
	if !sleepOn && as == nil {
		return ws, r.deleteScalingObjects(ctx, app)
	}
	if !r.kedaHTTPAvailable() {
		log.FromContext(ctx).V(1).Info("sleep or bounds set but KEDA's kinds are not installed; routing stays on the app's Service", "app", app.Name)
		return ws, r.deleteScalingObjects(ctx, app)
	}

	// The web Service and Deployment are named as every other object of
	// the process is (RFC-0076: "web" in p-<id>, "<slug>-web" for a legacy
	// App); nothing here recomputes a name from the slug. The ScaledObject
	// takes the same name: it is the process's.
	webSvc := workloadName(app, "web")
	declared := declaredWebReplicas(app)
	minR, maxR := declared, declared
	if as != nil {
		minR, maxR = int64(as.Min), int64(as.Max)
	}
	var cooldown int64
	resuming := ""
	if sleepOn {
		sp := r.webSleepSpec(app)
		cooldown = int64(parseSleepDuration(sp.After).Seconds())
		resuming = sleepResuming(sp, mounts)
	}

	// 1. ExternalName Service bridging to the KEDA interceptor.
	extSvc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: webInterceptorService, Namespace: app.Namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, extSvc, func() error {
		extSvc.Labels = mergeMaps(extSvc.Labels, map[string]string{shpyrdv1.LabelApp: app.Name, shpyrdv1.LabelManagedBy: "shpyrd"})
		extSvc.Spec = corev1.ServiceSpec{
			Type:         corev1.ServiceTypeExternalName,
			ExternalName: interceptorProxySvc,
			Ports:        []corev1.ServicePort{{Port: 8080, Name: "http"}},
		}
		return controllerutil.SetControllerReference(app, extSvc, r.Scheme)
	}); err != nil {
		return ws, fmt.Errorf("interceptor externalname svc: %w", err)
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
		// What the interceptor measures. The requests in flight per
		// instance: with bounds, the target an instance may carry before
		// another is added; without, a number so high it only decides
		// asleep versus awake. In wait mode held requests count too, so
		// that is what wakes the app. In page mode the placeholder is
		// answered at once and concurrency is back to zero before the
		// scaler looks, so the wake signal is the request rate instead
		// (every placeholder-served request counts for a minute); with
		// bounds both are set, and KEDA follows whichever asks for more.
		concurrency := wakeOnlyTarget
		if as != nil {
			concurrency = int64(as.TargetConcurrency())
		}
		scalingMetric := map[string]interface{}{
			"concurrency": map[string]interface{}{"targetValue": concurrency},
		}
		coldStart := map[string]interface{}{"maxPendingRequests": int64(100), "overflow": "Reject"}
		if resuming == "page" {
			rate := map[string]interface{}{"targetValue": wakeOnlyTarget, "window": "1m", "granularity": "1s"}
			if as != nil {
				scalingMetric["requestRate"] = rate
			} else {
				scalingMetric = map[string]interface{}{"requestRate": rate}
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
			"timeouts":  map[string]interface{}{"readiness": fmt.Sprintf("%.0fs", sleepWakeWindow.Seconds())},
		}
		return controllerutil.SetControllerReference(app, ir, r.Scheme)
	}); err != nil {
		if isNoMatchKind(err) {
			log.FromContext(ctx).V(1).Info("sleep or bounds set but keda-http is not installed", "app", app.Name)
			return ws, nil
		}
		return ws, fmt.Errorf("interceptorroute: %w", err)
	}

	// 3. ScaledObject. KEDA refuses a second one on the Deployment, so a
	// legacy "<app>-sleep" goes first — and goes without restoring the
	// count it found: the new object takes over where the process is, and
	// a wake for nothing is what restoring would mean to a sleeping app.
	ws.interceptor, ws.sleep, ws.autoscale = true, sleepOn, as != nil
	legacy := makeUnstructured(ScaledObjectGVK, legacyScaledObjectName(app), app.Namespace)
	switch err := r.Client.Get(ctx, client.ObjectKeyFromObject(legacy), legacy); {
	case err == nil:
		if legacy.GetDeletionTimestamp() == nil {
			_ = unstructured.SetNestedField(legacy.Object, false, "spec", "advanced", "restoreToOriginalReplicaCount")
			if err := r.Client.Update(ctx, legacy); err != nil && !apierrors.IsNotFound(err) {
				return ws, fmt.Errorf("legacy scaledobject: %w", err)
			}
			if err := r.Client.Delete(ctx, legacy); err != nil && !apierrors.IsNotFound(err) {
				return ws, fmt.Errorf("delete legacy scaledobject: %w", err)
			}
			log.FromContext(ctx).Info("legacy sleep ScaledObject replaced by the process's own", "app", app.Name)
		}
		ws.retry = scalingRetry
		return ws, nil
	case !apierrors.IsNotFound(err) && !isNoMatchKind(err):
		return ws, fmt.Errorf("legacy scaledobject: %w", err)
	}
	ws.cpu = as != nil && r.metricsAPIAvailable()
	so := makeUnstructured(ScaledObjectGVK, webSvc, app.Namespace)
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, so, func() error {
		so.SetLabels(mergeMaps(so.GetLabels(), map[string]string{shpyrdv1.LabelApp: app.Name, shpyrdv1.LabelManagedBy: "shpyrd"}))
		// The add-on's external scaler reads the InterceptorRoute named
		// here (v0.16 contract) and derives the metric from its
		// scalingMetric. CPU is a second trigger when the Metrics Server
		// measures it: the target is the average an instance may use;
		// KEDA follows the trigger that asks for more instances.
		triggers := []interface{}{
			map[string]interface{}{
				"type": "external-push",
				"metadata": map[string]interface{}{
					"scalerAddress":    "keda-add-ons-http-external-scaler.keda:9090",
					"interceptorRoute": app.Name,
				},
			},
		}
		if ws.cpu {
			triggers = append(triggers, map[string]interface{}{
				"type":       "cpu",
				"metricType": "AverageValue",
				"metadata":   map[string]interface{}{"value": cpuTarget(webRes, as.TargetCPU())},
			})
		}
		spec := map[string]interface{}{
			"scaleTargetRef": map[string]interface{}{
				"apiVersion": "apps/v1", "kind": "Deployment", "name": webSvc,
			},
			// Awake, the process has its declared count, or follows its
			// load between the bounds. minReplicaCount is what a wake
			// restores: with it at zero KEDA's HPA woke an app to one
			// instance whatever it declared (found on dev with two).
			"minReplicaCount": minR,
			"maxReplicaCount": maxR,
			"triggers":        triggers,
			"advanced": map[string]interface{}{
				// The controller writes the count back itself when the
				// object goes (mutateDeployment); KEDA restoring the count
				// it found would race it, and wake a sleeping app.
				"restoreToOriginalReplicaCount": false,
				"horizontalPodAutoscalerConfig": map[string]interface{}{
					"behavior": map[string]interface{}{
						"scaleDown": map[string]interface{}{"stabilizationWindowSeconds": scaleDownStabilization},
					},
				},
			},
		}
		if sleepOn {
			// Asleep is zero, after the quiet period without a request. A
			// trigger that has never been active has no cooldown to wait
			// for; without initialCooldownPeriod KEDA scales a freshly
			// enabled app to zero at once instead of after the quiet period.
			spec["idleReplicaCount"] = int64(0)
			spec["cooldownPeriod"] = cooldown
			spec["initialCooldownPeriod"] = cooldown
		}
		so.Object["spec"] = spec
		return controllerutil.SetControllerReference(app, so, r.Scheme)
	}); err != nil {
		if isNoMatchKind(err) {
			log.FromContext(ctx).V(1).Info("sleep or bounds set but keda is not installed", "app", app.Name)
			return webScaling{}, nil
		}
		return ws, fmt.Errorf("scaledobject: %w", err)
	}
	if broken, msg := r.scaledObjectBroken(so); broken {
		reason := "paused: KEDA could not scale this app (" + firstNonEmpty(msg, "ScaledObject not Ready") + "); set sleep or the bounds again to retry"
		r.sleepPaused.Store(app.UID, sleepPause{generation: app.Generation, reason: reason})
		r.Recorder.Event(app, corev1.EventTypeWarning, "ScalingPaused", reason)
		log.FromContext(ctx).Info("scaling paused", "app", app.Name, "reason", reason)
		if err := r.deleteScalingObjects(ctx, app); err != nil {
			return webScaling{}, err
		}
		// Give the web process its declared instances back now rather than
		// on the next event: KEDA may have scaled it to zero.
		dep := &appsv1.Deployment{}
		if err := r.Client.Get(ctx, types.NamespacedName{Name: webSvc, Namespace: app.Namespace}, dep); err == nil {
			if dep.Spec.Replicas == nil || *dep.Spec.Replicas != int32(declared) {
				dep.Spec.Replicas = ptr.To(int32(declared))
				if err := r.Client.Update(ctx, dep); err != nil {
					return webScaling{}, fmt.Errorf("restore replicas after scaling pause: %w", err)
				}
			}
		}
		return webScaling{}, nil
	}
	return ws, nil
}

// deleteLegacyBridge removes the bridge Service of the earlier name once
// the Ingress points at the current one (reconcileWorkloads, after the
// Ingress).
func (r *AppReconciler) deleteLegacyBridge(ctx context.Context, app *shpyrdv1.App) error {
	return r.deleteIfExists(ctx, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: legacyWebSleepService, Namespace: app.Namespace}})
}

// deleteScalingObjects removes every KEDA object of the app, the legacy
// names included.
func (r *AppReconciler) deleteScalingObjects(ctx context.Context, app *shpyrdv1.App) error {
	for _, o := range []client.Object{
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: webInterceptorService, Namespace: app.Namespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: legacyWebSleepService, Namespace: app.Namespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: sleepPageCMName(app), Namespace: app.Namespace}},
	} {
		if err := r.deleteIfExists(ctx, o); err != nil {
			return err
		}
	}
	for _, gvk := range []schema.GroupVersionKind{InterceptorRouteGVK, ScaledObjectGVK} {
		for _, name := range []string{app.Name, legacyScaledObjectName(app), workloadName(app, "web")} {
			u := makeUnstructured(gvk, name, app.Namespace)
			if err := r.Client.Delete(ctx, u); err != nil && !apierrors.IsNotFound(err) && !isNoMatchKind(err) {
				return fmt.Errorf("delete %s %s: %w", gvk.Kind, name, err)
			}
		}
	}
	return nil
}

// kedaHTTPAvailable asks the REST mapper (kept current by discovery) whether
// both KEDA kinds the objects need are installed.
func (r *AppReconciler) kedaHTTPAvailable() bool {
	return r.kindsAvailable(InterceptorRouteGVK, ScaledObjectGVK)
}

// metricsAPIAvailable says the Metrics Server answers for the pods' CPU:
// the CPU trigger has something to read.
func (r *AppReconciler) metricsAPIAvailable() bool {
	return r.kindsAvailable(PodMetricsGVK)
}

func (r *AppReconciler) kindsAvailable(gvks ...schema.GroupVersionKind) bool {
	m := r.Client.RESTMapper()
	if m == nil {
		return false
	}
	for _, gvk := range gvks {
		if _, err := m.RESTMapping(gvk.GroupKind(), gvk.Version); err != nil {
			return false
		}
	}
	return true
}
