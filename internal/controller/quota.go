package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// The ResourceQuotas the platform keeps in project namespaces of
// workspaces with a plan (RFC-0033 phase 6, RFC-0042): QuotaName caps what
// instances request, QuotaStorageName what volumes and databases claim.
const (
	QuotaName        = "shpyrd"
	QuotaStorageName = "shpyrd-storage"
)

// BuildPriorityClass marks image builds and ReleasePriorityClass release
// steps (deploy/components/shpyrd: the PriorityClasses). The compute quota
// leaves pods of these classes out (#243, #60): a build and a release step
// are steps of a deploy, not something the workspace keeps running, and
// counting them refused every deploy of a small workspace whose app was
// awake. A build is bounded by limits of its own, a release step by the
// release process's size.
const (
	BuildPriorityClass   = "shpyrd-build"
	ReleasePriorityClass = "shpyrd-release"
)

// computeScopes and computeSelector say which pods the compute quota
// counts: those that ask for resources (buildpack builds ask for none,
// BestEffort) and are not steps of a deploy.
var (
	computeScopes   = []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeNotBestEffort}
	computeSelector = &corev1.ScopeSelector{MatchExpressions: []corev1.ScopedResourceSelectorRequirement{{
		ScopeName: corev1.ResourceQuotaScopePriorityClass, Operator: corev1.ScopeSelectorOpNotIn, Values: []string{BuildPriorityClass, ReleasePriorityClass},
	}}}
)

// reconcileQuota backs the API's plan check with ResourceQuotas: the
// plan's ceilings on requests and storage, per project namespace. A
// namespace cannot hold more than the whole plan; the API keeps the sum
// across projects under it. Workspaces without a plan get no quota, and a
// quota left behind by ceilings that went away is removed.
//
// The compute quota is scoped to pods that ask for resources and are not
// steps of a deploy: buildpack builds run as pods with none (BestEffort),
// Dockerfile builds under BuildPriorityClass with limits of their own,
// release steps under ReleasePriorityClass with the release process's size.
// It counts
// requests, the coarse backstop behind
// the API's count of instance sizes (a shared size requests a share of
// its CPU); it is not the ceiling itself.
func (r *AppReconciler) reconcileQuota(ctx context.Context, app *shpyrdv1.App) error {
	var limits *store.Limits
	if r.Config.WorkspaceLimits != nil {
		limits = r.Config.WorkspaceLimits(workspaceOf(app))
	}
	cap := ""
	if r.Config.ProjectStorageCap != nil {
		cap = r.Config.ProjectStorageCap(workspaceOf(app), app.Spec.Slug)
	}
	compute, storage := quotaHard(limits, cap)
	if err := applyQuota(ctx, r.Client, app, QuotaName, compute, computeScopes, computeSelector); err != nil {
		return err
	}
	return applyQuota(ctx, r.Client, app, QuotaStorageName, storage, nil, nil)
}

// syncQuotas keeps the quotas of every project in step with its
// workspace's ceilings (#51): ceilings that change reach the projects
// already running within a pass of the workspace reconciler, not at their
// next deploy.
func (r *WorkspaceReconciler) syncQuotas(ctx context.Context, all []store.Workspace) error {
	var apps shpyrdv1.AppList
	if err := r.List(ctx, &apps); err != nil {
		return err
	}
	bySlug := map[string]*store.Workspace{}
	for i := range all {
		bySlug[all[i].Slug] = &all[i]
	}
	for i := range apps.Items {
		app := &apps.Items[i]
		if !app.DeletionTimestamp.IsZero() {
			continue
		}
		ws := bySlug[workspaceOf(app)]
		if ws == nil {
			continue // a workspace the store does not know: leave it be
		}
		compute, storage := quotaHard(ws.Settings.Limits, ws.Settings.ProjectCaps[app.Spec.Slug].Storage)
		if err := applyQuota(ctx, r.Client, app, QuotaName, compute, computeScopes, computeSelector); err != nil {
			return err
		}
		if err := applyQuota(ctx, r.Client, app, QuotaStorageName, storage, nil, nil); err != nil {
			return err
		}
	}
	return nil
}

