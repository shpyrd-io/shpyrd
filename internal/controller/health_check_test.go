package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ptr "k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// RFC-0019: probes, rollout strategy and graceful shutdown.

func TestProbesByProcessType(t *testing.T) {
	app := sampleApp("probes")
	app.Spec.Image = "registry.test/p@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	port := ptr.To[int32](9090)
	app.Spec.Processes = map[string]shpyrdv1.Process{
		"web":    {},
		"api":    {Port: port},
		"worker": {},
	}
	r, c := newTestReconciler(t, app)
	runReconcile(t, r, app)

	get := func(name string) *appsv1.Deployment {
		d := &appsv1.Deployment{}
		if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-probes", Name: "probes-" + name}, d); err != nil {
			t.Fatalf("%s Deployment: %v", name, err)
		}
		return d
	}
	container := func(d *appsv1.Deployment) corev1.Container { return d.Spec.Template.Spec.Containers[0] }

	// web: HTTP GET / on 8080.
	web := container(get("web"))
	if rp := web.ReadinessProbe; rp == nil || rp.HTTPGet == nil || rp.HTTPGet.Path != "/" || rp.HTTPGet.Port.IntValue() != 8080 {
		t.Errorf("web readiness = %+v", web.ReadinessProbe)
	}
	if web.LivenessProbe == nil || web.StartupProbe == nil {
		t.Error("web must have liveness and startup probes")
	}
	if web.Lifecycle == nil || web.Lifecycle.PreStop == nil {
		t.Error("web must have a preStop lifecycle hook")
	}
	if web.LivenessProbe.FailureThreshold <= web.ReadinessProbe.FailureThreshold {
		t.Error("liveness must have a higher failure threshold than readiness")
	}
	// web Deployment must use RollingUpdate.
	if d := get("web"); d.Spec.Strategy.Type != appsv1.RollingUpdateDeploymentStrategyType {
		t.Errorf("web strategy = %v", d.Spec.Strategy.Type)
	}
	// terminationGracePeriodSeconds must be set.
	if p := get("web").Spec.Template.Spec.TerminationGracePeriodSeconds; p == nil || *p < 10 {
		t.Errorf("terminationGracePeriodSeconds = %v", p)
	}

	// api (explicit port 9090): TCP.
	api := container(get("api"))
	if rp := api.ReadinessProbe; rp == nil || rp.TCPSocket == nil || rp.TCPSocket.Port.IntValue() != 9090 {
		t.Errorf("api readiness = %+v", api.ReadinessProbe)
	}

	// worker (no port): no probe at all.
	worker := container(get("worker"))
	if worker.ReadinessProbe != nil || worker.LivenessProbe != nil || worker.StartupProbe != nil {
		t.Errorf("worker must have no probes: %+v", worker.ReadinessProbe)
	}
}

func TestCustomHealthCheck(t *testing.T) {
	app := sampleApp("custom")
	app.Spec.Image = "registry.test/c@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	app.Spec.Processes = map[string]shpyrdv1.Process{
		"web": {HealthCheck: &shpyrdv1.HealthCheck{Path: "/healthz", Interval: "5s", GracePeriod: "60s", ShutdownDelay: "10s"}},
	}
	r, c := newTestReconciler(t, app)
	runReconcile(t, r, app)
	dep := &appsv1.Deployment{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-custom", Name: "custom-web"}, dep); err != nil {
		t.Fatal(err)
	}
	ct := dep.Spec.Template.Spec.Containers[0]
	if ct.ReadinessProbe.HTTPGet.Path != "/healthz" || ct.ReadinessProbe.PeriodSeconds != 5 {
		t.Errorf("custom path/interval: %+v", ct.ReadinessProbe)
	}
	if ct.StartupProbe.FailureThreshold != 12 { // 60s / 5s
		t.Errorf("startup failure threshold: %d (want 12)", ct.StartupProbe.FailureThreshold)
	}
	tgp := dep.Spec.Template.Spec.TerminationGracePeriodSeconds
	if tgp == nil || *tgp != 20 { // 10 + 5 + 5
		t.Errorf("terminationGracePeriodSeconds = %v (want 20)", tgp)
	}
	// Disabled: no probes.
	app2 := sampleApp("disabled")
	app2.Spec.Image = "registry.test/d@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	app2.Spec.Processes = map[string]shpyrdv1.Process{
		"web": {HealthCheck: &shpyrdv1.HealthCheck{Disabled: true}},
	}
	r2, c2 := newTestReconciler(t, app2)
	runReconcile(t, r2, app2)
	dep2 := &appsv1.Deployment{}
	if err := c2.Get(context.Background(), types.NamespacedName{Namespace: "app-disabled", Name: "disabled-web"}, dep2); err != nil {
		t.Fatal(err)
	}
	ct2 := dep2.Spec.Template.Spec.Containers[0]
	if ct2.ReadinessProbe != nil || ct2.LivenessProbe != nil {
		t.Errorf("disabled: probes must be nil, got %+v", ct2.ReadinessProbe)
	}
}

