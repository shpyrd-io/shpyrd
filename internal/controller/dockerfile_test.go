package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

const testDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func dockerfileApp() *shpyrdv1.App {
	return &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "dk", Namespace: "app-dk", Generation: 1},
		Spec: shpyrdv1.AppSpec{
			Source: &shpyrdv1.Source{Blob: &shpyrdv1.BlobSource{
				URL: "http://shpyrd-server.shpyrd-system.svc/api/sources/abc.tgz", SHA256: "abc123abc123abc123", Ref: "0123456789ab",
			}},
			Build: &shpyrdv1.Build{
				Strategy: shpyrdv1.StrategyDockerfile, Dockerfile: "deploy/Dockerfile", Target: "runtime",
				Env: []corev1.EnvVar{{Name: "NODE_ENV", Value: "production"}},
			},
		},
	}
}

func getJob(t *testing.T, c client.Client, ns, name string) *batchv1.Job {
	t.Helper()
	j := &batchv1.Job{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, j); err != nil {
		t.Fatalf("job %s: %v", name, err)
	}
	return j
}

// finishBuild marks the Job finished and creates its pod with the
// termination message the build container would have written.
func finishBuild(t *testing.T, c client.Client, job *batchv1.Job, exitCode int32, message string) {
	t.Helper()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: job.Name + "-x1", Namespace: job.Namespace, Labels: job.Spec.Template.Labels},
		Spec:       job.Spec.Template.Spec,
		Status: corev1.PodStatus{
			Phase: corev1.PodSucceeded,
			ContainerStatuses: []corev1.ContainerStatus{{Name: buildContainer, State: corev1.ContainerState{
				Terminated: &corev1.ContainerStateTerminated{ExitCode: exitCode, Message: message},
			}}},
		},
	}
	if err := c.Create(context.Background(), pod); err != nil {
		t.Fatal(err)
	}
	if exitCode == 0 {
		job.Status.Succeeded = 1
	} else {
		job.Status.Failed = 1
		job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}
	}
	if err := c.Status().Update(context.Background(), job); err != nil {
		t.Fatal(err)
	}
}