func applyQuota(ctx context.Context, c client.Client, app *shpyrdv1.App, name string, hard corev1.ResourceList, scopes []corev1.ResourceQuotaScope, selector *corev1.ScopeSelector) error {
	q := &corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: app.Namespace}}
	if len(hard) == 0 {
		if err := c.Get(ctx, client.ObjectKeyFromObject(q), q); apierrors.IsNotFound(err) {
			return nil
		}
		if err := c.Delete(ctx, q); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("remove quota %s: %w", name, err)
		}
		return nil
	}
	// A quota's scopes are immutable: one that counts other pods than these
	// (a quota from before builds left it, #243) is replaced, not updated.
	cur := &corev1.ResourceQuota{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(q), cur); err == nil && !sameScopes(cur.Spec, scopes, selector) {
		if err := c.Delete(ctx, cur); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("replace quota %s: %w", name, err)
		}
	}
	_, err := controllerutil.CreateOrUpdate(ctx, c, q, func() error {
		q.Labels = mergeMaps(q.Labels, commonLabels(app))
		q.Spec.Hard = floorAtUsed(hard, q.Status.Used)
		q.Spec.Scopes = scopes
		q.Spec.ScopeSelector = selector
		return nil
	})
	if err != nil {
		return fmt.Errorf("quota %s: %w", name, err)
	}
	return nil
}

// sameScopes says a quota counts the pods these scopes and selector say.
func sameScopes(spec corev1.ResourceQuotaSpec, scopes []corev1.ResourceQuotaScope, selector *corev1.ScopeSelector) bool {
	if len(spec.Scopes) != len(scopes) {
		return false
	}
	for i := range scopes {
		if spec.Scopes[i] != scopes[i] {
			return false
		}
	}
	return equality.Semantic.DeepEqual(spec.ScopeSelector, selector)
}

// floorAtUsed keeps a quota from being rendered below what the namespace
// already uses (storage plan step 4, the grandfather rule): what exists
// above a ceiling that shrank keeps working, and the API refuses what
// would be added. Kubernetes would otherwise refuse the platform's own
// claims, a database's second instance or a migration's copy.
func floorAtUsed(hard, used corev1.ResourceList) corev1.ResourceList {
	for name, h := range hard {
		if u, ok := used[name]; ok && u.Cmp(h) > 0 {
			hard[name] = u.DeepCopy()
		}
	}
	return hard
}

// quotaHard renders a plan as ResourceQuota ceilings, compute and storage
// apart (pod scopes cannot carry storage); each nil when the plan caps
// nothing Kubernetes can enforce there. Instances and projects are the
// API's to count: pods would also count builds and one-off commands. A
// project's own cap, when it has one, bounds its storage below the
// workspace's. A plan without disks is the API's to refuse: it is never
// a storage quota of zero, which would refuse every claim.
func quotaHard(l *store.Limits, projectCap string) (compute, storage corev1.ResourceList) {
	if l != nil {
		if q, err := resource.ParseQuantity(l.CPU); err == nil && l.CPU != "" {
			compute = corev1.ResourceList{corev1.ResourceRequestsCPU: q}
		}
		if q, err := resource.ParseQuantity(l.Memory); err == nil && l.Memory != "" {
			if compute == nil {
				compute = corev1.ResourceList{}
			}
			compute[corev1.ResourceRequestsMemory] = q
		}
		if q, err := resource.ParseQuantity(l.Storage); err == nil && l.Storage != "" {
			storage = corev1.ResourceList{corev1.ResourceRequestsStorage: q}
		}
	}
	if q, err := resource.ParseQuantity(projectCap); err == nil && projectCap != "" {
		if cur, has := storage[corev1.ResourceRequestsStorage]; !has || q.Cmp(cur) < 0 {
			storage = corev1.ResourceList{corev1.ResourceRequestsStorage: q}
		}
	}
	return compute, storage
}

var _ client.Object = &corev1.ResourceQuota{}
