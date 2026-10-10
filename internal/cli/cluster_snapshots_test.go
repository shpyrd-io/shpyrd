package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

func interimClaim(namespace, name, class string, phase corev1.PersistentVolumeClaimPhase) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: ptr.To(class)},
		Status:     corev1.PersistentVolumeClaimStatus{Phase: phase},
	}
}

func interimSnapshotFixture(namespace, claim, name string, interim bool) *unstructured.Unstructured {
	snap := &unstructured.Unstructured{}
	snap.SetGroupVersionKind(controller.VolumeSnapshotGVK)
	snap.SetName(name)
	snap.SetNamespace(namespace)
	if interim {
		snap.SetLabels(map[string]string{LabelInterimSnapshot: "true"})
	}
	_ = unstructured.SetNestedField(snap.Object, claim, "spec", "source", "persistentVolumeClaimName")
	_ = unstructured.SetNestedField(snap.Object, true, "status", "readyToUse")
	return snap
}

// A cluster with a project namespace, an unlabelled one and the platform's:
// disks of two classes, one still unbound, and old interim snapshots of the
// project's volume next to one a person took by hand.
func interimTestClient(t *testing.T) client.Client {
	t.Helper()
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	objs := []client.Object{
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "p-acme", Labels: map[string]string{shpyrdv1.LabelProject: "acme"}}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "shpyrd-system"}},
		interimClaim("p-acme", "vol-data", "oci-bv", corev1.ClaimBound),
		interimClaim("p-acme", "vol-new", "oci-bv", corev1.ClaimPending),
		interimClaim("p-acme", "cache", "shpyrd-local", corev1.ClaimBound),
		interimClaim("other", "vol-x", "oci-bv", corev1.ClaimBound),
		interimClaim("shpyrd-system", "pg-1", "oci-bv", corev1.ClaimBound),
		interimSnapshotFixture("p-acme", "vol-data", "vol-data-20261001-013000", true),
		interimSnapshotFixture("p-acme", "vol-data", "vol-data-20261002-013000", true),
		interimSnapshotFixture("p-acme", "vol-data", "vol-data-20261003-013000", true),
		interimSnapshotFixture("p-acme", "vol-data", "before-migration", false),
		interimSnapshotFixture("p-acme", "vol-new", "vol-new-20261001-013000", true),
	}
	return crfake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func interimSnapshotNames(t *testing.T, c client.Client, namespace string) []string {
	t.Helper()
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(controller.VolumeSnapshotGVK.GroupVersion().WithKind("VolumeSnapshotList"))
	if err := c.List(context.Background(), list, client.InNamespace(namespace)); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range list.Items {
		names = append(names, s.GetName())
	}
	return names
}

var interimNow = func() time.Time { return time.Date(2026, 10, 10, 1, 30, 0, 0, time.UTC) }

func TestInterimSnapshotsRefuseWithoutClass(t *testing.T) {
	var out bytes.Buffer
	res, err := takeInterimSnapshots(context.Background(), &out, interimTestClient(t), interimSnapshotOptions{Class: "oci-bv", Keep: 7})
	if !errors.Is(err, errNoSnapshotClass) || res != nil {
		t.Fatalf("expected the refusal, got %v (%v)", err, res)
	}
	for _, word := range []string{"SHPYRD_", "kubectl", "--", "VolumeSnapshot"} {
		if strings.Contains(err.Error(), word) {
			t.Errorf("refusal names an internal (%q): %s", word, err)
		}
	}
	if _, err := takeInterimSnapshots(context.Background(), &out, interimTestClient(t), interimSnapshotOptions{Class: "oci-bv", SnapshotClass: "oci-bv-backup", Keep: 0}); err == nil {
		t.Error("keep 0 accepted")
	}
}