func TestDockerfileBuildJob(t *testing.T) {
	app := dockerfileApp()
	r, c := newTestReconciler(t, app)

	got := runReconcile(t, r, app)
	if got.Status.Phase != shpyrdv1.PhaseBuilding || got.Status.LatestBuild != "dk-build-1" {
		t.Fatalf("phase = %q build=%q (%s)", got.Status.Phase, got.Status.LatestBuild, got.Status.Message)
	}
	img := &unstructured.Unstructured{}
	img.SetGroupVersionKind(KpackImageGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-dk", Name: "dk"}, img); err == nil {
		t.Error("no kpack Image must be created for Dockerfile builds")
	}

	job := getJob(t, c, "app-dk", "dk-build-1")
	if job.Labels[shpyrdv1.LabelBuildNumber] != "1" || job.Annotations[shpyrdv1.AnnotationBuildKey] == "" {
		t.Errorf("job labels/annotations: %v %v", job.Labels, job.Annotations)
	}
	if len(job.OwnerReferences) != 1 || job.OwnerReferences[0].Name != "dk" {
		t.Error("job must be owned by the app")
	}
	spec := job.Spec.Template.Spec
	if job.Spec.Template.Labels[shpyrdv1.LabelBuild] != "dk-build-1" {
		t.Errorf("build pods must carry %s: %v", shpyrdv1.LabelBuild, job.Spec.Template.Labels)
	}
	if len(spec.InitContainers) != 1 || !strings.Contains(spec.InitContainers[0].Command[2], "wget -qO /tmp/source.tgz 'http://shpyrd-server.shpyrd-system.svc:8082/api/sources/abc.tgz'") {
		t.Errorf("fetch script: %+v", spec.InitContainers)
	}
	args := strings.Join(spec.Containers[0].Args, " ")
	for _, want := range []string{
		"--local context=/workspace/src",
		"--opt filename=deploy/Dockerfile",
		"--opt target=runtime",
		"--opt build-arg:NODE_ENV=production",
		"type=image,name=10.96.0.50:5000/apps/default/dk:b1,push=true,registry.insecure=true",
		"ref=10.96.0.50:5000/apps/default/dk:cache",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("buildctl args miss %q: %s", want, args)
		}
	}
	if spec.Containers[0].TerminationMessagePolicy != corev1.TerminationMessageFallbackToLogsOnError {
		t.Error("build container must fall back to logs for the failure reason")
	}
	if sc := spec.Containers[0].SecurityContext; sc == nil || sc.SeccompProfile.Type != corev1.SeccompProfileTypeUnconfined || *sc.RunAsUser != 1000 {
		t.Errorf("rootless buildkit security context: %+v", sc)
	}

	// Same source again: no new build.
	runReconcile(t, r, app)
	var jobs batchv1.JobList
	if err := c.List(context.Background(), &jobs, client.InNamespace("app-dk")); err != nil || len(jobs.Items) != 1 {
		t.Fatalf("jobs = %d (%v)", len(jobs.Items), err)
	}

	// The build succeeds and reports its digest.
	finishBuild(t, c, job, 0, `{"image":"10.96.0.50:5000/apps/dk@`+testDigest+`","revision":""}`)
	got = runReconcile(t, r, app)
	if got.Status.Phase != shpyrdv1.PhaseDeploying {
		t.Fatalf("phase = %q (%s), want Deploying", got.Status.Phase, got.Status.Message)
	}
	if got.Status.Image != "10.96.0.50:5000/apps/dk@"+testDigest {
		t.Errorf("status.image = %q", got.Status.Image)
	}
	if len(got.Status.Releases) != 1 || got.Status.Releases[0].Description != "Initial deploy 0123456789ab" {
		t.Errorf("releases = %+v", got.Status.Releases)
	}
	if getJob(t, c, "app-dk", "dk-build-1").Annotations[shpyrdv1.AnnotationBuildImage] == "" {
		t.Error("finished job must be annotated with its image")
	}

	// A new archive triggers build 2; its failure keeps the release and
	// says, in words, which step failed (BuildKit's lines stay in the log).
	app = got
	app.Spec.Source.Blob.SHA256, app.Spec.Source.Blob.Ref = "def456def456def456", "fedcba987654"
	if err := c.Update(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	got = runReconcile(t, r, app)
	if got.Status.LatestBuild != "dk-build-2" || got.Status.Phase != shpyrdv1.PhaseBuilding {
		t.Fatalf("build=%q phase=%q", got.Status.LatestBuild, got.Status.Phase)
	}
	markDeploymentReady(t, c, "app-dk", "dk-web", 1)
	finishBuild(t, c, getJob(t, c, "app-dk", "dk-build-2"), 1, "#5 [2/3] RUN npm ci\n#5 ERROR: process \"/bin/sh -c npm ci\" did not complete successfully: exit code: 1\nerror: failed to solve: process did not complete successfully\n")
	got = runReconcile(t, r, app)
	if got.Status.Phase != shpyrdv1.PhaseFailed || !strings.Contains(got.Status.Message, "while running `npm ci` (exit status 1)") || !strings.Contains(got.Status.Message, "previous release keeps running") || strings.Contains(got.Status.Message, "failed to solve") {
		t.Fatalf("phase = %q (%s)", got.Status.Phase, got.Status.Message)
	}
	if got.Status.Image != "10.96.0.50:5000/apps/dk@"+testDigest || len(got.Status.Releases) != 1 {
		t.Error("previous release must keep running after a failed build")
	}
}

