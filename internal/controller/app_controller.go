package controller

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/audit"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
	"github.com/shpyrd-io/shpyrd/pkg/logs"
	"github.com/shpyrd-io/shpyrd/pkg/sizes"
)

// AppReconciler turns an App into a kpack Image plus one Deployment (and
// Service) per process type and an Ingress for the web process.
type AppReconciler struct {
	// BindableTypes are resource kinds apps can attach (from enabled
	// extensions); the controller watches them so an app is re-rendered
	// when its database becomes ready.
	BindableTypes []schema.GroupVersionKind

	client.Client
	// APIReader bypasses the cache for one-off reads (kpack Builds).
	APIReader client.Reader
	Scheme    *runtime.Scheme
	Recorder  record.EventRecorder
	Config    Config
	// ProcessTypes reads an image's process types (RFC-0066); nil uses the
	// platform registry. Tests inject one.
	ProcessTypes func(ctx context.Context, image string) []string
	// mirror remembers what the store already knows per App (RFC-0076).
	mirror projectMirror
	// Now returns the current time; nil means time.Now (tests override it).
	Now func() time.Time
	// sleepPaused remembers apps whose sleep objects were torn down because
	// KEDA could not scale them (RFC-0075): App UID → sleepPause. A new
	// policy (a new spec generation) retries. In memory on purpose: after a
	// restart the objects are tried once more and paused again if still
	// broken, and the status says why either way.
	sleepPaused sync.Map
	// Resolver checks custom domains' DNS (RFC-0034); nil uses the system's.
	Resolver interface {
		LookupCNAME(ctx context.Context, host string) (string, error)
		LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
	}
	// LookupLB returns the external front door's address when the Config
	// does not carry one (it may not exist at start); nil disables.
	LookupLB func(ctx context.Context) string
	// Kube writes the audit entries of a release's outcome (RFC-0022a);
	// nil records nothing.
	Kube kubernetes.Interface
	// workspaceEvents carries the Apps of a workspace whose names changed
	// (RequeueWorkspace): nothing of theirs changes, so no watch sees it.
	workspaceEvents chan event.GenericEvent
}

// RequeueGitHub reconciles the Apps of a workspace that build from
// github.com again: its GitHub connections changed (RFC-0054), and with
// them the credential <app>-git holds.
func (r *AppReconciler) RequeueGitHub(ctx context.Context, slug string) {
	r.requeueApps(ctx, slug, func(app *shpyrdv1.App) bool { return buildsFromGitHub(app) })
}

// buildsFromGitHub says the App's source is a repository on github.com.
func buildsFromGitHub(app *shpyrdv1.App) bool {
	if app.Spec.Source == nil || app.Spec.Source.Git == nil {
		return false
	}
	repo, err := gitauth.Parse(app.Spec.Source.Git.URL)
	return err == nil && strings.EqualFold(repo.Host, "github.com")
}

// RequeueWorkspace reconciles every App of a workspace again: its address
// or its domains changed, and with them the apps' hosts (RFC-0033 names).
// The default workspace's include the Apps from before workspaces, which
// carry no workspace label.
func (r *AppReconciler) RequeueWorkspace(ctx context.Context, slug string) {
	r.requeueApps(ctx, slug, func(*shpyrdv1.App) bool { return true })
}

// requeueApps reconciles the Apps of a workspace that pick says again.
func (r *AppReconciler) requeueApps(ctx context.Context, slug string, pick func(*shpyrdv1.App) bool) {
	if r.workspaceEvents == nil {
		return
	}
	var apps shpyrdv1.AppList
	if err := r.List(ctx, &apps); err != nil {
		log.FromContext(ctx).Error(err, "requeue the apps of a workspace", "workspace", slug)
		return
	}
	var picked []client.Object
	for i := range apps.Items {
		if workspaceOf(&apps.Items[i]) == slug && pick(&apps.Items[i]) {
			picked = append(picked, &apps.Items[i])
		}
	}
	// The channel is the controller's queue's way in; never block the
	// workspace pass on it.
	go func() {
		for _, app := range picked {
			r.workspaceEvents <- event.GenericEvent{Object: app}
		}
	}()
}

// SetupWithManager registers the controller and its watches.
func (r *AppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	b := r.builder(mgr)
	for _, gvk := range r.BindableTypes {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)
		kind := gvk.Kind
		b = b.Watches(obj, handler.EnqueueRequestsFromMapFunc(r.boundResourceToApps(kind)))
	}
	return b.Complete(r)
}

// boundResourceToApps maps a bindable resource to the apps attaching it.
func (r *AppReconciler) boundResourceToApps(kind string) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		var apps shpyrdv1.AppList
		if err := r.List(ctx, &apps, client.InNamespace(obj.GetNamespace())); err != nil {
			return nil
		}
		var reqs []reconcile.Request
		for _, app := range apps.Items {
			for _, b := range app.Spec.Bindings {
				if b.Kind == kind && b.Name == obj.GetName() {
					reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&app)})
					break
				}
			}
		}
		return reqs
	}
}

// builder is the controller definition without the extension watches.
func (r *AppReconciler) builder(mgr ctrl.Manager) *builder.Builder {
	r.Config = r.Config.Defaults()
	if r.APIReader == nil {
		r.APIReader = mgr.GetAPIReader()
	}
	kpackImage := &unstructured.Unstructured{}
	kpackImage.SetGroupVersionKind(KpackImageGVK)
	if r.workspaceEvents == nil {
		r.workspaceEvents = make(chan event.GenericEvent, 1024)
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named("app").
		For(&shpyrdv1.App{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Owns(kpackImage).
		Owns(&batchv1.Job{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.secretToApps)).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.workspaceTLSToApps)).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(r.sizesToAllApps)).
		Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(runPodToApp), builder.WithPredicates(isRunPod)).
		Watches(&shpyrdv1.Volume{}, handler.EnqueueRequestsFromMapFunc(r.volumeToApps)).
		WatchesRawSource(source.Channel(r.workspaceEvents, &handler.EnqueueRequestForObject{}))
}

// sizesToAllApps requeues every App when the size catalog changes so their
// Deployments pick up new allocations.
func (r *AppReconciler) sizesToAllApps(ctx context.Context, obj client.Object) []reconcile.Request {
	if obj.GetName() != sizes.ConfigMapName || obj.GetNamespace() != r.Config.SystemNamespace {
		return nil
	}
	var apps shpyrdv1.AppList
	if err := r.List(ctx, &apps); err != nil {
		return nil
	}
	out := make([]reconcile.Request, 0, len(apps.Items))
	for _, a := range apps.Items {
		out = append(out, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: a.Namespace, Name: a.Name}})
	}
	return out
}

// catalog loads the size catalog from the cluster, falling back to the
// built-in defaults when it is missing or invalid.
func (r *AppReconciler) catalog(ctx context.Context) sizes.Catalog {
	return loadCatalog(ctx, r.Client, r.Config.SystemNamespace)
}

