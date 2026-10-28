package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/buildtrust"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

// Dockerfile builds run as Kubernetes Jobs: an init container fetches the
// source (uploaded archive or git clone) into an emptyDir, then rootless
// BuildKit builds it with a local context and pushes to the registry. The
// pushed digest is written to the container's termination message; on
// failure Kubernetes puts the log tail there instead
// (FallbackToLogsOnError), which is what the user sees as the reason.

const (
	buildContainer = "build"
	fetchContainer = "fetch"
	workspaceDir   = "/workspace"

	// History kept per app, like kpack's build history limits.
	successfulBuildsKept = 10
	failedBuildsKept     = 5
)

// buildResult is what the build container reports on success.
type buildResult struct {
	Image    string `json:"image"`
	Revision string `json:"revision,omitempty"`
}

// reconcileDockerfileBuild makes sure a Job exists for the current source and
// build settings and reports its state.
func (r *AppReconciler) reconcileDockerfileBuild(ctx context.Context, app *shpyrdv1.App, git gitauth.Build) (buildState, error) {
	var jobs batchv1.JobList
	if err := r.List(ctx, &jobs, client.InNamespace(app.Namespace), client.MatchingLabels{shpyrdv1.LabelApp: app.Name}); err != nil {
		return buildState{}, fmt.Errorf("list builds: %w", err)
	}
	builds := buildJobs(jobs.Items)
	key := buildKey(app)

	var latest *batchv1.Job
	if len(builds) > 0 {
		latest = &builds[len(builds)-1]
	}
	if latest == nil || latest.Annotations[shpyrdv1.AnnotationBuildKey] != key {
		n := 1
		if latest != nil {
			n = buildNumber(latest) + 1
		}
		job, err := r.Config.desiredBuildJob(app, n, key, git)
		if err != nil {
			return buildState{}, err
		}
		if err := controllerutil.SetControllerReference(app, job, r.Scheme); err != nil {
			return buildState{}, err
		}
		if err := r.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
			return buildState{}, fmt.Errorf("create build job: %w", err)
		}
		// Only stop older builds once their replacement exists.
		if err := r.reconcileSupersededBuilds(ctx, builds); err != nil {
			return buildState{}, err
		}
		r.Recorder.Eventf(app, corev1.EventTypeNormal, "BuildRequested", "started Dockerfile build %s", job.Name)
		return buildState{LatestBuild: job.Name, Ready: "Unknown", Message: "build pending", LatestImage: lastImage(builds)}, nil
	}

	if err := r.reconcileSupersededBuilds(ctx, builds[:len(builds)-1]); err != nil {
		return buildState{}, err
	}
	st := buildState{LatestBuild: latest.Name, Ready: "Unknown", Message: "building"}
	switch {
	case latest.Annotations[shpyrdv1.AnnotationBuildImage] != "":
		st.Ready, st.LatestImage = "True", latest.Annotations[shpyrdv1.AnnotationBuildImage]
		st.Revision = latest.Annotations[shpyrdv1.AnnotationBuildRevision]
	case latest.Annotations[shpyrdv1.AnnotationBuildFailure] != "":
		st.Ready, st.Message = "False", latest.Annotations[shpyrdv1.AnnotationBuildFailure]
		st.LatestImage = lastImage(builds[:len(builds)-1])
	case latest.Status.Succeeded > 0:
		res, msg := r.buildOutcome(ctx, latest)
		if res == nil {
			st.Ready, st.Message = "False", firstNonEmpty(msg, "build finished without reporting an image")
			if err := r.annotateJob(ctx, latest, map[string]string{shpyrdv1.AnnotationBuildFailure: st.Message}); err != nil {
				return st, err
			}
			st.LatestImage = lastImage(builds[:len(builds)-1])
			break
		}
		if err := r.annotateJob(ctx, latest, map[string]string{
			shpyrdv1.AnnotationBuildImage:    res.Image,
			shpyrdv1.AnnotationBuildRevision: res.Revision,
		}); err != nil {
			return st, err
		}
		st.Ready, st.LatestImage, st.Revision = "True", res.Image, res.Revision
		r.Recorder.Eventf(app, corev1.EventTypeNormal, "BuildSucceeded", "%s pushed %s", latest.Name, shortImage(res.Image))
	case JobFailed(latest):
		_, msg := r.buildOutcome(ctx, latest)
		st.Ready, st.Message = "False", firstNonEmpty(msg, JobFailureMessage(latest))
		if err := r.annotateJob(ctx, latest, map[string]string{shpyrdv1.AnnotationBuildFailure: st.Message}); err != nil {
			return st, err
		}
		st.LatestImage = lastImage(builds[:len(builds)-1])
		r.Recorder.Eventf(app, corev1.EventTypeWarning, "BuildFailed", "%s: %s", latest.Name, st.Message)
	default:
		st.LatestImage = lastImage(builds[:len(builds)-1])
		if latest.Status.Active > 0 {
			st.Message = "building " + latest.Name
			break
		}
		// A quota refusal does not clear by waiting: the build fails now,
		// in words, rather than at its deadline 30 minutes on (#117).
		msg, by := r.jobRefusal(ctx, latest, "The build")
		if by != refusedQuota && by != refusedPlatform {
			break
		}
		if err := r.stopBuildJob(ctx, latest, msg); err != nil {
			return st, err
		}
		st.Ready, st.Message = "False", msg
		r.Recorder.Eventf(app, corev1.EventTypeWarning, "BuildFailed", "%s: %s", latest.Name, msg)
	}
	if err := r.pruneBuildJobs(ctx, builds); err != nil {
		return st, err
	}
	return st, nil
}

