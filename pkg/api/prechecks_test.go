package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
	"github.com/shpyrd-io/shpyrd/pkg/ext/all"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// What is checked before a change is accepted (storage plan step 4,
// #127): a growth the storage class cannot do, a size no machine can
// hold, a conversion above the shared cap; and the ledger a workspace
// reads.

// A class that cannot grow disks refuses the growth at once, in words;
// a class that can lets it through, and the volume says where the growth
// stands.
func TestAGrowthTheClassCannotDoIsRefusedBeforeAcceptance(t *testing.T) {
	s, cr, _ := newTenantServer(t)
	ctx := context.Background()
	fixed := &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "fixed"}, AllowVolumeExpansion: ptr.To(false)}
	elastic := &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "elastic"}, AllowVolumeExpansion: ptr.To(true)}
	for _, sc := range []*storagev1.StorageClass{fixed, elastic} {
		if _, err := s.kube.Kube.StorageV1().StorageClasses().Create(ctx, sc, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	for name, class := range map[string]string{"stuck": "fixed", "free": "elastic"} {
		vol := &shpyrdv1.Volume{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "app-acme-shop", Labels: map[string]string{shpyrdv1.LabelWorkspace: "acme", shpyrdv1.LabelProject: "shop"}},
			Spec:       shpyrdv1.VolumeSpec{Size: resource.MustParse("5Gi"), StorageClass: class, AccessMode: corev1.ReadWriteOnce},
		}
		if err := cr.Create(ctx, vol); err != nil {
			t.Fatal(err)
		}
	}
	rec := at(t, s, "acme.shpyrd.test", "PUT", "/api/projects/shop/volumes/stuck", `{"size":"10Gi"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "volumes of the fixed class cannot grow; create a new volume of the size you need and copy the data") {
		t.Errorf("growth on a fixed class = %d %s", rec.Code, rec.Body.String())
	}
	if rec := at(t, s, "acme.shpyrd.test", "PUT", "/api/projects/shop/volumes/free", `{"size":"10Gi"}`); rec.Code != http.StatusOK {
		t.Errorf("growth on an elastic class = %d %s", rec.Code, rec.Body.String())
	}
	// The standing the controller writes reaches the view. (The test
	// client knows no status subresource for volumes: a plain update
	// carries it.)
	vol := &shpyrdv1.Volume{}
	if err := cr.Get(ctx, types.NamespacedName{Namespace: "app-acme-shop", Name: "free"}, vol); err != nil {
		t.Fatal(err)
	}
	vol.Status.Resize = &shpyrdv1.VolumeResize{Phase: shpyrdv1.ResizePending, To: "10Gi", Message: "the provider is growing the disk"}
	if err := cr.Update(ctx, vol); err != nil {
		t.Fatal(err)
	}
	rec = at(t, s, "acme.shpyrd.test", "GET", "/api/projects/shop/volumes", "")
	if !strings.Contains(rec.Body.String(), `"resize":{"phase":"pending","to":"10Gi","message":"the provider is growing the disk"`) {
		t.Errorf("the view does not say where the growth stands: %s", rec.Body.String())
	}
}

// A size no machine of the apps pool can hold is refused before anything
// is stopped (#127), with the largest machine's numbers.
func TestASizeNoMachineCanHoldIsRefusedUpFront(t *testing.T) {
	s, _, _ := newTenantServer(t)
	ctx := context.Background()
	small := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "small", Labels: map[string]string{"shpyrd.io/pool": "apps"}}, Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1830m"), corev1.ResourceMemory: resource.MustParse("6Gi")}}}
	big := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "big", Labels: map[string]string{"shpyrd.io/pool": "data"}}, Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("8"), corev1.ResourceMemory: resource.MustParse("32Gi")}}}
	for _, n := range []*corev1.Node{small, big} {
		if _, err := s.kube.Kube.CoreV1().Nodes().Create(ctx, n, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	s.opts.Vars = func(key string) string { return map[string]string{"SHPYRD_APPS_POOL": "apps"}[key] }
	rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/resize", `{"process":"web","size":"dedicated-m"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "dedicated-m needs 2 cores and 4 GB; the largest machine in this cluster offers 1.83 cores and 6 GB") {
		t.Errorf("a size past the pool's machines = %d %s", rec.Code, rec.Body.String())
	}
	if rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/resize", `{"process":"web","size":"shared-l"}`); rec.Code != http.StatusOK {
		t.Errorf("a size a machine holds = %d %s", rec.Code, rec.Body.String())
	}
	// Without pools every node is the measure: the big one holds it.
	s.opts.Vars = func(string) string { return "" }
	if rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/resize", `{"process":"web","size":"dedicated-m"}`); rec.Code != http.StatusOK {
		t.Errorf("a size the cluster's largest machine holds = %d %s", rec.Code, rec.Body.String())
	}
}

