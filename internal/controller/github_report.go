package controller

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/project"
)

// The GitHub reporter (RFC-0054). For an App on a branch of a repository
// the GitHub App covers, it shows on GitHub what shpyrd does with each
// commit: a check run `shpyrd / <project>`, and a Deployment for each
// release that changes the code. It writes nothing to the App's status:
// what it sent is kept in the App's shpyrd.io/github-report annotation,
// and GitHub is written to only when what it would send differs, so a
// restart or a repeated reconcile never posts twice and a lost write is
// sent again. A failed write never touches what deploys.

const (
	// reportBackoff is the wait after a failed write; it doubles up to
	// reportMaxBackoff.
	reportBackoff    = 30 * time.Second
	reportMaxBackoff = 10 * time.Minute
	// reportLogEvery is how often one App's failures are logged.
	reportLogEvery = time.Minute
	// checkSummaryLines and checkSummaryMax cap the App's message in a
	// failed check's summary.
	checkSummaryLines = 20
	checkSummaryMax   = 4000
)

// GitHubReporter writes check runs and Deployments for the git Apps the
// GitHub App covers.
type GitHubReporter struct {
	client.Client
	GitHub *GitHubApp
	// Config gives the workspace dashboard's address, for the check's link.
	Config Config
	// Now is the clock; nil is time.Now.
	Now func() time.Time

	mu      sync.Mutex
	failing map[types.UID]reportFailure
}

// reportFailure is an App whose last writes failed: how many in a row,
// until when it waits, and when it was last logged.
type reportFailure struct {
	count  int
	until  time.Time
	logged time.Time
}

// githubReport is the github-report annotation: the check run sent for a
// commit and its state ("in_progress/deploying"), and the Deployment of a
// release and its state.
type githubReport struct {
	Commit          string `json:"commit,omitempty"`
	CheckRun        int64  `json:"checkRun,omitempty"`
	Check           string `json:"check,omitempty"`
	Release         int    `json:"release,omitempty"`
	Deployment      int64  `json:"deployment,omitempty"`
	DeploymentState string `json:"deploymentState,omitempty"`
}

// readGitHubReport reads the annotation; an unreadable one is empty.
func readGitHubReport(annotation string) githubReport {
	var r githubReport
	if annotation != "" {
		if err := json.Unmarshal([]byte(annotation), &r); err != nil {
			return githubReport{}
		}
	}
	return r
}

// SetupWithManager registers the reporter on the Apps on a GitHub branch.
func (r *GitHubReporter) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("github-report").
		For(&shpyrdv1.App{}, builder.WithPredicates(predicate.NewPredicateFuncs(func(o client.Object) bool {
			app, ok := o.(*shpyrdv1.App)
			return ok && reportsToGitHub(app)
		}))).
		WithOptions(controller.Options{MaxConcurrentReconciles: 2}).
		Complete(r)
}

// reportsToGitHub says the App follows a branch of a github.com
// repository: the reporter's Apps, before the App is asked.
func reportsToGitHub(app *shpyrdv1.App) bool { return buildsFromGitHub(app) && watchesGit(app) }

func (r *GitHubReporter) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	app := &shpyrdv1.App{}
	if err := r.Get(ctx, req.NamespacedName, app); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !app.DeletionTimestamp.IsZero() || !reportsToGitHub(app) {
		r.forget(app.UID)
		return ctrl.Result{}, nil
	}
	if wait := r.waiting(app.UID); wait > 0 {
		return ctrl.Result{RequeueAfter: wait}, nil
	}
	ref, ok, err := r.GitHub.cover(ctx, app)
	if err != nil {
		// Not known is not "not covered": wait through the backoff, and
		// keep what was sent.
		return ctrl.Result{RequeueAfter: r.failed(ctx, app, err)}, nil
	}
	if !ok {
		// Not covered, suspended or removed: nothing is reported, and
		// nothing is queued.
		r.forget(app.UID)
		return ctrl.Result{}, nil
	}
	old := readGitHubReport(app.Annotations[shpyrdv1.AnnotationGitHubReport])
	next, err := r.report(ctx, app, ref, old)
	if next != old {
		// What GitHub accepted is kept, also when a later write failed.
		if perr := r.saveReport(ctx, app, next); perr != nil {
			return ctrl.Result{}, perr
		}
	}
	if err != nil {
		return ctrl.Result{RequeueAfter: r.failed(ctx, app, err)}, nil
	}
	r.forget(app.UID)
	return ctrl.Result{}, nil
}

// report sends what changed since old, and says what GitHub now has.
func (r *GitHubReporter) report(ctx context.Context, app *shpyrdv1.App, ref githubRepoRef, old githubReport) (githubReport, error) {
	next := old
	if err := r.reportCheck(ctx, app, ref, &next); err != nil {
		return next, err
	}
	return next, nil
}