// buildJobs returns the app's build Jobs sorted by build number.
func buildJobs(items []batchv1.Job) []batchv1.Job {
	var out []batchv1.Job
	for _, j := range items {
		if _, ok := j.Labels[shpyrdv1.LabelBuildNumber]; ok && j.DeletionTimestamp == nil {
			out = append(out, j)
		}
	}
	sort.Slice(out, func(i, k int) bool { return buildNumber(&out[i]) < buildNumber(&out[k]) })
	return out
}

func buildNumber(j *batchv1.Job) int {
	n, _ := strconv.Atoi(j.Labels[shpyrdv1.LabelBuildNumber])
	return n
}

// lastImage is the newest image among finished builds.
func lastImage(builds []batchv1.Job) string {
	for i := len(builds) - 1; i >= 0; i-- {
		if img := builds[i].Annotations[shpyrdv1.AnnotationBuildImage]; img != "" {
			return img
		}
	}
	return ""
}

// JobFailed reports terminal Job failure, independently of failed pod counts.
func JobFailed(j *batchv1.Job) bool {
	for _, c := range j.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// JobFailureMessage describes a terminal failure even when no pod was created.
func JobFailureMessage(j *batchv1.Job) string {
	for _, c := range j.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			if c.Reason == "DeadlineExceeded" {
				return "The build did not finish within 30 minutes."
			}
			return firstNonEmpty(c.Message, c.Reason, "The Dockerfile build failed. "+buildLogHint)
		}
	}
	return "The Dockerfile build failed. " + buildLogHint
}

// reconcileSupersededBuilds preserves completed outcomes and stops older pending
// or active builds. Suspended Jobs retain their history without consuming quota.
func (r *AppReconciler) reconcileSupersededBuilds(ctx context.Context, builds []batchv1.Job) error {
	for i := range builds {
		job := &builds[i]
		if job.Annotations[shpyrdv1.AnnotationBuildImage] != "" || job.Annotations[shpyrdv1.AnnotationBuildFailure] != "" {
			continue
		}
		switch {
		case JobFailed(job):
			_, msg := r.buildOutcome(ctx, job)
			if err := r.annotateJob(ctx, job, map[string]string{shpyrdv1.AnnotationBuildFailure: firstNonEmpty(msg, JobFailureMessage(job))}); err != nil {
				return err
			}
		case job.Status.Succeeded > 0:
			res, msg := r.buildOutcome(ctx, job)
			ann := map[string]string{shpyrdv1.AnnotationBuildFailure: firstNonEmpty(msg, "build finished without reporting an image")}
			if res != nil {
				ann = map[string]string{shpyrdv1.AnnotationBuildImage: res.Image, shpyrdv1.AnnotationBuildRevision: res.Revision}
			}
			if err := r.annotateJob(ctx, job, ann); err != nil {
				return err
			}
		default:
			if err := r.stopBuildJob(ctx, job, "The build was superseded by a newer build."); err != nil {
				return err
			}
		}
	}
	return nil
}