// A conversion to shared is refused when the database holds more than the
// cap, with the measured size.
func TestAConversionAboveTheCapIsRefusedWithTheSize(t *testing.T) {
	s, cr, _ := newTenantServer(t)
	s.opts.Extensions = all.All()
	ctx := context.Background()
	pg := &shpyrdv1.Postgres{
		ObjectMeta: metav1.ObjectMeta{Name: "big", Namespace: "app-acme-shop", Labels: map[string]string{shpyrdv1.LabelProject: "shop"}, Annotations: map[string]string{controller.AnnotationDataBytes: "3221225472"}},
		Spec:       shpyrdv1.PostgresSpec{Type: shpyrdv1.PostgresTypeDedicated},
		Status:     shpyrdv1.ResourceStatus{Phase: shpyrdv1.ResourceReady},
	}
	if err := cr.Create(ctx, pg); err != nil {
		t.Fatal(err)
	}
	rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/resources/postgres/big/convert", `{"to":"shared"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "This database holds 3 GB; a shared database holds up to 1 GB.") {
		t.Errorf("conversion above the cap = %d %s", rec.Code, rec.Body.String())
	}
}

// The ledger a workspace reads sums what the metering loop measured, per
// project, component and metric, in the drain's units.
func TestTheWorkspaceReadsItsUsageLedger(t *testing.T) {
	s, _, st := newTenantServer(t)
	ctx := context.Background()
	w, err := st.Workspace(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Hour)
	var rows []store.UsageBucket
	for i := 0; i < 3; i++ {
		start := now.Add(time.Duration(-15+5*i) * time.Minute)
		rows = append(rows, store.UsageBucket{WorkspaceID: w.ID, Project: "shop", Component: "bucket/uploads", Metric: store.MetricObjectStorage, PeriodStart: start, PeriodEnd: start.Add(5 * time.Minute), Quantity: ptr.To(300.0), Unit: store.UnitGiBSeconds, Quality: store.QualityComplete, Revision: 1, Source: "usedbytes_v1"})
	}
	rows = append(rows, store.UsageBucket{WorkspaceID: w.ID, Project: "shop", Component: "postgres/db", Metric: store.MetricPostgresShared, PeriodStart: now.Add(-5 * time.Minute), PeriodEnd: now, Quantity: ptr.To(300.0), Unit: store.UnitGiBSeconds, Quality: store.QualityPartial, Revision: 1, Source: "cap_v1"})
	if err := st.WriteBuckets(ctx, rows); err != nil {
		t.Fatal(err)
	}
	rec := at(t, s, "acme.shpyrd.test", "GET", "/api/workspace/usage?from="+now.Add(-time.Hour).Format(time.RFC3339)+"&to="+now.Format(time.RFC3339), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("usage = %d %s", rec.Code, rec.Body.String())
	}
	var v UsageView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Lines) != 2 {
		t.Fatalf("lines = %+v", v.Lines)
	}
	if l := v.Lines[0]; l.Component != "bucket/uploads" || l.Metric != "object-storage" || l.Quantity != 900 || l.Buckets != 3 || l.Partial {
		t.Errorf("bucket line = %+v", l)
	}
	if l := v.Lines[1]; l.Component != "postgres/db" || !l.Partial {
		t.Errorf("database line = %+v", l)
	}
	if rec := at(t, s, "acme.shpyrd.test", "GET", "/api/workspace/usage?from=yesterday", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("a period that is not a time = %d", rec.Code)
	}
}
