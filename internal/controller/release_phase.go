package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/registry"
)

// The release phase (RFC-0066). Buildpacks turn a Procfile's
// `release: bundle exec rails db:migrate` into a process type of the image
// (/cnb/process/release); nothing runs it. The platform does, the way
// Heroku's runtime does: before a new release rolls out — a new image or a
// configuration change — the release command runs once as a Job with the
// release's image and config vars; the rollout waits for it; if it fails
// the previous release keeps serving and the project says why.

// releaseProcessType is the process type that gates releases.
const releaseProcessType = "release"

// processTypeCache remembers what the registry said about an image digest:
// process types never change for a digest. A failed lookup is remembered
// for a minute so an unreachable registry does not slow every reconcile.
type processTypeCache struct {
	mu     sync.Mutex
	types  map[string][]string
	failed map[string]time.Time
}

var imageProcessTypes = &processTypeCache{types: map[string][]string{}, failed: map[string]time.Time{}}

// processTypesOf reads the process types of an image (by its reference
// with digest) from the platform's registry, cached per digest. Images the
// registry client cannot read (another registry, no label) yield none: no
// release phase, and the rollout proceeds as before. Tests replace
// ProcessTypes.
func (r *AppReconciler) processTypesOf(ctx context.Context, image string) []string {
	if r.ProcessTypes != nil {
		return r.ProcessTypes(ctx, image)
	}
	host, repo, digest := registry.SplitReference(image)
	if digest == "" || host == "" || host != r.Config.RegistryHost {
		return nil
	}
	imageProcessTypes.mu.Lock()
	if t, ok := imageProcessTypes.types[digest]; ok {
		imageProcessTypes.mu.Unlock()
		return t
	}
	if at, ok := imageProcessTypes.failed[digest]; ok && time.Since(at) < time.Minute {
		imageProcessTypes.mu.Unlock()
		return nil
	}
	imageProcessTypes.mu.Unlock()
	cl, err := r.registryClient(ctx)
	if err != nil {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	types, err := cl.ProcessTypes(cctx, repo, digest)
	imageProcessTypes.mu.Lock()
	defer imageProcessTypes.mu.Unlock()
	if err != nil {
		imageProcessTypes.failed[digest] = time.Now()
		return nil
	}
	imageProcessTypes.types[digest] = types
	return types
}

// releaseCommand is the command the release phase runs: the image's
// process type, or a command declared in shpyrd.yaml for images built
// another way. Nil when the release has nothing to run.
func releaseCommand(app *shpyrdv1.App, processTypes []string) []string {
	if p, ok := app.Spec.Processes[releaseProcessType]; ok && len(p.Command) > 0 {
		return append(append([]string{}, p.Command...), p.Args...)
	}
	if slices.Contains(processTypes, releaseProcessType) {
		return []string{"/cnb/process/" + releaseProcessType}
	}
	return nil
}

// releaseTarget identifies what a release phase runs for.
func releaseTarget(image, configHash string) string {
	sum := sha256.Sum256([]byte(image + "\x00" + configHash))
	return hex.EncodeToString(sum[:])[:10]
}

// releasePending says the (image, config) pair about to roll out is not the
// current release: the release command must run first.
func releasePending(app *shpyrdv1.App, image, configHash string) bool {
	cur := app.CurrentRelease()
	return cur == nil || cur.Image != image || cur.ConfigHash != configHash
}

func releaseJobName(app *shpyrdv1.App, target string) string {
	return app.Name + "-release-" + target
}

// reconcileReleasePhase runs the release command for the target and reports
// its state. proceed is true when the rollout may go on (no command, or it
// succeeded); otherwise the caller sets the phase from the status and
// requeues.
func (r *AppReconciler) reconcileReleasePhase(ctx context.Context, app *shpyrdv1.App, image, configHash, revision string, res corev1.ResourceRequirements) (proceed bool, err error) {
	command := releaseCommand(app, app.Status.ProcessTypes)
	if command == nil {
		app.Status.Release = nil
		return true, nil
	}
	target := releaseTarget(image, configHash)
	if app.Status.Release != nil && app.Status.Release.Target == target && app.Status.Release.State == shpyrdv1.ReleaseSucceeded {
		return true, nil
	}
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: releaseJobName(app, target), Namespace: app.Namespace}}
	err = r.Get(ctx, client.ObjectKeyFromObject(job), job)
	switch {
	case apierrors.IsNotFound(err):
		if err := r.createReleaseJob(ctx, app, job, image, command, revision, res, target); err != nil {
			return false, err
		}
		r.pruneReleaseJobs(ctx, app, target)
		app.Status.Release = &shpyrdv1.ReleasePhaseStatus{Target: target, State: shpyrdv1.ReleaseRunning, Message: "running " + strings.Join(command, " "), Job: job.Name}
		return false, nil
	case err != nil:
		return false, fmt.Errorf("release job: %w", err)
	}
	st := &shpyrdv1.ReleasePhaseStatus{Target: target, Job: job.Name}
	switch {
	case jobSucceeded(job):
		st.State, st.Message = shpyrdv1.ReleaseSucceeded, "release command succeeded"
		app.Status.Release = st
		return true, nil
	case jobFailed(job):
		st.State = shpyrdv1.ReleaseFailed
		st.Message = fmt.Sprintf("release command failed (%s): fix it and deploy again; its output is in `shpyrd logs -p release`", jobFailureReason(job))
		app.Status.Release = st
		return false, nil
	default:
		st.State, st.Message = shpyrdv1.ReleaseRunning, "running "+strings.Join(command, " ")
		app.Status.Release = st
		return false, nil
	}
}