// buildKey identifies the source and build settings a Job builds.
func buildKey(app *shpyrdv1.App) string {
	h := sha256.New()
	if src := app.Spec.Source; src != nil {
		switch {
		case src.Blob != nil:
			h.Write([]byte("blob:" + firstNonEmpty(src.Blob.SHA256, src.Blob.URL)))
		case src.Git != nil:
			// The pinned commit: a new commit of the branch is a new build.
			h.Write([]byte("git:" + src.Git.URL + "#" + app.BuildRevision()))
		}
		h.Write([]byte("subpath:" + src.SubPath))
	}
	if b := app.Spec.Build; b != nil {
		raw, _ := json.Marshal(b)
		h.Write(raw)
	}
	// A redeploy of the same source builds again.
	h.Write([]byte("rebuild:" + app.Annotations[shpyrdv1.AnnotationRebuildAt]))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// buildOutcome reads the build container's termination message: the JSON
// result on success, the log tail on failure.
func (r *AppReconciler) buildOutcome(ctx context.Context, job *batchv1.Job) (*buildResult, string) {
	var pods corev1.PodList
	if err := r.List(ctx, &pods, client.InNamespace(job.Namespace), client.MatchingLabels{shpyrdv1.LabelBuild: job.Name}); err != nil || len(pods.Items) == 0 {
		return nil, ""
	}
	sort.Slice(pods.Items, func(i, k int) bool {
		return pods.Items[i].CreationTimestamp.After(pods.Items[k].CreationTimestamp.Time)
	})
	pod := pods.Items[0]
	for _, cs := range pod.Status.InitContainerStatuses {
		if t := cs.State.Terminated; t != nil && t.ExitCode != 0 {
			if t.Reason == "OOMKilled" {
				return nil, buildOutOfMemory("fetching the source", buildFetchMemoryLimit)
			}
			return nil, describeBuildFailure("prepare", t.Message)
		}
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name != buildContainer || cs.State.Terminated == nil {
			continue
		}
		t := cs.State.Terminated
		if t.ExitCode == 0 {
			var res buildResult
			if err := json.Unmarshal([]byte(strings.TrimSpace(t.Message)), &res); err == nil && res.Image != "" {
				return &res, ""
			}
			return nil, "build finished without reporting an image"
		}
		if t.Reason == "OOMKilled" {
			return nil, buildOutOfMemory("building the image", buildMemoryLimit)
		}
		return nil, describeBuildFailure("dockerfile", t.Message)
	}
	return nil, ""
}

func (r *AppReconciler) annotateJob(ctx context.Context, job *batchv1.Job, ann map[string]string) error {
	patch := client.MergeFrom(job.DeepCopy())
	job.Annotations = mergeMaps(job.Annotations, ann)
	if err := r.Patch(ctx, job, patch); err != nil {
		return fmt.Errorf("annotate build job: %w", err)
	}
	return nil
}

// stopBuildJob records a failed or superseded build and suspends its Job,
// stopping existing instances and preventing new ones.
func (r *AppReconciler) stopBuildJob(ctx context.Context, job *batchv1.Job, msg string) error {
	// Retry if the Job changed since listing, rather than suspend a build
	// that completed concurrently.
	patch := client.MergeFromWithOptions(job.DeepCopy(), client.MergeFromWithOptimisticLock{})
	job.Annotations = mergeMaps(job.Annotations, map[string]string{shpyrdv1.AnnotationBuildFailure: msg})
	job.Spec.Suspend = ptr.To(true)
	if err := r.Patch(ctx, job, patch); err != nil {
		return fmt.Errorf("stop build job: %w", err)
	}
	return nil
}

// pruneBuildJobs deletes old finished builds beyond the history limits; the
// newest build is always kept.
func (r *AppReconciler) pruneBuildJobs(ctx context.Context, builds []batchv1.Job) error {
	if len(builds) == 0 {
		return nil
	}
	var succeeded, failed []batchv1.Job
	for _, j := range builds[:len(builds)-1] {
		switch {
		case j.Annotations[shpyrdv1.AnnotationBuildImage] != "":
			succeeded = append(succeeded, j)
		case j.Annotations[shpyrdv1.AnnotationBuildFailure] != "" || JobFailed(&j):
			failed = append(failed, j)
		}
	}
	del := func(list []batchv1.Job, keep int) error {
		for len(list) > keep {
			j := list[0]
			list = list[1:]
			if err := r.Delete(ctx, &j, client.PropagationPolicy(metav1.DeletePropagationBackground)); client.IgnoreNotFound(err) != nil {
				return err
			}
		}
		return nil
	}
	if err := del(succeeded, successfulBuildsKept); err != nil {
		return err
	}
	return del(failed, failedBuildsKept)
}

// buildJobName names build n of an app.
func buildJobName(app *shpyrdv1.App, n int) string { return fmt.Sprintf("%s-build-%d", app.Name, n) }

// desiredBuildJob builds the Job for build number n.
func (c Config) desiredBuildJob(app *shpyrdv1.App, n int, key string, git gitauth.Build) (*batchv1.Job, error) {
	name := buildJobName(app, n)
	labels := mergeMaps(commonLabels(app), map[string]string{shpyrdv1.LabelBuildNumber: strconv.Itoa(n)})
	podLabels := mergeMaps(commonLabels(app), map[string]string{shpyrdv1.LabelBuild: name})
	uid := ptr.To[int64](1000)
	unconfined := &corev1.SecurityContext{
		RunAsUser:  uid,
		RunAsGroup: uid,
		// Rootless BuildKit needs user namespaces and setuid newuidmap, so
		// the usual restrictions (seccomp, AppArmor, no privilege
		// escalation) cannot apply. It still runs unprivileged.
		SeccompProfile:  &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeUnconfined},
		AppArmorProfile: &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeUnconfined},
	}

	contextDir := workspaceDir + "/src"
	if sub := path.Clean("/" + app.Spec.Source.SubPath); sub != "/" {
		contextDir += sub
	}
	repo, err := c.imageTag(app)
	if err != nil {
		return nil, err
	}
	// Only an external registry without TLS is pushed to over plain HTTP;
	// the in-cluster registry's certificate is trusted through the bundle.
	insecure := ""
	if c.RegistryInsecure {
		insecure = ",registry.insecure=true"
	}
	args := []string{
		"build",
		"--progress", "plain",
		"--frontend", "dockerfile.v0",
		"--local", "context=" + contextDir,
		"--local", "dockerfile=" + contextDir,
		"--opt", "filename=" + dockerfilePath(app),
		"--output", fmt.Sprintf("type=image,name=%s:b%d,push=true%s", repo, n, insecure),
		"--export-cache", fmt.Sprintf("type=registry,ref=%s:cache,mode=max%s", repo, insecure),
		"--import-cache", fmt.Sprintf("type=registry,ref=%s:cache%s", repo, insecure),
		"--metadata-file", "/tmp/meta.json",
	}
	if b := app.Spec.Build; b != nil {
		if b.Target != "" {
			args = append(args, "--opt", "target="+b.Target)
		}
		for _, e := range b.Env {
			args = append(args, "--opt", "build-arg:"+e.Name+"="+e.Value)
		}
	}

	// The script runs buildctl with the arguments above, then reports the
	// pushed digest as JSON in the termination message.
	buildScript := `buildctl-daemonless.sh "$@"
digest=$(sed -n 's/.*"containerimage.digest": *"\([^"]*\)".*/\1/p' /tmp/meta.json)
[ -n "$digest" ] || { echo "no image digest in metadata"; exit 1; }
revision=$(cat ` + workspaceDir + `/revision 2>/dev/null || true)
printf '{"image":"%s@%s","revision":"%s"}' "$IMAGE_REPO" "$digest" "$revision" > /dev/termination-log
echo "pushed $IMAGE_REPO@$digest"`

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   app.Namespace,
			Labels:      labels,
			Annotations: map[string]string{shpyrdv1.AnnotationBuildKey: key},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:          ptr.To[int32](0),
			ActiveDeadlineSeconds: ptr.To[int64](1800),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: podLabels},
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					EnableServiceLinks: ptr.To(false),
					NodeSelector:       c.appsNodeSelector(), // RFC-0077
					// Outside the workspace's ceiling (#243), bounded by the
					// limits below instead.
					PriorityClassName: BuildPriorityClass,
					SecurityContext:    &corev1.PodSecurityContext{FSGroup: uid},
					InitContainers: []corev1.Container{{
						Name:    fetchContainer,
						Image:   c.BuildKitImage,
						Command: []string{"sh", "-ec", c.fetchScript(app, git)},
						// Small requests for scheduling, a limit for protection: a
						// shallow clone of a large repository can need a few
						// hundred MiB.
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("10m"),
								corev1.ResourceMemory: resource.MustParse("64Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse(buildFetchCPULimit),
								corev1.ResourceMemory: resource.MustParse(buildFetchMemoryLimit),
							},
						},
						SecurityContext:          &corev1.SecurityContext{RunAsUser: uid, RunAsGroup: uid, AllowPrivilegeEscalation: ptr.To(false)},
						VolumeMounts:             append([]corev1.VolumeMount{{Name: "workspace", MountPath: workspaceDir}}, gitAuthMounts(git)...),
						TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
					}},
					Containers: []corev1.Container{{
						Name:    buildContainer,
						Image:   c.BuildKitImage,
						Command: []string{"sh", "-ec", buildScript, "buildctl"},
						Args:    args,
						Env: append(append([]corev1.EnvVar{
							{Name: "BUILDKITD_FLAGS", Value: "--oci-worker-no-process-sandbox"},
							{Name: "IMAGE_REPO", Value: repo},
						}, c.buildKitAuthEnv()...), c.trustEnv()...),
						// The requests place the build on a machine; the limits
						// are its hard ceiling, since the workspace's no longer
						// counts it (#243).
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("250m"),
								corev1.ResourceMemory: resource.MustParse("512Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse(buildCPULimit),
								corev1.ResourceMemory: resource.MustParse(buildMemoryLimit),
							},
						},
						SecurityContext: unconfined,
						VolumeMounts: append(append([]corev1.VolumeMount{
							{Name: "workspace", MountPath: workspaceDir},
							{Name: "buildkit", MountPath: "/home/user/.local/share/buildkit"},
						}, c.buildKitAuthMounts()...), c.trustMounts()...),
						TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
					}},
					ImagePullSecrets: c.imagePullSecrets(),
					Volumes: append(append([]corev1.Volume{
						{Name: "workspace", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
						{Name: "buildkit", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
					}, c.buildKitAuthVolumes()...), append(c.trustVolumes(), gitAuthVolumes(git)...)...),
				},
			},
		},
	}
	return job, nil
}