func TestDockerfileGitFetchAndProcesses(t *testing.T) {
	app := dockerfileApp()
	app.Spec.Source = &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: "https://github.com/o/r.git", Revision: "v1.2"}, SubPath: "services/api"}
	app.Spec.Processes = map[string]shpyrdv1.Process{"web": {}, "worker": {}}
	r, c := newTestReconciler(t, pinGit(app, testCommit))

	got := runReconcile(t, r, app)
	job := getJob(t, c, "app-dk", "dk-build-1")
	fetch := job.Spec.Template.Spec.InitContainers[0].Command[2]
	if !strings.Contains(fetch, "git clone --quiet --depth 1 --branch \"$rev\"") || !strings.Contains(fetch, "rev='"+testCommit+"'") {
		t.Errorf("git fetch script: %s", fetch)
	}
	if args := strings.Join(job.Spec.Template.Spec.Containers[0].Args, " "); !strings.Contains(args, "--local context=/workspace/src/services/api") {
		t.Errorf("subPath must select the context dir: %s", args)
	}

	// Finish with a resolved revision; the worker lacks a command so the
	// rollout is refused with a clear message.
	finishBuild(t, c, job, 0, `{"image":"10.96.0.50:5000/apps/dk@`+testDigest+`","revision":"9f8e7d6c5b4a3210"}`)
	got = runReconcile(t, r, app)
	if got.Status.Phase != shpyrdv1.PhaseFailed || !strings.Contains(got.Status.Message, `process "worker" needs a command`) {
		t.Fatalf("phase = %q (%s)", got.Status.Phase, got.Status.Message)
	}

	app = got
	app.Spec.Processes["worker"] = shpyrdv1.Process{Command: []string{"node", "worker.js"}}
	if err := c.Update(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	got = runReconcile(t, r, app)
	if got.Status.Phase != shpyrdv1.PhaseDeploying {
		t.Fatalf("phase = %q (%s)", got.Status.Phase, got.Status.Message)
	}
	if len(got.Status.Releases) != 1 || got.Status.Releases[0].Source != "9f8e7d6c5b4a" {
		t.Errorf("release source should be the resolved commit: %+v", got.Status.Releases)
	}
}

func TestBuildKeyAndFailureSummary(t *testing.T) {
	a := dockerfileApp()
	b := dockerfileApp()
	if buildKey(a) != buildKey(b) {
		t.Error("same spec must give the same key")
	}
	b.Spec.Build.Target = "debug"
	if buildKey(a) == buildKey(b) {
		t.Error("build settings are part of the key")
	}
	c := dockerfileApp()
	c.Spec.Source.Blob.SHA256 = "other"
	if buildKey(a) == buildKey(c) {
		t.Error("the source is part of the key")
	}

	// A failed BuildKit build is told in words: the RUN step's command,
	// never BuildKit's lines (#52).
	got := describeBuildFailure("dockerfile", "#7 [build 4/5] RUN npm run build\n#7 ERROR: process \"/bin/sh -c npm run build\" did not complete successfully: exit code: 1\n------\nerror: failed to solve: process \"/bin/sh -c npm run build\" did not complete successfully: exit code: 1\n")
	if got != "The build failed while running `npm run build` (exit status 1). "+buildLogHint {
		t.Errorf("summary = %q", got)
	}
	if got := describeBuildFailure("dockerfile", "#3 ERROR: not found\nerror: failed to solve: not found\n"); got != "The Dockerfile build failed. "+buildLogHint {
		t.Errorf("fallback summary = %q", got)
	}
	if shellQuote("it's") != `'it'\''s'` {
		t.Errorf("shellQuote = %s", shellQuote("it's"))
	}
}

// #117: a workspace with limits covers every pod that asks for resources
// with its quota, and Kubernetes then refuses a pod in which any container
// declares no CPU or memory request. The build's containers all declare
// both.
func TestBuildJobDeclaresRequestsInEveryContainer(t *testing.T) {
	r, _ := newTestReconciler(t)
	job, err := r.Config.desiredBuildJob(dockerfileApp(), 1, "k", gitauth.Build{})
	if err != nil {
		t.Fatal(err)
	}
	if fault := QuotaRequestsFault(job.Spec.Template.Spec); fault != "" {
		t.Error(fault)
	}
	pod := OneOffPod(dockerfileApp(), "registry.test/dk@"+testDigest, []string{"rake", "db:migrate"}, corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("256Mi")}}, false, false, "")
	if fault := QuotaRequestsFault(pod.Spec); fault != "" {
		t.Error(fault)
	}
	// The guard itself: one container asking, one not.
	bad := job.Spec.Template.Spec.DeepCopy()
	bad.InitContainers[0].Resources = corev1.ResourceRequirements{}
	if fault := QuotaRequestsFault(*bad); !strings.Contains(fault, fetchContainer) {
		t.Errorf("a container without requests beside one with: fault = %q", fault)
	}
}

