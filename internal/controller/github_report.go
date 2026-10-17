package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
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
	"sigs.k8s.io/controller-runtime/pkg/event"
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
	Seq         int64  `json:"seq,omitempty"`
	Commit      string `json:"commit,omitempty"`
	CheckRun    int64  `json:"checkRun,omitempty"`
	Check       string `json:"check,omitempty"`
	Latest      string `json:"latest,omitempty"`
	LatestRun   int64  `json:"latestRun,omitempty"`
	LatestCheck string `json:"latestCheck,omitempty"`
	// LatestRef is the revision Latest was held on.
	LatestRef       string `json:"latestRef,omitempty"`
	Release         int    `json:"release,omitempty"`
	Deployment      int64  `json:"deployment,omitempty"`
	DeploymentState string `json:"deploymentState,omitempty"`
}

// checkSlot is one of the report's two checks; ref, the latest's only, is
// the revision its commit was seen on.
type checkSlot struct {
	commit *string
	run    *int64
	check  *string
	ref    *string
}

func (s checkSlot) clear() { s.set("", 0, "", "") }

// set keeps a run GitHub accepted.
func (s checkSlot) set(commit string, run int64, check, ref string) {
	*s.commit, *s.run, *s.check = commit, run, check
	if s.ref != nil {
		*s.ref = ref
	}
}

// unfinished says the slot holds a run that is not completed.
func (s checkSlot) unfinished() bool { return *s.run != 0 && !checkDone(*s.check) }

func (rep *githubReport) pinSlot() checkSlot {
	return checkSlot{&rep.Commit, &rep.CheckRun, &rep.Check, nil}
}

func (rep *githubReport) latestSlot() checkSlot {
	return checkSlot{&rep.Latest, &rep.LatestRun, &rep.LatestCheck, &rep.LatestRef}
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
		For(&shpyrdv1.App{}, builder.WithPredicates(githubReportPredicate())).
		WithOptions(controller.Options{MaxConcurrentReconciles: 2}).
		Complete(r)
}

// githubReportPredicate wakes the reporter for the Apps it watches, and on
// an update only for what it reports on: its report, the pin, the
// watcher's record, a rollback, the spec, a deletion, the phase, the
// releases, and the Built and Ready conditions. An App's other writes
// (instances, messages) do not.
func githubReportPredicate() predicate.Funcs {
	watched := predicate.NewPredicateFuncs(func(o client.Object) bool {
		app, ok := o.(*shpyrdv1.App)
		return ok && githubReportWatched(app)
	})
	watched.UpdateFunc = func(e event.UpdateEvent) bool {
		old, okOld := e.ObjectOld.(*shpyrdv1.App)
		app, ok := e.ObjectNew.(*shpyrdv1.App)
		return ok && okOld && githubReportWatched(app) && reportChanged(old, app)
	}
	return watched
}

// reportedAnnotations are the annotations the reporter reads.
var reportedAnnotations = []string{shpyrdv1.AnnotationGitHubReport, shpyrdv1.AnnotationGitCommit, shpyrdv1.AnnotationGitCommitRef,
	shpyrdv1.AnnotationGitSource, shpyrdv1.AnnotationRollbackTo}

// reportChanged says an update changes what the reporter reports.
func reportChanged(old, app *shpyrdv1.App) bool {
	if old.Generation != app.Generation || !old.DeletionTimestamp.Equal(app.DeletionTimestamp) ||
		old.Status.Phase != app.Status.Phase || !reflect.DeepEqual(old.Status.Releases, app.Status.Releases) {
		return true
	}
	for _, k := range reportedAnnotations {
		if old.Annotations[k] != app.Annotations[k] {
			return true
		}
	}
	for _, t := range []string{shpyrdv1.ConditionBuilt, shpyrdv1.ConditionReady} {
		a, b := meta.FindStatusCondition(old.Status.Conditions, t), meta.FindStatusCondition(app.Status.Conditions, t)
		if (a == nil) != (b == nil) || (a != nil && (a.Status != b.Status || a.Reason != b.Reason || !a.LastTransitionTime.Equal(&b.LastTransitionTime))) {
			return true
		}
	}
	return false
}

