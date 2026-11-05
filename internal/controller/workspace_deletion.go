package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/audit"
	"github.com/shpyrd-io/shpyrd/pkg/ids"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// The reaper (#274). Every deletion is a date on the workspace row; this
// is the one place that acts on it. Each pass of the workspace reconciler:
//
//   - an active workspace past its expiry that nobody claimed is suspended
//     (SuspendReasonExpired) and scheduled a day ahead;
//   - a suspended workspace whose date has come moves to deleting;
//   - a deleting workspace has its cascade run, step by step, each step
//     recorded on the row until all are done, then the rows go and the
//     tombstone is written;
//   - a tombstone older than TombstoneHold is scrubbed, freeing its names.
//
// The steps: the project namespaces (everything of a project lives in its
// namespace; the ObjectBucket finalizer cleans the gateway side), the
// image repositories under apps/<workspace id>/, the front doors (removed
// by the door loop once the workspace is gone; this step waits for them),
// then the rows. A step that cannot finish says why on the row and is
// tried again on the next pass.

// Steps of the cascade, in order.
const (
	stepProjects = "projects"
	stepImages   = "images"
	stepDoors    = "doors"
	stepRows     = "rows"
)

// deletionRetry is how soon a pass runs again while a cascade is in
// progress.
const deletionRetry = 30 * time.Second

// activityRefreshEvery is how often the reaper recomputes when each
// workspace was last used (last_activity_at): the policies that read it
// act in days.
const activityRefreshEvery = 6 * time.Hour

// refreshActivity records each live workspace's last activity: what the
// store knows (people seen, tokens used, projects woken) and the last
// deploy of any of its apps, once per activityRefreshEvery.
func (r *WorkspaceReconciler) refreshActivity(ctx context.Context, all []store.Workspace) {
	if time.Since(r.activityAt) < activityRefreshEvery {
		return
	}
	r.activityAt = time.Now()
	logger := log.FromContext(ctx)
	var apps shpyrdv1.AppList
	if err := r.List(ctx, &apps); err != nil {
		logger.Error(err, "activity: list apps")
		return
	}
	lastDeploy := map[string]time.Time{} // by workspace slug and by short id
	for i := range apps.Items {
		a := &apps.Items[i]
		var latest time.Time
		for _, rel := range a.Status.Releases {
			if rel.CreatedAt.Time.After(latest) {
				latest = rel.CreatedAt.Time
			}
		}
		for _, key := range []string{a.Labels[shpyrdv1.LabelWorkspace], a.Labels[shpyrdv1.LabelWorkspaceID]} {
			if key != "" && latest.After(lastDeploy[key]) {
				lastDeploy[key] = latest
			}
		}
	}
	for i := range all {
		ws := &all[i]
		if ws.Gone() {
			continue
		}
		seen := lastDeploy[ws.Slug]
		if t := lastDeploy[ids.Short(ws.ID)]; t.After(seen) {
			seen = t
		}
		w, err := r.Store.RefreshWorkspaceActivity(ctx, ws.Slug, seen)
		if err != nil {
			logger.Error(err, "activity", "workspace", ws.Slug)
			continue
		}
		all[i] = *w
	}
}