func TestRolloutStrategyWithVolume(t *testing.T) {
	app := sampleApp("vol")
	app.Spec.Image = "registry.test/v@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	app.Spec.Processes = map[string]shpyrdv1.Process{"web": {Volumes: []shpyrdv1.VolumeMount{{Name: "data", Path: "/data"}}}}
	vol := testVolume("app-vol", "1Gi", corev1.ReadWriteOnce)
	vol.Namespace = "app-vol"
	vol.Name = "data"
	r, c := newTestReconciler(t, app, vol)
	runReconcile(t, r, app)
	dep := &appsv1.Deployment{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-vol", Name: "vol-web"}, dep); err != nil {
		t.Fatal(err)
	}
	if dep.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType {
		t.Errorf("volume-pinned process must use Recreate, got %v", dep.Spec.Strategy.Type)
	}
}

// A running instance that is not ready yet is starting, not failing, until
// its startup budget (startup probe window + one round of readiness
// failures) has passed.
func TestProcessHealthStartupBudget(t *testing.T) {
	app := sampleApp("grace")
	started := metav1.NewTime(time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC))
	d := rolledOut(app, "registry.test/grace:v1")
	d.Spec.Template.Spec.Containers[0].StartupProbe = &corev1.Probe{PeriodSeconds: 5, FailureThreshold: 6}
	d.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{PeriodSeconds: 10, FailureThreshold: 3}
	rs := replicaSetOf(d, "registry.test/grace:v1", "h1")
	pod := podOf(rs, "grace-web-1", corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: started}})
	pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.ContainersReady, Status: corev1.ConditionFalse, Message: "containers with unready status: [app]"}}
	r, _ := newTestReconciler(t, app, d, rs, pod)

	// 30 s startup + 30 s readiness = 60 s budget.
	r.Now = func() time.Time { return started.Add(20 * time.Second) }
	if failing, reason := r.processHealth(context.Background(), rs); failing != 0 || reason != "" {
		t.Errorf("20s after start: failing=%d reason=%q, want starting", failing, reason)
	}
	r.Now = func() time.Time { return started.Add(59 * time.Second) }
	if failing, _ := r.processHealth(context.Background(), rs); failing != 0 {
		t.Errorf("59s after start: still within budget, got failing=%d", failing)
	}
	r.Now = func() time.Time { return started.Add(75 * time.Second) }
	failing, reason := r.processHealth(context.Background(), rs)
	if failing != 1 || reason != "not ready after 1m15s: readiness probe failing" {
		t.Errorf("75s after start: failing=%d reason=%q", failing, reason)
	}

	// Without probes on the pod the minimum budget applies.
	if got := startupBudget(corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}}}); got != minStartupBudget {
		t.Errorf("budget without probes = %s", got)
	}
	// A crash loop is failing regardless of age.
	pod.Status.ContainerStatuses[0].State = corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}
	r2, _ := newTestReconciler(t, app, d, rs, pod)
	r2.Now = func() time.Time { return started.Add(time.Second) }
	if failing, reason := r2.processHealth(context.Background(), rs); failing != 1 || reason != "CrashLoopBackOff" {
		t.Errorf("crash loop: failing=%d reason=%q", failing, reason)
	}
}