// envSecretToApp maps Secret <app>-env to its App in the same namespace.
func envSecretToApp(_ context.Context, obj client.Object) []reconcile.Request {
	name, ok := strings.CutSuffix(obj.GetName(), shpyrdv1.EnvSecretSuffix)
	if !ok || name == "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: obj.GetNamespace(), Name: name}}}
}

// Reconcile implements the App state machine:
//
//	Pending   no source and no image
//	Building  kpack Image not Ready (a previous image may keep running)
//	Deploying workloads rolling out
//	Running   every process has its desired replicas ready
//	Failed    build failed or reconcile error
func (r *AppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	app := &shpyrdv1.App{}
	if err := r.Get(ctx, req.NamespacedName, app); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !app.DeletionTimestamp.IsZero() {
		return r.finalizeProject(ctx, app)
	}
	// Identity first (RFC-0076): a legacy App gets its id and labels in one
	// write. It happens before orig is taken, so the optimistic status
	// patch below still compares against the version this reconcile
	// worked from; nothing else may write the App in between.
	if _, err := r.ensureIdentity(ctx, app); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}
	orig := app.DeepCopy()

	out, err := r.reconcile(ctx, app)
	if apierrors.IsConflict(err) {
		// A stale cached object (typically a Deployment the deployment
		// controller just touched): retry, this is not an app failure.
		logger.V(1).Info("conflict during reconcile, retrying", "error", err.Error())
		return ctrl.Result{Requeue: true}, nil
	}
	if err != nil {
		logger.Error(err, "reconcile failed")
		app.Status.Phase = shpyrdv1.PhaseFailed
		app.Status.Message = err.Error()
		setCondition(app, shpyrdv1.ConditionReady, metav1.ConditionFalse, "ReconcileError", err.Error())
		r.Recorder.Event(app, corev1.EventTypeWarning, "ReconcileError", err.Error())
	}
	app.Status.ObservedGeneration = app.Generation

	// Optimistic locking: a reconcile working from a stale cache read must
	// not overwrite a newer status (e.g. re-record a release). Conflicts
	// simply requeue. Nothing else may write the App before this patch, or
	// the resourceVersion check fails.
	if statusErr := r.Status().Patch(ctx, app, client.MergeFromWithOptions(orig, client.MergeFromWithOptimisticLock{})); statusErr != nil {
		if apierrors.IsConflict(statusErr) {
			logger.V(1).Info("status conflict, requeueing")
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("update status: %w", statusErr)
	}

	// Side effects only once the status is durable.
	if out.newRelease != nil {
		r.Recorder.Eventf(app, corev1.EventTypeNormal, "Release", "v%d: %s", out.newRelease.Number, out.newRelease.Description)
	}
	r.auditReleaseOutcome(ctx, orig, app)
	if out.clearNote {
		r.clearReleaseNote(ctx, app)
	}
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	if next, gcErr := r.gcRunPods(ctx, app); gcErr != nil {
		logger.Error(gcErr, "clean up one-off instances")
	} else if next > 0 && (out.result.RequeueAfter == 0 || next < out.result.RequeueAfter) {
		out.result.RequeueAfter = next
	}
	if out.domainsPending && (out.result.RequeueAfter == 0 || out.result.RequeueAfter > 30*time.Second) {
		out.result.RequeueAfter = 30 * time.Second
	}
	// A build cloning through the GitHub App gets a new token before the
	// one in <app>-git is 50 minutes old (RFC-0054); only a reconcile that
	// wrote <app>-git knows when.
	if d := out.gitRefresh; d > 0 && (out.result.RequeueAfter == 0 || d < out.result.RequeueAfter) {
		out.result.RequeueAfter = d
	}
	return out.result, nil
}

// outcome carries what Reconcile must do after the status has been written.
type outcome struct {
	// domainsPending asks for a 30 s poll while a custom domain's DNS or
	// certificate is not settled (RFC-0034).
	domainsPending bool
	result         ctrl.Result
	newRelease     *shpyrdv1.Release
	clearNote      bool
	// gitRefresh is when <app>-git, written by this reconcile, needs the
	// GitHub App again (RFC-0054); 0: never.
	gitRefresh time.Duration
}

func requeue(d time.Duration) outcome { return outcome{result: ctrl.Result{RequeueAfter: d}} }

func (r *AppReconciler) reconcile(ctx context.Context, app *shpyrdv1.App) (outcome, error) {
	var gitRefresh time.Duration
	out, err := r.reconcileApp(ctx, app, &gitRefresh)
	out.gitRefresh = gitRefresh
	return out, err
}