// A Dockerfile build's hard ceiling (#243): out of the workspace's quota,
// it is bounded here. Past the memory limit the build is killed and says
// so; CPU above the limit is throttled.
const (
	buildMemoryLimit      = "2Gi"
	buildCPULimit         = "2"
	buildFetchMemoryLimit = "512Mi"
	buildFetchCPULimit    = "500m"
)

// buildOutOfMemory says a build passed its hard ceiling, in words.
func buildOutOfMemory(doing, limit string) string {
	return fmt.Sprintf("The build ran out of memory while %s: a build may use up to %s. Make the build lighter (fewer steps in parallel, a smaller dependency install, a multi-stage Dockerfile).", doing, limit)
}

// dockerfilePath is the Dockerfile location relative to the context.
func dockerfilePath(app *shpyrdv1.App) string {
	if app.Spec.Build != nil && app.Spec.Build.Dockerfile != "" {
		return strings.TrimPrefix(path.Clean(app.Spec.Build.Dockerfile), "/")
	}
	return "Dockerfile"
}

// gitAuthDir is where the fetch container finds <app>-git.
const gitAuthDir = "/git-auth"

// fetchScript downloads the uploaded archive or clones the git revision
// into the workspace and records the resolved revision. With a credential
// it reads it from gitAuthDir: never inlined, so a value with quotes or a
// newline cannot change the script.
func (c Config) fetchScript(app *shpyrdv1.App, git gitauth.Build) string {
	src := app.Spec.Source
	dst := workspaceDir + "/src"
	switch {
	case src.Blob != nil:
		return fmt.Sprintf(`mkdir -p %[1]s
wget -qO /tmp/source.tgz %[2]s
tar xzf /tmp/source.tgz -C %[1]s
printf '%%s' %[3]s > %[4]s/revision
echo "source ready"`, dst, shellQuote(c.sourceURL(src.Blob.URL)), shellQuote(src.Blob.Ref), workspaceDir)
	case src.Git != nil:
		url := src.Git.URL
		if git.CloneURL != "" {
			url = git.CloneURL
		}
		auth := ""
		switch git.Kind {
		case gitauth.KindToken, gitauth.KindGitHubApp:
			auth = fmt.Sprintf(`export HOME=/tmp
printf 'machine %%s login %%s password %%s\n' %[1]s "$(cat %[2]s/username)" "$(cat %[2]s/password)" > "$HOME/.netrc"
chmod 600 "$HOME/.netrc"
`, shellQuote(git.Host), gitAuthDir)
		case gitauth.KindDeployKey:
			auth = fmt.Sprintf(`cp %[1]s/ssh-privatekey /tmp/deploy-key
chmod 600 /tmp/deploy-key
export GIT_SSH_COMMAND="ssh -i /tmp/deploy-key -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=%[1]s/known_hosts"
`, gitAuthDir)
		}
		// Only with a deploy key is there a recorded host key for ssh to
		// refuse: git's output is the same without one, so the marker
		// says which it was (describeBuildFailure).
		hostKey := ""
		if git.Kind == gitauth.KindDeployKey {
			hostKey = fmt.Sprintf(`    if grep -q 'Host key verification failed' /tmp/clone.err; then echo %s >&2; fi
`, shellQuote(hostKeyChangedMarker))
		}
		// The pinned commit of a branch; the script already fetches a
		// commit that is not a branch or tag.
		rev := app.BuildRevision()
		return auth + fmt.Sprintf(`url=%[1]s
rev=%[2]s
if git clone --quiet --depth 1 --branch "$rev" "$url" %[3]s 2>/tmp/clone.err; then :; else
  if grep -qiE 'authentication|could not read|permission denied|repository not found|host key verification' /tmp/clone.err; then
    cat /tmp/clone.err >&2
%[5]s    exit 1
  fi
  # not a branch or tag: fetch the commit
  git init --quiet %[3]s
  git -C %[3]s fetch --quiet --depth 1 "$url" "$rev"
  git -C %[3]s checkout --quiet FETCH_HEAD
fi
git -C %[3]s rev-parse HEAD > %[4]s/revision
echo "source ready at $(cat %[4]s/revision)"`, shellQuote(url), shellQuote(rev), dst, workspaceDir, hostKey)
	}
	return "echo 'no source'; exit 1"
}

