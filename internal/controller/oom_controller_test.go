package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// #119: every out-of-memory kill of a project's container is recorded,
// with what the pod said of itself, and once.

var killedAt = time.Date(2026, 10, 8, 14, 3, 7, 0, time.UTC)

// webPod is a running web instance whose app container the kernel killed
// and the kubelet restarted: the death is in lastState.
func webPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "shop-web-7d9f-l8tzg", Namespace: "p-abc",
			Labels:      map[string]string{shpyrdv1.LabelApp: "shop", shpyrdv1.LabelProcess: "web", shpyrdv1.LabelProjectID: "abc", shpyrdv1.LabelWorkspaceID: "ws-1"},
			Annotations: map[string]string{shpyrdv1.AnnotationInstance: "web.1"},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name:      "app",
			Env:       []corev1.EnvVar{{Name: "SHPYRD_RELEASE", Value: "12"}},
			Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")}},
		}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{
			Name: "app", RestartCount: 1,
			State:                corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(killedAt.Add(12 * time.Second))}},
			LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 137, Reason: "OOMKilled", FinishedAt: metav1.NewTime(killedAt)}},
		}}},
	}
}

func newOOMRecorder(t *testing.T, objs ...client.Object) (*OOMRecorder, *store.Memory, client.Client) {
	t.Helper()
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	st := store.NewMemory()
	r := &OOMRecorder{Client: c, Store: st, Kube: k8sfake.NewSimpleClientset(), SystemNamespace: "shpyrd-system", Now: func() time.Time { return killedAt.Add(time.Minute) }}
	return r, st, c
}

func recordPod(t *testing.T, r *OOMRecorder, pod *corev1.Pod) {
	t.Helper()
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
}

func allKills(t *testing.T, st store.OOMKills) []store.OOMKill {
	t.Helper()
	kills, err := st.QueryOOMKills(context.Background(), store.OOMKillQuery{})
	if err != nil {
		t.Fatal(err)
	}
	return kills
}

func TestOOMKillOfARestartedInstanceIsRecordedOnce(t *testing.T) {
	pod := webPod()
	r, st, _ := newOOMRecorder(t, pod)
	if !r.watches(pod) {
		t.Fatal("a pod showing a kill is watched")
	}
	recordPod(t, r, pod)
	recordPod(t, r, pod) // the next status change reports the same death
	kills := allKills(t, st)
	if len(kills) != 1 {
		t.Fatalf("kills = %+v", kills)
	}
	want := store.OOMKill{WorkspaceID: "ws-1", Project: "abc", Namespace: "p-abc", Pod: pod.Name, Container: "app", Process: "web", Instance: "web.1", At: killedAt, LimitBytes: 256 << 20, Release: 12}
	if kills[0] != want {
		t.Fatalf("kill = %+v\nwant %+v", kills[0], want)
	}
	// The project's Activity hears of it: once for the burst.
	events, err := r.Kube.CoreV1().Events("p-abc").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events.Items) != 1 || !strings.Contains(events.Items[0].Message, "process.oom_killed web: web.1 was killed: out of memory at its 256Mi by platform") {
		t.Fatalf("audit = %+v", events.Items)
	}
}

func TestEveryOOMKillOfAnInstanceIsKept(t *testing.T) {
	pod := webPod()
	r, st, c := newOOMRecorder(t, pod)
	recordPod(t, r, pod)
	// The container dies again two minutes later: lastState moves on.
	pod.Status.ContainerStatuses[0].RestartCount = 2
	pod.Status.ContainerStatuses[0].LastTerminationState.Terminated.FinishedAt = metav1.NewTime(killedAt.Add(2 * time.Minute))
	if err := c.Status().Update(context.Background(), pod); err != nil {
		t.Fatal(err)
	}
	recordPod(t, r, pod)
	kills := allKills(t, st)
	if len(kills) != 2 || !kills[0].At.Equal(killedAt.Add(2*time.Minute)) || !kills[1].At.Equal(killedAt) {
		t.Fatalf("kills = %+v", kills)
	}
	// Two deaths in a minute are one burst in the Activity.
	events, _ := r.Kube.CoreV1().Events("p-abc").List(context.Background(), metav1.ListOptions{})
	if len(events.Items) != 1 {
		t.Fatalf("audit entries = %d", len(events.Items))
	}
}