// reconcileApp is reconcile; it sets gitRefresh once <app>-git is written.
func (r *AppReconciler) reconcileApp(ctx context.Context, app *shpyrdv1.App, gitRefresh *time.Duration) (outcome, error) {
	if maintenance(app) && app.Annotations[shpyrdv1.AnnotationMaintenance] != "starting" {
		return r.reconcileMaintenance(ctx, app)
	}
	if !maintenance(app) {
		if err := r.resumeMaintenanceJobs(ctx, app); err != nil {
			return outcome{}, err
		}
	}
	// 0. A requested rollback restores that release's config vars first, so
	// the release recorded below carries both the build and the config.
	var secret *corev1.Secret
	if n, err := strconv.Atoi(app.Annotations[shpyrdv1.AnnotationRollbackTo]); err == nil && n > 0 {
		restored, err := r.restoreRelease(ctx, app, n)
		if err != nil {
			return outcome{}, err
		}
		secret = restored
	}

	// 1. Configuration fingerprint.
	if secret == nil {
		secret = &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: app.EnvSecretName()}, secret); err != nil {
			if !apierrors.IsNotFound(err) {
				return outcome{}, fmt.Errorf("get env secret: %w", err)
			}
			secret = nil
		}
	}
	pending, err := r.pendingConfig(ctx, app)
	if err != nil {
		return outcome{}, err
	}
	// Attached resources contribute config vars through <app>-bindings. A
	// resource that is still provisioning is not a failure: the app keeps
	// its current release until the binding can be rendered.
	bindings, err := r.reconcileBindings(ctx, app)
	if err != nil {
		var notReady *NotReadyError
		if errors.As(err, &notReady) {
			app.Status.Phase = shpyrdv1.PhasePending
			app.Status.Message = "waiting for an attached resource: " + notReady.Msg
			setCondition(app, shpyrdv1.ConditionReady, metav1.ConditionFalse, "WaitingForResource", app.Status.Message)
			return requeue(15 * time.Second), nil
		}
		return outcome{}, err
	}
	// Cluster-wide config vars arrive through a per-project mirror the
	// processes read first (RFC-0016).
	r.labelRunningInstances(ctx, app)
	if err := r.reconcileRegistryCredentials(ctx, app); err != nil {
		return outcome{}, err
	}
	gitBuild, err := r.reconcileGitCredentials(ctx, app)
	if err != nil {
		return outcome{}, err
	}
	*gitRefresh = r.githubRefreshOf(gitBuild)
	if err := r.ensureBuildServiceAccount(ctx, app, gitBuild); err != nil {
		return outcome{}, err
	}
	globals, err := r.reconcileGlobals(ctx, app)
	if err != nil {
		return outcome{}, err
	}
	ghash := globalHash(globals)
	if err := r.ensureNamespaceLabels(ctx, app); err != nil {
		return outcome{}, err
	}
	if err := r.mirrorProject(ctx, app); err != nil {
		return outcome{}, err
	}
	if err := r.reconcileMovedHosts(ctx, app); err != nil {
		return outcome{}, err
	}
	if err := r.reconcileIsolation(ctx, app); err != nil {
		return outcome{}, err
	}
	if err := r.reconcileQuota(ctx, app); err != nil {
		return outcome{}, err
	}

	// What the source watcher saw of the branch (RFC-0018).
	app.Status.Source = sourceStatus(app)
	setSourceCondition(app)

	// 2. Image: pinned, or built from the source (kpack Image for
	// buildpacks, BuildKit Job for Dockerfiles).
	image := app.Spec.Image
	var build buildState
	var kpackBuild *unstructured.Unstructured
	if app.HasSource() {
		if msg, failed := waitingForCommit(app); msg != "" {
			// No commit pinned for the branch yet: nothing is built, and
			// what runs keeps running.
			build = buildState{Ready: "Unknown", Reason: reasonWaitingForCommit, Message: msg, LatestImage: app.Status.Image, LatestBuild: app.Status.LatestBuild}
			if failed {
				build.Ready, build.Reason = "False", reasonSourceUnreadable
			}
		} else if app.BuildStrategy() == shpyrdv1.StrategyDockerfile {
			st, err := r.reconcileDockerfileBuild(ctx, app, gitBuild)
			if err != nil {
				return outcome{}, err
			}
			build = st
			if err := r.reconcileBuildEnv(ctx, app, nil); err != nil {
				return outcome{}, err
			}
			// A strategy switch leaves a kpack Image behind; drop it.
			stale := kpackImageKey(app)
			if err := r.Get(ctx, client.ObjectKeyFromObject(stale), stale); err == nil {
				if err := r.deleteIfExists(ctx, stale); err != nil {
					return outcome{}, err
				}
			}
		} else {
			buildSecret := secret
			if pending != nil {
				buildSecret = pending
			}
			vars := buildVars(app, globals, buildSecret, bindings)
			if err := r.reconcileBuildEnv(ctx, app, vars); err != nil {
				return outcome{}, err
			}
			img, err := r.reconcileKpackImage(ctx, app, gitBuild)
			if errors.Is(err, errImageMoving) {
				app.Status.Phase = shpyrdv1.PhaseBuilding
				app.Status.Message = "moving the build to its new image repository"
				setCondition(app, shpyrdv1.ConditionBuilt, metav1.ConditionUnknown, "Moving", app.Status.Message)
				return requeue(5 * time.Second), nil
			}
			if err != nil {
				return outcome{}, err
			}
			build = readBuildState(img)
			if build.Ready == "Unknown" {
				// kpack names the Build it starts in latestBuildRef, but a
				// status update lost at that moment (a conflict) is not made
				// again while that Build runs: latestBuildRef stays empty, or
				// names the build before, until it ends, and a deploy waiting
				// for its build sees none start. The newest Build of the
				// Image is the one in progress.
				if b := r.newestKpackBuild(ctx, img, build.LatestBuild); b != "" {
					build.LatestBuild = b
				}
			}
			if build.LatestBuild != "" {
				kpackBuild = r.getBuild(ctx, app.Namespace, build.LatestBuild)
			}
			// kpack's own sentence names a pod and kubectl: the customer
			// reads what failed, in words, instead (#52).
			if build.Ready == "False" {
				switch {
				case kpackBuild != nil && buildFailed(kpackBuild):
					build.Message = r.kpackBuildFailure(ctx, kpackBuild)
				case composesBuild(app) && build.builderNotReady():
					// A builder of the project's own is made on its first
					// deploy, and kpack waits for it (#143).
					if msg := r.projectBuilderFailure(ctx, app, build.ReadySince); msg != "" {
						build.Message = msg
					} else {
						build.Ready = "Unknown"
						build.Reason = "PreparingBuilder"
						build.Message = "Preparing this project's builder (usually about a minute); wait for the build to start."
					}
				default:
					build.Message = "The build could not start: a fault of the platform, not of the app. Deploying again later usually passes."
				}
			}
		}
		app.Status.LatestBuild = build.LatestBuild
		if image == "" {
			image = build.LatestImage
		}
		switch build.Ready {
		case "True":
			setCondition(app, shpyrdv1.ConditionBuilt, metav1.ConditionTrue, "BuildSucceeded", "image "+shortImage(build.LatestImage))
		case "False":
			setCondition(app, shpyrdv1.ConditionBuilt, metav1.ConditionFalse, build.failedReason(), build.Message)
		default:
			reason := "Building"
			if build.Reason == "PreparingBuilder" || build.Reason == reasonWaitingForCommit {
				reason = build.Reason
			}
			setCondition(app, shpyrdv1.ConditionBuilt, metav1.ConditionUnknown, reason, firstNonEmpty(build.Message, "build in progress"))
		}
	} else {
		app.Status.LatestBuild = ""
		if image != "" {
			setCondition(app, shpyrdv1.ConditionBuilt, metav1.ConditionTrue, "PrebuiltImage", "using "+shortImage(image))
		}
	}

	// 3. Nothing to run yet.
	if image == "" {
		if !app.HasSource() {
			app.Status.Phase = shpyrdv1.PhasePending
			app.Status.Message = "Nothing deployed yet: deploy the app's source or an image."
			setCondition(app, shpyrdv1.ConditionReady, metav1.ConditionFalse, "Pending", app.Status.Message)
			return outcome{}, nil
		}
		if build.Ready == "False" {
			app.Status.Phase = shpyrdv1.PhaseFailed
			app.Status.Message = build.Message
			setCondition(app, shpyrdv1.ConditionReady, metav1.ConditionFalse, build.failedReason(), build.Message)
			return outcome{}, nil
		}
		app.Status.Phase = shpyrdv1.PhaseBuilding
		app.Status.Message = firstNonEmpty(build.Message, "waiting for the first build")
		setCondition(app, shpyrdv1.ConditionReady, metav1.ConditionFalse, "Building", app.Status.Message)
		if build.Reason == reasonWaitingForCommit {
			return requeue(5 * time.Second), nil
		}
		return requeue(30 * time.Second), nil
	}

	if pending != nil && (!app.HasSource() || build.Ready == "True") {
		secret, err = r.applyPendingConfig(ctx, app, pending)
		if err != nil {
			return outcome{}, err
		}
	}

	// The code the image was built from: the release history records it
	// and every process learns it through REVISION.
	source := sourceID(app, kpackBuild)
	if build.Revision != "" && app.Spec.Source != nil && app.Spec.Source.Git != nil {
		source = short(build.Revision)
	}
	source = sourceOf(app, image, source)
	revision := revisionValue(source)

	// 3b. The release phase (RFC-0066): an image with a "release" process
	// type (a Procfile's release: line) runs it before a new release rolls
	// out; the rollout waits, and a failure leaves the previous release
	// serving.
	if meta := r.metadataOf(ctx, image); meta != nil {
		app.Status.ProcessTypes = meta.ProcessTypes
		app.Status.Runtime = meta.Runtime
		users := []shpyrdv1.ImageUser{}
		for _, u := range app.Status.ImageUsers {
			if (u.Image == app.Status.Image && u.Image != image) || (u.Image == image && meta.UserError != nil) {
				users = append(users, u)
			}
		}
		if meta.User != nil {
			users = append(users, shpyrdv1.ImageUser{Image: image, UID: meta.User.UID, GID: meta.User.GID})
		}
		app.Status.ImageUsers = users
	}
	app.Status.DefaultSize = r.catalog(ctx).DefaultForRuntime(app.Status.Runtime)
	// Fingerprint the effective sizes only after reading the image runtime:
	// otherwise the next reconcile sees a new target and reruns the migration.
	sizeByProcess := r.processSizes(ctx, app)
	hash := configHash(app, secret, sizeByProcess, bindings, globals)
	if releasePending(app, image, hash) {
		res, _, _ := processResources(namedProcess{Name: releaseProcessType, Process: releaseProcess(app)}, r.catalog(ctx))
		proceed, refused, err := r.reconcileReleasePhase(ctx, app, image, hash, revision, res, r.releaseMounts(ctx, app))
		if err != nil {
			return outcome{}, err
		}
		if !proceed {
			st := app.Status.Release
			if st.State == shpyrdv1.ReleaseFailed {
				app.Status.Phase = shpyrdv1.PhaseFailed
				app.Status.Message = st.Message
				setCondition(app, shpyrdv1.ConditionReady, metav1.ConditionFalse, "ReleaseFailed", st.Message)
				return outcome{}, nil
			}
			reason := "ReleasePhase"
			if refused {
				reason = "QuotaExceeded"
			}
			app.Status.Phase = shpyrdv1.PhaseDeploying
			app.Status.Message = st.Message
			setCondition(app, shpyrdv1.ConditionReady, metav1.ConditionFalse, reason, app.Status.Message)
			if refused {
				return requeue(30 * time.Second), nil
			}
			return requeue(5 * time.Second), nil
		}
	}

	// Refresh domain readiness before choosing whether to redirect traffic.
	var domainsPending bool
	app.Status.Domains, domainsPending = r.domainStatuses(ctx, app)

	// 4. Workloads.
	procStatus, err := r.reconcileWorkloads(ctx, app, image, hash, revision)
	if err != nil {
		return outcome{}, err
	}
	app.Status.Image = image
	app.Status.Processes = procStatus
	if hasWeb(app) {
		app.Status.URL = r.Config.url(app)
	} else {
		app.Status.URL = ""
	}
	// 5. Release history and phase.
	var out outcome
	// Redirects also poll after becoming ready so invalid destinations fall back.
	out.domainsPending = domainsPending || r.Config.requestedRedirect(app) != ""
	configDesc := ""
	if cur := app.CurrentRelease(); cur != nil && cur.Image == image && cur.ConfigHash != hash {
		configDesc = r.describeConfigChangeSince(ctx, app, cur.Number, secret)
		if (configDesc == "" || configDesc == "Config change") && cur.GlobalHash != ghash {
			configDesc = "Global config change"
		}
	}
	if recordRelease(app, image, hash, ghash, source, metav1.Now(), configDesc, sizeByProcess) {
		out.newRelease = app.CurrentRelease()
		for _, p := range processes(app) {
			out.newRelease.Processes = append(out.newRelease.Processes, p.Name)
		}
		out.newRelease.Bindings = append([]shpyrdv1.Binding(nil), app.Spec.Bindings...)
		if err := r.snapshotRelease(ctx, app, out.newRelease.Number, secret); err != nil {
			return outcome{}, err
		}
		r.pruneSnapshots(ctx, app)
	}
	r.pruneImages(ctx, app)
	// The note and rollback request are one-shot: consume them whether or
	// not they produced a release, so they cannot label an unrelated later
	// change. While a build for the noted change is still running they must
	// survive until the image lands.
	if (app.Annotations[shpyrdv1.AnnotationReleaseNote] != "" || app.Annotations[shpyrdv1.AnnotationRollbackTo] != "") &&
		(!app.HasSource() || app.Spec.Image != "" || build.Ready == "True") {
		out.clearNote = true
	}

	ready, summary := summarizeProcesses(procStatus)
	failing, failMsg := failingSummary(procStatus)
	if cur := app.CurrentRelease(); cur != nil && !ready {
		summary = fmt.Sprintf("Releasing v%d: %s", cur.Number, summary)
	}
	refusal := ""
	if !ready && !failing {
		refusal = r.processRefusals(ctx, app, procStatus)
	}
	switch {
	case refusal != "":
		// The workspace's ceiling leaves no room for the new instances
		// (#52); older ones may still be serving.
		app.Status.Phase = shpyrdv1.PhaseFailed
		app.Status.Message = refusal
		setCondition(app, shpyrdv1.ConditionReady, metav1.ConditionFalse, "QuotaExceeded", refusal)
		out.result = ctrl.Result{RequeueAfter: 30 * time.Second}
		return out, nil
	case app.HasSource() && app.Spec.Image == "" && build.Ready != "True" && build.Ready != "False":
		// The next release is building. Instances failing now are the
		// previous release's, not its outcome (#91).
		app.Status.Phase = shpyrdv1.PhaseBuilding
		app.Status.Message = firstNonEmpty(build.Message, "building new release; "+summary)
		setCondition(app, shpyrdv1.ConditionReady, metav1.ConditionFalse, "Building", app.Status.Message)
		out.result = ctrl.Result{RequeueAfter: 30 * time.Second}
		if build.Reason == reasonWaitingForCommit {
			out.result.RequeueAfter = 5 * time.Second
		}
	case failing && !ready:
		// New instances cannot start; older ones may still be serving.
		app.Status.Phase = shpyrdv1.PhaseFailed
		app.Status.Message = failMsg
		setCondition(app, shpyrdv1.ConditionReady, metav1.ConditionFalse, "InstancesFailing", failMsg)
		out.result = ctrl.Result{RequeueAfter: 30 * time.Second}
		return out, nil
	case app.HasSource() && app.Spec.Image == "" && build.Ready == "False":
		app.Status.Phase = shpyrdv1.PhaseFailed
		app.Status.Message = build.Message + " The previous release keeps running."
		setCondition(app, shpyrdv1.ConditionReady, metav1.ConditionFalse, build.failedReason(), build.Message)
	case !ready:
		app.Status.Phase = shpyrdv1.PhaseDeploying
		app.Status.Message = summary
		setCondition(app, shpyrdv1.ConditionReady, metav1.ConditionFalse, "Deploying", app.Status.Message)
		out.result = ctrl.Result{RequeueAfter: 15 * time.Second}
	default:
		app.Status.Phase = shpyrdv1.PhaseRunning
		app.Status.Message = summary
		setCondition(app, shpyrdv1.ConditionReady, metav1.ConditionTrue, "Running", summary)
	}
	if r.Config.suspended(app) {
		app.Status.Message = "workspace suspended: the app is stopped and not served until it is activated; " + app.Status.Message
	}
	return out, nil
}

