package controller

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/registry"
	"github.com/shpyrd-io/shpyrd/pkg/sizes"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func releaseFixture(t *testing.T) (*AppReconciler, *shpyrdv1.App, *batchv1.Job, *corev1.Pod) {
	t.Helper()
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "app-web", Generation: 1},
		Spec: shpyrdv1.AppSpec{Image: "registry.test/app@sha256:abc", Processes: map[string]shpyrdv1.Process{
			"web":     {Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("128Mi")}}},
			"release": {Command: []string{"migrate"}},
		}},
	}
	r, c := newTestReconciler(t, app)
	app = runReconcile(t, r, app)
	job := &batchv1.Job{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: app.Namespace, Name: app.Status.Release.Job}, job); err != nil {
		t.Fatal(err)
	}
	job.UID = "attempt-1"
	if err := c.Update(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: job.Name + "-pod", Namespace: app.Namespace, UID: "pod-1",
		Labels: map[string]string{"job-name": job.Name}, OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: job.UID, Controller: ptr.To(true)}},
	}, Spec: job.Spec.Template.Spec, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	return r, app, job, pod
}

func TestReleaseFailsBeforeJobCondition(t *testing.T) {
	for _, reason := range []string{"OOMKilled", "Evicted", "OOM event", "named USER"} {
		t.Run(reason, func(t *testing.T) {
			ctx := context.Background()
			r, app, job, pod := releaseFixture(t)
			switch reason {
			case "named USER":
				pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "app", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CreateContainerConfigError", Message: "container has runAsNonRoot and image has non-numeric user (node)"}}}}
			case "OOMKilled":
				pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "app", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137}}}}
			case "Evicted":
				pod.Status.Phase, pod.Status.Reason = corev1.PodFailed, "Evicted"
			case "OOM event":
				r.Kube = kubefake.NewSimpleClientset(&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "oom", Namespace: pod.Namespace}, InvolvedObject: corev1.ObjectReference{Name: pod.Name, UID: pod.UID}, Reason: "OOMKilled"})
			}
			if err := r.Create(ctx, pod); err != nil {
				t.Fatal(err)
			}
			got := runReconcile(t, r, app)
			want := "ran out of memory at 128Mi, the size of the web process"
			if reason == "named USER" {
				want = "USER 1000"
			}
			if reason == "Evicted" {
				want = "machine ran short of resources"
			}
			if got.Status.Phase != shpyrdv1.PhaseFailed || !strings.Contains(got.Status.Message, want) {
				t.Fatalf("status = %+v", got.Status)
			}
			if bad := PlatformWordingFault(got.Status.Message); bad != "" {
				t.Fatalf("message names %q", bad)
			}
			if err := r.Get(ctx, client.ObjectKeyFromObject(job), job); err != nil {
				t.Fatal(err)
			}
			if !ptr.Deref(job.Spec.Suspend, false) {
				t.Fatal("failed attempt was not stopped")
			}
			// The diagnosis survives deletion of the stopped instance.
			if err := r.Delete(ctx, pod); err != nil {
				t.Fatal(err)
			}
			// A lost/conflicting App status write must not lose the diagnosis.
			got.Status.Release.State = shpyrdv1.ReleaseRunning
			got.Status.Release.Message = releaseRunningMessage
			if err := r.Status().Update(ctx, got); err != nil {
				t.Fatal(err)
			}
			got = runReconcile(t, r, got)
			if got.Status.Phase != shpyrdv1.PhaseFailed || !strings.Contains(got.Status.Message, want) {
				t.Fatalf("lost diagnosis: %+v", got.Status)
			}
			var deployments appsv1.DeploymentList
			if err := r.List(ctx, &deployments); err != nil {
				t.Fatal(err)
			}
			if len(deployments.Items) != 0 {
				t.Fatal("failed release rolled out")
			}
			// Redeploy can retry an early failure even without JobFailed.
			got.Annotations = mergeMaps(got.Annotations, map[string]string{shpyrdv1.AnnotationRestartedAt: "retry"})
			if err := r.Update(ctx, got); err != nil {
				t.Fatal(err)
			}
			got = runReconcile(t, r, got)
			if got.Status.Release.State != shpyrdv1.ReleaseRunning {
				t.Fatalf("retry = %+v", got.Status.Release)
			}
		})
	}
}