// reap runs the lifecycle over every live workspace and the tombstones.
// It updates all in place (a workspace that moved to deleting is seen so
// by the door loop of the same pass) and says whether a cascade is in
// progress.
func (r *WorkspaceReconciler) reap(ctx context.Context, all []store.Workspace) bool {
	logger := log.FromContext(ctx)
	r.refreshActivity(ctx, all)
	now := time.Now()
	busy := false
	for i := range all {
		ws := &all[i]
		if r.Config.isDefault(ws.Slug) || ws.OwnedByOperator() {
			continue // never by a policy; the API refuses them too
		}
		switch {
		case ws.Status == store.WorkspaceActive && ws.ExpiresAt != nil && now.After(*ws.ExpiresAt) && !ws.Protected:
			// Expired and unclaimed: a day suspended, then gone.
			if _, err := r.Store.SuspendWorkspace(ctx, ws.Slug, store.SuspendReasonExpired); err != nil {
				logger.Error(err, "expired workspace: suspend", "workspace", ws.Slug)
				continue
			}
			w, err := r.Store.ScheduleWorkspaceDeletion(ctx, ws.Slug, now.Add(store.ExpiredGrace), store.DeleteReasonExpired)
			if err != nil {
				logger.Error(err, "expired workspace: schedule", "workspace", ws.Slug)
				continue
			}
			all[i] = *w
			r.auditWorkspace(ctx, w, "workspace.expired", fmt.Sprintf("unclaimed past %s; deleted after %s", ws.ExpiresAt.UTC().Format(time.RFC3339), w.DeleteAfter.UTC().Format(time.RFC3339)))
			logger.Info("workspace expired unclaimed, suspended", "workspace", ws.Slug, "deleteAfter", w.DeleteAfter)
			if r.Forget != nil {
				r.Forget()
			}
		case ws.Status == store.WorkspaceSuspended && ws.DeleteAfter != nil && now.After(*ws.DeleteAfter):
			w, err := r.Store.BeginWorkspaceDeletion(ctx, ws.Slug)
			if err != nil {
				logger.Error(err, "workspace deletion: begin", "workspace", ws.Slug)
				continue
			}
			logger.Info("workspace deletion begins", "workspace", ws.Slug, "reason", w.DeleteReason)
			all[i] = *w
			r.cascade(ctx, &all[i])
		case ws.Status == store.WorkspaceDeleting:
			r.cascade(ctx, ws)
		}
		if all[i].Status == store.WorkspaceDeleting {
			busy = true // the cascade has steps left: come back soon
		}
	}
	dead, err := r.Store.ListDeletedWorkspaces(ctx)
	if err != nil {
		logger.Error(err, "tombstones")
		return busy
	}
	for i := range dead {
		t := &dead[i]
		if t.DeletedAt == nil || now.Before(t.DeletedAt.Add(store.TombstoneHold)) || t.Slug == store.ScrubbedSlug(t.ID) {
			continue
		}
		if _, err := r.Store.ScrubWorkspace(ctx, t.Slug); err != nil {
			logger.Error(err, "scrub tombstone", "workspace", t.Slug)
			continue
		}
		logger.Info("workspace tombstone scrubbed, its names are free", "workspace", t.Slug, "deletedAt", t.DeletedAt)
	}
	return busy
}

// cascade runs the steps of one deleting workspace as far as they go now
// and records them; ws is updated with what the store answered.
func (r *WorkspaceReconciler) cascade(ctx context.Context, ws *store.Workspace) {
	logger := log.FromContext(ctx).WithValues("workspace", ws.Slug)
	d := store.WorkspaceDeletion{StartedAt: time.Now().UTC()}
	if ws.Deletion != nil {
		d = *ws.Deletion
	}
	changed := false
	record := func(name string, done bool, detail string) {
		st := d.Step(name)
		if st.Done == done && st.Detail == detail {
			return
		}
		st.Done, st.Detail = done, detail
		if done && st.DoneAt == nil {
			now := time.Now().UTC()
			st.DoneAt = &now
		}
		changed = true
	}

	// 1. The projects: their namespaces, and everything in them.
	remaining, deleted, err := r.deleteProjectNamespaces(ctx, ws)
	switch {
	case err != nil:
		record(stepProjects, false, err.Error())
	case remaining > 0:
		record(stepProjects, false, fmt.Sprintf("%d namespace(s) terminating", remaining))
	default:
		record(stepProjects, true, fmt.Sprintf("%d project namespace(s) removed on the last pass", deleted))
	}

	// 2. The images, once no build can push: the projects are gone.
	if d.Step(stepProjects).Done {
		done, detail, err := r.deleteImages(ctx, ws)
		if err != nil {
			record(stepImages, false, err.Error())
		} else {
			record(stepImages, done, detail)
		}
	}

	// 3. The doors: the door loop removes what a gone workspace had; this
	// step waits until none is left.
	var doors networkingv1.IngressList
	if err := r.List(ctx, &doors, client.InNamespace(r.Config.SystemNamespace), client.MatchingLabels{"shpyrd.io/workspace-front-door": "true", shpyrdv1.LabelWorkspace: ws.Slug}); err != nil {
		record(stepDoors, false, err.Error())
	} else if n := len(doors.Items); n > 0 {
		record(stepDoors, false, fmt.Sprintf("%d front door(s) still up", n))
	} else {
		record(stepDoors, true, "front doors removed")
	}

	// 4. The rows, when everything else is done.
	allDone := d.Step(stepProjects).Done && d.Step(stepImages).Done && d.Step(stepDoors).Done
	if changed || allDone {
		w, err := r.Store.SetWorkspaceDeletion(ctx, ws.Slug, d)
		if err != nil {
			logger.Error(err, "workspace deletion: record")
			return
		}
		*ws = *w
	}
	if !allDone {
		return
	}
	d.Step(stepRows).Done = true
	w, err := r.Store.SetWorkspaceDeletion(ctx, ws.Slug, d)
	if err != nil {
		logger.Error(err, "workspace deletion: record")
		return
	}
	w, err = r.Store.TombstoneWorkspace(ctx, w.Slug)
	if err != nil {
		logger.Error(err, "workspace deletion: rows")
		return
	}
	*ws = *w
	delete(r.names, ws.Slug)
	if r.Forget != nil {
		r.Forget()
	}
	r.auditWorkspace(ctx, w, "workspace.deleted", fmt.Sprintf("%s; owners %s", w.DeleteReason, strings.Join(w.TombstoneOwners, ", ")))
	logger.Info("workspace deleted", "reason", w.DeleteReason, "owners", w.TombstoneOwners)
}