// githubReportWatched says the reporter reconciles the App: one it reports
// on, or one with a report left from when it did, to close it.
func githubReportWatched(app *shpyrdv1.App) bool {
	_, reported := app.Annotations[shpyrdv1.AnnotationGitHubReport]
	return reportsToGitHub(app) || reported
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
		return r.stopReporting(ctx, app)
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
		// nothing is queued. While the reporter still holds a token for the
		// repository, the unfinished reports are closed as when reporting
		// stops; without one, they are left.
		if ref.client != nil && r.unfinished(app) {
			if _, held := ref.client.CachedInstallationToken(ref.installation, ref.name, github.ReportWrite()); held {
				return r.endReports(ctx, app, ref)
			}
		}
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

// stoppedReporting is the words of a report closed because shpyrd no
// longer reports on the project.
const stoppedReporting = "shpyrd stopped reporting on this project"

// stopReporting ends the reports on an App shpyrd no longer reports on
// (deleted, or no longer on a branch of github.com): endReports, when a
// token can still be had.
func (r *GitHubReporter) stopReporting(ctx context.Context, app *shpyrdv1.App) (ctrl.Result, error) {
	var ref githubRepoRef
	if r.unfinished(app) {
		if got, ok, err := r.GitHub.coverQuietly(ctx, app); err == nil && ok {
			ref = got
		}
	}
	return r.endReports(ctx, app, ref)
}

// unfinished says the App's report holds an unfinished check run or
// Deployment.
func (r *GitHubReporter) unfinished(app *shpyrdv1.App) bool {
	rep := r.lastSent(app.UID, readGitHubReport(app.Annotations[shpyrdv1.AnnotationGitHubReport]))
	return (rep.Deployment != 0 && !deploymentDone(rep.DeploymentState)) || rep.pinSlot().unfinished() || rep.latestSlot().unfinished()
}

// endReports closes the App's reports, through ref when it has a client:
// unfinished check runs neutral and a Deployment still in progress
// inactive, once, best effort. Then the annotation is removed (on a
// terminating App too, so its finalization never closes them again) and
// what the reporter kept of the App is dropped; while the removal fails,
// memory keeps what was closed.
func (r *GitHubReporter) endReports(ctx context.Context, app *shpyrdv1.App, ref githubRepoRef) (ctrl.Result, error) {
	saved := readGitHubReport(app.Annotations[shpyrdv1.AnnotationGitHubReport])
	old := r.lastSent(app.UID, saved)
	rep := old
	pinned, latest := rep.pinSlot(), rep.latestSlot()
	if ref.client != nil {
		// Best effort: a refused close is dropped with the rest.
		for _, slot := range []checkSlot{pinned, latest} {
			if slot.unfinished() {
				_ = r.close(ctx, app, ref, slot, checkWant{commit: *slot.commit, status: "completed", conclusion: "neutral",
					label: "stopped", title: stoppedReporting, summary: stoppedReporting + "."})
			}
		}
		_ = r.retire(ctx, app, ref, &rep, stoppedReporting)
	}
	if _, ok := app.Annotations[shpyrdv1.AnnotationGitHubReport]; ok {
		patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]any{shpyrdv1.AnnotationGitHubReport: nil}}})
		if err == nil {
			err = r.Patch(ctx, app, client.RawPatch(types.MergePatchType, patch))
		}
		if err != nil {
			if rep != old {
				rep.Seq = old.Seq + 1
				r.remember(app.UID, rep)
			}
			return ctrl.Result{}, err
		}
	}
	r.forget(app.UID)
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

// checkWant is the check run shpyrd shows for a commit; ref is the
// revision a commit not pinned was seen on.
type checkWant struct {
	commit             string
	ref                string
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
	w := checkWant{commit: latest, ref: rec.Ref}
	switch latest {
	case rec.Held:
		w.status, w.label, w.title = "queued", "waiting", "Waiting for CI"
		w.summary = note + "\n\nshpyrd deploys this commit once its other checks pass, and gives up an hour after it was pushed."
		return w, true
	case rec.NotDeployed:
		w.status, w.conclusion, w.label, w.title = "completed", "neutral", "not-deployed", rec.NotDeployedReason
		w.summary = note + "\n\nNot deployed: " + rec.NotDeployedReason + "."
		return w, true
	case rec.Skipped:
		w.status, w.conclusion, w.label, w.title = "completed", "neutral", "skipped", "Skipped: [skip deploy]"
		w.summary = note + "\n\nThe commit message asked not to deploy it."
		return w, true
	}
	return checkWant{}, false
}