func TestReleaseQuietWarning(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		age          time.Duration
		logStatus    int
		warn         bool
	}{
		{"quiet", "", 6 * time.Minute, 200, true},
		{"too soon", "", 4 * time.Minute, 200, false},
		{"recent output", "migration running\n", 6 * time.Minute, 200, false},
		{"unreadable logs", "", 6 * time.Minute, 503, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, app, _, pod := releaseFixture(t)
			now := time.Now()
			r.Now = func() time.Time { return now }
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "app", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(now.Add(-tc.age))}}}}
			if err := r.Create(context.Background(), pod); err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if strings.HasSuffix(req.URL.Path, "/log") {
					if req.URL.Query().Get("sinceSeconds") != "300" || req.URL.Query().Get("limitBytes") != "1024" {
						t.Errorf("unbounded logs: %s", req.URL)
					}
					w.WriteHeader(tc.logStatus)
					_, _ = io.WriteString(w, tc.output)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"kind":"EventList","apiVersion":"v1","items":[]}`)
			}))
			defer srv.Close()
			k, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
			if err != nil {
				t.Fatal(err)
			}
			r.Kube = k
			got := runReconcile(t, r, app)
			if got.Status.Phase != shpyrdv1.PhaseDeploying {
				t.Fatalf("quiet task failed: %+v", got.Status)
			}
			if strings.Contains(got.Status.Message, "no output for at least 5 minutes") != tc.warn {
				t.Fatalf("message = %q", got.Status.Message)
			}
		})
	}
}

func TestReleaseIgnoresPreviousAttemptPod(t *testing.T) {
	r, app, _, pod := releaseFixture(t)
	pod.OwnerReferences[0].UID = "previous-attempt"
	pod.Status.Reason = "Evicted"
	if err := r.Create(context.Background(), pod); err != nil {
		t.Fatal(err)
	}
	got := runReconcile(t, r, app)
	if got.Status.Phase != shpyrdv1.PhaseDeploying {
		t.Fatalf("stale pod failed release: %+v", got.Status)
	}
}

func TestRuntimeSizeUsedForReleaseAndWeb(t *testing.T) {
	r, app, job, _ := releaseFixture(t)
	// Seed the immutable image metadata cache; registry decoding is tested
	// separately. This is also the path used by remote Git deploys.
	r.ProcessTypes = nil
	r.Config.RegistryHost = "registry.test"
	imageProcessTypes.mu.Lock()
	imageProcessTypes.types["sha256:abc"] = &registry.BuildMetadata{Runtime: "Node.js", User: &registry.ImageUser{UID: 1000, GID: 1000}, ProcessTypes: []string{"web", "release"}}
	imageProcessTypes.mu.Unlock()
	t.Cleanup(func() {
		imageProcessTypes.mu.Lock()
		delete(imageProcessTypes.types, "sha256:abc")
		delete(imageProcessTypes.failed, "sha256:abc")
		imageProcessTypes.mu.Unlock()
	})
	app.Spec.Processes["web"] = shpyrdv1.Process{}
	if err := r.Update(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	got := runReconcile(t, r, app)
	if got.Status.DefaultSize != "shared-m" {
		t.Fatalf("default = %q", got.Status.DefaultSize)
	}
	if err := r.Get(context.Background(), client.ObjectKey{Namespace: app.Namespace, Name: got.Status.Release.Job}, job); err != nil {
		t.Fatal(err)
	}
	if got := job.Spec.Template.Spec.Containers[0].Resources.Limits.Memory().String(); got != "256Mi" {
		t.Fatalf("release memory = %q", got)
	}
	assertNonRootIdentity(t, job.Spec.Template.Spec.Containers[0].SecurityContext, 1000)
	if len(got.Status.ImageUsers) != 1 || got.Status.ImageUsers[0].Image != app.Spec.Image {
		t.Fatalf("image users = %+v", got.Status.ImageUsers)
	}
	// An unchanged reconcile must not replace the migration with a new target.
	target := got.Status.Release.Target
	got = runReconcile(t, r, got)
	if got.Status.Release.Target != target {
		t.Fatal("runtime default changed the release target on the next reconcile")
	}
	job.Status.Succeeded = 1
	if err := r.Status().Update(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	got = runReconcile(t, r, got)
	var deployment appsv1.Deployment
	if err := r.Get(context.Background(), types.NamespacedName{Name: "web-web", Namespace: app.Namespace}, &deployment); err != nil {
		t.Fatal(err)
	}
	assertNonRootIdentity(t, deployment.Spec.Template.Spec.Containers[0].SecurityContext, 1000)
	if got := deployment.Spec.Template.Spec.Containers[0].Resources.Limits.Memory().String(); got != "256Mi" {
		t.Fatalf("web memory = %q", got)
	}
	// A temporary registry failure must not strip a known identity from an
	// already deployed image and make the next rollout refuse to start.
	imageProcessTypes.mu.Lock()
	imageProcessTypes.types["sha256:abc"].User = nil
	imageProcessTypes.types["sha256:abc"].UserError = io.ErrUnexpectedEOF
	imageProcessTypes.failed["sha256:abc"] = time.Now()
	imageProcessTypes.mu.Unlock()
	got = runReconcile(t, r, got)
	assertNonRootIdentity(t, imageSecurityContext(got, app.Spec.Image), 1000)
}

func TestReleaseAllocationOverrides(t *testing.T) {
	for _, tc := range []struct {
		name    string
		release shpyrdv1.Process
		want    string
	}{
		{"command inherits web", shpyrdv1.Process{Command: []string{"migrate"}}, "128Mi"},
		{"explicit size", shpyrdv1.Process{Size: "shared-l"}, "512Mi"},
		{"explicit memory", shpyrdv1.Process{Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")}}}, "1Gi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := &shpyrdv1.App{Spec: shpyrdv1.AppSpec{Processes: map[string]shpyrdv1.Process{
				"web": {Size: "shared-m", Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("128Mi")}}}, "release": tc.release,
			}}}
			res, _, err := processResources(namedProcess{Name: "release", Process: releaseProcess(app)}, sizes.Defaults())
			if err != nil {
				t.Fatal(err)
			}
			if got := res.Limits.Memory().String(); got != tc.want {
				t.Fatalf("release memory = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestReleaseReportsFailedCreateAndIgnoresStaleEvents(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(map[bool]string{false: "current", true: "previous attempt"}[stale], func(t *testing.T) {
			r, app, job, _ := releaseFixture(t)
			uid := job.UID
			if stale {
				uid = "previous-attempt"
			}
			r.Kube = kubefake.NewSimpleClientset(&corev1.Event{
				ObjectMeta:     metav1.ObjectMeta{Name: "refused", Namespace: job.Namespace},
				InvolvedObject: corev1.ObjectReference{Name: job.Name, UID: uid}, Reason: "FailedCreate", Message: "pods are forbidden", LastTimestamp: metav1.Now(),
			})
			got := runReconcile(t, r, app)
			if !stale && readyReason(got) == "QuotaExceeded" {
				t.Fatal("non-quota refusal reported as quota failure")
			}
			if strings.Contains(got.Status.Message, "platform refused to create its instance") == stale {
				t.Fatalf("message = %q", got.Status.Message)
			}
			if bad := PlatformWordingFault(got.Status.Message); bad != "" {
				t.Fatalf("message names %q", bad)
			}
		})
	}
}

func TestReleaseImagePullGrace(t *testing.T) {
	r, app, job, pod := releaseFixture(t)
	now := time.Now()
	r.Now = func() time.Time { return now }
	pod.CreationTimestamp = metav1.NewTime(now.Add(-10 * time.Second))
	pod.Status.Phase = corev1.PodPending
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "app", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ErrImagePull", Message: "registry unavailable"}}}}
	if err := r.Create(context.Background(), pod); err != nil {
		t.Fatal(err)
	}
	message, failed, err := r.releaseDiagnostic(context.Background(), app, job)
	if err != nil || failed || !strings.Contains(message, "Retrying") {
		t.Fatalf("during grace: %q %v %v", message, failed, err)
	}
	now = now.Add(time.Minute)
	message, failed, err = r.releaseDiagnostic(context.Background(), app, job)
	if err != nil || !failed || !strings.Contains(message, "registry unavailable") {
		t.Fatalf("after grace: %q %v %v", message, failed, err)
	}
}

func TestReleaseDeadlineDoesNotClaimCommandRan(t *testing.T) {
	r, app, job, _ := releaseFixture(t)
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: batchv1.JobReasonDeadlineExceeded}}
	if err := r.Status().Update(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	got := runReconcile(t, r, app)
	if got.Status.Release.State != shpyrdv1.ReleaseFailed || !strings.Contains(got.Status.Message, "did not finish within 30 minutes") || strings.Contains(got.Status.Message, "ran longer") {
		t.Fatalf("status = %+v", got.Status.Release)
	}
}