// shellQuote single-quotes s for POSIX sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// The project's git credential for the fetch container only: the build's
// RUN steps never see it.
func gitAuthMounts(git gitauth.Build) []corev1.VolumeMount {
	if git.Secret == nil {
		return nil
	}
	return []corev1.VolumeMount{{Name: "git-auth", MountPath: gitAuthDir, ReadOnly: true}}
}

func gitAuthVolumes(git gitauth.Build) []corev1.Volume {
	if git.Secret == nil {
		return nil
	}
	// Optional: a credential removed between the Job and its pod fails the
	// clone at once rather than holding the pod in ContainerCreating.
	return []corev1.Volume{{Name: "git-auth", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
		SecretName: git.Secret.Name, DefaultMode: ptr.To[int32](0o440), Optional: ptr.To(true),
	}}}}
}

// Registry credentials for BuildKit: the mirrored dockerconfigjson Secret
// mounted where the rootless user's docker config lives.
const dockerConfigDir = "/home/user/.docker"

func (c Config) buildKitAuthEnv() []corev1.EnvVar {
	if c.RegistrySecret == "" {
		return nil
	}
	return []corev1.EnvVar{{Name: "DOCKER_CONFIG", Value: dockerConfigDir}}
}

func (c Config) buildKitAuthMounts() []corev1.VolumeMount {
	if c.RegistrySecret == "" {
		return nil
	}
	return []corev1.VolumeMount{{Name: "registry-auth", MountPath: dockerConfigDir, ReadOnly: true}}
}