// checkWant is the check run shpyrd shows for a commit.
type checkWant struct {
	commit             string
	status, conclusion string
	label              string
	title, summary     string
}

// key is what the annotation keeps of it: "in_progress/deploying".
func (w checkWant) key() string { return w.status + "/" + w.label }

// checkDone says a kept check is completed.
func checkDone(key string) bool { return strings.HasPrefix(key, "completed/") }

// wantCheck is the check the App's state calls for, and false when it
// calls for none: nothing pinned yet; a release of config or sizes, or a
// rollback (the commit's check stays as it was); or an older release still
// running before the commit's build starts.
func wantCheck(app *shpyrdv1.App) (checkWant, bool) {
	rec := gitauth.ReadSourceRecord(app.Annotations[shpyrdv1.AnnotationGitSource])
	if rec.Ref != app.GitRevision() {
		rec = gitauth.SourceRecord{}
	}
	pin := app.PinnedCommit()
	if latest := rec.Latest; latest != "" && latest != pin {
		note := commitNote(latest, rec)
		switch latest {
		case rec.Held:
			return checkWant{commit: latest, status: "queued", label: "waiting", title: "Waiting for CI",
				summary: note + "\n\nshpyrd deploys this commit once its other checks pass, and gives up an hour after it was pushed."}, true
		case rec.NotDeployed:
			return checkWant{commit: latest, status: "completed", conclusion: "neutral", label: "not-deployed", title: rec.NotDeployedReason,
				summary: note + "\n\nNot deployed: " + rec.NotDeployedReason + "."}, true
		case rec.Skipped:
			return checkWant{commit: latest, status: "completed", conclusion: "neutral", label: "skipped", title: "Skipped: [skip deploy]",
				summary: note + "\n\nThe commit message asked not to deploy it."}, true
		}
	}
	if pin == "" {
		return checkWant{}, false
	}
	cur := app.CurrentRelease()
	onPin := releaseIs(cur, pin)
	// code: the current release is the pinned commit's code release, not a
	// release of config or sizes after it.
	code := onPin && !configRelease(app)
	w := checkWant{commit: pin, summary: commitNote(pin, rec)}
	if onPin && cur.Description != "" {
		w.summary = cur.Description
	}
	switch phase := app.Status.Phase; {
	case !code && deployedBefore(app, pin) &&
		(phase == shpyrdv1.PhaseRunning || phase == shpyrdv1.PhaseFailed || phase == shpyrdv1.PhaseDeploying):
		// A release of config or sizes, or a rollback, rolls out, runs or
		// fails: the commit's check stays as it was.
		return checkWant{}, false
	case phase == shpyrdv1.PhaseFailed:
		w.status, w.conclusion, w.label, w.title = "completed", "failure", "failure", "Failed"
		if msg := lastLines(app.Status.Message, checkSummaryLines, checkSummaryMax); msg != "" {
			w.summary += "\n\n```\n" + msg + "\n```"
		}
	case phase == shpyrdv1.PhaseRunning && code:
		w.status, w.conclusion, w.label, w.title = "completed", "success", "success", "Running"
		if app.Status.URL != "" {
			w.title = "Running at " + app.Status.URL
		}
	case phase == shpyrdv1.PhaseRunning:
		// Still running an older release: the commit is not built yet, and
		// its check waits for the build.
		return checkWant{}, false
	case phase == shpyrdv1.PhaseDeploying && code:
		w.status, w.label, w.title = "in_progress", "deploying", "Deploying"
	default:
		w.status, w.label, w.title = "in_progress", "building", "Building"
	}
	return w, true
}

// commitNote names a commit as its release would: "Push a1b2c3d by
// alice: fix login" when the record knows it.
func commitNote(sha string, rec gitauth.SourceRecord) string {
	if rec.Latest == sha {
		return pushNote(sha, rec.Author, rec.Message)
	}
	return "Commit " + shortCommit(sha)
}

// releaseIs says the release was built from sha (Release.Source keeps 12
// characters of it).
func releaseIs(rel *shpyrdv1.Release, sha string) bool {
	return rel != nil && len(rel.Source) >= 7 && strings.HasPrefix(sha, rel.Source)
}

// deployedBefore says a release of the history ran sha.
func deployedBefore(app *shpyrdv1.App, sha string) bool {
	for i := range app.Status.Releases {
		if releaseIs(&app.Status.Releases[i], sha) {
			return true
		}
	}
	return false
}

// configRelease says the current release changed no code: its source is
// the one before it.
func configRelease(app *shpyrdv1.App) bool {
	rels := app.Status.Releases
	n := len(rels)
	return n >= 2 && rels[n-1].Source == rels[n-2].Source
}