// reconcileKpackImage creates or updates the kpack Image and returns its
// current state.
func (r *AppReconciler) reconcileKpackImage(ctx context.Context, app *shpyrdv1.App, git gitauth.Build) (*unstructured.Unstructured, error) {
	// The project's own builder when it composes its build (RFC-0065),
	// else the platform's.
	builderRef, err := r.reconcileBuilder(ctx, app)
	if err != nil {
		return nil, err
	}
	desired, err := r.Config.desiredKpackImage(app, git)
	if err != nil {
		return nil, err
	}
	_ = unstructured.SetNestedField(desired.Object, builderRef, "spec", "builder")
	if err := controllerutil.SetControllerReference(app, desired, r.Scheme); err != nil {
		return nil, err
	}

	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(KpackImageGVK)
	err = r.Get(ctx, client.ObjectKeyFromObject(desired), current)
	switch {
	case apierrors.IsNotFound(err):
		// A move in progress (above) also waits for the old cache claim.
		if err := r.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: app.Name + "-cache"}, &corev1.PersistentVolumeClaim{}); err == nil {
			return nil, errImageMoving
		} else if !apierrors.IsNotFound(err) {
			return nil, err
		}
		if err := r.Create(ctx, desired); err != nil {
			return nil, fmt.Errorf("create kpack image: %w", err)
		}
		r.Recorder.Event(app, corev1.EventTypeNormal, "BuildRequested", "created kpack Image "+desired.GetName())
		return desired, nil
	case err != nil:
		return nil, fmt.Errorf("get kpack image: %w", err)
	}

	// spec.tag is immutable in kpack: when the repository changed (an
	// external registry replaced by the in-cluster one, RFC-0059; the
	// workspace's own repository, RFC-0033) the Image is recreated and
	// built once more. Its build history goes; releases live on the App.
	curTag, _, _ := unstructured.NestedString(current.Object, "spec", "tag")
	newTag, _, _ := unstructured.NestedString(desired.Object, "spec", "tag")
	if curTag != "" && curTag != newTag {
		// The old Image and its build cache claim (which kpack names after
		// the Image and garbage-collects with it) must be gone before the
		// new Image takes the name: created any sooner, its first build
		// mounts a claim that is about to disappear and waits forever.
		if current.GetDeletionTimestamp() == nil {
			if err := r.Delete(ctx, current); err != nil && !apierrors.IsNotFound(err) {
				return nil, fmt.Errorf("delete kpack image for the new repository: %w", err)
			}
			r.Recorder.Eventf(app, corev1.EventTypeNormal, "BuildRequested", "image repository changed (%s -> %s): the build moves there", curTag, newTag)
		}
		cache := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: app.Name + "-cache", Namespace: app.Namespace}}
		if err := r.Delete(ctx, cache); err != nil && !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("delete the old build cache: %w", err)
		}
		return nil, errImageMoving
	}

	// A redeploy of the same source: kpack builds again when its latest
	// Build carries the trigger annotation (what `kp image trigger` sets).
	// The request is remembered on the Image so it fires once per redeploy.
	rebuild := app.Annotations[shpyrdv1.AnnotationRebuildAt]
	needsTrigger := rebuild != "" && current.GetAnnotations()[shpyrdv1.AnnotationRebuildAt] != rebuild

	// The same archive at another address (the sources port where the API
	// port was, a token added, a host renamed) is not a new source:
	// rewriting the URL in the Image would make kpack rebuild every app on
	// an upgrade (#93). The archive is named by its sha256, so the current
	// URL stays while that name is the same, until a build happens anyway:
	// a new archive, or a rebuild asked for, which takes the new address.
	if !needsTrigger {
		curURL, _, _ := unstructured.NestedString(current.Object, "spec", "source", "blob", "url")
		desURL, _, _ := unstructured.NestedString(desired.Object, "spec", "source", "blob", "url")
		if curURL != "" && curURL != desURL && (r.Config.sourceURL(curURL) == desURL || sameArchive(curURL, desURL)) {
			_ = unstructured.SetNestedField(desired.Object, curURL, "spec", "source", "blob", "url")
		}
	}

	// An Image from before builds read the project's variables binds their
	// Secret with its next build (a new archive, a redeploy), not by
	// starting one (build_env.go). A Git source is built by kpack on every
	// commit without the Image changing, so there it binds at once, with
	// one build.
	if !needsTrigger && app.Spec.Source != nil && app.Spec.Source.Git == nil && !buildChanges(current, desired) {
		desired = withServicesOf(desired, current)
	}

	// The redeploy's build: the trigger, unless the update below changes
	// what kpack builds (another source address, other build.env values,
	// the Secret bound), which builds by itself; both would build twice,
	// the first time without the change.
	if needsTrigger && !buildChanges(current, desired) && equalJSON(buildServices(current), buildServices(desired)) {
		if err := r.triggerKpackBuild(ctx, app, current); err != nil {
			return nil, err
		}
	}

	// Compare the fields we own.
	if needsTrigger || !equalJSON(current.Object["spec"], desired.Object["spec"]) || !labelsSubset(current.GetLabels(), desired.GetLabels()) {
		updated := current.DeepCopy()
		updated.Object["spec"] = desired.Object["spec"]
		updated.SetLabels(mergeMaps(updated.GetLabels(), desired.GetLabels()))
		updated.SetOwnerReferences(desired.GetOwnerReferences())
		if needsTrigger {
			updated.SetAnnotations(mergeMaps(updated.GetAnnotations(), map[string]string{shpyrdv1.AnnotationRebuildAt: rebuild}))
		}
		if err := r.Update(ctx, updated); err != nil {
			return nil, fmt.Errorf("update kpack image: %w", err)
		}
		if !needsTrigger {
			r.Recorder.Event(app, corev1.EventTypeNormal, "BuildRequested", "updated kpack Image source")
		}
		return updated, nil
	}
	return current, nil
}

