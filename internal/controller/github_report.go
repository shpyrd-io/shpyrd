package controller

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	// sent is, by App, the report last accepted by GitHub: newer than the
	// annotation in a stale cache, or after a patch of it failed.
	sent map[types.UID]githubReport
}

// reportFailure is an App whose last writes failed: how many in a row,
// until when it waits, and when it was last logged.
type reportFailure struct {
	count  int
	until  time.Time
	logged time.Time
}

// githubReport is the github-report annotation. It keeps two checks: the
// pinned commit's (Commit) and the newest commit not pinned, one held for
// its CI, not deployed or skipped (Latest); each its run and its state
// ("in_progress/deploying"). Then the Deployment of a release and its
// state. Seq counts the changes, so the newer of two copies is known.
type githubReport struct {
	Seq             int64  `json:"seq,omitempty"`
	Commit          string `json:"commit,omitempty"`
	CheckRun        int64  `json:"checkRun,omitempty"`
	Check           string `json:"check,omitempty"`
	Latest          string `json:"latest,omitempty"`
	LatestRun       int64  `json:"latestRun,omitempty"`
	LatestCheck     string `json:"latestCheck,omitempty"`
	Release         int    `json:"release,omitempty"`
	Deployment      int64  `json:"deployment,omitempty"`
	DeploymentState string `json:"deploymentState,omitempty"`
}

// checkSlot is one of the report's two checks.
type checkSlot struct {
	commit *string
	run    *int64
	check  *string
}

func (s checkSlot) clear() { *s.commit, *s.run, *s.check = "", 0, "" }

// unfinished says the slot holds a run that is not completed.
func (s checkSlot) unfinished() bool { return *s.run != 0 && !checkDone(*s.check) }

func (rep *githubReport) pinSlot() checkSlot {
	return checkSlot{&rep.Commit, &rep.CheckRun, &rep.Check}
}

func (rep *githubReport) latestSlot() checkSlot {
	return checkSlot{&rep.Latest, &rep.LatestRun, &rep.LatestCheck}
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
	// cover's own log line would come every retry: failed() logs once a
	// minute instead.
	ref, ok, err := r.GitHub.coverQuietly(ctx, app)
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
	saved := readGitHubReport(app.Annotations[shpyrdv1.AnnotationGitHubReport])
	old := r.lastSent(app.UID, saved)
	next, err := r.report(ctx, app, ref, old)
	if next != old {
		// What GitHub accepted is kept, also when a later write failed:
		// in memory first, so a failed patch or a stale cache never
		// posts it again.
		next.Seq = old.Seq + 1
		r.remember(app.UID, next)
	}
	if next != saved {
		if perr := r.saveReport(ctx, app, next); perr != nil {
			return ctrl.Result{}, perr
		}
	}
	if err != nil {
		return ctrl.Result{RequeueAfter: r.failed(ctx, app, err)}, nil
	}
	r.recovered(app.UID)
	return ctrl.Result{}, nil
}