// createReleaseJob renders the one-off Job: the release's image and config
// vars, the process type's environment, no volumes, one attempt.
func (r *AppReconciler) createReleaseJob(ctx context.Context, app *shpyrdv1.App, job *batchv1.Job, image string, command []string, revision string, res corev1.ResourceRequirements, target string) error {
	labels := processLabels(app, releaseProcessType)
	labels["shpyrd.io/release-target"] = target
	container := corev1.Container{
		Name:            "app",
		Image:           image,
		Command:         command,
		Resources:       res,
		SecurityContext: hardenedSecurityContext(),
		EnvFrom:         EnvSources(app),
		Env:             append(r.Config.platformEnv(app, revision), corev1.EnvVar{Name: "SHPYRD_RELEASE_PHASE", Value: "1"}),
	}
	container.Env = append(container.Env, app.Spec.Env...)
	job.Labels = labels
	job.Spec = batchv1.JobSpec{
		BackoffLimit:            ptr.To[int32](0),
		ActiveDeadlineSeconds:   ptr.To[int64](1800),
		TTLSecondsAfterFinished: ptr.To[int32](86400),
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: labels},
			Spec: corev1.PodSpec{
				RestartPolicy:      corev1.RestartPolicyNever,
				EnableServiceLinks: ptr.To(false),
				ImagePullSecrets:   r.Config.imagePullSecrets(),
				SecurityContext:    &corev1.PodSecurityContext{SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
				Containers:         []corev1.Container{container},
			},
		},
	}
	if err := controllerutil.SetControllerReference(app, job, r.Scheme); err != nil {
		return err
	}
	if err := r.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create release job: %w", err)
	}
	r.Recorder.Event(app, corev1.EventTypeNormal, "ReleasePhase", "running the release command before rolling out")
	return nil
}

// pruneReleaseJobs removes release Jobs of other targets: a superseded
// release's command never runs, and its output is not what people look for.
func (r *AppReconciler) pruneReleaseJobs(ctx context.Context, app *shpyrdv1.App, keep string) {
	var jobs batchv1.JobList
	if err := r.List(ctx, &jobs, client.InNamespace(app.Namespace), client.MatchingLabels{shpyrdv1.LabelApp: app.Name, shpyrdv1.LabelProcess: releaseProcessType}); err != nil {
		return
	}
	for i := range jobs.Items {
		j := &jobs.Items[i]
		if j.Labels["shpyrd.io/release-target"] == keep {
			continue
		}
		_ = r.Delete(ctx, j, client.PropagationPolicy(metav1.DeletePropagationBackground))
	}
}

func jobSucceeded(j *batchv1.Job) bool {
	for _, c := range j.Status.Conditions {
		if c.Type == batchv1.JobComplete && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return j.Status.Succeeded > 0
}

func jobFailureReason(j *batchv1.Job) string {
	for _, c := range j.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			return firstNonEmpty(c.Message, c.Reason, "exit status not 0")
		}
	}
	return "exit status not 0"
}