// lastLines is the end of a message: at most n lines and max bytes.
func lastLines(msg string, n, maxBytes int) string {
	lines := strings.Split(strings.TrimSpace(msg), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	out := strings.Join(lines, "\n")
	if r := []rune(out); len(out) > maxBytes {
		for len(string(r)) > maxBytes {
			r = r[1:]
		}
		out = string(r)
	}
	return out
}

// reportCheck brings the commit's check run to what the App's state calls
// for: a new run for a new commit (an unfinished one for the commit before
// is marked replaced), or the same run updated. A completed run is never
// taken back: a new state that is not completed is a new run. A run
// deleted on GitHub (404) is made again.
func (r *GitHubReporter) reportCheck(ctx context.Context, app *shpyrdv1.App, ref githubRepoRef, rep *githubReport) error {
	want, ok := wantCheck(app)
	if !ok || (want.commit == rep.Commit && want.key() == rep.Check) {
		return nil
	}
	now := r.now()
	cl := ref.client
	if rep.Commit != "" && rep.Commit != want.commit && rep.CheckRun != 0 && !checkDone(rep.Check) {
		replaced := checkWant{commit: rep.Commit, status: "completed", conclusion: "neutral", label: "replaced",
			title: "Replaced by " + shortCommit(want.commit), summary: "shpyrd moved on to " + shortCommit(want.commit) + " before this commit was deployed."}
		if err := cl.UpdateCheckRun(ctx, ref.installation, ref.owner, ref.name, rep.CheckRun, r.checkRun(app, replaced, now)); err != nil && !github.IsNotFound(err) {
			return err
		}
		rep.Check = replaced.key()
	}
	w := r.checkRun(app, want, now)
	if rep.Commit == want.commit && rep.CheckRun != 0 && (!checkDone(rep.Check) || checkDone(want.key())) {
		err := cl.UpdateCheckRun(ctx, ref.installation, ref.owner, ref.name, rep.CheckRun, w)
		if err == nil {
			rep.Check = want.key()
			return nil
		}
		if !github.IsNotFound(err) {
			return err
		}
	}
	id, err := cl.CreateCheckRun(ctx, ref.installation, ref.owner, ref.name, w)
	if err != nil {
		return err
	}
	rep.Commit, rep.CheckRun, rep.Check = want.commit, id, want.key()
	return nil
}

// checkRun is what GitHub is sent for a check.
func (r *GitHubReporter) checkRun(app *shpyrdv1.App, w checkWant, now time.Time) github.CheckRunWrite {
	out := github.CheckRunWrite{Name: "shpyrd / " + project.SlugOf(app), HeadSHA: w.commit, Status: w.status, Conclusion: w.conclusion,
		DetailsURL: r.projectPage(app), ExternalID: string(app.UID), Title: w.title, Summary: w.summary}
	switch w.status {
	case "in_progress":
		out.StartedAt = now
	case "completed":
		out.CompletedAt = now
	}
	return out
}

// projectPage is the project's page on its workspace's dashboard.
func (r *GitHubReporter) projectPage(app *shpyrdv1.App) string {
	return strings.TrimSuffix(r.Config.issuer(app), "/") + "/projects/" + project.SlugOf(app)
}

// saveReport writes the annotation alone, in a merge patch.
func (r *GitHubReporter) saveReport(ctx context.Context, app *shpyrdv1.App, rep githubReport) error {
	raw, err := json.Marshal(rep)
	if err != nil {
		return err
	}
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{shpyrdv1.AnnotationGitHubReport: string(raw)}}})
	if err != nil {
		return err
	}
	return r.Patch(ctx, app, client.RawPatch(types.MergePatchType, patch))
}

// failed counts a failed write and says how long to wait: 30 s, doubling
// up to 10 minutes. It logs at most once a minute per App.
func (r *GitHubReporter) failed(ctx context.Context, app *shpyrdv1.App, err error) time.Duration {
	now := r.now()
	r.mu.Lock()
	if r.failing == nil {
		r.failing = map[types.UID]reportFailure{}
	}
	f := r.failing[app.UID]
	f.count++
	wait := min(reportBackoff<<min(f.count-1, 10), reportMaxBackoff)
	f.until = now.Add(wait)
	logIt := f.logged.IsZero() || now.Sub(f.logged) >= reportLogEvery
	if logIt {
		f.logged = now
	}
	r.failing[app.UID] = f
	r.mu.Unlock()
	if logIt {
		log.FromContext(ctx).Info("could not report to GitHub; the deploy goes on, the report is sent later",
			"app", app.Namespace+"/"+app.Name, "retry", wait.String(), "err", err.Error())
	}
	return wait
}

// waiting is how long a failing App still waits; 0 when it does not.
func (r *GitHubReporter) waiting(uid types.UID) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.failing[uid]
	if !ok {
		return 0
	}
	return max(f.until.Sub(r.now()), 0)
}

func (r *GitHubReporter) forget(uid types.UID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.failing, uid)
}

func (r *GitHubReporter) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}