// deleteProjectNamespaces deletes the namespaces of the workspace's
// projects that are not terminating yet, and counts those still there.
func (r *WorkspaceReconciler) deleteProjectNamespaces(ctx context.Context, ws *store.Workspace) (remaining, deleted int, err error) {
	var list corev1.NamespaceList
	if err := r.List(ctx, &list, client.HasLabels{shpyrdv1.LabelProject}); err != nil {
		return 0, 0, err
	}
	short := ids.Short(ws.ID)
	for i := range list.Items {
		ns := &list.Items[i]
		if ns.Labels[shpyrdv1.LabelWorkspace] != ws.Slug && ns.Labels[shpyrdv1.LabelWorkspaceID] != short {
			continue
		}
		remaining++
		if !ns.DeletionTimestamp.IsZero() {
			continue
		}
		if err := r.Delete(ctx, ns); err != nil && !apierrors.IsNotFound(err) {
			return remaining, deleted, fmt.Errorf("delete namespace %s: %w", ns.Name, err)
		}
		deleted++
		log.FromContext(ctx).Info("project namespace deleted with its workspace", "workspace", ws.Slug, "namespace", ns.Name)
	}
	// The fake client and a fast cluster have them gone at once; a real
	// one terminates them over the next passes.
	var after corev1.NamespaceList
	if err := r.List(ctx, &after, client.HasLabels{shpyrdv1.LabelProject}); err != nil {
		return remaining, deleted, err
	}
	remaining = 0
	for i := range after.Items {
		ns := &after.Items[i]
		if ns.Labels[shpyrdv1.LabelWorkspace] == ws.Slug || ns.Labels[shpyrdv1.LabelWorkspaceID] == short {
			remaining++
		}
	}
	return remaining, deleted, nil
}

// deleteImages removes every tag of the workspace's repositories
// (apps/<workspace id>/...) from the in-cluster registry; the blobs go
// with the next collection. No registry: nothing to do.
func (r *WorkspaceReconciler) deleteImages(ctx context.Context, ws *store.Workspace) (done bool, detail string, err error) {
	if r.Config.RegistryHost == "" || r.Registry == nil {
		return true, "no in-cluster registry", nil
	}
	c, err := r.Registry(ctx)
	if err != nil {
		return false, "", err
	}
	repos, err := c.Catalog(ctx, 100000)
	if err != nil {
		return false, "", fmt.Errorf("registry catalog: %w", err)
	}
	prefix := "apps/" + ids.Short(ws.ID) + "/"
	images, repositories := 0, 0
	for _, repo := range repos {
		if !strings.HasPrefix(repo, prefix) {
			continue
		}
		repositories++
		tags, err := c.Tags(ctx, repo)
		if err != nil {
			return false, "", fmt.Errorf("registry tags of %s: %w", repo, err)
		}
		for _, tag := range tags {
			digest, err := c.Digest(ctx, repo, tag)
			if err != nil {
				return false, "", fmt.Errorf("registry %s:%s: %w", repo, tag, err)
			}
			if err := c.DeleteManifest(ctx, repo, digest); err != nil {
				return false, "", fmt.Errorf("registry delete %s@%s: %w", repo, digest, err)
			}
			images++
		}
	}
	return true, fmt.Sprintf("%d image(s) removed from %d repositor(ies); blobs go with the next collection", images, repositories), nil
}

// auditWorkspace records a lifecycle event against the cluster, for the
// console's activity; nothing when the reconciler has no clientset.
func (r *WorkspaceReconciler) auditWorkspace(ctx context.Context, ws *store.Workspace, action, detail string) {
	if r.Kube == nil {
		return
	}
	entry := audit.Entry{Time: time.Now(), Actor: "platform", Action: action, Target: ws.Slug, Detail: detail, Via: "controller", Realm: "console"}
	if err := audit.Record(ctx, r.Kube, audit.ClusterRef(r.Config.SystemNamespace), entry); err != nil {
		log.FromContext(ctx).Info("could not audit the workspace lifecycle", "action", action, "err", err.Error())
	}
}
