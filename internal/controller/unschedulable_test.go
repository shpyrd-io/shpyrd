package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// #127: an instance no machine is large enough for is said in words, at
// once, and counts as a failure; one waiting for room past the grace while
// the autoscaler refuses to add a machine as well; one a machine is on its
// way for is a wait.
func TestUnschedulableInstancesAreSaidInWords(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	node := func(name, cpu, mem string, labels map[string]string) *corev1.Node {
		return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}, Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpu), corev1.ResourceMemory: resource.MustParse(mem)}}}
	}
	pod := func(cpu, mem string, selector map[string]string, age time.Duration, condition string) *corev1.Pod {
		p := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "app-shop", UID: "pod-1", Labels: map[string]string{shpyrdv1.LabelProcess: "web"}, CreationTimestamp: metav1.NewTime(now.Add(-age))},
			Spec:       corev1.PodSpec{NodeSelector: selector, Containers: []corev1.Container{{Name: "app", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpu), corev1.ResourceMemory: resource.MustParse(mem)}}}}},
			Status:     corev1.PodStatus{Phase: corev1.PodPending},
		}
		if condition != "" {
			p.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Message: condition}}
		}
		return p
	}
	event := func(reason string) *corev1.Event {
		return &corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: reason, Namespace: "app-shop"}, InvolvedObject: corev1.ObjectReference{Name: "web-1", UID: "pod-1"}, Reason: reason, FirstTimestamp: metav1.NewTime(now.Add(-time.Minute)), LastTimestamp: metav1.NewTime(now)}
	}
	for _, tc := range []struct {
		name  string
		pod   *corev1.Pod
		objs  []runtime.Object
		fits  string // noMachineFits
		words string // schedulingHopeless
	}{
		{"too many cores", pod("2", "1Gi", nil, time.Minute, "0/3 nodes are available: 3 Insufficient cpu"), []runtime.Object{node("a", "1830m", "8Gi", nil), node("b", "1830m", "8Gi", nil)},
			"web cannot start: it asks for 2 cores; no machine here has more than 1.83 cores. Choose a smaller size.", "web cannot start: it asks for 2 cores"},
		{"too much memory", pod("500m", "16Gi", nil, time.Minute, "Insufficient memory"), []runtime.Object{node("a", "1830m", "8Gi", nil)},
			"web cannot start: it asks for 16Gi; no machine here has more than 8Gi. Choose a smaller size.", "it asks for 16Gi"},
		{"both", pod("4", "32Gi", nil, time.Minute, "Insufficient cpu, Insufficient memory"), []runtime.Object{node("a", "1830m", "8Gi", nil)},
			"web cannot start: it asks for 4 cores and 32Gi; no machine here has more than 1.83 cores and 8Gi. Choose a smaller size.", "4 cores and 32Gi"},
		{"fits a machine", pod("1", "1Gi", nil, time.Minute, "Insufficient cpu"), []runtime.Object{node("a", "1830m", "8Gi", nil)}, "", ""},
		{"only its pool counts", pod("1", "1Gi", map[string]string{PoolLabel: "apps"}, time.Minute, "Insufficient cpu"), []runtime.Object{node("big", "8", "32Gi", map[string]string{PoolLabel: "data"}), node("small", "500m", "2Gi", map[string]string{PoolLabel: "apps"})},
			"web cannot start: it asks for 1 core; no machine here has more than 0.5 cores. Choose a smaller size.", "1 core"},
		{"pinned to its disk's machine", pod("2", "1Gi", map[string]string{corev1.LabelHostname: "10.0.1.38"}, time.Minute, "Insufficient cpu"), []runtime.Object{node("10.0.1.38", "1830m", "8Gi", map[string]string{corev1.LabelHostname: "10.0.1.38"}), node("other", "8", "32Gi", map[string]string{corev1.LabelHostname: "other"})},
			"web cannot start on 10.0.1.38, the machine that holds its disk: it asks for 2 cores; that machine has 1.83 cores. Choose a smaller size, or move the disk.", "the machine that holds its disk"},
		{"unsized machines say nothing", pod("2", "1Gi", nil, time.Minute, "Insufficient cpu"), []runtime.Object{&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "fresh"}}}, "", ""},
		{"room may come: young", pod("1", "1Gi", nil, time.Minute, "Insufficient cpu"), []runtime.Object{node("a", "1830m", "8Gi", nil), event("NotTriggerScaleUp")}, "", ""},
		{"room is not coming", pod("1", "1Gi", nil, 6*time.Minute, "0/2 nodes are available: 2 Insufficient cpu"), []runtime.Object{node("a", "1830m", "8Gi", nil), event("NotTriggerScaleUp")}, "",
			"web cannot start: no machine has room for it and the cluster is not adding one (0/2 nodes are available: 2 Insufficient cpu). Choose a smaller size, or ask the operator for capacity."},
		{"no autoscaler at all", pod("1", "1Gi", nil, 6*time.Minute, "Insufficient cpu"), []runtime.Object{node("a", "1830m", "8Gi", nil)}, "", "the cluster is not adding one"},
		{"a machine is on its way", pod("1", "1Gi", nil, 6*time.Minute, "Insufficient cpu"), []runtime.Object{node("a", "1830m", "8Gi", nil), event("TriggeredScaleUp")}, "", ""},
		{"not a capacity problem", pod("1", "1Gi", nil, 6*time.Minute, "pod has unbound immediate PersistentVolumeClaims"), []runtime.Object{node("a", "1830m", "8Gi", nil)}, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newTestReconciler(t)
			r.Now = func() time.Time { return now }
			r.Kube = kubefake.NewSimpleClientset(tc.objs...)
			if got := r.noMachineFits(context.Background(), tc.pod); got != tc.fits {
				t.Errorf("noMachineFits = %q, want %q", got, tc.fits)
			}
			got := r.schedulingHopeless(context.Background(), tc.pod)
			if (tc.words == "" && got != "") || (tc.words != "" && !strings.Contains(got, tc.words)) {
				t.Errorf("schedulingHopeless = %q, want %q", got, tc.words)
			}
		})
	}
	for q, want := range map[string]string{"2": "2 cores", "1830m": "1.83 cores", "1": "1 core", "500m": "0.5 cores", "250m": "0.25 cores"} {
		if got := coreWords(resource.MustParse(q)); got != want {
			t.Errorf("coreWords(%s) = %q, want %q", q, got, want)
		}
	}
}

