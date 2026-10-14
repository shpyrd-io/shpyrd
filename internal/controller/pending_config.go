package controller

import (
	"context"
	"fmt"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/configvars"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *AppReconciler) pendingConfig(ctx context.Context, app *shpyrdv1.App) (*corev1.Secret, error) {
	pending := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: app.Namespace, Name: configvars.PendingName(app)}, pending); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	token := configvars.DeployToken(app)
	// A rollback restores its snapshot, retaining draft edits for a later deploy.
	if app.Annotations[shpyrdv1.AnnotationRollbackTo] != "" {
		if pending.Annotations[configvars.AnnotationPendingBase] != token {
			if pending.Annotations == nil {
				pending.Annotations = map[string]string{}
			}
			pending.Annotations[configvars.AnnotationPendingBase] = token
			if err := r.Update(ctx, pending); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}
	if app.CurrentRelease() != nil && pending.Annotations[configvars.AnnotationPendingBase] == token {
		return nil, nil
	}
	return pending, nil
}

// applyPendingConfig changes the environment only for a requested deployment.
// The resource-version precondition keeps edits made during this operation:
// their base token is the current deployment and they wait for the next one.
func (r *AppReconciler) applyPendingConfig(ctx context.Context, app *shpyrdv1.App, pending *corev1.Secret) (*corev1.Secret, error) {
	active := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: app.EnvSecretName(), Namespace: app.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, active, func() error {
		active.Type = corev1.SecretTypeOpaque
		active.Data = pending.DeepCopy().Data
		active.Labels = mergeMaps(active.Labels, map[string]string{shpyrdv1.LabelApp: app.Name})
		active.Annotations = mergeMaps(active.Annotations, map[string]string{configvars.AnnotationMeta: pending.Annotations[configvars.AnnotationMeta]})
		return controllerutil.SetControllerReference(app, active, r.Scheme)
	})
	if err != nil {
		return nil, fmt.Errorf("apply saved configuration: %w", err)
	}
	rv, uid := pending.ResourceVersion, pending.UID
	err = r.Delete(ctx, pending, &client.DeleteOptions{Preconditions: &metav1.Preconditions{ResourceVersion: &rv, UID: &uid}})
	if err != nil && !apierrors.IsNotFound(err) && !apierrors.IsConflict(err) {
		return nil, err
	}
	return active, nil
}
