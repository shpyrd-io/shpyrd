package controller

import (
	"context"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/pages"
	"github.com/shpyrd-io/shpyrd/pkg/project"
)

// HTTP sleep (RFC-0075): a web process with a sleep policy scales to zero
// after its quiet period and wakes on the first request. The objects that
// do it are KEDA's, shared with autoscaling (RFC-0047): scaling.go renders
// them; this file holds the policy — what applies, where it comes from,
// and the waking page.

// sleepEnabled reports whether the web process has a valid sleep policy,
// its own or the workspace's default.
func (r *AppReconciler) sleepEnabled(app *shpyrdv1.App) bool {
	if maintenance(app) {
		return false
	}
	spec := r.webSleepSpec(app)
	return spec != nil && parseSleepDuration(spec.After) > 0
}

// webSleepSpec returns the effective SleepSpec of the web process, when
// processes may sleep at all (Config.SleepAllowed): the process's own when
// set (an explicit "off" is a policy too — it opts the
// project out of the workspace default), else the workspace's default,
// else nil.
func (r *AppReconciler) webSleepSpec(app *shpyrdv1.App) *shpyrdv1.SleepSpec {
	if r.Config.SleepAllowed == nil || !r.Config.SleepAllowed() {
		return nil // nothing sleeps by itself without auto sleep
	}
	for _, p := range processes(app) {
		if p.Name == "web" && p.Sleep != nil {
			return p.Sleep
		}
	}
	if r.Config.WorkspaceSleepDefault == nil {
		return nil
	}
	after, resuming := r.Config.WorkspaceSleepDefault(app.Labels[shpyrdv1.LabelWorkspace])
	if parseSleepDuration(after) == 0 {
		return nil
	}
	return &shpyrdv1.SleepSpec{After: after, Resuming: resuming}
}

// sleepSource says where the effective policy comes from, for the status:
// "project" for a quiet period of the project's own, "off" for an explicit
// off (awake whatever the workspace's default, #135), "workspace" otherwise.
func (r *AppReconciler) sleepSource(app *shpyrdv1.App) string {
	for _, p := range processes(app) {
		if p.Name == "web" && p.Sleep != nil {
			if parseSleepDuration(p.Sleep.After) > 0 {
				return "project"
			}
			return "off"
		}
	}
	return "workspace"
}

// webSleepStatus is the read-back of the web process's sleep (#120): the
// quiet period and wake mode as they apply, where they come from, and the
// state before the reconcile decides it. A policy that auto sleep does not
// honour (no license) is reported as asked, with the state unavailable.
func (r *AppReconciler) webSleepStatus(app *shpyrdv1.App, mounts []resolvedMount) *shpyrdv1.SleepStatus {
	st := &shpyrdv1.SleepStatus{State: "awake", Source: r.sleepSource(app)}
	if st.Source == "off" {
		return st
	}
	if sp := r.webSleepSpec(app); sp != nil && parseSleepDuration(sp.After) > 0 {
		st.After, st.Resuming = sp.After, sleepResuming(sp, mounts)
		return st
	}
	if st.Source == "project" {
		for _, p := range processes(app) {
			if p.Name == "web" && p.Sleep != nil {
				st.After, st.Resuming = p.Sleep.After, sleepResuming(p.Sleep, mounts)
			}
		}
		if !maintenance(app) {
			st.State, st.Message = "unavailable", sleepLicensedMessage
		}
	}
	return st
}

// sleepResuming is the wake mode that applies: the policy's (wait when it
// says none), except that an app with a disk resumes behind the waiting
// page, whatever the policy says: the disk takes a moment to reattach,
// longer than a held connection should wait. The API refuses "wait" on a
// process with a disk; a workspace default saying "wait", or a disk added
// later, lands here.
func sleepResuming(sp *shpyrdv1.SleepSpec, mounts []resolvedMount) string {
	if hasDisk(mounts) {
		return "page"
	}
	return firstNonEmpty(sp.Resuming, "wait")
}

// hasDisk says the process mounts a single-instance volume (a disk).
func hasDisk(mounts []resolvedMount) bool {
	for _, m := range mounts {
		if !m.Shared {
			return true
		}
	}
	return false
}

