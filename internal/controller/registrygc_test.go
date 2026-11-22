package controller

import (
	"context"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestRegistryGarbageCollectionUsesTheConfiguredStorage(t *testing.T) {
	g := &RegistryGC{Namespace: "shpyrd-system", Image: "registry:3"}
	job := g.job("registry-node")
	pod := &job.Spec.Template.Spec
	if pod.EnableServiceLinks == nil || *pod.EnableServiceLinks {
		t.Error("GC must disable registry service discovery environment variables")
	}
	if pod.NodeName != "registry-node" || pod.Volumes[0].PersistentVolumeClaim.ClaimName != "registry-data" {
		t.Fatal("filesystem GC needs the registry's volume and node")
	}
	g.useBucket(job)
	if pod.NodeName != "" {
		t.Error("bucket GC should run on any node")
	}
	for _, v := range pod.Volumes {
		if v.PersistentVolumeClaim != nil {
			t.Error("bucket GC still needs a PVC")
		}
	}
	c := pod.Containers[0]
	if strings.Contains(strings.Join(c.Command, " "), "du -") {
		t.Error("bucket GC measures a local directory")
	}
	if len(c.Env) != 2 {
		t.Fatal("missing S3 credentials")
	}
	for _, e := range c.Env {
		if e.ValueFrom.SecretKeyRef.Name != "registry-s3" || e.ValueFrom.SecretKeyRef.Key != e.Name {
			t.Errorf("wrong credential reference: %+v", e)
		}
	}
}

// registryFixture is the registry as the installer leaves it: config on a
// filesystem volume, the Deployment at replicas with the status given.
func registryFixture(t *testing.T, replicas int32, status appsv1.DeploymentStatus) (*RegistryGC, client.Client) {
	t.Helper()
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: registryDeploymentName, Namespace: "shpyrd-system", Generation: 1},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(replicas),
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "registry", Image: "registry:3"}},
				Volumes:    []corev1.Volume{{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: registryConfigName}}}}},
			}},
		},
		Status: status,
	}
	cfg := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: registryConfigName, Namespace: "shpyrd-system"},
		Data:       map[string]string{"config.yml": "storage:\n  filesystem:\n    rootdirectory: /var/lib/registry\n"},
	}
	c := fake.NewClientBuilder().WithObjects(dep, cfg).WithStatusSubresource(dep).Build()
	return &RegistryGC{Client: c, Namespace: "shpyrd-system", Image: "registry:3", RolloutTimeout: 300 * time.Millisecond}, c
}

func registryConfigOf(t *testing.T, c client.Client) string {
	t.Helper()
	dep := &appsv1.Deployment{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "shpyrd-system", Name: registryDeploymentName}, dep); err != nil {
		t.Fatal(err)
	}
	return dep.Spec.Template.Spec.Volumes[0].ConfigMap.Name
}

// Two replicas on object storage (rolled one at a time) are rolled out
// when both run the new configuration and no old pod is left; the wait
// used to want exactly one replica and timed out every time.
func TestRegistryReadOnlySwitchWaitsForEveryReplica(t *testing.T) {
	g, c := registryFixture(t, 2, appsv1.DeploymentStatus{ObservedGeneration: 100, Replicas: 2, UpdatedReplicas: 2, ReadyReplicas: 2})
	if err := g.setReadOnly(context.Background(), true); err != nil {
		t.Fatalf("two replicas rolled out: %v", err)
	}
	if got := registryConfigOf(t, c); got != registryReadOnlyConfig {
		t.Errorf("config = %s, want the read-only one", got)
	}

	// An old pod still serving (three running for two wanted) is not done.
	g, _ = registryFixture(t, 2, appsv1.DeploymentStatus{ObservedGeneration: 100, Replicas: 3, UpdatedReplicas: 2, ReadyReplicas: 2})
	if err := g.setReadOnly(context.Background(), true); err == nil || !strings.Contains(err.Error(), "did not roll out") {
		t.Errorf("an old pod left: %v", err)
	}
}

// A collection whose switch to read-only times out after changing the
// Deployment still switches it back: the registry never stays read-only
// because a rollout was slow (25 Oct 2026, every build refused for 35 h).
func TestRegistryGarbageCollectionSwitchesBackWhenTheSwitchFails(t *testing.T) {
	g, c := registryFixture(t, 2, appsv1.DeploymentStatus{ObservedGeneration: 100, Replicas: 2, UpdatedReplicas: 1, ReadyReplicas: 1})
	g.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, _, err := g.run(context.Background()); err == nil || !strings.Contains(err.Error(), "read-only=true") {
		t.Fatalf("run = %v, want the switch's timeout", err)
	}
	if got := registryConfigOf(t, c); got != registryConfigName {
		t.Errorf("registry left on %s after a failed switch", got)
	}
}

