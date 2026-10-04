package controller

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// AnnotationWorkspaceSuspended marks the apps, databases and Redis of a
// suspended workspace. The workspace reconciler sets and removes it, so each
// is reconciled as soon as its workspace is suspended or activated; the
// stores read it to stop, the apps their workspace's status.
const AnnotationWorkspaceSuspended = "shpyrd.io/workspace-suspended"

// suspendedWithWorkspace says a store is marked down with its workspace.
func suspendedWithWorkspace(obj client.Object) bool {
	return obj.GetAnnotations()[AnnotationWorkspaceSuspended] == "true"
}

// syncSuspension marks what belongs to a suspended workspace and unmarks
// what belongs to an active one. A database or Redis belongs to the
// workspace of the apps in its namespace.
func (r *WorkspaceReconciler) syncSuspension(ctx context.Context, all []store.Workspace) error {
	suspended := map[string]bool{}
	for i := range all {
		if all[i].Status == store.WorkspaceSuspended && !r.Config.isDefault(all[i].Slug) {
			suspended[all[i].Slug] = true
		}
	}
	var apps shpyrdv1.AppList
	if err := r.List(ctx, &apps); err != nil {
		return err
	}
	byNamespace := map[string]bool{}
	for i := range apps.Items {
		app := &apps.Items[i]
		on := suspended[workspaceOf(app)]
		byNamespace[app.Namespace] = on
		if err := r.markSuspended(ctx, app, on); err != nil {
			return err
		}
	}
	var pgs shpyrdv1.PostgresList
	if err := r.List(ctx, &pgs); err != nil {
		return err
	}
	for i := range pgs.Items {
		if err := r.markSuspended(ctx, &pgs.Items[i], byNamespace[pgs.Items[i].Namespace]); err != nil {
			return err
		}
	}
	var redises shpyrdv1.RedisList
	if err := r.List(ctx, &redises); err != nil {
		return err
	}
	for i := range redises.Items {
		if err := r.markSuspended(ctx, &redises.Items[i], byNamespace[redises.Items[i].Namespace]); err != nil {
			return err
		}
	}
	return nil
}

func (r *WorkspaceReconciler) markSuspended(ctx context.Context, obj client.Object, on bool) error {
	if !obj.GetDeletionTimestamp().IsZero() || suspendedWithWorkspace(obj) == on {
		return nil
	}
	patch := client.MergeFrom(obj.DeepCopyObject().(client.Object))
	ann := obj.GetAnnotations()
	if on {
		if ann == nil {
			ann = map[string]string{}
		}
		ann[AnnotationWorkspaceSuspended] = "true"
	} else {
		delete(ann, AnnotationWorkspaceSuspended)
	}
	obj.SetAnnotations(ann)
	return client.IgnoreNotFound(r.Patch(ctx, obj, patch))
}