// releaseProcess is the process the release command runs as: the declared
// release process, else the web process's settings (its size in
// particular: rails db:prepare needs what rails server needs, and the
// catalog default is sized for a static site), else nothing declared.
func releaseProcess(app *shpyrdv1.App) shpyrdv1.Process {
	web := app.EffectiveProcesses()["web"]
	if p, ok := app.Spec.Processes[releaseProcessType]; ok {
		if p.Size == "" {
			p.Size = firstNonEmpty(web.Size, app.Status.DefaultSize)
			if len(p.Resources.Limits) == 0 && len(p.Resources.Requests) == 0 {
				p.Resources = web.Resources
			}
		}
		return p
	}
	return shpyrdv1.Process{Size: firstNonEmpty(web.Size, app.Status.DefaultSize), Resources: web.Resources}
}

// errImageMoving says the kpack Image is between repositories: the old one
// (and its cache claim) is going, the new one is created once they are gone.
var errImageMoving = errors.New("kpack image moving to a new repository")

// kpackBuildNeededAnnotation on an Image's latest Build makes kpack schedule
// another build of the same source (build reason TRIGGER).
// sameArchive says two source URLs name the same archive: the file is
// named by its sha256 (<sha256>.tgz), whatever host, port or token carries
// it (#93).
func sameArchive(a, b string) bool {
	name := func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil || u.Path == "" {
			return ""
		}
		base := path.Base(u.Path)
		if !archiveName.MatchString(base) {
			return ""
		}
		return base
	}
	n := name(a)
	return n != "" && n == name(b)
}