// gcFailure is how a collector run ends in the tests: the Job's state, and
// the pod it left, if any.
type gcFailure struct {
	job batchv1.JobStatus
	pod *corev1.PodStatus
}

// failingCollector makes every collector Job the run creates end as given,
// as the Job controller and the kubelet would have left it.
func failingCollector(g *RegistryGC, c client.Client, f gcFailure) {
	g.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	// The filesystem registry's pod, whose node the collector must share.
	_ = c.Create(context.Background(), &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "registry-0", Namespace: "shpyrd-system", Labels: map[string]string{"app.kubernetes.io/name": registryDeploymentName}},
		Spec:       corev1.PodSpec{NodeName: "node-1"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	})
	g.Client = interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
		Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if err := cl.Create(ctx, obj, opts...); err != nil {
				return err
			}
			job, ok := obj.(*batchv1.Job)
			if !ok {
				return nil
			}
			job.Status = f.job
			if err := cl.Status().Update(ctx, job); err != nil {
				return err
			}
			if f.pod != nil {
				return cl.Create(ctx, &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: job.Name + "-x", Namespace: job.Namespace, Labels: map[string]string{"job-name": job.Name}},
					Status:     *f.pod,
				})
			}
			return nil
		},
	})
}

func failedJob(reason, message string) batchv1.JobStatus {
	return batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: reason, Message: message}}}
}

func terminated(message string) *corev1.PodStatus {
	return &corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, Message: message}}}}}
}

func runFailing(t *testing.T, f gcFailure) error {
	t.Helper()
	g, c := registryFixture(t, 1, appsv1.DeploymentStatus{ObservedGeneration: 100, Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1})
	failingCollector(g, c, f)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	start := time.Now()
	_, _, _, err := g.run(ctx)
	if err == nil {
		t.Fatal("run succeeded, want the collector's failure")
	}
	if time.Since(start) > 10*time.Second || ctx.Err() != nil {
		t.Fatalf("run waited for the context: %v (after %s)", err, time.Since(start))
	}
	return err
}

// A Job stopped by its deadline has no failed pod to count: it is marked
// by its Failed condition only, and the reason must reach the record.
func TestRegistryGarbageCollectionNamesTheJobFailureReason(t *testing.T) {
	for _, reason := range []string{"DeadlineExceeded", "BackoffLimitExceeded"} {
		err := runFailing(t, gcFailure{job: failedJob(reason, "Job was active longer than specified deadline")})
		if !strings.Contains(err.Error(), reason) {
			t.Errorf("%s: error = %q", reason, err)
		}
	}
}

// A pod that never ran has no termination message: say what it was waiting for.
func TestRegistryGarbageCollectionNamesAPodThatNeverStarted(t *testing.T) {
	unscheduled := &corev1.PodStatus{Phase: corev1.PodPending, Conditions: []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: "Unschedulable", Message: "0/1 nodes are available: volume node affinity conflict"}}}
	err := runFailing(t, gcFailure{job: failedJob("DeadlineExceeded", "late"), pod: unscheduled})
	if !strings.Contains(err.Error(), "Unschedulable") || !strings.Contains(err.Error(), "volume node affinity conflict") {
		t.Errorf("unschedulable pod: %q", err)
	}

	pulling := &corev1.PodStatus{Phase: corev1.PodPending, ContainerStatuses: []corev1.ContainerStatus{{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}}}}
	err = runFailing(t, gcFailure{job: failedJob("DeadlineExceeded", "late"), pod: pulling})
	if !strings.Contains(err.Error(), "ImagePullBackOff") {
		t.Errorf("image pull: %q", err)
	}
}