func TestOOMKillOfAJobPodIsRecordedWithItsRole(t *testing.T) {
	pod := webPod()
	pod.Name, pod.Labels[shpyrdv1.LabelProcess] = "shop-release-v13-x8k2", releaseProcessType
	delete(pod.Annotations, shpyrdv1.AnnotationInstance)
	pod.Spec.Containers[0].Env[0].Value = "13"
	// A Job's container is not restarted: the death is its state.
	pod.Status.Phase = corev1.PodFailed
	pod.Status.ContainerStatuses[0] = corev1.ContainerStatus{Name: "app", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 137, Reason: "OOMKilled", FinishedAt: metav1.NewTime(killedAt)}}}
	r, st, _ := newOOMRecorder(t, pod)
	recordPod(t, r, pod)
	kills := allKills(t, st)
	if len(kills) != 1 || kills[0].Process != "release" || kills[0].Instance != "" || kills[0].Release != 13 {
		t.Fatalf("kills = %+v", kills)
	}
	events, _ := r.Kube.CoreV1().Events("p-abc").List(context.Background(), metav1.ListOptions{})
	if len(events.Items) != 1 || !strings.Contains(events.Items[0].Message, "the release step was killed: out of memory at its 256Mi") {
		t.Fatalf("audit = %+v", events.Items)
	}

	// A Dockerfile build carries the build label and no process.
	build := webPod()
	build.Name = "shop-build-4-q1"
	delete(build.Labels, shpyrdv1.LabelProcess)
	build.Labels[shpyrdv1.LabelBuild] = "shop-build-4"
	build.Status.ContainerStatuses[0].Name = buildContainer
	build.Spec.Containers[0].Name = buildContainer
	if got := oomKillsOf(build); len(got) != 1 || got[0].Process != "build" || got[0].Container != buildContainer {
		t.Fatalf("build kills = %+v", got)
	}
}

func TestExit137WithoutTheReasonIsNoOOMKill(t *testing.T) {
	// A liveness probe's SIGKILL, or a scale-down: 137 but not OOMKilled.
	probe := webPod()
	probe.Name = "shop-web-7d9f-probe"
	probe.Status.ContainerStatuses[0].LastTerminationState.Terminated.Reason = "Error"
	// A kill the kubelet gave no time to cannot be told from the next.
	timeless := webPod()
	timeless.Name = "shop-web-7d9f-timeless"
	timeless.Status.ContainerStatuses[0].LastTerminationState.Terminated.FinishedAt = metav1.Time{}
	// A pod of nobody's: no app label, not the platform's namespace.
	stranger := webPod()
	stranger.Namespace = "kube-system"
	delete(stranger.Labels, shpyrdv1.LabelApp)
	r, st, _ := newOOMRecorder(t, probe, timeless, stranger)
	for _, pod := range []*corev1.Pod{probe, timeless, stranger} {
		if r.watches(pod) {
			t.Errorf("%s is watched", pod.Name)
		}
		recordPod(t, r, pod)
	}
	if kills := allKills(t, st); len(kills) != 0 {
		t.Fatalf("kills = %+v", kills)
	}
}

func TestOOMKillOfAPlatformComponentIsTheOperators(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-backup-29330123-q7x", Namespace: "shpyrd-system", Labels: map[string]string{"job-name": "platform-backup-29330123"}},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "backup", Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("512Mi")}}}}},
		Status: corev1.PodStatus{Phase: corev1.PodFailed, ContainerStatuses: []corev1.ContainerStatus{{
			Name: "backup", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 137, Reason: "OOMKilled", FinishedAt: metav1.NewTime(killedAt)}},
		}}},
	}
	r, st, _ := newOOMRecorder(t, pod)
	if !r.watches(pod) {
		t.Fatal("the platform's own pods are watched")
	}
	recordPod(t, r, pod)
	kills := allKills(t, st)
	want := store.OOMKill{Namespace: "shpyrd-system", Pod: pod.Name, Container: "backup", Process: "platform-backup", At: killedAt, LimitBytes: 512 << 20}
	if len(kills) != 1 || kills[0] != want {
		t.Fatalf("kills = %+v\nwant %+v", kills, want)
	}
	// No project to tell: nothing in any Activity.
	events, _ := r.Kube.CoreV1().Events("shpyrd-system").List(context.Background(), metav1.ListOptions{})
	if len(events.Items) != 0 {
		t.Fatalf("audit = %+v", events.Items)
	}
}

func TestOOMRecorderWithoutAStoreDoesNothing(t *testing.T) {
	pod := webPod()
	r, _, _ := newOOMRecorder(t, pod)
	r.Store = nil
	recordPod(t, r, pod)
}
