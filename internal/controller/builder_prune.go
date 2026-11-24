package controller

import (
	"context"
	"regexp"
	"sort"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/registry"
)

// Pruning of a project's builder repository.
//
// kpack saves the project's builder under two named tags and, each time it
// saves a digest other than the one under the builder's named tag, under a
// tag that is the time (YYYYMMDDhhmmss). The time tags pile up, and every
// manifest they name stays tagged, so the weekly collection reclaims none of
// them. The pass below removes time tags that are old and name a digest
// nothing uses any more. It removes tags only, never manifests: what is left
// untagged is the collection's to reclaim.
//
// What is in use is read from the fields kpack writes (shpyrd-les-001): the
// Builder's status and the Builds that have not finished. Names and order
// of tags say nothing about it.

const (
	// builderPruneMargin is how old a time tag must be before it may go.
	builderPruneMargin = 24 * time.Hour
	// builderPruneMaxDeletes bounds the tags one pass removes.
	builderPruneMaxDeletes = 50
	// builderPruneAgain is when a pass that has more to do, or that failed,
	// runs again.
	builderPruneAgain = time.Minute
	// builderPruneIdle is how long a repository is left alone once a pass
	// has nothing more to remove.
	builderPruneIdle = time.Hour
)

// builderPruneBudget bounds the time of one pass. A variable so a test can
// shorten it.
var builderPruneBudget = 10 * time.Second