// Selection by class and project label, the name carries the disk and the
// time, the oldest interim snapshots go beyond --keep (a hand-taken one and
// another disk's stay), an unbound disk is skipped in words.
func TestInterimSnapshotsSelectNameAndKeep(t *testing.T) {
	c := interimTestClient(t)
	var out bytes.Buffer
	res, err := takeInterimSnapshots(context.Background(), &out, c, interimSnapshotOptions{Class: "oci-bv", SnapshotClass: "oci-bv-backup", Keep: 2, Now: interimNow})
	if err != nil {
		t.Fatalf("take: %v\n%s", err, out.String())
	}
	if len(res) != 2 || res[0].Claim != "vol-data" || res[1].Claim != "vol-new" {
		t.Fatalf("selected disks: %+v", res)
	}
	if res[0].Status != "requested" || res[0].Snapshot != "vol-data-20261010-013000" {
		t.Errorf("vol-data: %+v", res[0])
	}
	if res[1].Status != "skipped" || res[1].Snapshot != "" {
		t.Errorf("vol-new: %+v", res[1])
	}
	if got := strings.Join(res[0].Removed, ","); got != "vol-data-20261001-013000,vol-data-20261002-013000" {
		t.Errorf("removed: %s", got)
	}
	names := strings.Join(interimSnapshotNames(t, c, "p-acme"), ",")
	for _, want := range []string{"vol-data-20261010-013000", "vol-data-20261003-013000", "before-migration", "vol-new-20261001-013000"} {
		if !strings.Contains(names, want) {
			t.Errorf("%s missing after the run: %s", want, names)
		}
	}
	for _, gone := range []string{"vol-data-20261001-013000", "vol-data-20261002-013000"} {
		if strings.Contains(names, gone) {
			t.Errorf("%s still there: %s", gone, names)
		}
	}
	if other := interimSnapshotNames(t, c, "other"); len(other) != 0 {
		t.Errorf("unlabelled namespace snapshotted: %v", other)
	}
	if sys := interimSnapshotNames(t, c, "shpyrd-system"); len(sys) != 0 {
		t.Errorf("platform namespace snapshotted without --system: %v", sys)
	}

	snap := &unstructured.Unstructured{}
	snap.SetGroupVersionKind(controller.VolumeSnapshotGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "p-acme", Name: "vol-data-20261010-013000"}, snap); err != nil {
		t.Fatal(err)
	}
	if l := snap.GetLabels(); l[LabelInterimSnapshot] != "true" || l[shpyrdv1.LabelVolumeOf] != "data" || l[shpyrdv1.LabelManagedBy] != "shpyrd" {
		t.Errorf("labels: %v", l)
	}
	if class, _, _ := unstructured.NestedString(snap.Object, "spec", "volumeSnapshotClassName"); class != "oci-bv-backup" {
		t.Errorf("snapshot class: %q", class)
	}
	if src, _, _ := unstructured.NestedString(snap.Object, "spec", "source", "persistentVolumeClaimName"); src != "vol-data" {
		t.Errorf("source: %q", src)
	}

	text := out.String()
	for _, want := range []string{
		"p-acme/vol-data: snapshot vol-data-20261010-013000 requested; it finishes in the background; 2 older removed",
		"p-acme/vol-new: no disk yet, skipped",
		"2 disks of class oci-bv: 1 requested, 1 skipped (no disk yet).",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q:\n%s", want, text)
		}
	}
}

func TestInterimSnapshotsSystemAndPrefix(t *testing.T) {
	c := interimTestClient(t)
	var out bytes.Buffer
	res, err := takeInterimSnapshots(context.Background(), &out, c, interimSnapshotOptions{Class: "oci-bv", SnapshotClass: "oci-bv-backup", Keep: 7, System: true, Now: interimNow})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 3 || res[2].Namespace != "shpyrd-system" || res[2].Snapshot != "pg-1-20261010-013000" {
		t.Fatalf("with --system: %+v", res)
	}
	snap := &unstructured.Unstructured{}
	snap.SetGroupVersionKind(controller.VolumeSnapshotGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "shpyrd-system", Name: "pg-1-20261010-013000"}, snap); err != nil {
		t.Fatal(err)
	}
	if _, ok := snap.GetLabels()[shpyrdv1.LabelVolumeOf]; ok {
		t.Errorf("a platform claim is not a project volume: %v", snap.GetLabels())
	}

	out.Reset()
	res, err = takeInterimSnapshots(context.Background(), &out, interimTestClient(t), interimSnapshotOptions{Class: "oci-bv", SnapshotClass: "oci-bv-backup", Keep: 7, NamespacePrefix: "oth", Now: interimNow})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Namespace != "other" || res[0].Claim != "vol-x" {
		t.Fatalf("with a namespace prefix: %+v", res)
	}

	out.Reset()
	res, err = takeInterimSnapshots(context.Background(), &out, interimTestClient(t), interimSnapshotOptions{Class: "gp3", SnapshotClass: "ebs-snapshot", Keep: 7, Now: interimNow})
	if err != nil || len(res) != 0 || !strings.Contains(out.String(), "No disks of class gp3") {
		t.Errorf("no disks of the class: %v %+v\n%s", err, res, out.String())
	}
}

