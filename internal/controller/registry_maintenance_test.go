package controller

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// The registry's maintenance (shpyrd-spec-002): builds wait for, or run
// again after, the collector's read-only window. The registry is put in and
// out of read-only by the collector's own switch, never by writing the mark.

// maintenanceKit is a reconciler and a collector over one fake cluster that
// has the registry's Deployment.
type maintenanceKit struct {
	r  *AppReconciler
	gc *RegistryGC
	c  client.Client
}

func newMaintenanceKit(t *testing.T, objs ...client.Object) maintenanceKit {
	t.Helper()
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: registryDeploymentName, Namespace: "shpyrd-system", Generation: 1},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "registry", Image: "registry:3"}},
				Volumes:    []corev1.Volume{{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: registryConfigName}}}}},
			}},
		},
		Status: appsv1.DeploymentStatus{ObservedGeneration: 100, Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1},
	}
	cfg := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: registryConfigName, Namespace: "shpyrd-system"},
		Data:       map[string]string{"config.yml": "storage:\n  filesystem:\n    rootdirectory: /var/lib/registry\n"},
	}
	r, c := newTestReconciler(t, append(objs, dep, cfg)...)
	r.Kube = kubefake.NewSimpleClientset()
	gc := &RegistryGC{Client: c, Namespace: "shpyrd-system", Image: "registry:3", RolloutTimeout: 300 * time.Millisecond, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	return maintenanceKit{r: r, gc: gc, c: c}
}

// readOnly switches the registry with the collector's own switch.
func (k maintenanceKit) readOnly(t *testing.T, on bool) {
	t.Helper()
	if err := k.gc.setReadOnly(context.Background(), on); err != nil {
		t.Fatalf("switch read-only=%v: %v", on, err)
	}
}

func (k maintenanceKit) inMaintenance() bool {
	return k.r.registryInMaintenance(context.Background())
}

func (k maintenanceKit) reconcile(t *testing.T, app *shpyrdv1.App) (*shpyrdv1.App, ctrl.Result) {
	t.Helper()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: app.Namespace, Name: app.Name}}
	res, err := k.r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	out := &shpyrdv1.App{}
	if err := k.c.Get(context.Background(), req.NamespacedName, out); err != nil {
		t.Fatal(err)
	}
	return out, res
}

func (k maintenanceKit) kpackImage(t *testing.T, app *shpyrdv1.App) (*unstructured.Unstructured, bool) {
	t.Helper()
	img := &unstructured.Unstructured{}
	img.SetGroupVersionKind(KpackImageGVK)
	err := k.c.Get(context.Background(), types.NamespacedName{Namespace: app.Namespace, Name: app.Name}, img)
	return img, err == nil
}

func (k maintenanceKit) build(t *testing.T, ns, name string) *unstructured.Unstructured {
	t.Helper()
	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(KpackBuildGVK)
	if err := k.c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, b); err != nil {
		t.Fatal(err)
	}
	return b
}

func (k maintenanceKit) jobs(t *testing.T, app *shpyrdv1.App) []batchv1.Job {
	t.Helper()
	var jobs batchv1.JobList
	if err := k.c.List(context.Background(), &jobs, client.InNamespace(app.Namespace)); err != nil {
		t.Fatal(err)
	}
	return jobs.Items
}

func maintenanceGitApp() *shpyrdv1.App {
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "src", Namespace: "app-src", Generation: 1},
		Spec: shpyrdv1.AppSpec{
			Source:    &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: "https://github.com/example/repo"}},
			Processes: map[string]shpyrdv1.Process{"web": {}},
		},
	}
	return pinGit(app, testCommit)
}

const refusedWrite = `Error verifying write access to "10.96.0.50:5000/apps/ws/app": POST https://10.96.0.50:5000/v2/apps/ws/app/blobs/uploads/: unexpected status code 405 Method Not Allowed: Method not allowed`