// wantCheck is the pinned commit's check, given what rep says was sent
// for it, and false when it calls for none. A finished check is never
// reopened, and never failed. A release of config or sizes, or a
// rollback, never changes a finished check and never fails one; a
// rollback or a deploy by image ends an unfinished one neutral; while the
// pinned commit's check is not finished and its code runs, it succeeds. A
// commit not yet running is Building at once, even while an older release
// runs or fails: a failure is the pin's only when its own release failed,
// or its build did (pinFailed).
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
	case rolledBack(app), deployedByImage(app):
		// A rollback runs older code, a deploy by image no commit: the
		// commit's finished check stays, and a rollback that fails never
		// fails it, also when the commit never ran. Its unfinished check
		// ends: the commit is not deployed.
		if rep.Commit != pin || rep.CheckRun == 0 || done {
			return checkWant{}, false
		}
		why := "Not deployed: deployed from an image"
		if rolledBack(app) {
			why = "Not deployed: rolled back"
			if n := rollbackTarget(app); n > 0 {
				why += fmt.Sprintf(" to release %d", n)
			}
		}
		w.status, w.conclusion, w.label, w.title = "completed", "neutral", "not-deployed", why
		w.summary += "\n\n" + why + "."
		return w, true
	case !onPin && deployedBefore(app, pin):
		// A rollback runs older code: the commit's check stays.
		return checkWant{}, false
	case phase == shpyrdv1.PhaseFailed && !done && (onPin || pinBuildFailed(app)):
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

