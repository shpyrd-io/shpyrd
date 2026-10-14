package controller

import (
	"strings"
	"testing"
	"time"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/registry"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
)

func TestReleaseAndRolloutExplainWaiting(t *testing.T) {
	for _, tc := range []struct {
		name, event, eventMessage, container, containerMessage, schedulingMessage, want string
		unscheduled, oldNode, staleEvent, failed                                        bool
	}{
		{name: "scale up", event: "TriggeredScaleUp", unscheduled: true, want: "new machine is being added"},
		{name: "size limit", event: "NotTriggerScaleUp", eventMessage: "max node group size reached", unscheduled: true, want: "cluster is at its size limit"},
		{name: "unschedulable", event: "NotTriggerScaleUp", eventMessage: "untolerated taint", unscheduled: true, want: "no available machine"},
		{name: "cpu capacity", unscheduled: true, schedulingMessage: "0/2 nodes are available: 1 Insufficient cpu, 1 untolerated taint", want: "Waiting for cluster resources: not enough capacity"},
		{name: "memory capacity", unscheduled: true, schedulingMessage: "Insufficient memory", want: "Waiting for cluster resources: not enough capacity"},
		{name: "pod slots", unscheduled: true, schedulingMessage: "Too many pods", want: "Waiting for cluster resources: not enough capacity"},
		{name: "volume placement", unscheduled: true, schedulingMessage: "pod has unbound immediate PersistentVolumeClaims", want: "Waiting for scheduling:"},
		{name: "new network", event: "FailedCreatePodSandBox", want: "Starting on a new machine"},
		{name: "old network", event: "FailedCreatePodSandBox", oldNode: true, want: "instance's network"},
		{name: "registry trust", event: "Failed", eventMessage: "x509: certificate signed by unknown authority", container: "ImagePullBackOff", want: "trust the platform's registry"},
		{name: "old node pull failure", event: "Failed", container: "ErrImagePull", containerMessage: "x509: certificate signed by unknown authority", oldNode: true, want: "ErrImagePull", failed: true},
		{name: "download", event: "Pulling", want: "Downloading the app's image (1.9 GB)"},
		{name: "stale event", event: "Pulling", staleEvent: true, want: "Starting the instance"},
		{name: "named user", container: "CreateContainerConfigError", containerMessage: "runAsNonRoot and image has non-numeric user (node)", want: "use a numeric USER", failed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, app, _, pod := releaseFixture(t)
			now := time.Now().UTC().Truncate(time.Second)
			r.Now = func() time.Time { return now }
			r.Config.RegistryHost = "registry.test"
			r.ProcessTypes = nil
			// Use the same cached immutable manifest metadata as normal reconciliation.
			imageProcessTypes.mu.Lock()
			old, existed := imageProcessTypes.types["sha256:abc"]
			imageProcessTypes.types["sha256:abc"] = &registry.BuildMetadata{ImageSize: 1_900_000_000}
			imageProcessTypes.mu.Unlock()
			t.Cleanup(func() {
				imageProcessTypes.mu.Lock()
				defer imageProcessTypes.mu.Unlock()
				if existed {
					imageProcessTypes.types["sha256:abc"] = old
				} else {
					delete(imageProcessTypes.types, "sha256:abc")
				}
			})
			pod.CreationTimestamp = metav1.NewTime(now.Add(-2 * time.Minute))
			pod.Status.Phase = corev1.PodPending
			pod.Spec.NodeName = "machine"
			if tc.unscheduled {
				pod.Spec.NodeName = ""
				pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Message: tc.schedulingMessage}}
			}
			reason := tc.container
			if reason == "" {
				reason = "ContainerCreating"
			}
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "app", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason, Message: tc.containerMessage}}}}
			nodeAge := time.Minute
			if tc.oldNode {
				nodeAge = time.Hour
			}
			node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "machine", CreationTimestamp: metav1.NewTime(now.Add(-nodeAge))}}
			event := &corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "progress", Namespace: pod.Namespace}, InvolvedObject: corev1.ObjectReference{Name: pod.Name, UID: pod.UID}, Reason: tc.event, Message: tc.eventMessage, FirstTimestamp: metav1.NewTime(now.Add(-time.Minute)), LastTimestamp: metav1.NewTime(now)}
			if tc.staleEvent {
				event.InvolvedObject.UID = "old-instance"
			}
			r.Kube = kubefake.NewSimpleClientset(node, event)
			if err := r.Create(t.Context(), pod); err != nil {
				t.Fatal(err)
			}
			got := runReconcile(t, r, app)
			if !strings.Contains(got.Status.Message, tc.want) {
				t.Fatalf("status=%+v", got.Status)
			}
			if got.Status.Message != got.Status.Release.Message {
				t.Fatal("app status lost release progress")
			}
			if (got.Status.Phase == shpyrdv1.PhaseFailed) != tc.failed {
				t.Fatalf("phase=%s", got.Status.Phase)
			}
			if !tc.failed && !strings.Contains(got.Status.Message, "Waiting ") {
				t.Fatalf("missing elapsed time: %s", got.Status.Message)
			}

			// The same pod state explains a new instance of the current rollout.
			rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "current", Namespace: pod.Namespace, UID: "current-rs"}, Spec: appsv1.ReplicaSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"rollout": "current"}}}}
			pod.Labels["rollout"] = "current"
			pod.OwnerReferences = []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: ptr.To(true)}}
			if err := r.Update(t.Context(), pod); err != nil {
				t.Fatal(err)
			}
			failing, message := r.processHealth(t.Context(), rs)
			if (failing > 0) != tc.failed || !strings.Contains(message, tc.want) {
				t.Fatalf("rollout failing=%d message=%q", failing, message)
			}
			_, summary := summarizeProcesses(map[string]shpyrdv1.ProcessStatus{"web": {Desired: 1, Reason: message, Failing: failing}})
			if !tc.failed && !strings.Contains(summary, tc.want) {
				t.Fatalf("rollout summary=%s", summary)
			}
		})
	}
}

func TestRunningContainerIgnoresOldWaitingEvents(t *testing.T) {
	r, app, _, pod := releaseFixture(t)
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "app", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.Now()}}}}
	r.Kube = kubefake.NewSimpleClientset(&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "old-pull", Namespace: pod.Namespace}, InvolvedObject: corev1.ObjectReference{Name: pod.Name, UID: pod.UID}, Reason: "Pulling"})
	if err := r.Create(t.Context(), pod); err != nil {
		t.Fatal(err)
	}
	got := runReconcile(t, r, app)
	if !strings.Contains(got.Status.Message, "Running migrate") || strings.Contains(got.Status.Message, "Downloading") {
		t.Fatalf("message=%s", got.Status.Message)
	}
}

func TestNewerPulledEventReplacesPulling(t *testing.T) {
	r, _, _, pod := releaseFixture(t)
	pod.Status.Phase = corev1.PodPending
	now := time.Now()
	events := []*corev1.Event{}
	for i, reason := range []string{"Pulling", "Pulled"} {
		events = append(events, &corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: reason, Namespace: pod.Namespace}, InvolvedObject: corev1.ObjectReference{Name: pod.Name, UID: pod.UID}, Reason: reason, EventTime: metav1.NewMicroTime(now.Add(time.Duration(i) * time.Second))})
	}
	r.Kube = kubefake.NewSimpleClientset(events[0], events[1])
	message, _ := r.podWaiting(t.Context(), pod)
	if strings.Contains(message, "Downloading") {
		t.Fatalf("stale download: %s", message)
	}
}