// The pod's own message is kept next to the Job's reason.
func TestRegistryGarbageCollectionKeepsThePodMessage(t *testing.T) {
	err := runFailing(t, gcFailure{
		job: batchv1.JobStatus{Failed: 1, Conditions: failedJob("BackoffLimitExceeded", "Job has reached the specified backoff limit").Conditions},
		pod: terminated("failed to garbage collect: storage unreachable"),
	})
	for _, want := range []string{"BackoffLimitExceeded", "failed to garbage collect: storage unreachable"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

// A failed run says why in the record and keeps the size the last run
// measured, the registry goes back to read-write and no Job is left.
func TestRegistryGarbageCollectionRecordsAFailure(t *testing.T) {
	g, c := registryFixture(t, 1, appsv1.DeploymentStatus{ObservedGeneration: 100, Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1})
	old := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: registryGCRecordName, Namespace: "shpyrd-system"},
		Data:       map[string]string{"lastRun": "2026-11-01T04:00:00Z", "lastResult": "ok", "usedBytes": "123456", "reclaimedBytes": "10"},
	}
	if err := c.Create(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = "line " + strconv.Itoa(i) + " of the collector's output that goes on and on"
	}
	// Long enough, with the pod's message, to exceed the limit once assembled.
	longJobMessage := strings.Repeat("the Job was stopped\n", 40)
	failingCollector(g, c, gcFailure{job: failedJob("DeadlineExceeded", longJobMessage), pod: terminated(strings.Join(lines, "\n"))})

	before := time.Now().Truncate(time.Second)
	if err := g.Trigger(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The run is over when the record no longer holds the earlier result.
	for deadline := time.Now().Add(15 * time.Second); g.Status(context.Background()).LastResult == "ok"; {
		if time.Now().After(deadline) {
			t.Fatal("the collection did not end")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cm := &corev1.ConfigMap{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "shpyrd-system", Name: registryGCRecordName}, cm); err != nil {
		t.Fatal(err)
	}
	if got := cm.Data["lastResult"]; !strings.Contains(got, "DeadlineExceeded") || strings.Contains(got, "\n") || len([]rune(got)) > 500 {
		t.Errorf("lastResult = %q (%d characters)", got, len([]rune(got)))
	}
	if cm.Data["usedBytes"] != "123456" {
		t.Errorf("usedBytes = %q, want the earlier size", cm.Data["usedBytes"])
	}
	if lastRun, err := time.Parse(time.RFC3339, cm.Data["lastRun"]); err != nil || lastRun.Before(before) || lastRun.After(time.Now()) {
		t.Errorf("lastRun = %q, want the failed run's start", cm.Data["lastRun"])
	}
	st := g.Status(context.Background())
	if st.LastResult != cm.Data["lastResult"] || st.UsedBytes != 123456 {
		t.Errorf("status = %q, %d", st.LastResult, st.UsedBytes)
	}
	if got := registryConfigOf(t, c); got != registryConfigName {
		t.Errorf("registry left on %s", got)
	}
	var jobs batchv1.JobList
	if err := c.List(context.Background(), &jobs); err != nil || len(jobs.Items) != 0 {
		t.Errorf("jobs left: %d, %v", len(jobs.Items), err)
	}
}

// The cut keeps the ellipsis inside the limit, from either end.
func TestOneLineNeverExceedsItsLimit(t *testing.T) {
	long := strings.Repeat("word\n", 1000)
	for _, tail := range []bool{false, true} {
		for _, limit := range []int{2, 3, 4, 300, 500} {
			if got := oneLine(long, limit, tail); len([]rune(got)) > limit || strings.Contains(got, "\n") {
				t.Errorf("oneLine(limit %d, tail %v) = %d characters", limit, tail, len([]rune(got)))
			}
		}
	}
	if got := oneLine("a\nb", 500, false); got != "a b" {
		t.Errorf("short input = %q", got)
	}
}

// A successful run is unchanged: the summary the collector wrote is
// recorded with what it reclaimed.
func TestRegistryGarbageCollectionRecordsASuccess(t *testing.T) {
	g, c := registryFixture(t, 1, appsv1.DeploymentStatus{ObservedGeneration: 100, Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1})
	failingCollector(g, c, gcFailure{job: batchv1.JobStatus{Succeeded: 1}, pod: terminated(`{"before":1000,"after":400}`)})
	result, reclaimed, used, err := g.run(context.Background())
	if err != nil || result != "ok" || reclaimed != 600 || used != 400 {
		t.Fatalf("run = %q, %d, %d, %v", result, reclaimed, used, err)
	}
	started := time.Now()
	g.record(context.Background(), started, result, reclaimed, used)
	st := g.Status(context.Background())
	if st.LastResult != "ok" || st.ReclaimedBytes != 600 || st.UsedBytes != 400 {
		t.Errorf("status = %+v", st)
	}
}

// The collector keeps its limits: the failure is reported, never waited out.
func TestRegistryCollectorJobLimits(t *testing.T) {
	job := (&RegistryGC{Image: "registry:3"}).job("")
	if job.Spec.ActiveDeadlineSeconds == nil || *job.Spec.ActiveDeadlineSeconds != 2700 || job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 0 {
		t.Errorf("limits = %v, %v", job.Spec.ActiveDeadlineSeconds, job.Spec.BackoffLimit)
	}
}