var archiveName = regexp.MustCompile(`^[a-f0-9]{64}\.tgz$`)

const kpackBuildNeededAnnotation = "image.kpack.io/additionalBuildNeeded"

// triggerKpackBuild annotates the Image's latest Build so kpack builds
// again. Without a previous build there is nothing to trigger: kpack builds
// on its own.
func (r *AppReconciler) triggerKpackBuild(ctx context.Context, app *shpyrdv1.App, img *unstructured.Unstructured) error {
	name, _, _ := unstructured.NestedString(img.Object, "status", "latestBuildRef")
	if name == "" {
		return nil
	}
	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(KpackBuildGVK)
	if err := r.Get(ctx, types.NamespacedName{Namespace: app.Namespace, Name: name}, b); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("latest kpack build: %w", err)
	}
	b.SetAnnotations(mergeMaps(b.GetAnnotations(), map[string]string{kpackBuildNeededAnnotation: "true"}))
	if err := r.Update(ctx, b); err != nil {
		return fmt.Errorf("trigger kpack build: %w", err)
	}
	r.Recorder.Event(app, corev1.EventTypeNormal, "BuildRequested", "building the same source again (redeploy)")
	return nil
}

// registryOf is the registry part of an image reference (up to the first slash).
func registryOf(ref string) string {
	if i := strings.IndexByte(ref, '/'); i > 0 {
		return ref[:i]
	}
	return ref
}

// processSizes resolves the size name of every process against the catalog
// (for the release fingerprint); unresolvable sizes are recorded as given.
func (r *AppReconciler) processSizes(ctx context.Context, app *shpyrdv1.App) map[string]string {
	catalog := r.catalog(ctx)
	out := map[string]string{}
	for _, p := range processes(app) {
		if _, name, err := processResources(p, catalog); err == nil {
			out[p.Name] = name
		} else {
			out[p.Name] = firstNonEmpty(p.Size, "custom")
		}
	}
	return out
}

// getBuild reads a kpack Build without populating the cache; failures only
// degrade the release description.
func (r *AppReconciler) getBuild(ctx context.Context, namespace, name string) *unstructured.Unstructured {
	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(KpackBuildGVK)
	if err := r.APIReader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, b); err != nil {
		return nil
	}
	return b
}

// newestKpackBuild is the Build of the kpack Image img numbered above the
// one named current, the highest of them; "" when there is none.
func (r *AppReconciler) newestKpackBuild(ctx context.Context, img *unstructured.Unstructured, current string) string {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(KpackBuildGVK.GroupVersion().WithKind(KpackBuildGVK.Kind + "List"))
	if err := r.APIReader.List(ctx, list, client.InNamespace(img.GetNamespace()), client.MatchingLabels{"image.kpack.io/image": img.GetName()}); err != nil {
		return ""
	}
	newest, top, cur := "", -1, -1
	for _, b := range list.Items {
		n, err := strconv.Atoi(b.GetLabels()["image.kpack.io/buildNumber"])
		if err != nil {
			continue
		}
		if b.GetName() == current {
			cur = n
		}
		if n > top {
			newest, top = b.GetName(), n
		}
	}
	if top <= cur {
		return ""
	}
	return newest
}

// reconcileWorkloads makes Deployments, Services and the Ingress match the
// process map and removes workloads of dropped process types.
// previousRelease is the last release that ran the process with another
// image or another size than the spec asks now: what a fallback puts back
// (#127). The current rollout's own record, and any record of the same
// target, are skipped.
func previousRelease(app *shpyrdv1.App, process, image, size string) *shpyrdv1.Release {
	for i := len(app.Status.Releases) - 1; i >= 0; i-- {
		rel := &app.Status.Releases[i]
		if rel.Image == "" {
			continue
		}
		if rel.Image == image && (rel.Sizes[process] == size || rel.Sizes[process] == "") {
			continue
		}
		return rel
	}
	return nil
}

// fallbackWorkload is what a process runs while a fallback holds (#127):
// the previous release's image and, when that release recorded a size for
// the process, that size's resources; the spec's resources otherwise.
func (r *AppReconciler) fallbackWorkload(prev *shpyrdv1.Release, p namedProcess, catalog sizes.Catalog, res corev1.ResourceRequirements) (string, corev1.ResourceRequirements, int) {
	if size := prev.Sizes[p.Name]; size != "" && size != "custom" {
		if got, _, err := catalog.Resolve(size, corev1.ResourceRequirements{}); err == nil {
			res = got
		}
	}
	return prev.Image, res, prev.Number
}

// fallbackNote completes a failure reason while the previous release runs.
func fallbackNote(release int) string {
	return fmt.Sprintf(" The previous release (v%d) runs again meanwhile; resize the process, or roll back.", release)
}