// #94: an instance the node evicted (disk pressure) is dead and already
// replaced; it is not a failing instance of the release.
func TestEvictedInstancesAreNotFailing(t *testing.T) {
	app := sampleApp("evict")
	d := rolledOut(app, "registry.test/evict:v1")
	rs := replicaSetOf(d, "registry.test/evict:v1", "h1")
	evicted := podOf(rs, "evict-web-old", corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 137}})
	evicted.Status.Phase, evicted.Status.Reason = corev1.PodFailed, "Evicted"
	evicted.Status.Message = "The node was low on resource: ephemeral-storage."
	running := podOf(rs, "evict-web-new", corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.Now()}})
	running.Status.ContainerStatuses[0].Ready = true
	r, _ := newTestReconciler(t, app, d, rs, evicted, running)
	if failing, reason := r.processHealth(context.Background(), rs); failing != 0 || reason != "" {
		t.Errorf("evicted instance: failing=%d reason=%q, want none", failing, reason)
	}
	// An instance that failed on its own still counts.
	crashed := podOf(rs, "evict-web-crashed", corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1}})
	crashed.Status.Phase = corev1.PodFailed
	r2, _ := newTestReconciler(t, app, d, rs, evicted, crashed)
	if failing, reason := r2.processHealth(context.Background(), rs); failing != 1 || reason != "exited with code 1" {
		t.Errorf("crashed instance: failing=%d reason=%q", failing, reason)
	}
}

// rolledOut is a web Deployment of app at image.
func rolledOut(app *shpyrdv1.App, image string) *appsv1.Deployment {
	labels := selectorLabels(app, "web")
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: app.Name + "-web", Namespace: app.Namespace, UID: "deploy", Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: image}}},
			},
		},
	}
}

// replicaSetOf is the ReplicaSet the Deployment controller made for d at
// image, with the pod-template-hash it adds to its pods.
func replicaSetOf(d *appsv1.Deployment, image, hash string) *appsv1.ReplicaSet {
	tmpl := *d.Spec.Template.DeepCopy()
	tmpl.Spec.Containers[0].Image = image
	tmpl.Labels = mergeMaps(tmpl.Labels, map[string]string{appsv1.DefaultDeploymentUniqueLabelKey: hash})
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: d.Name + "-" + hash, Namespace: d.Namespace, UID: types.UID("rs-" + hash), Labels: tmpl.Labels,
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: d.Name, UID: d.UID, Controller: ptr.To(true)}},
		},
		Spec: appsv1.ReplicaSetSpec{Selector: d.Spec.Selector, Template: tmpl},
	}
}

// podOf is an instance of rs whose app container is in state.
func podOf(rs *appsv1.ReplicaSet, name string, state corev1.ContainerState) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: rs.Namespace, Labels: rs.Spec.Template.Labels,
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: ptr.To(true)}},
		},
		Spec:   rs.Spec.Template.Spec,
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: "app", State: state}}},
	}
}

// #91: a deploy's outcome follows the instances of the release being
// deployed. The previous release's pods stay until the new ones are ready,
// and their crash loop is not the new release's.
func TestProcessHealthFollowsTheCurrentRelease(t *testing.T) {
	app := sampleApp("rollout")
	d := rolledOut(app, "registry.test/rollout:v2")
	old := replicaSetOf(d, "registry.test/rollout:v1", "v1hash")
	cur := replicaSetOf(d, "registry.test/rollout:v2", "v2hash")
	crashing := corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}
	creating := corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}}

	health := func(objs ...client.Object) (int32, string) {
		t.Helper()
		r, _ := newTestReconciler(t, append([]client.Object{app, d, old}, objs...)...)
		current, err := r.currentReplicaSet(context.Background(), d)
		if err != nil {
			t.Fatal(err)
		}
		return r.processHealth(context.Background(), current)
	}
	if failing, reason := health(cur, podOf(old, "rollout-web-v1", crashing), podOf(cur, "rollout-web-v2", creating)); failing != 0 || reason != "" {
		t.Errorf("v1 crash-looping, v2 starting: failing=%d reason=%q, want v2's health alone", failing, reason)
	}
	if failing, reason := health(cur, podOf(old, "rollout-web-v1", crashing), podOf(cur, "rollout-web-v2", crashing)); failing != 1 || reason != "CrashLoopBackOff" {
		t.Errorf("v2 crash-looping too: failing=%d reason=%q, want v2's crash loop", failing, reason)
	}
	// Before the Deployment controller has made v2's ReplicaSet there is no
	// instance of the new release to judge.
	if failing, reason := health(podOf(old, "rollout-web-v1", crashing)); failing != 0 || reason != "" {
		t.Errorf("v2 not created yet: failing=%d reason=%q, want nothing failing", failing, reason)
	}
}