// #117: a build whose instance the workspace's quota refuses does not sit
// "building" until its 30-minute deadline. It fails at once, says why in
// words, and the platform stops asking.
func TestBuildRefusedAtCreation(t *testing.T) {
	for _, tc := range []struct {
		name, event, want string
	}{
		{
			"the platform's own fault",
			`Error creating: pods "dk-build-1-kr98m" is forbidden: failed quota: shpyrd: must specify requests.cpu for: fetch; requests.memory for: fetch`,
			"The build cannot start: the platform asked for it without the resource requests the workspace's ceiling needs. This is a platform error, not your app's",
		},
		{
			"the workspace's ceiling",
			`Error creating: pods "dk-build-1-kr98m" is forbidden: exceeded quota: shpyrd, requested: requests.memory=512Mi, used: requests.memory=256Mi, limited: requests.memory=512Mi`,
			"The build cannot start: the workspace's memory ceiling is 512Mi, 256Mi of it taken",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			app := dockerfileApp()
			r, c := newTestReconciler(t, app)
			kube := kubefake.NewSimpleClientset()
			r.Kube = kube
			got := runReconcile(t, r, app)
			job := getJob(t, c, "app-dk", "dk-build-1")
			if _, err := kube.CoreV1().Events("app-dk").Create(ctx, &corev1.Event{
				ObjectMeta:     metav1.ObjectMeta{Name: "refused", Namespace: "app-dk"},
				InvolvedObject: corev1.ObjectReference{Kind: "Job", Name: job.Name, Namespace: "app-dk"},
				Reason:         "FailedCreate", Message: tc.event, LastTimestamp: metav1.Now(),
			}, metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
			got = runReconcile(t, r, got)
			if got.Status.Phase != shpyrdv1.PhaseFailed || !strings.HasPrefix(got.Status.Message, tc.want) {
				t.Fatalf("phase=%q message=%q, want Failed: %s", got.Status.Phase, got.Status.Message, tc.want)
			}
			if bad := PlatformWordingFault(got.Status.Message); bad != "" {
				t.Errorf("%q names %q", got.Status.Message, bad)
			}
			job = getJob(t, c, "app-dk", "dk-build-1")
			if job.Spec.Suspend == nil || !*job.Spec.Suspend {
				t.Error("the refused build must stop asking for its instance")
			}
			// It stays failed: the next pass reads the outcome back.
			got = runReconcile(t, r, got)
			if got.Status.Phase != shpyrdv1.PhaseFailed || !strings.HasPrefix(got.Status.Message, tc.want) {
				t.Errorf("next pass: phase=%q message=%q", got.Status.Phase, got.Status.Message)
			}
		})
	}
}

func TestDockerfileSupersededBuilds(t *testing.T) {
	for _, state := range []string{"pending", "active", "failed without pod", "succeeded"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			app := dockerfileApp()
			r, c := newTestReconciler(t, app)
			if _, err := r.reconcileDockerfileBuild(ctx, app, gitauth.Build{}); err != nil {
				t.Fatal(err)
			}
			old := getJob(t, c, app.Namespace, "dk-build-1")
			switch state {
			case "active":
				old.Status.Active = 1
				if err := c.Status().Update(ctx, old); err != nil {
					t.Fatal(err)
				}
			case "failed without pod":
				old.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "DeadlineExceeded"}}
				if err := c.Status().Update(ctx, old); err != nil {
					t.Fatal(err)
				}
			case "succeeded":
				finishBuild(t, c, old, 0, `{"image":"registry.test/dk@`+testDigest+`"}`)
			}
			app.Spec.Source.Blob.SHA256 = "new-source"
			for pass := 0; pass < 2; pass++ {
				got, err := r.reconcileDockerfileBuild(ctx, app, gitauth.Build{})
				if err != nil {
					t.Fatal(err)
				}
				if got.LatestBuild != "dk-build-2" || got.Ready != "Unknown" {
					t.Fatalf("replacement state = %+v", got)
				}
				old = getJob(t, c, app.Namespace, "dk-build-1")
				switch state {
				case "pending", "active":
					if !ptr.Deref(old.Spec.Suspend, false) || old.Annotations[shpyrdv1.AnnotationBuildFailure] != "The build was superseded by a newer build." {
						t.Fatalf("superseded build = %+v", old)
					}
				case "failed without pod":
					if ptr.Deref(old.Spec.Suspend, false) || old.Annotations[shpyrdv1.AnnotationBuildFailure] != buildDeadlineMessage {
						t.Fatalf("failed build = %+v", old)
					}
				case "succeeded":
					if ptr.Deref(old.Spec.Suspend, false) || old.Annotations[shpyrdv1.AnnotationBuildImage] != "registry.test/dk@"+testDigest || got.LatestImage != "registry.test/dk@"+testDigest {
						t.Fatalf("successful history lost: job=%+v state=%+v", old, got)
					}
				}
			}
		})
	}
}