// #127: a Recreate rollout (a single-instance volume) whose new instance
// no machine can hold puts the previous release back, says so, and lets
// the spec be changed; a change to the spec starts over from the spec.
func TestRecreateRolloutFallsBackWhenUnschedulable(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop", Generation: 2},
		Spec: shpyrdv1.AppSpec{Image: "ghcr.io/acme/shop:2", Processes: map[string]shpyrdv1.Process{"web": {
			Port: ptr.To[int32](8080), Volumes: []shpyrdv1.VolumeMount{{Name: "data", Path: "/data"}},
			Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("1Gi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("1Gi")}},
		}}},
		Status: shpyrdv1.AppStatus{Releases: []shpyrdv1.Release{
			{Number: 1, Image: "ghcr.io/acme/shop:1", Sizes: map[string]string{"web": "shared-s"}},
			{Number: 2, Image: "ghcr.io/acme/shop:2", Sizes: map[string]string{"web": "shared-s"}},
		}},
	}
	disk := &shpyrdv1.Volume{ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "app-shop"}}
	r, c := newTestReconciler(t, app, disk)
	r.Now = func() time.Time { return now }
	r.Kube = kubefake.NewSimpleClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "small"}, Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1830m"), corev1.ResourceMemory: resource.MustParse("8Gi")}}})
	got := runReconcile(t, r, app)
	d := &appsv1.Deployment{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-shop", Name: "shop-web"}, d); err != nil {
		t.Fatal(err)
	}
	if d.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType || d.Spec.Template.Spec.Containers[0].Image != "ghcr.io/acme/shop:2" {
		t.Fatalf("first render: strategy %s image %s", d.Spec.Strategy.Type, d.Spec.Template.Spec.Containers[0].Image)
	}
	// The Deployment controller's ReplicaSet for that template, and its one
	// instance, which no machine can hold.
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "shop-web-abc", Namespace: "app-shop", UID: "rs-1", Labels: d.Spec.Selector.MatchLabels,
		OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: d.Name, UID: d.UID, Controller: ptr.To(true)}}},
		Spec: appsv1.ReplicaSetSpec{Selector: d.Spec.Selector, Template: *d.Spec.Template.DeepCopy()}}
	if err := c.Create(ctx, rs); err != nil {
		t.Fatal(err)
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "shop-web-abc-1", Namespace: "app-shop", UID: "pod-1", Labels: d.Spec.Template.Labels, CreationTimestamp: metav1.NewTime(now.Add(-time.Minute)),
		OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: ptr.To(true)}}},
		Spec:   *d.Spec.Template.Spec.DeepCopy(),
		Status: corev1.PodStatus{Phase: corev1.PodPending, Conditions: []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Message: "0/1 nodes are available: 1 Insufficient cpu"}}}}
	if err := c.Create(ctx, pod); err != nil {
		t.Fatal(err)
	}
	got = runReconcile(t, r, got)
	fb := got.Status.Fallback
	// The previous release is v2 at its recorded size (the failing one is
	// the spec's 2 cores on the same image, recorded as v3).
	if fb == nil || fb.Process != "web" || fb.Release != 2 || fb.Generation != 2 {
		t.Fatalf("fallback = %+v", fb)
	}
	if got.Status.Phase != shpyrdv1.PhaseFailed || !strings.Contains(got.Status.Message, "web cannot start: it asks for 2 cores") || !strings.Contains(got.Status.Message, "The previous release (v2) runs again meanwhile") {
		t.Fatalf("status = %s %q", got.Status.Phase, got.Status.Message)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(d), d); err != nil {
		t.Fatal(err)
	}
	ct := d.Spec.Template.Spec.Containers[0]
	if ct.Image != "ghcr.io/acme/shop:2" || ct.Resources.Requests.Cpu().String() == "2" {
		t.Fatalf("fallback render: image %s cpu %s, want the previous release at its size", ct.Image, ct.Resources.Requests.Cpu())
	}
	// The failure stays visible while the previous release runs, pass after pass.
	got = runReconcile(t, r, got)
	if got.Status.Phase != shpyrdv1.PhaseFailed || got.Status.Fallback == nil {
		t.Fatalf("second pass: %s fallback=%v", got.Status.Phase, got.Status.Fallback)
	}
	// A change to the spec (a smaller size) starts over from the spec.
	fresh := &shpyrdv1.App{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(app), fresh); err != nil {
		t.Fatal(err)
	}
	p := fresh.Spec.Processes["web"]
	p.Resources, p.Size = corev1.ResourceRequirements{}, "shared-s"
	fresh.Spec.Processes["web"] = p
	fresh.Generation = 3
	if err := c.Update(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	got = runReconcile(t, r, fresh)
	if got.Status.Fallback != nil {
		t.Errorf("a spec change must clear the fallback, got %+v", got.Status.Fallback)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(d), d); err != nil {
		t.Fatal(err)
	}
	if img := d.Spec.Template.Spec.Containers[0].Image; img != "ghcr.io/acme/shop:2" {
		t.Errorf("after the change the spec's image rolls out, got %s", img)
	}
}
