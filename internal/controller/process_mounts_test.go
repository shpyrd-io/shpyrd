package controller

import (
	"context"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// #126: a one-off pod gets a process's volumes at their paths, the group
// that lets any image user write them, and the machine that holds a
// single-instance disk right now; a shared volume pins nothing.
func TestAPodTakesAProcessVolumes(t *testing.T) {
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"}, Spec: shpyrdv1.AppSpec{Processes: map[string]shpyrdv1.Process{
		"web":    {Volumes: []shpyrdv1.VolumeMount{{Name: "data", Path: "/data/"}, {Name: "assets", Path: "/srv/assets"}}},
		"worker": {},
	}}}
	volumes := map[string]*shpyrdv1.Volume{
		"data":   {ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "app-shop"}},
		"assets": {ObjectMeta: metav1.ObjectMeta{Name: "assets", Namespace: "app-shop"}, Spec: shpyrdv1.VolumeSpec{AccessMode: corev1.ReadWriteMany}},
	}
	mounts, err := ProcessMounts(app, "web", volumes)
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 2 || mounts[0].Name != "assets" || !mounts[0].Shared || mounts[1].Name != "data" || mounts[1].Path != "/data" || mounts[1].Shared || mounts[1].Claim != "vol-data" {
		t.Fatalf("mounts = %+v", mounts)
	}
	if _, err := ProcessMounts(app, "cron", volumes); err == nil || !strings.Contains(err.Error(), `no process "cron"`) {
		t.Errorf("unknown process: %v", err)
	}
	if _, err := ProcessMounts(app, "web", map[string]*shpyrdv1.Volume{"assets": volumes["assets"]}); err == nil || !strings.Contains(err.Error(), `the volume "data" the process web mounts does not exist`) {
		t.Errorf("missing volume: %v", err)
	}
	if got, _ := ProcessMounts(app, "worker", volumes); len(got) != 0 {
		t.Errorf("a process without volumes mounts none, got %+v", got)
	}

	holder := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-1"}, Spec: corev1.PodSpec{NodeName: "10.0.1.38", Volumes: []corev1.Volume{{Name: "vol-data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "vol-data"}}}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	done := *holder.DeepCopy()
	done.Status.Phase = corev1.PodSucceeded
	if node := ClaimHolderNode([]corev1.Pod{done, holder}, mounts); node != "10.0.1.38" {
		t.Errorf("holder node = %q", node)
	}
	if node := ClaimHolderNode([]corev1.Pod{done}, mounts); node != "" {
		t.Errorf("a finished pod holds nothing, got %q", node)
	}
	if node := ClaimHolderNode([]corev1.Pod{holder}, mounts[:1]); node != "" {
		t.Errorf("a shared volume pins nothing, got %q", node)
	}

	pod := OneOffPod(app, "ghcr.io/acme/shop:1", []string{"rake", "db:migrate"}, corev1.ResourceRequirements{}, false, false, "apps")
	MountVolumesOnPod(&pod.Spec, mounts, "10.0.1.38")
	if len(pod.Spec.Volumes) != 2 || pod.Spec.Volumes[1].PersistentVolumeClaim.ClaimName != "vol-data" {
		t.Errorf("volumes = %+v", pod.Spec.Volumes)
	}
	if vm := pod.Spec.Containers[0].VolumeMounts; len(vm) != 2 || vm[1].MountPath != "/data" || vm[0].MountPath != "/srv/assets" {
		t.Errorf("mounts = %+v", vm)
	}
	if pod.Spec.SecurityContext == nil || ptr.Deref(pod.Spec.SecurityContext.FSGroup, 0) != VolumeGroup {
		t.Errorf("fsGroup = %+v", pod.Spec.SecurityContext)
	}
	if pod.Spec.NodeSelector[corev1.LabelHostname] != "10.0.1.38" || pod.Spec.NodeSelector[PoolLabel] != "apps" {
		t.Errorf("node selector = %v", pod.Spec.NodeSelector)
	}
	if got := MountsNote("web", mounts, "10.0.1.38"); got != "with the volumes of web (assets at /srv/assets, data at /data), on 10.0.1.38, the machine that holds the disk" {
		t.Errorf("note = %q", got)
	}
	if got := MountsNote("worker", nil, ""); got != "the process worker mounts no volume" {
		t.Errorf("empty note = %q", got)
	}
}

// #126: the release step declared with the process's volume sees it, on
// the machine that holds the disk while the process still runs there; the
// release process is not a second user of a single-instance volume.
func TestTheReleaseStepMountsItsVolumes(t *testing.T) {
	ctx := context.Background()
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "app-web", Generation: 1},
		Spec: shpyrdv1.AppSpec{Image: "registry.test/app@sha256:abc", Processes: map[string]shpyrdv1.Process{
			"web":     {Volumes: []shpyrdv1.VolumeMount{{Name: "data", Path: "/data"}}},
			"release": {Command: []string{"migrate"}, Volumes: []shpyrdv1.VolumeMount{{Name: "data", Path: "/data"}}},
		}},
	}
	disk := &shpyrdv1.Volume{ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "app-web"}}
	holder := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-old", Namespace: "app-web"}, Spec: corev1.PodSpec{NodeName: "10.0.1.38", Volumes: []corev1.Volume{{Name: "vol-data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "vol-data"}}}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	r, c := newTestReconciler(t, app, disk, holder)
	got := runReconcile(t, r, app)
	if got.Status.Release == nil || got.Status.Release.Job == "" {
		t.Fatalf("no release job: %+v", got.Status)
	}
	job := &batchv1.Job{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-web", Name: got.Status.Release.Job}, job); err != nil {
		t.Fatal(err)
	}
	spec := job.Spec.Template.Spec
	if len(spec.Volumes) != 1 || spec.Volumes[0].PersistentVolumeClaim.ClaimName != "vol-data" || spec.Containers[0].VolumeMounts[0].MountPath != "/data" {
		t.Errorf("release pod volumes = %+v mounts = %+v", spec.Volumes, spec.Containers[0].VolumeMounts)
	}
	if spec.NodeSelector[corev1.LabelHostname] != "10.0.1.38" {
		t.Errorf("release pod must run where the disk is held, got %v", spec.NodeSelector)
	}
	if ptr.Deref(spec.SecurityContext.FSGroup, 0) != VolumeGroup {
		t.Errorf("fsGroup = %+v", spec.SecurityContext)
	}
	// Two long-running processes on one single-instance volume are still refused.
	twice := app.DeepCopy()
	twice.Spec.Processes["worker"] = shpyrdv1.Process{Command: []string{"work"}, Volumes: []shpyrdv1.VolumeMount{{Name: "data", Path: "/data"}}}
	if _, err := r.resolveMounts(ctx, twice); err == nil || !strings.Contains(err.Error(), "mounted by both") {
		t.Errorf("two processes on one disk: %v", err)
	}
	_ = client.ObjectKeyFromObject
}