// #91: while the next release builds, the instances that crash belong to
// the release it replaces. The project reads Building, so a deploy waiting
// on it does not take the old crash loop for the new release's outcome.
func TestBuildingNextReleaseOverPreviousCrashLoop(t *testing.T) {
	ctx := context.Background()
	app := dockerfileApp()
	r, c := newTestReconciler(t, app)
	runReconcile(t, r, app)
	finishBuild(t, c, getJob(t, c, "app-dk", "dk-build-1"), 0, `{"image":"10.96.0.50:5000/apps/dk@`+testDigest+`","revision":""}`)
	runReconcile(t, r, app)

	// v1's instance crash-loops.
	d := &appsv1.Deployment{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-dk", Name: "dk-web"}, d); err != nil {
		t.Fatal(err)
	}
	rs := replicaSetOf(d, d.Spec.Template.Spec.Containers[0].Image, "v1hash")
	if err := c.Create(ctx, rs); err != nil {
		t.Fatal(err)
	}
	if err := c.Create(ctx, podOf(rs, "dk-web-v1", corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}})); err != nil {
		t.Fatal(err)
	}
	got := runReconcile(t, r, app)
	if got.Status.Phase != shpyrdv1.PhaseFailed || !strings.Contains(got.Status.Message, "CrashLoopBackOff") {
		t.Fatalf("v1 crash-looping: phase=%q (%s), want Failed", got.Status.Phase, got.Status.Message)
	}

	// The fix is uploaded: build 2 runs while v1 still crash-loops.
	got.Spec.Source.Blob.SHA256, got.Spec.Source.Blob.Ref = "def456def456def456", "fedcba987654"
	if err := c.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	got = runReconcile(t, r, got)
	if got.Status.LatestBuild != "dk-build-2" || got.Status.Phase != shpyrdv1.PhaseBuilding {
		t.Fatalf("building v2: build=%q phase=%q (%s), want Building", got.Status.LatestBuild, got.Status.Phase, got.Status.Message)
	}
}

// #91, the other way round: a release whose instances crash-loop is not
// running because the previous release's instance is still ready. The
// Deployment's ready count spans both; the release's is its ReplicaSet's.
func TestCrashingReleaseOverReadyPrevious(t *testing.T) {
	ctx := context.Background()
	app := dockerfileApp()
	r, c := newTestReconciler(t, app)
	runReconcile(t, r, app)
	finishBuild(t, c, getJob(t, c, "app-dk", "dk-build-1"), 0, `{"image":"10.96.0.50:5000/apps/dk@`+testDigest+`","revision":""}`)
	runReconcile(t, r, app)
	markDeploymentReady(t, c, "app-dk", "dk-web", 1)
	got := runReconcile(t, r, app)
	if got.Status.Phase != shpyrdv1.PhaseRunning {
		t.Fatalf("v1 ready: phase=%q (%s), want Running", got.Status.Phase, got.Status.Message)
	}

	// v2 builds and rolls out; its instance crash-loops while v1's serves.
	got.Spec.Source.Blob.SHA256, got.Spec.Source.Blob.Ref = "def456def456def456", "fedcba987654"
	if err := c.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	got = runReconcile(t, r, got)
	v2 := "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	finishBuild(t, c, getJob(t, c, "app-dk", "dk-build-2"), 0, `{"image":"10.96.0.50:5000/apps/dk@`+v2+`","revision":""}`)
	runReconcile(t, r, got)
	d := &appsv1.Deployment{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-dk", Name: "dk-web"}, d); err != nil {
		t.Fatal(err)
	}
	if img := d.Spec.Template.Spec.Containers[0].Image; !strings.HasSuffix(img, v2) {
		t.Fatalf("Deployment image = %q, want v2's", img)
	}
	rs := replicaSetOf(d, d.Spec.Template.Spec.Containers[0].Image, "v2hash")
	if err := c.Create(ctx, rs); err != nil {
		t.Fatal(err)
	}
	if err := c.Create(ctx, podOf(rs, "dk-web-v2", corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}})); err != nil {
		t.Fatal(err)
	}
	// What the Deployment reports mid-rollout: v2's instance is updated,
	// v1's is the one ready.
	d.Status.ObservedGeneration = d.Generation
	d.Status.Replicas, d.Status.UpdatedReplicas, d.Status.ReadyReplicas, d.Status.AvailableReplicas = 2, 1, 1, 1
	if err := c.Status().Update(ctx, d); err != nil {
		t.Fatal(err)
	}
	got = runReconcile(t, r, got)
	if got.Status.Phase != shpyrdv1.PhaseFailed || !strings.Contains(got.Status.Message, "CrashLoopBackOff") {
		t.Fatalf("v2 crash-looping beside a ready v1: phase=%q (%s), want Failed", got.Status.Phase, got.Status.Message)
	}
}
