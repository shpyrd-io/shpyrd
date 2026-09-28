package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ids"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// ProjectFinalizer keeps an App until its row in the store is marked
// deleted (RFC-0076): invoices name projects that no longer exist.
const ProjectFinalizer = "shpyrd.io/project"

// ensureIdentity gives every App what RFC-0076 expects of it: a stable ID
// (legacy Apps created before IDs receive one here and keep their names),
// the identity and display labels, and the finalizer that mirrors deletion
// into the store. It writes the App at most once per reconcile and reports
// whether it did, so the caller can requeue with a fresh object instead of
// racing its own status patch.
func (r *AppReconciler) ensureIdentity(ctx context.Context, app *shpyrdv1.App) (bool, error) {
	changed := false
	if app.Spec.ID == "" {
		app.Spec.ID = uuid.NewString()
		changed = true
		log.FromContext(ctx).Info("legacy project given an id", "project", app.Name, "id", app.Spec.ID)
	}
	want := map[string]string{
		shpyrdv1.LabelProjectID: ids.Short(app.Spec.ID),
		shpyrdv1.LabelProject:   project.SlugOf(app),
	}
	if wsID := r.Config.workspaceIDOf(app); wsID != "" {
		want[shpyrdv1.LabelWorkspaceID] = wsID
	}
	for k, v := range want {
		if app.Labels[k] != v {
			if app.Labels == nil {
				app.Labels = map[string]string{}
			}
			app.Labels[k] = v
			changed = true
		}
	}
	if r.Config.Projects != nil && controllerutil.AddFinalizer(app, ProjectFinalizer) {
		changed = true
	}
	if !changed {
		return false, nil
	}
	if err := r.Update(ctx, app); err != nil {
		return false, fmt.Errorf("identity: %w", err)
	}
	return true, nil
}

// workspaceIDOf is the short form of the App's workspace id, "" when the
// store does not know the workspace (or there is no store, as in tests).
func (c Config) workspaceIDOf(app *shpyrdv1.App) string {
	if c.WorkspaceID == nil {
		return ""
	}
	if id := c.WorkspaceID(workspaceOf(app)); id != "" {
		return ids.Short(id)
	}
	return ""
}

// finalizeProject runs when an App is being deleted: the store's row is
// marked deleted and the finalizer removed. Without a store there is
// nothing to do but let go.
func (r *AppReconciler) finalizeProject(ctx context.Context, app *shpyrdv1.App) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(app, ProjectFinalizer) {
		return ctrl.Result{}, nil
	}
	if r.Config.Projects != nil && app.Spec.ID != "" {
		if err := r.Config.Projects.DeleteProject(ctx, app.Spec.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
			return ctrl.Result{}, fmt.Errorf("mark project deleted: %w", err)
		}
	}
	controllerutil.RemoveFinalizer(app, ProjectFinalizer)
	if err := r.Update(ctx, app); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	return ctrl.Result{}, nil
}

// projectMirror remembers what was last written to the store per App so a
// reconcile that changes nothing writes nothing.
type projectMirror struct {
	mu      sync.Mutex
	last    map[types.UID]string
	rekeyed map[types.UID]bool
}

func (m *projectMirror) init() {
	if m.last == nil {
		m.last = map[types.UID]string{}
		m.rekeyed = map[types.UID]bool{}
	}
}

// mirrorProject keeps the store's projects table in step with the App
// (RFC-0076): identity, current slug and name, namespace. The first time a
// legacy App is seen with its new id, the ledger rows written under its
// slug are re-keyed to the id — once per process lifetime, idempotent.
func (r *AppReconciler) mirrorProject(ctx context.Context, app *shpyrdv1.App) error {
	if r.Config.Projects == nil || app.Spec.ID == "" {
		return nil
	}
	r.mirror.mu.Lock()
	r.mirror.init()
	ws := workspaceOf(app)
	key := app.Spec.ID + "\x00" + project.SlugOf(app) + "\x00" + project.DisplayName(app) + "\x00" + app.Namespace + "\x00" + ws
	unchanged := r.mirror.last[app.UID] == key
	needsRekey := !project.IDNamed(app) && !r.mirror.rekeyed[app.UID]
	r.mirror.mu.Unlock()
	if unchanged && !needsRekey {
		return nil
	}
	if !unchanged {
		_, err := r.Config.Projects.UpsertProject(ctx, store.Project{
			ID: app.Spec.ID, WorkspaceID: ws, Slug: project.SlugOf(app), Name: project.DisplayName(app), Namespace: app.Namespace,
		})
		switch {
		case errors.Is(err, store.ErrConflict):
			// Two live projects with one slug cannot both be mirrored; the
			// API refuses to create the second, so this is a legacy
			// inconsistency worth a log line, not a failed reconcile.
			log.FromContext(ctx).Info("project not mirrored: slug already taken in the store", "project", app.Name, "slug", project.SlugOf(app))
		case errors.Is(err, store.ErrNotFound):
			return nil // the workspace is not in the store yet; next reconcile
		case err != nil:
			return fmt.Errorf("mirror project: %w", err)
		default:
			r.mirror.mu.Lock()
			r.mirror.last[app.UID] = key
			r.mirror.mu.Unlock()
		}
	}
	if needsRekey {
		moved, err := r.Config.Projects.RekeyProject(ctx, ws, app.Name, app.Spec.ID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil
			}
			return fmt.Errorf("rekey ledger: %w", err)
		}
		if moved > 0 {
			log.FromContext(ctx).Info("ledger re-keyed from slug to id", "project", app.Name, "rows", moved)
		}
		r.mirror.mu.Lock()
		r.mirror.rekeyed[app.UID] = true
		r.mirror.mu.Unlock()
	}
	return nil
}

// projectIDBySlug finds the short id of another project of the same
// workspace by its slug, for selectors that must not depend on display
// labels (RFC-0076); "" when it is unknown or has no id yet.
func (r *AppReconciler) projectIDBySlug(ctx context.Context, app *shpyrdv1.App, slug string) string {
	list := &shpyrdv1.AppList{}
	if err := r.List(ctx, list, client.MatchingLabels{shpyrdv1.LabelProject: slug}); err != nil {
		return ""
	}
	ws := workspaceOf(app)
	for i := range list.Items {
		other := &list.Items[i]
		if workspaceOf(other) != ws || other.Spec.ID == "" {
			continue
		}
		return ids.Short(other.Spec.ID)
	}
	return ""
}

// namespaceIdentityLabels are the labels the project namespace carries for
// its identity (RFC-0076), next to the display ones.
func namespaceIdentityLabels(app *shpyrdv1.App) map[string]string {
	out := map[string]string{}
	if app.Spec.ID != "" {
		out[shpyrdv1.LabelProjectID] = ids.Short(app.Spec.ID)
	}
	if v := app.Labels[shpyrdv1.LabelWorkspaceID]; v != "" {
		out[shpyrdv1.LabelWorkspaceID] = v
	}
	return out
}