// pinBuildFailed says the pinned commit's own build failed: Built is False
// since the watcher pinned it (Built's time is to the second, as is the
// pin's). A record from before pins carried their time takes Built=False
// alone.
func pinBuildFailed(app *shpyrdv1.App) bool {
	c := meta.FindStatusCondition(app.Status.Conditions, shpyrdv1.ConditionBuilt)
	if c == nil || c.Status != metav1.ConditionFalse {
		return false
	}
	pinned := sourceRecordOf(app).PinnedAt
	return pinned == nil || !c.LastTransitionTime.Time.Before(pinned.Truncate(time.Second))
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
	// The history keeps maxReleases releases: a rollback to one trimmed
	// from it is taken for a deploy by image once the rollback request is
	// consumed.
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

// rollbackTarget is the release the App is rolled back to: the one asked
// for, else the newest earlier release with its pinned image; 0 when not
// known.
func rollbackTarget(app *shpyrdv1.App) int {
	if n, err := strconv.Atoi(app.Annotations[shpyrdv1.AnnotationRollbackTo]); err == nil && n > 0 {
		return n
	}
	rels := app.Status.Releases
	for i := len(rels) - 2; i >= 0; i-- {
		if app.Spec.Image != "" && rels[i].Image == app.Spec.Image {
			return rels[i].Number
		}
	}
	return 0
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
			if err := r.close(ctx, app, ref, latest, stoppedWaiting(app, rep.Latest, rep.LatestRef)); err != nil {
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
// waits for it: superseded when a newer commit overtook it on the branch
// it was held on (heldRef, still the one followed), else neutral with why.
func stoppedWaiting(app *shpyrdv1.App, sha, heldRef string) checkWant {
	w := checkWant{commit: sha, status: "completed", conclusion: "neutral", label: "superseded"}
	raw := gitauth.ReadSourceRecord(app.Annotations[shpyrdv1.AnnotationGitSource])
	rev := app.GitRevision()
	if newer := sourceRecordOf(app).Latest; heldRef == rev && newer != "" && newer != sha {
		w.title, w.summary = "Superseded by "+shortCommit(newer), "A newer commit, "+shortCommit(newer)+", came before this one was deployed."
		return w
	}
	w.label = "stopped"
	why := ""
	switch {
	case (heldRef != "" && heldRef != rev) || (raw.Ref != "" && raw.Ref != rev):
		why = "the project follows " + rev + " now"
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
		if slot.ref != nil {
			*slot.ref = want.ref
		}
		return nil
	}
	cl, now := ref.client, r.now()
	w := r.checkRun(app, want, now)
	reopens := func(check string) bool { return checkDone(check) && !checkDone(want.key()) }
	if *slot.commit == want.commit && *slot.run != 0 && !reopens(*slot.check) {
		err := cl.UpdateCheckRun(ctx, ref.installation, ref.owner, ref.name, *slot.run, w)
		if err == nil {
			slot.set(want.commit, *slot.run, want.key(), want.ref)
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
			slot.set(want.commit, own.ID, want.key(), want.ref)
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
	slot.set(want.commit, id, want.key(), want.ref)
	return nil
}

// commitSource is a release's source when it is a commit (Release.Source
// keeps 12 characters of it).
var commitSource = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// deployWant is the Deployment of the newest release that changed the
// code: the release, the commit, and the state of its rollout ("" says
// nothing new: a build of the next commit, or a build that failed).
// pending says a newer release is to come: the release gets no Deployment
// now. finishOnly says the state only finishes an unfinished Deployment
// (a release of config or sizes that failed while the code runs on).
type deployWant struct {
	release     int
	ref         string
	description string
	state       string
	pending     bool
	finishOnly  bool
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
	// The pinned commit's build failed while the newest release runs on:
	// that release's rollout finishes from Ready, whatever the phase says.
	runsOn := app.Annotations[shpyrdv1.AnnotationRollbackTo] == "" &&
		meta.IsStatusConditionFalse(app.Status.Conditions, shpyrdv1.ConditionBuilt) &&
		meta.IsStatusConditionTrue(app.Status.Conditions, shpyrdv1.ConditionReady)
	rels := app.Status.Releases
	for i := len(rels) - 1; i >= 0; i-- {
		if i > 0 && rels[i].Source == rels[i-1].Source {
			continue // the same code: config vars, sizes, a restart
		}
		if !commitSource.MatchString(rels[i].Source) {
			return deployWant{}, false
		}
		w := deployWant{release: rels[i].Number, ref: fullCommitOf(app, rels[i].Source), description: firstLine(rels[i].Description)}
		if runsOn {
			w.state = "success"
			return w, true
		}
		if newerToCome(app, rels, i) {
			w.pending = true
			return w, true
		}
		config := i < len(rels)-1
		switch app.Status.Phase {
		case shpyrdv1.PhaseDeploying:
			if !config {
				w.state = "in_progress"
			}
		case shpyrdv1.PhaseRunning:
			// The commit runs: a redeploy of it repairs a failure.
			w.state = "success"
		case shpyrdv1.PhaseFailed:
			// A release of config or sizes that fails leaves the code
			// running: it finishes an unfinished Deployment as a success,
			// and never fails a finished one.
			w.state = "failure"
			if config {
				w.state, w.finishOnly = "success", true
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
//
// Built=False is read for the whole App, not for one commit: a failed
// build is always of code newer than the release history's, so while it
// stands nothing new is said of an older release's Deployment.
//
// A rollback is told by its source appearing earlier in the history. The
// history keeps maxReleases releases: a rollback to a release trimmed from
// it is taken for a newer release to come, and says nothing until the
// pinned commit is released again.
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
func deploymentDone(state string) bool {
	return state == "success" || state == "failure" || state == "inactive"
}

// settled says the Deployment's state have needs no new status: it is the
// one wanted, or final while the rollout is only starting or the state
// only finishes an unfinished one.
func (w deployWant) settled(have string) bool {
	return w.state == have || (deploymentDone(have) && (w.state == "in_progress" || w.finishOnly))
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
		if want.pending {
			// No Deployment for a release a newer one replaces.
			return nil
		}
		if want.release != rep.Release {
			if err := r.retire(ctx, app, ref, rep, fmt.Sprintf("Replaced by release %d", want.release)); err != nil {
				return err
			}
		}
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

// retire closes the report's Deployment when it is unfinished: inactive,
// saying why. One deleted on GitHub needs nothing.
func (r *GitHubReporter) retire(ctx context.Context, app *shpyrdv1.App, ref githubRepoRef, rep *githubReport, why string) error {
	if rep.Deployment == 0 || deploymentDone(rep.DeploymentState) {
		return nil
	}
	err := ref.client.CreateDeploymentStatus(ctx, ref.installation, ref.owner, ref.name, rep.Deployment,
		github.DeploymentStatusWrite{State: "inactive", LogURL: r.projectPage(app), Description: why})
	if err != nil && !github.IsNotFound(err) {
		return err
	}
	rep.DeploymentState = "inactive"
	return nil
}

// newDeployment gives the release its Deployment: one shpyrd already made
// for it (a patch of the annotation that failed, a restart), found by the
// App's UID, the release and shpyrd's GitHub App, with its state; else a
// new one, environment the project, shpyrd having decided to deploy (no
// merge, no required contexts).
//
// The lookup reads one page, the environment's newest 100 Deployments:
// the release wanted is the newest, so its Deployment, when made, is among
// them; one pushed off that page by 100 newer ones is made again.
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