// failedBuild is a kpack Build that failed in step, with message as kpack's
// Succeeded condition, and the pod whose step exited with an error.
func failedBuild(step, message string) (*unstructured.Unstructured, *corev1.Pod) {
	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(KpackBuildGVK)
	b.SetNamespace("app-src")
	b.SetName("src-build-1")
	_ = unstructured.SetNestedSlice(b.Object, []interface{}{map[string]interface{}{"type": "Succeeded", "status": "False", "message": message}}, "status", "conditions")
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "src-build-1-build-pod", Namespace: "app-src"},
		Status: corev1.PodStatus{InitContainerStatuses: []corev1.ContainerStatus{
			{Name: step, State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1}}},
		}},
	}
	return b, pod
}

// reportFailedBuild makes kpack's Image say its latest Build failed.
func (k maintenanceKit) reportFailedBuild(t *testing.T, app *shpyrdv1.App, message string) {
	t.Helper()
	img, ok := k.kpackImage(t, app)
	if !ok {
		t.Fatal("no kpack Image to report on")
	}
	_ = unstructured.SetNestedField(img.Object, "src-build-1", "status", "latestBuildRef")
	_ = unstructured.SetNestedField(img.Object, img.GetGeneration(), "status", "observedGeneration")
	_ = unstructured.SetNestedSlice(img.Object, []interface{}{map[string]interface{}{"type": "Ready", "status": "False", "message": "Error: Build 'src-build-1' in namespace 'app-src' failed: " + message}}, "status", "conditions")
	if err := k.c.Update(context.Background(), img); err != nil {
		t.Fatal(err)
	}
}

func refusedMessage(step string) string {
	return "Error:  Container " + step + " terminated with error " + refusedWrite + ": For more info use `kubectl logs -n app-src src-build-1-build-pod -c " + step + "`"
}

func checkWords(t *testing.T, what string, texts ...string) {
	t.Helper()
	for _, s := range texts {
		if !strings.Contains(s, "scheduled maintenance") || !strings.Contains(s, "by itself") || strings.Contains(s, "Deploying again") {
			t.Errorf("%s says %q", what, s)
		}
		if bad := PlatformWordingFault(s); bad != "" {
			t.Errorf("%s: %q names %q", what, s, bad)
		}
	}
}

// C1: the registry is in maintenance from the collector's switch to
// read-only until the switch back, and not because its configuration was
// pointed at the read-only copy by hand.
func TestRegistryMaintenanceFollowsTheCollectorsSwitch(t *testing.T) {
	k := newMaintenanceKit(t)
	if k.inMaintenance() {
		t.Fatal("a writable registry is in maintenance")
	}
	k.readOnly(t, true)
	if !k.inMaintenance() {
		t.Error("after the collector's switch to read-only, the registry is not in maintenance")
	}
	k.readOnly(t, false)
	if k.inMaintenance() {
		t.Error("after the switch back, the registry is still in maintenance")
	}
	dep := &appsv1.Deployment{}
	_ = k.c.Get(context.Background(), types.NamespacedName{Namespace: "shpyrd-system", Name: registryDeploymentName}, dep)
	if _, ok := dep.Annotations[shpyrdv1.AnnotationRegistryReadOnlySince]; ok {
		t.Errorf("the mark stays after the switch back: %v", dep.Annotations)
	}

	// By hand: the configuration swapped, no mark.
	dep.Spec.Template.Spec.Volumes[0].ConfigMap.Name = registryReadOnlyConfig
	if err := k.c.Update(context.Background(), dep); err != nil {
		t.Fatal(err)
	}
	if k.inMaintenance() {
		t.Error("a configuration pointed at the read-only copy by hand is maintenance")
	}
}

// C8: a mark older than the bound is a collection that died, not
// maintenance.
func TestRegistryMaintenanceIsBounded(t *testing.T) {
	k := newMaintenanceKit(t)
	k.readOnly(t, true)
	now := time.Now()
	k.r.Now = func() time.Time { return now.Add(RegistryMaintenanceBound - time.Minute) }
	if !k.inMaintenance() {
		t.Error("a mark younger than the bound is not maintenance")
	}
	k.r.Now = func() time.Time { return now.Add(RegistryMaintenanceBound + time.Minute) }
	if k.inMaintenance() {
		t.Error("a mark older than the bound is still maintenance")
	}
}