func (r *AppReconciler) reconcileWorkloads(ctx context.Context, app *shpyrdv1.App, image, hash, revision string) (map[string]shpyrdv1.ProcessStatus, error) {
	status := map[string]shpyrdv1.ProcessStatus{}
	wanted := map[string]bool{}

	catalog := r.catalog(ctx)
	mounts, err := r.resolveMounts(ctx, app)
	if err != nil {
		return nil, err
	}
	// Sleep (RFC-0075): with a policy, the add-on installed and no pause,
	// KEDA owns the web Deployment's replica count.
	sleepWanted := r.sleepEnabled(app)
	_, paused := r.sleepPause(app)
	kedaScales := sleepWanted && !paused && r.kedaHTTPAvailable()
	var webReplicas *int32 // what the web Deployment currently asks for
	release := releaseNumber(app, image, hash)
	// A fallback holds for the generation it was decided for (#127); a
	// change to the spec starts a rollout from the spec again.
	if fb := app.Status.Fallback; fb != nil && fb.Generation != app.Generation {
		app.Status.Fallback = nil
	}
	for _, p := range processes(app) {
		wanted[p.Name] = true
		if p.Name != "web" && len(p.Command) == 0 && app.BuildStrategy() == shpyrdv1.StrategyDockerfile {
			return nil, fmt.Errorf("process %q needs a command: Dockerfile images have a single entrypoint (set processes.%s.command in shpyrd.yaml)", p.Name, p.Name)
		}
		res, sizeName, err := processResources(p, catalog)
		if err != nil {
			return nil, err
		}
		// What this process runs: the spec, or, while a fallback holds for
		// it, its previous release (#127).
		pImage, pRes, pRelease := image, res, release
		if fb := app.Status.Fallback; fb != nil && fb.Process == p.Name {
			if prev := app.ReleaseByNumber(fb.Release); prev != nil {
				pImage, pRes, pRelease = r.fallbackWorkload(prev, p, catalog, res)
			}
		}
		d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: workloadName(app, p.Name), Namespace: app.Namespace}}
		scaledExternally := kedaScales && p.Name == "web"
		op, err := controllerutil.CreateOrUpdate(ctx, r.Client, d, func() error {
			r.Config.mutateDeployment(app, p, pImage, hash, revision, pRelease, pRes, mounts[p.Name], d, scaledExternally)
			return controllerutil.SetControllerReference(app, d, r.Scheme)
		})
		if err != nil {
			return nil, fmt.Errorf("deployment %s: %w", d.Name, err)
		}
		if op != controllerutil.OperationResultNone {
			log.FromContext(ctx).Info("deployment reconciled", "name", d.Name, "op", op)
		}
		// The release's instances are those of the ReplicaSet for the
		// current template: mid-rollout the Deployment's counts include the
		// previous release's (#91).
		current, err := r.currentReplicaSet(ctx, d)
		if err != nil {
			return nil, fmt.Errorf("replicasets of %s: %w", d.Name, err)
		}
		ps := shpyrdv1.ProcessStatus{
			Desired: p.replicas(),
			Updated: d.Status.UpdatedReplicas,
		}
		if current != nil {
			ps.Ready = current.Status.ReadyReplicas
		}
		// The Deployment controller needs a moment after an update.
		if d.Status.ObservedGeneration < d.Generation {
			ps.Ready, ps.Updated = 0, 0
		}
		health := r.processHealthDetail(ctx, current)
		ps.Failing, ps.Reason = health.failing, health.reason
		if fb := app.Status.Fallback; fb != nil && fb.Process == p.Name {
			// The previous release runs; the failure is the spec's, and it
			// shows until the spec changes.
			ps.Failing = max(ps.Failing, 1)
			ps.Reason = fb.Message + fallbackNote(fb.Release)
		} else if health.unschedulable && rolloutStrategy(p, mounts[p.Name]).Type == appsv1.RecreateDeploymentStrategyType {
			// A Recreate rollout stopped the running instance before the new
			// one proved impossible to place: put the previous release back
			// (#127) rather than leave the app down behind a locked rollout.
			if prev := previousRelease(app, p.Name, image, sizeName); prev != nil {
				app.Status.Fallback = &shpyrdv1.FallbackStatus{Generation: app.Generation, Process: p.Name, Release: prev.Number, Message: health.reason}
				fImage, fRes, fRelease := r.fallbackWorkload(prev, p, catalog, res)
				if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, d, func() error {
					r.Config.mutateDeployment(app, p, fImage, hash, revision, fRelease, fRes, mounts[p.Name], d, scaledExternally)
					return controllerutil.SetControllerReference(app, d, r.Scheme)
				}); err != nil {
					return nil, fmt.Errorf("deployment %s (fallback): %w", d.Name, err)
				}
				r.Recorder.Eventf(app, corev1.EventTypeWarning, "Fallback", "%s: %s The previous release (v%d) runs again meanwhile.", p.Name, health.reason, prev.Number)
				ps.Reason = health.reason + fallbackNote(prev.Number)
			}
		}
		ps.Size = sizeName
		// The size's CPU is the limit; a shared size requests only a share.
		ps.CPU = res.Limits.Cpu().String()
		if res.Limits.Cpu().IsZero() {
			ps.CPU = res.Requests.Cpu().String()
		}
		ps.Memory = res.Requests.Memory().String()
		ps.Pinned = singleInstanceNote(mounts[p.Name])
		if p.Name == "web" {
			webReplicas = d.Spec.Replicas
		}
		status[p.Name] = ps

		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: workloadName(app, p.Name), Namespace: app.Namespace}}
		if p.port() > 0 {
			if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
				r.Config.mutateService(app, p, svc)
				return controllerutil.SetControllerReference(app, svc, r.Scheme)
			}); err != nil {
				return nil, fmt.Errorf("service %s: %w", svc.Name, err)
			}
		} else if err := r.deleteIfExists(ctx, svc); err != nil {
			return nil, err
		}
	}

	// Sleep objects (RFC-0075) come before the Ingress: the backend only
	// points at the KEDA interceptor once the objects exist.
	sleepActive, err := r.reconcileSleep(ctx, app, mounts["web"])
	if err != nil {
		return nil, err
	}
	if ps, ok := status["web"]; ok {
		// The policy as it applies and where it comes from, for every web
		// process (#120, #132), then the state, decided after the objects
		// were reconciled (a pause may have just happened).
		ps.Sleep = r.webSleepStatus(app, mounts["web"])
		if sleepWanted {
			switch pause, paused := r.sleepPause(app); {
			case paused:
				ps.Sleep.State, ps.Sleep.Message = "unavailable", pause.reason
			case !sleepActive:
				ps.Sleep.State, ps.Sleep.Message = "unavailable", sleepUnavailableMessage
			case webReplicas != nil && *webReplicas == 0:
				ps.Sleep.State = "sleeping"
				ps.Desired = 0 // the scaler's decision, not a failure
			}
			if ps.Sleep.Message == "" && ps.Sleep.Source == "workspace" {
				ps.Sleep.Message = "the workspace's default: sleeps after " + ps.Sleep.After + " (" + ps.Sleep.Resuming + " mode), unless the project sets a sleep policy of its own or none"
			}
		}
		status["web"] = ps
	}
	// Ingress for web, and the certificates its hosts need (RFC-0034). A
	// suspended workspace's apps are not served: no Ingress, so the front
	// door's default backend answers for the host with the suspension page.
	serving := hasWeb(app) && !r.Config.suspended(app)
	// Remove the redirect before restoring the same host/path on the app ingress.
	if !serving || r.Config.redirectTarget(app) == "" {
		if err := r.reconcileDomainRedirect(ctx, app, false); err != nil {
			return nil, err
		}
	}
	ing := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: ingressName(app), Namespace: app.Namespace}}
	if serving {
		if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, ing, func() error {
			r.Config.mutateIngress(app, ing, sleepActive)
			return controllerutil.SetControllerReference(app, ing, r.Scheme)
		}); err != nil {
			return nil, fmt.Errorf("ingress: %w", err)
		}
	} else if err := r.deleteIfExists(ctx, ing); err != nil {
		return nil, err
	}
	if serving && r.Config.redirectTarget(app) != "" {
		if err := r.reconcileDomainRedirect(ctx, app, true); err != nil {
			return nil, err
		}
	}
	// The edge's companions (RFC-0033): only for apps that are not public.
	edgeIng := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: edgeName(app), Namespace: app.Namespace}}
	edgeSvc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: EdgeServiceName, Namespace: app.Namespace}}
	if serving && (app.EffectiveAccess() != shpyrdv1.AccessPublic || maintenance(app)) {
		if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, edgeSvc, func() error {
			r.Config.mutateEdgeService(app, edgeSvc)
			return controllerutil.SetControllerReference(app, edgeSvc, r.Scheme)
		}); err != nil {
			return nil, fmt.Errorf("edge service: %w", err)
		}
		if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, edgeIng, func() error {
			r.Config.mutateEdgeIngress(app, edgeIng)
			return controllerutil.SetControllerReference(app, edgeIng, r.Scheme)
		}); err != nil {
			return nil, fmt.Errorf("edge ingress: %w", err)
		}
	} else {
		if err := r.deleteIfExists(ctx, edgeIng); err != nil {
			return nil, err
		}
		if err := r.deleteIfExists(ctx, edgeSvc); err != nil {
			return nil, err
		}
	}
	if err := r.reconcileCertificates(ctx, app); err != nil {
		return nil, err
	}
	if _, err := r.reconcileWorkspaceTLS(ctx, app); err != nil {
		return nil, err
	}
	if err := r.reconcileAppsTLS(ctx, app); err != nil {
		return nil, err
	}
	// Garbage collect workloads of removed process types.
	var deployments appsv1.DeploymentList
	if err := r.List(ctx, &deployments, client.InNamespace(app.Namespace), client.MatchingLabels{shpyrdv1.LabelApp: app.Name}); err != nil {
		return nil, err
	}
	for i := range deployments.Items {
		d := &deployments.Items[i]
		proc := d.Labels[shpyrdv1.LabelProcess]
		if wanted[proc] || !metav1.IsControlledBy(d, app) {
			continue
		}
		if err := r.deleteIfExists(ctx, d); err != nil {
			return nil, err
		}
		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: d.Name, Namespace: d.Namespace}}
		if err := r.deleteIfExists(ctx, svc); err != nil {
			return nil, err
		}
	}
	return status, nil
}