// markSnapshots plays the snapshot controller: every interim snapshot not
// yet ready gets the given status.
func markSnapshots(ctx context.Context, c client.Client, set func(*unstructured.Unstructured)) {
	for ctx.Err() == nil {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(controller.VolumeSnapshotGVK.GroupVersion().WithKind("VolumeSnapshotList"))
		if err := c.List(ctx, list, client.MatchingLabels{LabelInterimSnapshot: "true"}); err == nil {
			for i := range list.Items {
				s := &list.Items[i]
				if ready, _, _ := unstructured.NestedBool(s.Object, "status", "readyToUse"); ready {
					continue
				}
				if msg, _, _ := unstructured.NestedString(s.Object, "status", "error", "message"); msg != "" {
					continue
				}
				set(s)
				_ = c.Update(ctx, s)
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestInterimSnapshotsWaitReadyOrFail(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := interimTestClient(t)
	go markSnapshots(ctx, c, func(s *unstructured.Unstructured) {
		_ = unstructured.SetNestedField(s.Object, true, "status", "readyToUse")
		_ = unstructured.SetNestedField(s.Object, "50Gi", "status", "restoreSize")
	})
	var out bytes.Buffer
	opts := interimSnapshotOptions{Class: "oci-bv", SnapshotClass: "oci-bv-backup", Keep: 7, System: true, Wait: 5 * time.Second, Poll: 5 * time.Millisecond, Now: interimNow}
	res, err := takeInterimSnapshots(ctx, &out, c, opts)
	if err != nil {
		t.Fatalf("ready: %v\n%s", err, out.String())
	}
	if res[0].Status != "ready" || res[0].Size != "50Gi" || res[2].Status != "ready" {
		t.Errorf("results: %+v", res)
	}
	if !strings.Contains(out.String(), "p-acme/vol-data: snapshot vol-data-20261010-013000 ready (50Gi)") || !strings.Contains(out.String(), "3 disks of class oci-bv: 2 ready, 1 skipped (no disk yet).") {
		t.Errorf("output:\n%s", out.String())
	}
	cancel()

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	c = interimTestClient(t)
	go markSnapshots(ctx, c, func(s *unstructured.Unstructured) {
		_ = unstructured.SetNestedField(s.Object, "the provider refused the backup", "status", "error", "message")
	})
	out.Reset()
	res, err = takeInterimSnapshots(ctx, &out, c, opts)
	if err == nil || !strings.Contains(err.Error(), "2 of 3 snapshots did not complete") {
		t.Fatalf("failures must fail the run: %v\n%s", err, out.String())
	}
	if res[0].Status != "failed" || res[0].Message != "the provider refused the backup" {
		t.Errorf("failed result: %+v", res[0])
	}
	if !strings.Contains(out.String(), "p-acme/vol-data: snapshot vol-data-20261010-013000 failed: the provider refused the backup") {
		t.Errorf("output:\n%s", out.String())
	}
	// A failed snapshot does not prune: the old ones are all still there.
	if names := strings.Join(interimSnapshotNames(t, c, "p-acme"), ","); !strings.Contains(names, "vol-data-20261001-013000") {
		t.Errorf("pruned after a failure: %s", names)
	}

	// The wait runs out: still in progress, and the run fails so the
	// operator looks again.
	out.Reset()
	opts.Wait = 20 * time.Millisecond
	res, err = takeInterimSnapshots(context.Background(), &out, interimTestClient(t), opts)
	if err == nil || res[0].Status != "in-progress" || !strings.Contains(out.String(), "still being taken after") {
		t.Errorf("timeout: %v %+v\n%s", err, res, out.String())
	}
}

func TestInterimSnapshotName(t *testing.T) {
	now := interimNow()
	if got := interimSnapshotName("vol-data", now); got != "vol-data-20261010-013000" {
		t.Errorf("name: %s", got)
	}
	long := strings.Repeat("a", 40) + "-" + strings.Repeat("b", 20)
	got := interimSnapshotName(long, now)
	if len(got) > 63 || !strings.HasSuffix(got, "-20261010-013000") || !strings.HasPrefix(got, strings.Repeat("a", 40)+"-bbbbbb") {
		t.Errorf("long name: %s (%d)", got, len(got))
	}
	// A cut that ends on a dash drops it rather than doubling.
	dash := strings.Repeat("a", 46) + "-zzzz"
	if got := interimSnapshotName(dash, now); got != strings.Repeat("a", 46)+"-20261010-013000" {
		t.Errorf("cut on a dash: %s", got)
	}
}

func TestInterimSnapshotsList(t *testing.T) {
	views, err := listInterimSnapshots(context.Background(), interimTestClient(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 4 || views[0].Name != "vol-new-20261001-013000" || views[1].Name != "vol-data-20261003-013000" || views[3].Name != "vol-data-20261001-013000" {
		t.Fatalf("views: %+v", views)
	}
	if views[1].Claim != "vol-data" || !views[1].Ready || views[1].Namespace != "p-acme" {
		t.Errorf("view: %+v", views[1])
	}
	var out bytes.Buffer
	printInterimSnapshots(&out, views)
	if !strings.Contains(out.String(), "NAMESPACE") || !strings.Contains(out.String(), "vol-data-20261003-013000") {
		t.Errorf("table:\n%s", out.String())
	}
}