// An external registry (its own credentials) is not the collector's.
func TestExternalRegistryIsNeverInMaintenance(t *testing.T) {
	k := newMaintenanceKit(t)
	k.readOnly(t, true)
	k.r.Config.RegistrySecret = "shpyrd-registry"
	if k.inMaintenance() {
		t.Error("an external registry is in maintenance")
	}
}

// C2, C4, C6, C10: a buildpacks build is held, then starts by itself.
func TestBuildpacksBuildIsHeldDuringMaintenance(t *testing.T) {
	app := maintenanceGitApp()
	k := newMaintenanceKit(t, app)
	k.readOnly(t, true)

	got, res := k.reconcile(t, app)
	if _, ok := k.kpackImage(t, app); ok {
		t.Fatal("the kpack Image was created during maintenance")
	}
	if got.Status.Phase != shpyrdv1.PhaseBuilding {
		t.Errorf("phase = %s %q", got.Status.Phase, got.Status.Message)
	}
	built := meta.FindStatusCondition(got.Status.Conditions, shpyrdv1.ConditionBuilt)
	if built == nil || built.Status != metav1.ConditionUnknown || built.Reason != shpyrdv1.ReasonRegistryMaintenance {
		t.Errorf("Built = %+v", built)
	}
	if res.RequeueAfter <= 0 {
		t.Error("the held build asks for no new reconcile")
	}
	checkWords(t, "status", got.Status.Message, conditionMessage(got, shpyrdv1.ConditionBuilt), conditionMessage(got, shpyrdv1.ConditionReady))

	// C10: the commit's check is in progress, not failed.
	if w, ok := wantCheck(got, githubReport{}); !ok || w.status != "in_progress" || w.conclusion != "" {
		t.Errorf("check = %+v %v", w, ok)
	}

	// Still held on the next reconcile.
	k.reconcile(t, app)
	if _, ok := k.kpackImage(t, app); ok {
		t.Fatal("the kpack Image was created on a later reconcile of the window")
	}

	// C4: the window closes; the next reconcile starts the build.
	k.readOnly(t, false)
	got, _ = k.reconcile(t, app)
	if _, ok := k.kpackImage(t, app); !ok {
		t.Fatalf("the kpack Image was not created after the window: %s %q", got.Status.Phase, got.Status.Message)
	}
	if built := meta.FindStatusCondition(got.Status.Conditions, shpyrdv1.ConditionBuilt); built == nil || built.Reason == shpyrdv1.ReasonRegistryMaintenance {
		t.Errorf("Built = %+v", built)
	}
}

// A changed source is held too: the Image keeps its previous source.
func TestChangedSourceIsHeldDuringMaintenance(t *testing.T) {
	app := maintenanceGitApp()
	k := newMaintenanceKit(t, app)
	k.reconcile(t, app)
	img, ok := k.kpackImage(t, app)
	if !ok {
		t.Fatal("no kpack Image")
	}
	before, _, _ := unstructured.NestedString(img.Object, "spec", "source", "git", "revision")

	k.readOnly(t, true)
	cur := &shpyrdv1.App{}
	_ = k.c.Get(context.Background(), client.ObjectKeyFromObject(app), cur)
	cur.Annotations[shpyrdv1.AnnotationGitCommit] = "1111111111111111111111111111111111111111"
	if err := k.c.Update(context.Background(), cur); err != nil {
		t.Fatal(err)
	}
	got, _ := k.reconcile(t, app)
	img, _ = k.kpackImage(t, app)
	if after, _, _ := unstructured.NestedString(img.Object, "spec", "source", "git", "revision"); after != before {
		t.Errorf("the Image's source moved during maintenance: %q -> %q", before, after)
	}
	if got.Status.Phase != shpyrdv1.PhaseBuilding {
		t.Errorf("phase = %s %q", got.Status.Phase, got.Status.Message)
	}
	k.readOnly(t, false)
	k.reconcile(t, app)
	img, _ = k.kpackImage(t, app)
	if after, _, _ := unstructured.NestedString(img.Object, "spec", "source", "git", "revision"); after == before {
		t.Error("the Image's source did not move after the window")
	}
}

