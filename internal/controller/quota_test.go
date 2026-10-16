package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

func TestReconcileQuota(t *testing.T) {
	ctx := context.Background()
	acme := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-acme-shop", Labels: project.NamespaceLabels("acme", "shop")}}
	free := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "blog", Namespace: "app-blog"}}
	r, c := newTestReconciler(t, acme, free)
	plans := map[string]*store.Limits{"acme": {CPU: "4", Memory: "8Gi", Storage: "50Gi", Instances: 10}}
	r.Config.WorkspaceLimits = func(slug string) *store.Limits { return plans[slug] }

	if err := r.reconcileQuota(ctx, acme); err != nil {
		t.Fatal(err)
	}
	q := &corev1.ResourceQuota{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-acme-shop", Name: QuotaName}, q); err != nil {
		t.Fatalf("quota: %v", err)
	}
	for res, want := range map[corev1.ResourceName]string{corev1.ResourceRequestsCPU: "4", corev1.ResourceRequestsMemory: "8Gi"} {
		if got := q.Spec.Hard[res]; got.Cmp(mustQ(want)) != 0 {
			t.Errorf("hard %s = %s, want %s", res, got.String(), want)
		}
	}
	if _, ok := q.Spec.Hard[corev1.ResourcePods]; ok {
		t.Error("pods must not be capped: builds and one-off commands would count")
	}
	if _, ok := q.Spec.Hard[corev1.ResourceRequestsStorage]; ok {
		t.Error("storage cannot share a quota with pod scopes")
	}
	// Buildpack builds run as pods without resources; a quota that made
	// them declare some would refuse every build.
	if len(q.Spec.Scopes) != 1 || q.Spec.Scopes[0] != corev1.ResourceQuotaScopeNotBestEffort {
		t.Errorf("compute quota scopes = %v, want NotBestEffort", q.Spec.Scopes)
	}
	st := &corev1.ResourceQuota{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-acme-shop", Name: QuotaStorageName}, st); err != nil {
		t.Fatalf("storage quota: %v", err)
	}
	if got := st.Spec.Hard[corev1.ResourceRequestsStorage]; got.Cmp(mustQ("50Gi")) != 0 || len(st.Spec.Scopes) != 0 {
		t.Errorf("storage quota = %v scopes %v", st.Spec.Hard, st.Spec.Scopes)
	}
	// The implicit workspace has no plan: no quota.
	if err := r.reconcileQuota(ctx, free); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-blog", Name: QuotaName}, q); err == nil {
		t.Error("a workspace without a plan got a quota")
	}
	// A plan that goes away takes its quotas along.
	delete(plans, "acme")
	if err := r.reconcileQuota(ctx, acme); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-acme-shop", Name: QuotaName}, q); err == nil {
		t.Error("quota survived its plan")
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-acme-shop", Name: QuotaStorageName}, st); err == nil {
		t.Error("storage quota survived its plan")
	}
}

func mustQ(s string) resource.Quantity { return resource.MustParse(s) }

// A quota is never rendered below what the namespace already uses
// (storage plan step 4): what exists above a ceiling that shrank keeps
// working, the API refuses what would be added.
func TestAQuotaNeverFallsBelowWhatIsUsed(t *testing.T) {
	ctx := context.Background()
	acme := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-acme-shop", Labels: project.NamespaceLabels("acme", "shop")}, Spec: shpyrdv1.AppSpec{Slug: "shop"}}
	used := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: QuotaStorageName, Namespace: "app-acme-shop"},
		Spec:       corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{corev1.ResourceRequestsStorage: mustQ("100Gi")}},
		Status:     corev1.ResourceQuotaStatus{Used: corev1.ResourceList{corev1.ResourceRequestsStorage: mustQ("80Gi")}},
	}
	r, c := newTestReconciler(t, acme, used)
	r.Config.WorkspaceLimits = func(string) *store.Limits { return &store.Limits{Storage: "50Gi"} }
	if err := r.reconcileQuota(ctx, acme); err != nil {
		t.Fatal(err)
	}
	q := &corev1.ResourceQuota{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-acme-shop", Name: QuotaStorageName}, q); err != nil {
		t.Fatal(err)
	}
	if got := q.Spec.Hard[corev1.ResourceRequestsStorage]; got.Cmp(mustQ("80Gi")) != 0 {
		t.Errorf("storage quota = %s, want 80Gi (what is used), not the 50Gi ceiling", got.String())
	}
}

// A project's own cap bounds its namespace below the workspace's ceiling,
// and never above it; a plan without disks is the API's to refuse, never
// a storage quota of zero.
func TestAProjectCapBoundsItsQuotaAndNoDisksIsNotAZeroQuota(t *testing.T) {
	ctx := context.Background()
	shop := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-acme-shop", Labels: project.NamespaceLabels("acme", "shop")}, Spec: shpyrdv1.AppSpec{Slug: "shop"}}
	wiki := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "wiki", Namespace: "app-acme-wiki", Labels: project.NamespaceLabels("acme", "wiki")}, Spec: shpyrdv1.AppSpec{Slug: "wiki"}}
	r, c := newTestReconciler(t, shop, wiki)
	r.Config.WorkspaceLimits = func(string) *store.Limits { return &store.Limits{Storage: "50Gi"} }
	r.Config.ProjectStorageCap = func(_, p string) string { return map[string]string{"shop": "10Gi", "wiki": "100Gi"}[p] }
	for _, app := range []*shpyrdv1.App{shop, wiki} {
		if err := r.reconcileQuota(ctx, app); err != nil {
			t.Fatal(err)
		}
	}
	q := &corev1.ResourceQuota{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-acme-shop", Name: QuotaStorageName}, q); err != nil {
		t.Fatal(err)
	}
	if got := q.Spec.Hard[corev1.ResourceRequestsStorage]; got.Cmp(mustQ("10Gi")) != 0 {
		t.Errorf("shop's quota = %s, want its own 10Gi cap", got.String())
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-acme-wiki", Name: QuotaStorageName}, q); err != nil {
		t.Fatal(err)
	}
	if got := q.Spec.Hard[corev1.ResourceRequestsStorage]; got.Cmp(mustQ("50Gi")) != 0 {
		t.Errorf("wiki's quota = %s, want the workspace's 50Gi (its cap is above it)", got.String())
	}
	no := false
	compute, storage := quotaHard(&store.Limits{CPU: "1", AllowDisks: &no}, "")
	if storage != nil {
		t.Errorf("no disks rendered a storage quota: %v", storage)
	}
	if cpu := compute[corev1.ResourceRequestsCPU]; cpu.Cmp(mustQ("1")) != 0 {
		t.Errorf("compute = %v", compute)
	}
}
