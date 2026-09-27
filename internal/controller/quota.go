package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
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

// reconcileQuota backs the API's plan check with ResourceQuotas: the
// plan's ceilings on requests and storage, per project namespace. A
// namespace cannot hold more than the whole plan; the API keeps the sum
// across projects under it. Workspaces without a plan get no quota, and a
// quota left behind by a plan that went away is removed.
//
// The compute quota is scoped to pods that ask for resources: buildpack
// builds run as pods with none (BestEffort) and must not be refused for
// failing to declare them. It counts requests, the coarse backstop behind
// the API's count of instance sizes (a shared size requests a share of
// its CPU); it is not the plan itself.
func (r *AppReconciler) reconcileQuota(ctx context.Context, app *shpyrdv1.App) error {
	var limits *store.Limits
	if r.Config.WorkspaceLimits != nil {
		limits = r.Config.WorkspaceLimits(workspaceOf(app))
	}
	compute, storage := quotaHard(limits)
	notBestEffort := []corev1.ResourceQuotaScope{corev1.ResourceQuotaScopeNotBestEffort}
	if err := r.applyQuota(ctx, app, QuotaName, compute, notBestEffort); err != nil {
		return err
	}
	return r.applyQuota(ctx, app, QuotaStorageName, storage, nil)
}

func (r *AppReconciler) applyQuota(ctx context.Context, app *shpyrdv1.App, name string, hard corev1.ResourceList, scopes []corev1.ResourceQuotaScope) error {
	q := &corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: app.Namespace}}
	if len(hard) == 0 {
		if err := r.Delete(ctx, q); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("remove quota %s: %w", name, err)
		}
		return nil
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, q, func() error {
		q.Labels = mergeMaps(q.Labels, commonLabels(app))
		q.Spec.Hard = hard
		q.Spec.Scopes = scopes
		return nil
	})
	if err != nil {
		return fmt.Errorf("quota %s: %w", name, err)
	}
	return nil
}

// quotaHard renders a plan as ResourceQuota ceilings, compute and storage
// apart (pod scopes cannot carry storage); each nil when the plan caps
// nothing Kubernetes can enforce there. Instances and projects are the
// API's to count: pods would also count builds and one-off commands.
func quotaHard(l *store.Limits) (compute, storage corev1.ResourceList) {
	if l == nil {
		return nil, nil
	}
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
	return compute, storage
}

var _ client.Object = &corev1.ResourceQuota{}