// C3, C4, C6: a Dockerfile build is held, then starts by itself.
func TestDockerfileBuildIsHeldDuringMaintenance(t *testing.T) {
	app := dockerfileApp()
	k := newMaintenanceKit(t, app)
	k.readOnly(t, true)

	got, res := k.reconcile(t, app)
	if n := len(k.jobs(t, app)); n != 0 {
		t.Fatalf("%d build Jobs during maintenance", n)
	}
	if got.Status.Phase != shpyrdv1.PhaseBuilding {
		t.Errorf("phase = %s %q", got.Status.Phase, got.Status.Message)
	}
	built := meta.FindStatusCondition(got.Status.Conditions, shpyrdv1.ConditionBuilt)
	if built == nil || built.Status != metav1.ConditionUnknown || built.Reason != shpyrdv1.ReasonRegistryMaintenance {
		t.Errorf("Built = %+v", built)
	}
	if res.RequeueAfter <= 0 {
		t.Error("the held build asks for no new reconcile")
	}
	checkWords(t, "status", got.Status.Message, conditionMessage(got, shpyrdv1.ConditionBuilt))

	k.readOnly(t, false)
	k.reconcile(t, app)
	if n := len(k.jobs(t, app)); n != 1 {
		t.Errorf("%d build Jobs after the window, want 1", n)
	}
}

// C9: with no registry Deployment, the project builds as before.
func TestNoRegistryDeploymentBuildsAsBefore(t *testing.T) {
	app := maintenanceGitApp()
	r, c := newTestReconciler(t, app)
	got := runReconcile(t, r, app)
	img := &unstructured.Unstructured{}
	img.SetGroupVersionKind(KpackImageGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: app.Namespace, Name: app.Name}, img); err != nil {
		t.Fatalf("no kpack Image: %v (%s %q)", err, got.Status.Phase, got.Status.Message)
	}
}

// C5, C6: a build refused by the read-only registry waits, and runs again
// once, after the window.
func TestRefusedBuildRunsAgainOnceAfterMaintenance(t *testing.T) {
	for _, step := range []string{"prepare", "export", "completion"} {
		t.Run(step, func(t *testing.T) {
			app := maintenanceGitApp()
			message := refusedMessage(step)
			if step != "prepare" {
				message = "Error:  Container " + step + " terminated with error ERROR: failed to export: exit status 1: For more info use `kubectl logs -n app-src src-build-1-build-pod -c " + step + "`"
			}
			b, pod := failedBuild(step, message)
			k := newMaintenanceKit(t, app, b, pod)
			k.reconcile(t, app) // the Image, before the window
			k.readOnly(t, true)
			k.reportFailedBuild(t, app, message)

			got, res := k.reconcile(t, app)
			if got.Status.Phase != shpyrdv1.PhaseBuilding {
				t.Fatalf("phase = %s %q", got.Status.Phase, got.Status.Message)
			}
			built := meta.FindStatusCondition(got.Status.Conditions, shpyrdv1.ConditionBuilt)
			if built == nil || built.Status == metav1.ConditionFalse || built.Reason != shpyrdv1.ReasonRegistryMaintenance {
				t.Errorf("Built = %+v", built)
			}
			if res.RequeueAfter <= 0 {
				t.Error("the waiting build asks for no new reconcile")
			}
			cur := k.build(t, "app-src", "src-build-1")
			if cur.GetAnnotations()[kpackBuildNeededAnnotation] != "" {
				t.Fatal("a new build was asked for while the registry is read-only")
			}
			checkWords(t, "status", got.Status.Message, conditionMessage(got, shpyrdv1.ConditionBuilt))
			checkWords(t, "KpackFailure", KpackFailure(cur))

			// Reconciled again in the window: nothing triggered.
			k.reconcile(t, app)
			if k.build(t, "app-src", "src-build-1").GetAnnotations()[kpackBuildNeededAnnotation] != "" {
				t.Fatal("a new build was asked for while the registry is read-only")
			}

			// The window closes: one new build.
			k.readOnly(t, false)
			got, _ = k.reconcile(t, app)
			cur = k.build(t, "app-src", "src-build-1")
			if cur.GetAnnotations()[kpackBuildNeededAnnotation] != "true" {
				t.Fatalf("no new build after the window: %v", cur.GetAnnotations())
			}
			if got.Status.Phase != shpyrdv1.PhaseBuilding {
				t.Errorf("phase after the window = %s %q", got.Status.Phase, got.Status.Message)
			}
			asked := cur.GetAnnotations()[shpyrdv1.AnnotationBuildRerun]

			// Not again: take away what kpack would have consumed.
			a := cur.GetAnnotations()
			delete(a, kpackBuildNeededAnnotation)
			cur.SetAnnotations(a)
			if err := k.c.Update(context.Background(), cur); err != nil {
				t.Fatal(err)
			}
			k.reconcile(t, app)
			k.reconcile(t, app)
			cur = k.build(t, "app-src", "src-build-1")
			if cur.GetAnnotations()[kpackBuildNeededAnnotation] != "" || cur.GetAnnotations()[shpyrdv1.AnnotationBuildRerun] != asked {
				t.Errorf("the build was asked for again: %v", cur.GetAnnotations())
			}
		})
	}
}