// The trust bundle for BuildKit (RFC-0059): buildkitd and buildctl are Go
// programs, so SSL_CERT_FILE makes them trust the in-cluster registry's
// certificate along with the public roots the bundle carries.
func (c Config) trustEnv() []corev1.EnvVar {
	if c.CABundle == "" {
		return nil
	}
	return []corev1.EnvVar{{Name: buildtrust.EnvVar, Value: buildtrust.MountPath + "/" + buildtrust.BundleKey}}
}

func (c Config) trustMounts() []corev1.VolumeMount {
	if c.CABundle == "" {
		return nil
	}
	return []corev1.VolumeMount{{Name: buildtrust.VolumeName, MountPath: buildtrust.MountPath, ReadOnly: true}}
}

func (c Config) trustVolumes() []corev1.Volume {
	if c.CABundle == "" {
		return nil
	}
	return []corev1.Volume{{Name: buildtrust.VolumeName, VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
		LocalObjectReference: corev1.LocalObjectReference{Name: c.CABundle},
	}}}}
}

func (c Config) buildKitAuthVolumes() []corev1.Volume {
	if c.RegistrySecret == "" {
		return nil
	}
	return []corev1.Volume{{Name: "registry-auth", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
		SecretName: c.RegistrySecret,
		Items:      []corev1.KeyToPath{{Key: corev1.DockerConfigJsonKey, Path: "config.json"}},
	}}}}
}