var (
	// timeTagPattern is a time tag: exactly 14 digits.
	timeTagPattern = regexp.MustCompile(`^[0-9]{14}$`)
	// digestPattern is a digest as the registry writes it.
	digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

// builderPruneState is what is remembered, in memory, of one builder
// repository.
type builderPruneState struct {
	// next is the time before which no pass runs.
	next time.Time
	// again says that next is a retry: the pass had more to do.
	again bool
	// cursor is the last time tag a pass finished with, so that the next
	// pass goes on after it: a bounded pass must not look at the same
	// first tags for ever.
	cursor string
}

// pruneBuilderTags runs one bounded pass over the project's builder
// repository and returns when the reconcile should run again for it (0:
// no need). Nothing in it changes the project, and nothing in it fails the
// reconcile: what goes wrong is logged and tried again later.
func (r *AppReconciler) pruneBuilderTags(ctx context.Context, app *shpyrdv1.App) time.Duration {
	if !app.DeletionTimestamp.IsZero() || !r.Config.RegistryDeletes {
		return 0
	}
	logger := log.FromContext(ctx).WithName("builder-prune")
	tag, err := r.Config.builderTag(app)
	if err != nil {
		return 0 // the project has no repository yet
	}
	host, repo, _ := registry.SplitReference(tag)
	now := r.now()
	state := r.builderPruneStateOf(repo)
	if now.Before(state.next) {
		if state.again {
			return state.next.Sub(now)
		}
		return 0
	}
	if r.registryInMaintenance(ctx) {
		return 0 // a reconcile follows the maintenance on its own
	}

	// What is in use, from the cluster. Nothing has been asked of the
	// registry yet.
	keep, named, ok, retry := r.builderInUse(ctx, app, host, repo)
	if retry {
		return r.builderPruneLater(repo, state, now, true)
	}
	if !ok {
		return 0
	}

	cl, err := r.registryClient(ctx)
	if err != nil {
		logger.Info("registry client", "err", err.Error())
		return r.builderPruneLater(repo, state, now, true)
	}
	if cl.Host != host {
		return 0
	}
	ctx, cancel := context.WithTimeout(ctx, builderPruneBudget)
	defer cancel()

	// The registry must say what the Builder says before anything goes.
	for name, want := range named {
		got, err := cl.Digest(ctx, repo, name)
		if err != nil {
			logger.Info("could not resolve a builder tag", "tag", name, "err", err.Error())
			return r.builderPruneLater(repo, state, now, true)
		}
		if got != want {
			logger.Info("a builder tag and the Builder disagree; nothing is removed", "tag", name)
			return r.builderPruneLater(repo, state, now, true)
		}
	}

	tags, err := cl.Tags(ctx, repo)
	if err != nil {
		logger.Info("could not list the builder repository", "err", err.Error())
		return r.builderPruneLater(repo, state, now, true)
	}
	var old []string
	for _, t := range tags {
		if timeTagOlderThan(t, now, builderPruneMargin) {
			old = append(old, t)
		}
	}
	sort.Strings(old)
	// Go on after the last tag the previous pass finished with, and come
	// round to the start.
	start := sort.SearchStrings(old, state.cursor)
	if start < len(old) && old[start] == state.cursor {
		start++
	}
	order := append(append([]string{}, old[start:]...), old[:start]...)

	deleted, visited, failed := 0, 0, false
	cursor := state.cursor
	for _, t := range order {
		if deleted >= builderPruneMaxDeletes || ctx.Err() != nil {
			break
		}
		digest, err := cl.Digest(ctx, repo, t)
		switch {
		case registry.IsNotFound(err):
			// Gone since the listing.
		case err != nil:
			failed = true
		case !keep[digest]:
			if err = cl.DeleteTag(ctx, repo, t); err != nil {
				failed = true
			} else {
				deleted++
			}
		}
		if failed {
			logger.Info("could not prune a builder tag; it is tried again", "tag", t, "err", err.Error())
			break
		}
		visited++
		cursor = t
	}
	if deleted > 0 {
		logger.Info("pruned builder tags", "repository", repo, "removed", deleted)
	}
	if !failed && visited == len(order) {
		r.builderPrune.Store(repo, builderPruneState{next: now.Add(builderPruneIdle)})
		return 0
	}
	r.builderPrune.Store(repo, builderPruneState{next: now.Add(builderPruneAgain), again: true, cursor: cursor})
	return builderPruneAgain
}

func (r *AppReconciler) builderPruneStateOf(repo string) builderPruneState {
	if v, ok := r.builderPrune.Load(repo); ok {
		return v.(builderPruneState)
	}
	return builderPruneState{}
}

// builderPruneLater asks for another pass in a minute, keeping the cursor.
func (r *AppReconciler) builderPruneLater(repo string, state builderPruneState, now time.Time, again bool) time.Duration {
	r.builderPrune.Store(repo, builderPruneState{next: now.Add(builderPruneAgain), again: again, cursor: state.cursor})
	return builderPruneAgain
}

// builderInUse reads what a pass must keep. keep holds the digests in use;
// named maps the two named tags to the digest the Builder says each one has,
// empty when the project no longer composes its build and its Builder is
// confirmed absent. ok false says there is nothing to prune now (no registry
// request has been made); retry says the cluster could not be read and the
// pass should run again soon.
func (r *AppReconciler) builderInUse(ctx context.Context, app *shpyrdv1.App, host, repo string) (keep map[string]bool, named map[string]string, ok, retry bool) {
	logger := log.FromContext(ctx).WithName("builder-prune")
	reader := r.uncachedReader()
	keep = map[string]bool{}
	named = map[string]string{}

	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(BuilderGVK)
	err := reader.Get(ctx, client.ObjectKey{Namespace: app.Namespace, Name: builderName(app)}, b)
	switch {
	case err == nil:
		if !composesBuild(app) {
			return nil, nil, false, false // being removed
		}
		builder, run, ready := builderStatusDigests(b, host, repo)
		if !ready {
			return nil, nil, false, false
		}
		keep[builder], keep[run] = true, true
		prefix := "builder-" + app.Namespace + "-" + builderName(app)
		named[prefix], named[prefix+"-run-image"] = builder, run
	case apierrors.IsNotFound(err) && !composesBuild(app):
		// No status to read: only the builds in progress keep anything.
	case apierrors.IsNotFound(err):
		return nil, nil, false, false // not made yet
	default:
		logger.Info("could not read the Builder; nothing is removed", "err", err.Error())
		return nil, nil, false, true
	}

	builds := &unstructured.UnstructuredList{}
	builds.SetGroupVersionKind(KpackBuildGVK.GroupVersion().WithKind(KpackBuildGVK.Kind + "List"))
	if err := reader.List(ctx, builds, client.InNamespace(app.Namespace)); err != nil {
		logger.Info("could not read the builds; nothing is removed", "err", err.Error())
		return nil, nil, false, true
	}
	for i := range builds.Items {
		if kpackBuildFinished(&builds.Items[i]) {
			continue
		}
		for _, path := range [][]string{{"spec", "builder", "image"}, {"spec", "runImage", "image"}} {
			ref, _, _ := unstructured.NestedString(builds.Items[i].Object, path...)
			_, _, digest := registry.SplitReference(ref)
			if !digestPattern.MatchString(digest) {
				// An image in use that cannot be told: keep everything.
				logger.Info("a build in progress names an image without a digest; nothing is removed", "build", builds.Items[i].GetName())
				return nil, nil, false, false
			}
			keep[digest] = true
		}
	}
	return keep, named, true, false
}

// uncachedReader reads past the cache, as the Builds are read elsewhere.
func (r *AppReconciler) uncachedReader() client.Reader {
	if r.APIReader != nil {
		return r.APIReader
	}
	return r.Client
}

// builderStatusDigests reads the digest of the Builder's current image and
// of its run image, and says the Builder is ready and both are in the
// project's builder repository. Anything less and ready is false.
func builderStatusDigests(b *unstructured.Unstructured, host, repo string) (builder, run string, ready bool) {
	if !unstructuredReady(b) {
		return "", "", false
	}
	if gen, found, _ := unstructured.NestedInt64(b.Object, "status", "observedGeneration"); found && gen != b.GetGeneration() {
		return "", "", false // the status is of an older spec
	}
	digestOf := func(path ...string) (string, bool) {
		ref, _, _ := unstructured.NestedString(b.Object, path...)
		h, rp, digest := registry.SplitReference(ref)
		return digest, h == host && rp == repo && digestPattern.MatchString(digest)
	}
	builder, ok1 := digestOf("status", "latestImage")
	run, ok2 := digestOf("status", "stack", "runImage")
	if !ok1 || !ok2 {
		return "", "", false
	}
	return builder, run, true
}

// unstructuredReady says the Ready condition is True.
func unstructuredReady(u *unstructured.Unstructured) bool {
	conds, _, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
	for _, raw := range conds {
		if m, ok := raw.(map[string]interface{}); ok && m["type"] == "Ready" {
			return m["status"] == "True"
		}
	}
	return false
}

// kpackBuildFinished says the Build's Succeeded condition is True or False;
// a Build without it, or with it Unknown, has not finished.
func kpackBuildFinished(b *unstructured.Unstructured) bool {
	conds, _, _ := unstructured.NestedSlice(b.Object, "status", "conditions")
	for _, raw := range conds {
		if m, ok := raw.(map[string]interface{}); ok && m["type"] == "Succeeded" {
			return m["status"] == "True" || m["status"] == "False"
		}
	}
	return false
}

// timeTagOlderThan says the tag is exactly 14 digits that read as a date
// and time (UTC), and that time is more than d before now. A time in the
// future is not.
func timeTagOlderThan(tag string, now time.Time, d time.Duration) bool {
	if !timeTagPattern.MatchString(tag) {
		return false
	}
	t, err := time.ParseInLocation("20060102150405", tag, time.UTC)
	if err != nil {
		return false
	}
	return now.Sub(t) > d
}