func TestDockerfileOldFailureIsAnnotated(t *testing.T) {
	ctx := context.Background()
	app := dockerfileApp()
	r, c := newTestReconciler(t, app)
	for n := 1; n <= 2; n++ {
		job, err := r.Config.desiredBuildJob(app, n, buildKey(app), gitauth.Build{})
		if err != nil {
			t.Fatal(err)
		}
		if n == 1 {
			job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "DeadlineExceeded"}}
		}
		if err := c.Create(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.reconcileDockerfileBuild(ctx, app, gitauth.Build{}); err != nil {
		t.Fatal(err)
	}
	if got := getJob(t, c, app.Namespace, "dk-build-1").Annotations[shpyrdv1.AnnotationBuildFailure]; got != buildDeadlineMessage {
		t.Fatalf("old failure = %q", got)
	}
}

func TestStopBuildDoesNotOverwriteConcurrentCompletion(t *testing.T) {
	ctx := context.Background()
	app := dockerfileApp()
	r, c := newTestReconciler(t, app)
	if _, err := r.reconcileDockerfileBuild(ctx, app, gitauth.Build{}); err != nil {
		t.Fatal(err)
	}
	stale := getJob(t, c, app.Namespace, "dk-build-1")
	finishBuild(t, c, stale.DeepCopy(), 0, `{"image":"registry.test/dk@`+testDigest+`"}`)
	if err := r.stopBuildJob(ctx, stale, "superseded"); !apierrors.IsConflict(err) {
		t.Fatalf("stale cancellation error = %v, want conflict", err)
	}
	got := getJob(t, c, app.Namespace, stale.Name)
	if ptr.Deref(got.Spec.Suspend, false) || got.Annotations[shpyrdv1.AnnotationBuildFailure] != "" {
		t.Fatalf("completed build was stopped: %+v", got)
	}
}

// githubAppBuild is the App's credential for url, as the controller makes
// it: a token Secret, marked as the App's.
func githubAppBuild(t *testing.T, url, token string) gitauth.Build {
	t.Helper()
	tok := &corev1.Secret{Data: map[string][]byte{gitauth.KeyToken: []byte(token), gitauth.KeyUsername: []byte("x-access-token")}}
	b, err := gitauth.Resolve(url, "dk", "app-dk", nil, tok)
	if err != nil {
		t.Fatal(err)
	}
	b.Kind = gitauth.KindGitHubApp
	return b
}

func TestFetchScriptOfTheGitHubAppWritesNetrc(t *testing.T) {
	app := dockerfileGitApp("https://github.com/acme/dk")
	script := Config{}.fetchScript(app, githubAppBuild(t, app.Spec.Source.Git.URL, "ghs_s3cret"))
	if !strings.Contains(script, ".netrc") || !strings.Contains(script, "machine %s login %s password %s") || !strings.Contains(script, gitAuthDir+"/password") {
		t.Errorf("the App's token is not given to git:\n%s", script)
	}
	if strings.Contains(script, "ghs_s3cret") {
		t.Errorf("the token is inlined:\n%s", script)
	}
}