func (r *AppReconciler) deleteIfExists(ctx context.Context, obj client.Object) error {
	if err := r.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete %T %s: %w", obj, obj.GetName(), err)
	}
	return nil
}

// auditReleaseOutcome records what became of a release (RFC-0022a): once
// per release when the rollout reaches Running, and every time the app
// turns Failed (a build that failed, instances that cannot start). The
// entry is in the name of whoever asked for the deploy, as the API left
// on the App; a release nobody asked for by hand (a config change) is the
// platform's. Writing the trail never fails the reconcile.
func (r *AppReconciler) auditReleaseOutcome(ctx context.Context, before, app *shpyrdv1.App) {
	if r.Kube == nil {
		return
	}
	cur := app.CurrentRelease()
	target := "build"
	if cur != nil {
		target = fmt.Sprintf("v%d", cur.Number)
	}
	entry := audit.Entry{
		Actor:   firstNonEmpty(app.Annotations[shpyrdv1.AnnotationDeployedBy], "platform"),
		Subject: app.Annotations[shpyrdv1.AnnotationDeployedSubject],
		Client:  app.Annotations[shpyrdv1.AnnotationDeployedClient],
		Target:  target, Via: "controller", Realm: "workspace",
	}
	ref := audit.AppRefIn(app.Namespace, app.Name)
	switch {
	// A rollout that reaches Running: the transition, so apps already
	// running when this controller first sees them are not back-filled;
	// the annotation keeps a rollout that flaps from saying it twice.
	case app.Status.Phase == shpyrdv1.PhaseRunning && before.Status.Phase != shpyrdv1.PhaseRunning && cur != nil && app.Annotations[shpyrdv1.AnnotationAuditedRelease] != strconv.Itoa(cur.Number):
		entry.Action, entry.Detail = "release.succeeded", cur.Description
		if err := audit.Record(ctx, r.Kube, ref, entry); err != nil {
			log.FromContext(ctx).Info("could not audit the release", "err", err.Error())
			return
		}
		// Drop who asked for the release, unless someone (a push) asked
		// for the next one meanwhile.
		asked := map[string]string{}
		for _, k := range []string{shpyrdv1.AnnotationDeployedBy, shpyrdv1.AnnotationDeployedSubject, shpyrdv1.AnnotationDeployedClient} {
			if v, ok := app.Annotations[k]; ok {
				asked[k] = v
			}
		}
		patch := client.MergeFrom(app.DeepCopy())
		if app.Annotations == nil {
			app.Annotations = map[string]string{}
		}
		app.Annotations[shpyrdv1.AnnotationAuditedRelease] = strconv.Itoa(cur.Number)
		if err := r.Patch(ctx, app, patch); err != nil {
			log.FromContext(ctx).Info("could not mark the release audited", "err", err.Error())
			return
		}
		for k, v := range asked {
			if _, err := removeAnnotationIfUnchanged(ctx, r.Client, app, k, v); err != nil {
				logRemoveAnnotation(ctx, err, "kept who asked for the next release", "who asked for the release", "annotation", k)
			}
		}
	case app.Status.Phase == shpyrdv1.PhaseFailed && before.Status.Phase != shpyrdv1.PhaseFailed:
		entry.Action, entry.Detail = "release.failed", app.Status.Message
		if err := audit.Record(ctx, r.Kube, ref, entry); err != nil {
			log.FromContext(ctx).Info("could not audit the failure", "err", err.Error())
		}
	}
}

// clearReleaseNote removes the one-shot annotations once consumed, each
// only while it holds the value this reconcile read: a note the source
// watcher wrote meanwhile is the next release's (RFC-0018).
func (r *AppReconciler) clearReleaseNote(ctx context.Context, app *shpyrdv1.App) {
	read := map[string]string{}
	for _, k := range []string{shpyrdv1.AnnotationReleaseNote, shpyrdv1.AnnotationRollbackTo} {
		if v, ok := app.Annotations[k]; ok {
			read[k] = v
		}
	}
	for k, v := range read {
		if _, err := removeAnnotationIfUnchanged(ctx, r.Client, app, k, v); err != nil {
			logRemoveAnnotation(ctx, err, "kept a newer one-shot annotation", "a one-shot annotation", "annotation", k)
		}
	}
}

func hasWeb(app *shpyrdv1.App) bool {
	for _, p := range processes(app) {
		if p.Name == "web" && p.port() > 0 {
			return true
		}
	}
	return false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func labelsSubset(have, want map[string]string) bool {
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}

// labelRunningInstances patches the running pods of an app with the
// shpyrd.io/instance annotation (web.1, worker.2) so the Vector log agent
// can label log lines without calling the API per line (RFC-0022a).
func (r *AppReconciler) labelRunningInstances(ctx context.Context, app *shpyrdv1.App) {
	var pods corev1.PodList
	if err := r.List(ctx, &pods, client.InNamespace(app.Namespace), client.MatchingLabels{shpyrdv1.LabelApp: app.Name}); err != nil {
		return
	}
	names := logs.InstanceNames(pods.Items)
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.DeletionTimestamp != nil {
			continue
		}
		want := names[pod.Name]
		if want == "" || pod.Annotations[shpyrdv1.AnnotationInstance] == want {
			continue
		}
		patch := fmt.Sprintf(`{"metadata":{"annotations":{%q:%q}}}`, shpyrdv1.AnnotationInstance, want)
		_ = r.Patch(ctx, pod, client.RawPatch(types.MergePatchType, []byte(patch)))
	}
}