// report sends what changed since old, and says what GitHub now has.
func (r *GitHubReporter) report(ctx context.Context, app *shpyrdv1.App, ref githubRepoRef, old githubReport) (githubReport, error) {
	next := old
	if err := r.reportChecks(ctx, app, ref, &next); err != nil {
		return next, err
	}
	if err := r.reportDeployment(ctx, app, ref, &next); err != nil {
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

// deployFailedSummary is a failed check's summary when the App's message
// is not a build's, which is in words and masked: any other message may
// carry what must not reach GitHub.
const deployFailedSummary = "The deploy failed: see the project on shpyrd."

// sourceRecordOf is the watcher's record of the branch the App follows.
func sourceRecordOf(app *shpyrdv1.App) gitauth.SourceRecord {
	rec := gitauth.ReadSourceRecord(app.Annotations[shpyrdv1.AnnotationGitSource])
	if rec.Ref != app.GitRevision() {
		return gitauth.SourceRecord{}
	}
	return rec
}

// wantLatest is the check of the newest commit on the branch when it is
// not the pinned one: held for its CI, not deployed, or skipped. False
// when there is none.
func wantLatest(app *shpyrdv1.App) (checkWant, bool) {
	rec := sourceRecordOf(app)
	latest := rec.Latest
	if latest == "" || latest == app.PinnedCommit() {
		return checkWant{}, false
	}
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
	return checkWant{}, false
}

// wantCheck is the pinned commit's check, given what rep says was sent
// for it, and false when it calls for none. A finished check is never
// reopened. A release of config or sizes, or a rollback, never changes a
// finished check and never fails one; while the pinned commit's check is
// not finished and its code runs, it succeeds. A commit not yet running
// is Building at once, even while an older release runs.
func wantCheck(app *shpyrdv1.App, rep githubReport) (checkWant, bool) {
	pin := app.PinnedCommit()
	if pin == "" {
		return checkWant{}, false
	}
	done := rep.Commit == pin && rep.CheckRun != 0 && checkDone(rep.Check)
	cur := app.CurrentRelease()
	onPin := releaseIs(cur, pin)
	w := checkWant{commit: pin, summary: commitNote(pin, sourceRecordOf(app))}
	if onPin && cur.Description != "" {
		w.summary = cur.Description
	}
	phase := app.Status.Phase
	success := func() (checkWant, bool) {
		w.status, w.conclusion, w.label, w.title = "completed", "success", "success", "Running"
		if app.Status.URL != "" {
			w.title = "Running at " + app.Status.URL
		}
		return w, true
	}
	building := func() (checkWant, bool) {
		if done {
			return checkWant{}, false
		}
		w.status, w.label, w.title = "in_progress", "building", "Building"
		return w, true
	}
	switch {
	case onPin && configRelease(app):
		// A release of config or sizes: the commit's code runs on.
		if done || (phase != shpyrdv1.PhaseRunning && phase != shpyrdv1.PhaseFailed) {
			return checkWant{}, false
		}
		return success()
	case !onPin && deployedBefore(app, pin), rolledBack(app):
		// A rollback runs older code: the commit's check stays, and a
		// rollback that fails never fails it, also when the commit never
		// ran.
		return checkWant{}, false
	case phase == shpyrdv1.PhaseFailed:
		w.status, w.conclusion, w.label, w.title = "completed", "failure", "failure", "Failed"
		if msg := lastLines(app.Status.Message, checkSummaryLines, checkSummaryMax); msg != "" && failedInBuild(app) {
			w.summary += "\n\n```\n" + msg + "\n```"
		} else {
			w.summary += "\n\n" + deployFailedSummary
		}
		return w, true
	case onPin && phase == shpyrdv1.PhaseRunning:
		return success()
	case onPin && phase == shpyrdv1.PhaseDeploying:
		if done {
			return checkWant{}, false
		}
		w.status, w.label, w.title = "in_progress", "deploying", "Deploying"
		return w, true
	default:
		return building()
	}
}

// rolledBack says the App rolls out, or runs, a release it was rolled
// back to: the rollback is asked for, or its image is pinned to the image
// of an earlier release. No build of the pinned commit is under way then.
func rolledBack(app *shpyrdv1.App) bool {
	if app.Annotations[shpyrdv1.AnnotationRollbackTo] != "" {
		return true
	}
	if !app.HasSource() || app.Spec.Image == "" {
		return false
	}
	rels := app.Status.Releases
	for i := range rels {
		// The newest release is the pinned image's own; an earlier one
		// with it is what was rolled back to.
		if rels[i].Image == app.Spec.Image && i < len(rels)-1 {
			return true
		}
	}
	return false
}

// deployedByImage says a git App runs an image given to it, not one
// rolled back to: no commit of the repository is deployed.
func deployedByImage(app *shpyrdv1.App) bool {
	return app.HasSource() && app.Spec.Image != "" && !rolledBack(app)
}

// failedInBuild says the App failed in its build: its message is then the
// build's failure in words, already masked (build_failure.go).
func failedInBuild(app *shpyrdv1.App) bool {
	c := meta.FindStatusCondition(app.Status.Conditions, shpyrdv1.ConditionReady)
	return c != nil && c.Status == metav1.ConditionFalse && c.Reason == "BuildFailed"
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

// lastLines is the end of a message: at most n lines and max bytes, cut
// at a character's start.
func lastLines(msg string, n, maxBytes int) string {
	lines := strings.Split(strings.TrimSpace(msg), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	out := strings.Join(lines, "\n")
	if len(out) > maxBytes {
		cut := len(out) - maxBytes
		for cut < len(out) && !utf8.RuneStart(out[cut]) {
			cut++
		}
		out = out[cut:]
	}
	return out
}

// reportChecks brings the two checks to what the App's state calls for.
// When the pin moves on, the old pin's unfinished check is marked
// replaced, and the newest commit's unfinished check, when that commit is
// the new pin, goes on as the pin's. A non-pinned commit's unfinished
// check shpyrd no longer waits for is always closed: superseded when a
// newer commit overtook it, else with why.
func (r *GitHubReporter) reportChecks(ctx context.Context, app *shpyrdv1.App, ref githubRepoRef, rep *githubReport) error {
	pinned, latest := rep.pinSlot(), rep.latestSlot()
	if pin := app.PinnedCommit(); pin != "" && rep.Commit != pin {
		if pinned.unfinished() {
			replaced := checkWant{commit: rep.Commit, status: "completed", conclusion: "neutral", label: "replaced",
				title: "Replaced by " + shortCommit(pin), summary: "shpyrd moved on to " + shortCommit(pin) + " before this commit was deployed."}
			if err := r.close(ctx, app, ref, pinned, replaced); err != nil {
				return err
			}
		}
		pinned.clear()
		if rep.Latest == pin {
			if latest.unfinished() {
				rep.Commit, rep.CheckRun, rep.Check = rep.Latest, rep.LatestRun, rep.LatestCheck
			}
			latest.clear()
		}
	}
	lw, lok := wantLatest(app)
	if rep.Latest != "" && (!lok || rep.Latest != lw.commit) {
		if latest.unfinished() {
			if err := r.close(ctx, app, ref, latest, stoppedWaiting(app, rep.Latest)); err != nil {
				return err
			}
		}
		latest.clear()
	}
	if w, ok := wantCheck(app, *rep); ok {
		if err := r.sendCheck(ctx, app, ref, pinned, w); err != nil {
			return err
		}
	}
	if lok {
		if err := r.sendCheck(ctx, app, ref, latest, lw); err != nil {
			return err
		}
	}
	return nil
}

// stoppedWaiting is how a held commit's check ends when shpyrd no longer
// waits for it: superseded when a newer commit overtook it on the branch,
// else neutral with why.
func stoppedWaiting(app *shpyrdv1.App, sha string) checkWant {
	w := checkWant{commit: sha, status: "completed", conclusion: "neutral", label: "superseded"}
	raw := gitauth.ReadSourceRecord(app.Annotations[shpyrdv1.AnnotationGitSource])
	if newer := sourceRecordOf(app).Latest; newer != "" && newer != sha {
		w.title, w.summary = "Superseded by "+shortCommit(newer), "A newer commit, "+shortCommit(newer)+", came before this one was deployed."
		return w
	}
	w.label = "stopped"
	why := ""
	switch {
	case raw.Ref != "" && raw.Ref != app.GitRevision():
		why = "the project follows " + app.GitRevision() + " now"
	case !app.AutoDeploys():
		why = "auto-deploy is off"
	case app.Spec.Source != nil && app.Spec.Source.Git != nil && !app.Spec.Source.Git.WaitForChecks:
		why = "wait for CI is off"
	default:
		w.title, w.summary = "No longer waiting for CI", "shpyrd stopped waiting for this commit's CI before it was deployed."
		return w
	}
	w.title, w.summary = "No longer waiting: "+why, "shpyrd stopped waiting for this commit's CI before it was deployed: "+why+"."
	return w
}

// close completes the slot's unfinished run as w says; a run deleted on
// GitHub needs nothing.
func (r *GitHubReporter) close(ctx context.Context, app *shpyrdv1.App, ref githubRepoRef, slot checkSlot, w checkWant) error {
	err := ref.client.UpdateCheckRun(ctx, ref.installation, ref.owner, ref.name, *slot.run, r.checkRun(app, w, r.now()))
	if err != nil && !github.IsNotFound(err) {
		return err
	}
	*slot.check = w.key()
	return nil
}

// sendCheck brings the slot's run to w: the same run updated, else one
// shpyrd already made on the commit (found by name, the App's UID and
// shpyrd's GitHub App),
// else a new one. A completed run is never taken back: a new state that is
// not completed is a new run. A run deleted on GitHub (404) is made again.
func (r *GitHubReporter) sendCheck(ctx context.Context, app *shpyrdv1.App, ref githubRepoRef, slot checkSlot, want checkWant) error {
	if *slot.commit == want.commit && *slot.check == want.key() && *slot.run != 0 {
		return nil
	}
	cl, now := ref.client, r.now()
	w := r.checkRun(app, want, now)
	reopens := func(check string) bool { return checkDone(check) && !checkDone(want.key()) }
	if *slot.commit == want.commit && *slot.run != 0 && !reopens(*slot.check) {
		err := cl.UpdateCheckRun(ctx, ref.installation, ref.owner, ref.name, *slot.run, w)
		if err == nil {
			*slot.check = want.key()
			return nil
		}
		if !github.IsNotFound(err) {
			return err
		}
	}
	// A run made before the annotation said so (a patch that failed, a
	// restart) is found, not made twice.
	runs, err := cl.ReportCheckRuns(ctx, ref.installation, ref.owner, ref.name, want.commit, w.Name)
	if err != nil {
		return err
	}
	var own *github.CheckRun
	appID := cl.App().ID
	for i := range runs {
		if runs[i].AppID == appID && runs[i].ExternalID == w.ExternalID && runs[i].ID != *slot.run && (own == nil || runs[i].ID > own.ID) {
			own = &runs[i]
		}
	}
	if own != nil && !(own.Status == "completed" && !checkDone(want.key())) {
		err := cl.UpdateCheckRun(ctx, ref.installation, ref.owner, ref.name, own.ID, w)
		if err == nil {
			*slot.commit, *slot.run, *slot.check = want.commit, own.ID, want.key()
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
	*slot.commit, *slot.run, *slot.check = want.commit, id, want.key()
	return nil
}

// commitSource is a release's source when it is a commit (Release.Source
// keeps 12 characters of it).
var commitSource = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// deployWant is the Deployment of the newest release that changed the
// code: the release, the commit, and the state of its rollout ("" says
// nothing new: a build of the next commit, or a build that failed). config
// says a release of config or sizes rolls out on top of it: it never
// changes a finished state.
type deployWant struct {
	release     int
	ref         string
	description string
	state       string
	config      bool
}

// wantDeployment is the Deployment the App's history calls for; false
// when its newest code is not a commit, or the App runs an image given to
// it. A rollback is a release of other code, so it has its own; a release
// of config or sizes has none. A Deployment's states are only ever its
// own release's: while a newer release is still to come (a rollback asked
// for, a build that failed, the pinned commit not yet released), nothing
// new is said.
func wantDeployment(app *shpyrdv1.App) (deployWant, bool) {
	if deployedByImage(app) {
		return deployWant{}, false
	}
	rels := app.Status.Releases
	for i := len(rels) - 1; i >= 0; i-- {
		if i > 0 && rels[i].Source == rels[i-1].Source {
			continue // the same code: config vars, sizes, a restart
		}
		if !commitSource.MatchString(rels[i].Source) {
			return deployWant{}, false
		}
		w := deployWant{release: rels[i].Number, ref: fullCommitOf(app, rels[i].Source), description: firstLine(rels[i].Description), config: i < len(rels)-1}
		if newerToCome(app, rels, i) {
			return w, true
		}
		switch app.Status.Phase {
		case shpyrdv1.PhaseDeploying:
			if !w.config {
				w.state = "in_progress"
			}
		case shpyrdv1.PhaseRunning:
			w.state = "success"
		case shpyrdv1.PhaseFailed:
			// A release of config or sizes that fails leaves the code
			// running: it finishes an unfinished Deployment as a success.
			w.state = "failure"
			if w.config {
				w.state = "success"
			}
		}
		return w, true
	}
	return deployWant{}, false
}

// newerToCome says a release newer than rels[i] is under way and has no
// Deployment yet: a rollback asked for, a build that failed, or a pinned
// commit not yet released (rels[i] neither runs it nor is a rollback to
// older code).
func newerToCome(app *shpyrdv1.App, rels []shpyrdv1.Release, i int) bool {
	if app.Annotations[shpyrdv1.AnnotationRollbackTo] != "" || meta.IsStatusConditionFalse(app.Status.Conditions, shpyrdv1.ConditionBuilt) {
		return true
	}
	pin := app.PinnedCommit()
	if pin == "" || releaseIs(&rels[i], pin) {
		return false
	}
	for j := range i {
		if rels[j].Source == rels[i].Source {
			return false // a rollback to older code
		}
	}
	return true
}

// fullCommitOf is the full SHA a release's 12 characters name, when the
// pin or the watcher's record knows it; else the 12 characters, which
// GitHub resolves.
func fullCommitOf(app *shpyrdv1.App, src string) string {
	rec := gitauth.ReadSourceRecord(app.Annotations[shpyrdv1.AnnotationGitSource])
	for _, sha := range []string{app.PinnedCommit(), rec.Latest, rec.Held, rec.NotDeployed, rec.Skipped} {
		if sha != "" && strings.HasPrefix(sha, src) {
			return sha
		}
	}
	return src
}

// deploymentDone says a Deployment state is final; a config release
// rolling out after it does not take it back to in progress.
func deploymentDone(state string) bool { return state == "success" || state == "failure" }

// settled says the Deployment's state have needs no new status: it is the
// one wanted, or final while the rollout is only starting or is a release
// of config or sizes.
func (w deployWant) settled(have string) bool {
	return w.state == have || (deploymentDone(have) && (w.state == "in_progress" || w.config))
}

// reportDeployment makes the newest code release's Deployment, once, and
// moves its state on. One deleted on GitHub (404) is made again. rep
// changes only by what GitHub accepted.
func (r *GitHubReporter) reportDeployment(ctx context.Context, app *shpyrdv1.App, ref githubRepoRef, rep *githubReport) error {
	want, ok := wantDeployment(app)
	if !ok {
		return nil
	}
	if want.release != rep.Release || rep.Deployment == 0 {
		if err := r.newDeployment(ctx, app, ref, want, rep); err != nil {
			return err
		}
	}
	if want.state == "" || want.settled(rep.DeploymentState) {
		return nil
	}
	cl := ref.client
	err := cl.CreateDeploymentStatus(ctx, ref.installation, ref.owner, ref.name, rep.Deployment, r.deploymentStatus(app, want.state))
	if github.IsNotFound(err) {
		if err = r.newDeployment(ctx, app, ref, want, rep); err == nil {
			if want.settled(rep.DeploymentState) {
				return nil // found, already there
			}
			err = cl.CreateDeploymentStatus(ctx, ref.installation, ref.owner, ref.name, rep.Deployment, r.deploymentStatus(app, want.state))
		}
	}
	if err != nil {
		return err
	}
	rep.DeploymentState = want.state
	return nil
}

// newDeployment gives the release its Deployment: one shpyrd already made
// for it (a patch of the annotation that failed, a restart), found by the
// App's UID, the release and shpyrd's GitHub App, with its state; else a
// new one, environment the project, shpyrd having decided to deploy (no
// merge, no required contexts).
func (r *GitHubReporter) newDeployment(ctx context.Context, app *shpyrdv1.App, ref githubRepoRef, want deployWant, rep *githubReport) error {
	cl, env := ref.client, project.SlugOf(app)
	found, err := cl.ReportDeployments(ctx, ref.installation, ref.owner, ref.name, env)
	if err != nil {
		return err
	}
	appID := cl.App().ID
	for _, d := range found {
		if d.AppID != appID || d.App != string(app.UID) || d.Release != want.release || d.ID == rep.Deployment {
			continue
		}
		state, err := cl.DeploymentState(ctx, ref.installation, ref.owner, ref.name, d.ID)
		if github.IsNotFound(err) {
			continue
		}
		if err != nil {
			return err
		}
		rep.Release, rep.Deployment, rep.DeploymentState = want.release, d.ID, state
		return nil
	}
	id, err := cl.CreateDeployment(ctx, ref.installation, ref.owner, ref.name,
		github.DeploymentWrite{Ref: want.ref, Environment: env, Description: want.description, App: string(app.UID), Release: want.release})
	if err != nil {
		return err
	}
	rep.Release, rep.Deployment, rep.DeploymentState = want.release, id, ""
	return nil
}

// deploymentStatus is a Deployment's state, with the app's URL once it
// runs and the project's page for its log.
func (r *GitHubReporter) deploymentStatus(app *shpyrdv1.App, state string) github.DeploymentStatusWrite {
	s := github.DeploymentStatusWrite{State: state, LogURL: r.projectPage(app)}
	if state == "success" {
		s.EnvironmentURL = app.Status.URL
	}
	return s
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

// forget drops all the reporter keeps of an App it no longer reports.
func (r *GitHubReporter) forget(uid types.UID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.failing, uid)
	delete(r.sent, uid)
}

// recovered ends an App's backoff: its writes went through.
func (r *GitHubReporter) recovered(uid types.UID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.failing, uid)
}

// remember keeps the report GitHub accepted for the App.
func (r *GitHubReporter) remember(uid types.UID, rep githubReport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sent == nil {
		r.sent = map[types.UID]githubReport{}
	}
	r.sent[uid] = rep
}

// lastSent is the newer of the annotation and what was last accepted.
func (r *GitHubReporter) lastSent(uid types.UID, saved githubReport) githubReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	if mem, ok := r.sent[uid]; ok && mem.Seq > saved.Seq {
		return mem
	}
	return saved
}

func (r *GitHubReporter) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}