// sleepLicensedMessage is what the status says when a policy is set and
// auto sleep is off on the platform (the API refuses new ones; an existing
// one, or a lapsed license, leaves it in place).
const sleepLicensedMessage = "sleep is set but auto sleep is off on this platform: available with a license"

// parseSleepDuration parses the after value; returns 0 when disabled.
func parseSleepDuration(after string) time.Duration {
	after = strings.TrimSpace(strings.ToLower(after))
	if after == "" || after == "off" || after == "false" {
		return 0
	}
	d, err := time.ParseDuration(after)
	if err != nil || d < 5*time.Minute {
		return 0
	}
	if d > 24*time.Hour {
		return 24 * time.Hour
	}
	return d
}

// sleepUnavailableMessage is what the process status says when the policy
// is set but the cluster lacks the KEDA HTTP add-on.
const sleepUnavailableMessage = "sleep needs the sleep extension (shpyrd-ctl extensions enable sleep)"

// scaledObjectGrace is how long a ScaledObject may stay not Ready before
// sleep is torn down for the app. KEDA treats a trigger it cannot evaluate
// as inactive and scales the Deployment to zero at once, so a broken
// scaler is an outage, not a no-op: the controller notices, removes the
// objects (KEDA then restores the replica count and the Ingress goes back
// to the app's Service) and pauses sleep for this spec generation.
const scaledObjectGrace = 3 * time.Minute

// sleepPause is why sleep is paused for an app, and for which generation.
type sleepPause struct {
	generation int64
	reason     string
}

// sleepPause reports whether sleep is paused for the app's current spec.
func (r *AppReconciler) sleepPause(app *shpyrdv1.App) (sleepPause, bool) {
	v, ok := r.sleepPaused.Load(app.UID)
	if !ok {
		return sleepPause{}, false
	}
	p := v.(sleepPause)
	if p.generation != app.Generation {
		r.sleepPaused.Delete(app.UID) // a new policy retries
		return sleepPause{}, false
	}
	return p, true
}

// scaledObjectBroken reports whether the ScaledObject has been not Ready
// for longer than the grace period, and KEDA's message.
func (r *AppReconciler) scaledObjectBroken(so *unstructured.Unstructured) (bool, string) {
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}
	if now.Sub(so.GetCreationTimestamp().Time) < scaledObjectGrace {
		return false, ""
	}
	conditions, _, _ := unstructured.NestedSlice(so.Object, "status", "conditions")
	for _, c := range conditions {
		m, _ := c.(map[string]interface{})
		if m["type"] == "Ready" && m["status"] == "False" {
			msg, _ := m["message"].(string)
			return true, msg
		}
	}
	return false, ""
}

// ensureSleepPage creates or updates the ConfigMap that serves the branded
// "waking up" HTML when resuming: page is set.
func (r *AppReconciler) ensureSleepPage(ctx context.Context, app *shpyrdv1.App) error {
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: sleepPageCMName(app), Namespace: app.Namespace,
		Labels: map[string]string{
			shpyrdv1.LabelApp:            app.Name,
			shpyrdv1.LabelManagedBy:      "shpyrd",
			"http.keda.sh/response-body": "true",
		},
	}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		cm.Data = map[string]string{"index.html": buildSleepPageHTML(project.DisplayName(app))}
		return controllerutil.SetControllerReference(app, cm, r.Scheme)
	})
	return err
}

func sleepPageCMName(app *shpyrdv1.App) string { return app.Name + "-sleep-page" }

// buildSleepPageHTML is the page KEDA's interceptor answers with while
// the app wakes up: it opens itself again every few seconds, until the
// app answers. It travels whole in a ConfigMap, so it is one file.
func buildSleepPageHTML(name string) string {
	return pages.HTML(pages.Waking, pages.Page{
		Title:   "Waking " + name + " up.",
		Text:    "It was asleep, as it is when nobody asks for it. It answers in a few seconds, up to a minute when it has a disk or a database to wake too; this page goes to it by itself.",
		Refresh: 3,
	})
}

// isNoMatchKind returns true when KEDA's CRDs are not installed.
func isNoMatchKind(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "no matches for kind") ||
		strings.Contains(err.Error(), "no kind is registered"))
}

// makeUnstructured creates an empty unstructured object.
func makeUnstructured(gvk schema.GroupVersionKind, name, namespace string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)
	u.SetName(name)
	u.SetNamespace(namespace)
	return u
}