// TestFetchScriptClonesThroughTheGitHubApp runs the fetch script with git
// against a git host that answers only the App's token.
func TestFetchScriptClonesThroughTheGitHubApp(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git")
	}
	out, err := exec.Command(gitBin, "--exec-path").Output()
	if err != nil {
		t.Skip("no git exec path")
	}
	backend := filepath.Join(strings.TrimSpace(string(out)), "git-http-backend")
	if _, err := os.Stat(backend); err != nil {
		t.Skip("no git-http-backend")
	}
	root := t.TempDir()
	env := append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=a@a", "GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=a@a")
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command(gitBin, args...)
		cmd.Dir, cmd.Env = dir, env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	work := filepath.Join(root, "work")
	run(root, "init", "--quiet", "-b", "main", work)
	if err := os.WriteFile(filepath.Join(work, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(work, "add", "Dockerfile")
	run(work, "commit", "--quiet", "-m", "first")
	run(root, "clone", "--quiet", "--bare", work, filepath.Join(root, "srv", "acme", "dk.git"))

	git := &cgi.Handler{Path: backend, Env: []string{"GIT_PROJECT_ROOT=" + filepath.Join(root, "srv"), "GIT_HTTP_EXPORT_ALL=1"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if u, p, ok := req.BasicAuth(); !ok || u != "x-access-token" || p != "ghs_s3cret" {
			w.Header().Set("WWW-Authenticate", `Basic realm="GitHub"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		git.ServeHTTP(w, req)
	}))
	defer srv.Close()

	app := dockerfileGitApp(srv.URL + "/acme/dk.git")
	delete(app.Annotations, shpyrdv1.AnnotationGitCommit) // not pinned: clone the branch
	b := githubAppBuild(t, app.Spec.Source.Git.URL, "ghs_s3cret")
	script := Config{}.fetchScript(app, b)

	// The script's fixed paths, moved under the test's directory.
	tmp, auth, ws := filepath.Join(root, "tmp"), filepath.Join(root, "git-auth"), filepath.Join(root, "workspace")
	for _, d := range []string{tmp, auth, ws} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for k, v := range b.Secret.Data {
		if err := os.WriteFile(filepath.Join(auth, k), v, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	script = strings.ReplaceAll(script, "/tmp", tmp)
	script = strings.ReplaceAll(script, gitAuthDir, auth)
	script = strings.ReplaceAll(script, workspaceDir, ws)
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fetch: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(ws, "src", "Dockerfile")); err != nil {
		t.Errorf("not cloned: %v", err)
	}
}

// A Dockerfile build runs outside the workspace's ceiling (#243), under the
// build priority class the compute quota leaves out, and every container of
// it has a hard memory and CPU ceiling of its own instead.
func TestABuildIsOutsideTheCeilingAndBoundedByItsOwnLimits(t *testing.T) {
	r, _ := newTestReconciler(t)
	job, err := r.Config.desiredBuildJob(dockerfileApp(), 1, "k", gitauth.Build{})
	if err != nil {
		t.Fatal(err)
	}
	pod := job.Spec.Template.Spec
	if pod.PriorityClassName != BuildPriorityClass {
		t.Errorf("priority class = %q, want %q", pod.PriorityClassName, BuildPriorityClass)
	}
	for _, c := range append(append([]corev1.Container{}, pod.InitContainers...), pod.Containers...) {
		mem, cpu := c.Resources.Limits[corev1.ResourceMemory], c.Resources.Limits[corev1.ResourceCPU]
		if mem.IsZero() || cpu.IsZero() {
			t.Errorf("%s has no hard ceiling: %v", c.Name, c.Resources.Limits)
		}
		if req := c.Resources.Requests[corev1.ResourceMemory]; req.Cmp(mem) > 0 {
			t.Errorf("%s asks for more memory (%s) than its limit (%s)", c.Name, req.String(), mem.String())
		}
	}
	if got := pod.Containers[0].Resources.Limits[corev1.ResourceMemory]; got.Cmp(resource.MustParse(buildMemoryLimit)) != 0 {
		t.Errorf("build memory limit = %s, want %s", got.String(), buildMemoryLimit)
	}
}

// A build past its memory ceiling says so, and names the ceiling.
func TestABuildOutOfMemorySaysSo(t *testing.T) {
	ctx := context.Background()
	app := dockerfileApp()
	r, c := newTestReconciler(t, app)
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "dk-build-1", Namespace: "app-dk"}}
	for _, tc := range []struct {
		init bool
		want string
	}{
		{false, "The build ran out of memory while building the image: a build may use up to " + buildMemoryLimit},
		{true, "The build ran out of memory while fetching the source: a build may use up to " + buildFetchMemoryLimit},
	} {
		oom := corev1.ContainerStatus{State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 137, Reason: "OOMKilled", Message: "killed"}}}
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "dk-build-1-oom", Namespace: "app-dk", Labels: map[string]string{shpyrdv1.LabelBuild: job.Name}}}
		if tc.init {
			oom.Name = fetchContainer
			pod.Status.InitContainerStatuses = []corev1.ContainerStatus{oom}
		} else {
			oom.Name = buildContainer
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{oom}
		}
		_ = c.Delete(ctx, pod)
		if err := c.Create(ctx, pod); err != nil {
			t.Fatal(err)
		}
		if _, msg := r.buildOutcome(ctx, job); !strings.HasPrefix(msg, tc.want) {
			t.Errorf("init=%v: message = %q, want %q", tc.init, msg, tc.want)
		}
	}
}

// A failed Dockerfile build keeps the end of its failed step's output on
// the Job (the runtime's termination message holds the last lines), so the
// log is still there to read once the instance is gone; what it printed of
// the project's secrets is masked, like the log.
func TestAFailedBuildKeepsItsOutput(t *testing.T) {
	ctx := context.Background()
	app := dockerfileApp()
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: app.BuildEnvName(), Namespace: app.Namespace}, Data: map[string][]byte{"NPM_TOKEN": []byte("s3cr3t-token")}}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "dk-build-1", Namespace: "app-dk", Labels: map[string]string{shpyrdv1.LabelApp: app.Name}},
		Status:     batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded", Message: "Job has reached the specified backoff limit"}}},
	}
	out := "#8 [5/6] RUN npm run build\n#8 1.2 npm error token s3cr3t-token rejected\n#8 ERROR: process \"/bin/sh -c npm run build\" did not complete successfully: exit code: 1\n"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "dk-build-1-x", Namespace: "app-dk", Labels: map[string]string{shpyrdv1.LabelBuild: job.Name}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name:  buildContainer,
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, Message: out}},
		}}},
	}
	r, c := newTestReconciler(t, app, secret, job, pod)
	if err := r.reconcileSupersededBuilds(ctx, app, []batchv1.Job{*job}); err != nil {
		t.Fatal(err)
	}
	got := getJob(t, c, app.Namespace, job.Name).Annotations
	if !strings.Contains(got[shpyrdv1.AnnotationBuildFailure], "while running `npm run build`") {
		t.Errorf("failure = %q", got[shpyrdv1.AnnotationBuildFailure])
	}
	if got[shpyrdv1.AnnotationBuildFailedStep] != "build" {
		t.Errorf("failed step = %q", got[shpyrdv1.AnnotationBuildFailedStep])
	}
	kept := got[shpyrdv1.AnnotationBuildOutput]
	if !strings.Contains(kept, "RUN npm run build") || !strings.Contains(kept, "exit code: 1") {
		t.Errorf("kept output = %q", kept)
	}
	if strings.Contains(kept, "s3cr3t-token") {
		t.Errorf("the kept output shows a secret: %q", kept)
	}
}

// The kept output is the end of the step's output, bounded, without colour
// codes, and never a line cut in the middle.
func TestKeptOutputIsTheEnd(t *testing.T) {
	if got := keptOutput("\x1b[31mred\x1b[0m line\n\n"); got != "red line" {
		t.Errorf("keptOutput = %q", got)
	}
	if keptOutput("   \n") != "" {
		t.Error("blank output kept")
	}
	var b strings.Builder
	for i := 0; b.Len() < 2*keptOutputBytes; i++ {
		fmt.Fprintf(&b, "line %06d of the build's output, long enough to count\n", i)
	}
	b.WriteString("the last line")
	got := keptOutput(b.String())
	if len(got) > keptOutputBytes || !strings.HasSuffix(got, "the last line") || !strings.HasPrefix(got, "line ") {
		t.Errorf("kept %d bytes, starts %q, ends %q", len(got), got[:10], got[len(got)-13:])
	}
}