// If kpack never starts the new build, the failure stands, in its own words
// and not as maintenance.
func TestRerunThatNeverStartsEndsFailed(t *testing.T) {
	app := maintenanceGitApp()
	message := refusedMessage("prepare")
	b, pod := failedBuild("prepare", message)
	k := newMaintenanceKit(t, app, b, pod)
	k.reconcile(t, app)
	k.readOnly(t, true)
	k.reportFailedBuild(t, app, message)
	k.reconcile(t, app)
	k.readOnly(t, false)
	k.reconcile(t, app)

	now := time.Now()
	k.r.Now = func() time.Time { return now.Add(maintenanceRerunGrace + time.Minute) }
	got, _ := k.reconcile(t, app)
	if got.Status.Phase != shpyrdv1.PhaseFailed {
		t.Fatalf("phase = %s %q", got.Status.Phase, got.Status.Message)
	}
	if strings.Contains(got.Status.Message, "scheduled maintenance") || !strings.Contains(got.Status.Message, "not of the app") {
		t.Errorf("message = %q", got.Status.Message)
	}
}

// C7: the same refused build with a registry that is not in maintenance
// ends as it does today.
func TestRefusedBuildOutsideMaintenanceFails(t *testing.T) {
	prepare := func(t *testing.T) (maintenanceKit, *shpyrdv1.App) {
		app := maintenanceGitApp()
		message := refusedMessage("prepare")
		b, pod := failedBuild("prepare", message)
		k := newMaintenanceKit(t, app, b, pod)
		k.reconcile(t, app)
		return k, app
	}
	check := func(t *testing.T, k maintenanceKit, app *shpyrdv1.App) {
		t.Helper()
		k.reportFailedBuild(t, app, refusedMessage("prepare"))
		got, _ := k.reconcile(t, app)
		built := meta.FindStatusCondition(got.Status.Conditions, shpyrdv1.ConditionBuilt)
		if got.Status.Phase != shpyrdv1.PhaseFailed || built == nil || built.Status != metav1.ConditionFalse || built.Reason != "BuildFailed" {
			t.Fatalf("status = %s %q, Built = %+v", got.Status.Phase, got.Status.Message, built)
		}
		if !strings.Contains(got.Status.Message, "a fault of the platform, not of the app") || strings.Contains(got.Status.Message, "scheduled maintenance") {
			t.Errorf("message = %q", got.Status.Message)
		}
		if _, ok := k.build(t, "app-src", "src-build-1").GetAnnotations()[shpyrdv1.AnnotationBuildRerun]; ok {
			t.Error("the build is marked to run again")
		}
	}
	t.Run("never switched", func(t *testing.T) {
		k, app := prepare(t)
		check(t, k, app)
	})
	t.Run("switched by hand", func(t *testing.T) {
		k, app := prepare(t)
		dep := &appsv1.Deployment{}
		_ = k.c.Get(context.Background(), types.NamespacedName{Namespace: "shpyrd-system", Name: registryDeploymentName}, dep)
		dep.Spec.Template.Spec.Volumes[0].ConfigMap.Name = registryReadOnlyConfig
		if err := k.c.Update(context.Background(), dep); err != nil {
			t.Fatal(err)
		}
		check(t, k, app)
	})
	// C8: the collector's mark, past the bound.
	t.Run("mark past the bound", func(t *testing.T) {
		k, app := prepare(t)
		k.readOnly(t, true)
		now := time.Now()
		k.r.Now = func() time.Time { return now.Add(RegistryMaintenanceBound + time.Minute) }
		check(t, k, app)
	})
}

// C8: a pending build is started once the mark is past the bound.
func TestPendingBuildStartsWhenTheMarkIsPastTheBound(t *testing.T) {
	app := maintenanceGitApp()
	k := newMaintenanceKit(t, app)
	k.readOnly(t, true)
	k.reconcile(t, app)
	if _, ok := k.kpackImage(t, app); ok {
		t.Fatal("the build was not held")
	}
	now := time.Now()
	k.r.Now = func() time.Time { return now.Add(RegistryMaintenanceBound + time.Minute) }
	k.reconcile(t, app)
	if _, ok := k.kpackImage(t, app); !ok {
		t.Error("the build is still held past the bound")
	}
}

// C9: the app's own failure, in the build step, is not excused during
// maintenance.
func TestAppFailureDuringMaintenanceStaysFailed(t *testing.T) {
	app := maintenanceGitApp()
	message := "Error:  Container build terminated with error ERROR: failed to build: exit status 1: For more info use `kubectl logs -n app-src src-build-1-build-pod -c build`"
	b, pod := failedBuild("build", message)
	k := newMaintenanceKit(t, app, b, pod)
	k.reconcile(t, app)
	k.readOnly(t, true)
	k.reportFailedBuild(t, app, message)
	got, _ := k.reconcile(t, app)
	if got.Status.Phase != shpyrdv1.PhaseFailed || !strings.HasPrefix(got.Status.Message, "The build failed") || strings.Contains(got.Status.Message, "maintenance") {
		t.Fatalf("status = %s %q", got.Status.Phase, got.Status.Message)
	}
	k.readOnly(t, false)
	k.reconcile(t, app)
	cur := k.build(t, "app-src", "src-build-1")
	if cur.GetAnnotations()[kpackBuildNeededAnnotation] != "" || cur.GetAnnotations()[shpyrdv1.AnnotationBuildRerun] != "" {
		t.Errorf("the app's failure was run again: %v", cur.GetAnnotations())
	}
}

// C9: a project with nothing to build keeps running, and says what it did.
func TestRunningProjectIsUntouchedByMaintenance(t *testing.T) {
	app := sampleApp("idle")
	app.Spec.Image = "10.96.0.50:5000/apps/idle@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	k := newMaintenanceKit(t, app)
	k.reconcile(t, app)
	markDeploymentReady(t, k.c, "app-idle", "idle-web", 1)
	before, _ := k.reconcile(t, app)
	if before.Status.Phase != shpyrdv1.PhaseRunning {
		t.Fatalf("before: %s %q", before.Status.Phase, before.Status.Message)
	}
	k.readOnly(t, true)
	got, _ := k.reconcile(t, app)
	if got.Status.Phase != shpyrdv1.PhaseRunning || got.Status.Message != before.Status.Message {
		t.Errorf("during maintenance: %s %q, before %q", got.Status.Phase, got.Status.Message, before.Status.Message)
	}
}
