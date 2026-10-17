package controller

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/go-logr/logr/funcr"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/log"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
	"github.com/shpyrd-io/shpyrd/pkg/github"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// githubReporter is the reporter on a platform with the fake's App, its
// installation 42 on Acme connected, on the fake's clock.
func githubReporter(t *testing.T, objs ...client.Object) (*GitHubReporter, client.Client, *githubtest.Server, store.Store) {
	t.Helper()
	for _, o := range objs {
		if o.GetUID() == "" {
			o.SetUID(types.UID(o.GetNamespace() + "/" + o.GetName()))
		}
	}
	fake, st := connectedGitHub(t)
	_, c := newTestReconciler(t, append([]client.Object{fake.Secret("shpyrd-system")}, objs...)...)
	cfg := Config{Domain: "example.test", DashboardURL: "https://acme.shpyrd.test"}.Defaults()
	r := &GitHubReporter{Client: c, GitHub: &GitHubApp{Source: fake.Source(c, "shpyrd-system"), Installations: st}, Config: cfg, Now: fake.Time}
	return r, c, fake, st
}

func reconcileReport(t *testing.T, r *GitHubReporter, app *shpyrdv1.App) (ctrl.Result, *shpyrdv1.App) {
	t.Helper()
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(app)})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	out := &shpyrdv1.App{}
	if err := r.Get(context.Background(), client.ObjectKeyFromObject(app), out); err != nil {
		t.Fatal(err)
	}
	return res, out
}

// shopAt is the shop app on github.com/acme/shop, main pinned at sha, as
// a push by alice left it.
func shopAt(sha, phase string, rels ...shpyrdv1.Release) *shpyrdv1.App {
	app := pinGit(gitApp("shop", "https://github.com/acme/shop"), sha)
	app.Status.Phase, app.Status.URL, app.Status.Releases = phase, "https://shop.example.test", rels
	return withRecord(app, gitauth.SourceRecord{Ref: "main", Latest: sha, Message: "fix login", Author: "alice", Via: "push"})
}

func release(n int, sha, description string) shpyrdv1.Release {
	return shpyrdv1.Release{Number: n, Image: fmt.Sprintf("10.96.0.50:5000/apps/shop@sha256:%064d", n), Source: short(sha), Description: description, CreatedAt: metav1.NewTime(sourceNow)}
}

// setStatus changes what the App controller would have written.
func setStatus(t *testing.T, c client.Client, app *shpyrdv1.App, change func(*shpyrdv1.AppStatus)) {
	t.Helper()
	got := &shpyrdv1.App{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(app), got); err != nil {
		t.Fatal(err)
	}
	change(&got.Status)
	if err := c.Status().Update(context.Background(), got); err != nil {
		t.Fatal(err)
	}
}

// setAnnotations changes what the source watcher would have written.
func setAnnotations(t *testing.T, c client.Client, app *shpyrdv1.App, change func(*shpyrdv1.App)) {
	t.Helper()
	got := &shpyrdv1.App{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(app), got); err != nil {
		t.Fatal(err)
	}
	change(got)
	if err := c.Update(context.Background(), got); err != nil {
		t.Fatal(err)
	}
}

func reportOf(app *shpyrdv1.App) githubReport {
	return readGitHubReport(app.Annotations[shpyrdv1.AnnotationGitHubReport])
}

func TestACheckRunFollowsTheCommitFromBuildingToRunning(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseBuilding)
	r, c, fake, _ := githubReporter(t, app)
	_, got := reconcileReport(t, r, app)
	runs := fake.CheckRunsOn(testCommit)
	if len(runs) != 1 || runs[0].Name != "shpyrd / shop" || runs[0].Status != "in_progress" || runs[0].Title != "Building" ||
		runs[0].DetailsURL != "https://acme.shpyrd.test/projects/shop" || runs[0].ExternalID != string(app.UID) ||
		!strings.Contains(runs[0].Summary, "Push 0123456 by alice: fix login") {
		t.Fatalf("%+v", runs)
	}
	if rep := reportOf(got); rep.Commit != testCommit || rep.CheckRun != runs[0].ID || rep.Check != "in_progress/building" {
		t.Errorf("annotation %+v", rep)
	}
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
		st.Phase, st.Releases = shpyrdv1.PhaseDeploying, []shpyrdv1.Release{release(1, testCommit, "Push 0123456 by alice: fix login")}
	})
	reconcileReport(t, r, app)
	if runs = fake.CheckRunsOn(testCommit); len(runs) != 1 || runs[0].Title != "Deploying" || runs[0].Status != "in_progress" {
		t.Fatalf("deploying: %+v", runs)
	}
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) { st.Phase = shpyrdv1.PhaseRunning })
	_, got = reconcileReport(t, r, app)
	if runs = fake.CheckRunsOn(testCommit); len(runs) != 1 || runs[0].Status != "completed" || runs[0].Conclusion != "success" || runs[0].Title != "Running at https://shop.example.test" {
		t.Fatalf("running: %+v", runs)
	}
	if rep := reportOf(got); rep.Check != "completed/success" {
		t.Errorf("annotation %+v", rep)
	}
}

func TestOneCheckRunPerCommitAcrossARestart(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseRunning, release(1, testCommit, "Push 0123456 by alice: fix login"))
	r, _, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	reconcileReport(t, r, app)
	writes := len(fake.Writes())
	// A new process: nothing in memory, the annotation says what was sent.
	again := &GitHubReporter{Client: r.Client, GitHub: &GitHubApp{Source: fake.Source(r.Client, "shpyrd-system"), Installations: r.GitHub.Installations}, Config: r.Config, Now: fake.Time}
	reconcileReport(t, again, app)
	if len(fake.Writes()) != writes || len(fake.CheckRunsOn(testCommit)) != 1 {
		t.Errorf("writes %d then %d, runs %+v", writes, len(fake.Writes()), fake.CheckRunsOn(testCommit))
	}
}

func TestAFailedBuildFailsTheCheckWithTheLastLinesOfItsMessage(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseFailed)
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	app.Status.Message = strings.Join(lines, "\n")
	// The build's failure, in words and masked (build_failure.go).
	setCondition(app, shpyrdv1.ConditionReady, metav1.ConditionFalse, "BuildFailed", app.Status.Message)
	r, _, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	runs := fake.CheckRunsOn(testCommit)
	if len(runs) != 1 || runs[0].Conclusion != "failure" || runs[0].Title != "Failed" ||
		!strings.Contains(runs[0].Summary, "line 11\n") || !strings.Contains(runs[0].Summary, "line 30") || strings.Contains(runs[0].Summary, "line 10\n") {
		t.Errorf("%+v", runs)
	}
}

func TestHeldNotDeployedAndSkippedCommitsHaveTheirOwnCheck(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseRunning, release(1, testCommit, "Push 0123456 by alice: fix login"))
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	held := gitauth.SourceRecord{Ref: "main", Latest: otherCommit, Message: "fix it", Author: "alice", Via: "push", Held: otherCommit, HeldSince: at(sourceNow)}
	setAnnotations(t, c, app, func(a *shpyrdv1.App) { withRecord(a, held) })
	reconcileReport(t, r, app)
	runs := fake.CheckRunsOn(otherCommit)
	if len(runs) != 1 || runs[0].Status != "queued" || runs[0].Title != "Waiting for CI" {
		t.Fatalf("held: %+v", runs)
	}
	if first := fake.CheckRunsOn(testCommit); len(first) != 1 || first[0].Conclusion != "success" {
		t.Errorf("the running commit's finished check is left as it was: %+v", first)
	}
	failed := held
	failed.Held, failed.HeldSince, failed.NotDeployed, failed.NotDeployedReason = "", nil, otherCommit, "CI failed: test"
	setAnnotations(t, c, app, func(a *shpyrdv1.App) { withRecord(a, failed) })
	reconcileReport(t, r, app)
	if runs = fake.CheckRunsOn(otherCommit); len(runs) != 1 || runs[0].Status != "completed" || runs[0].Conclusion != "neutral" || runs[0].Title != "CI failed: test" {
		t.Fatalf("not deployed: %+v", runs)
	}
	skipped := gitauth.SourceRecord{Ref: "main", Latest: thirdCommit, Message: "[skip deploy] typo", Via: "push", Skipped: thirdCommit}
	setAnnotations(t, c, app, func(a *shpyrdv1.App) { withRecord(a, skipped) })
	reconcileReport(t, r, app)
	if runs = fake.CheckRunsOn(thirdCommit); len(runs) != 1 || runs[0].Conclusion != "neutral" || runs[0].Title != "Skipped: [skip deploy]" {
		t.Fatalf("skipped: %+v", runs)
	}
}

func TestAMovedPinReplacesAnUnfinishedCheck(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseBuilding)
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	setAnnotations(t, c, app, func(a *shpyrdv1.App) {
		pinGit(a, otherCommit)
		withRecord(a, gitauth.SourceRecord{Ref: "main", Latest: otherCommit, Message: "again", Author: "alice", Via: "push"})
	})
	_, got := reconcileReport(t, r, app)
	old := fake.CheckRunsOn(testCommit)
	if len(old) != 1 || old[0].Status != "completed" || old[0].Conclusion != "neutral" || old[0].Title != "Replaced by 89abcde" {
		t.Errorf("the old check: %+v", old)
	}
	if runs := fake.CheckRunsOn(otherCommit); len(runs) != 1 || runs[0].Title != "Building" || reportOf(got).Commit != otherCommit {
		t.Errorf("the new one: %+v, annotation %+v", runs, reportOf(got))
	}
}

func TestReportingWaitsOutAGitHubOutageAndNeverTouchesTheDeploy(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseBuilding)
	r, _, fake, _ := githubReporter(t, app)
	fake.SetDown(true)
	res, got := reconcileReport(t, r, app)
	if res.RequeueAfter != 30*time.Second || reportOf(got) != (githubReport{}) {
		t.Fatalf("first failure: %v, annotation %+v", res.RequeueAfter, reportOf(got))
	}
	if !reflect.DeepEqual(got.Spec, app.Spec) || got.PinnedCommit() != testCommit {
		t.Error("the reporter changed what deploys")
	}
	calls := len(fake.Calls())
	if res, _ = reconcileReport(t, r, app); res.RequeueAfter <= 0 || res.RequeueAfter > 30*time.Second || len(fake.Calls()) != calls {
		t.Errorf("waiting: %v, %d calls", res.RequeueAfter, len(fake.Calls())-calls)
	}
	advance(fake, 30*time.Second)
	if res, _ = reconcileReport(t, r, app); res.RequeueAfter != time.Minute {
		t.Errorf("second failure: %v", res.RequeueAfter)
	}
	fake.SetDown(false)
	advance(fake, time.Minute)
	res, got = reconcileReport(t, r, app)
	if res.RequeueAfter != 0 || len(fake.CheckRunsOn(testCommit)) != 1 || reportOf(got).Check != "in_progress/building" {
		t.Errorf("sent later: %v, %+v", res.RequeueAfter, fake.CheckRunsOn(testCommit))
	}
}

func TestEveryReportWriteUsesTheReportToken(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseDeploying, release(1, testCommit, "Push 0123456 by alice: fix login"))
	r, c, fake, _ := githubReporter(t, app)
	cl, err := r.GitHub.Source.Client(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cl.InstallationToken(context.Background(), 42, "shop", github.ContentsRead()); err != nil {
		t.Fatal(err)
	}
	reconcileReport(t, r, app)
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) { st.Phase = shpyrdv1.PhaseRunning })
	reconcileReport(t, r, app)
	writes := fake.Writes()
	if len(writes) == 0 {
		t.Fatal("nothing written")
	}
	for _, w := range writes {
		if fmt.Sprint(w.Permissions) != "map[checks:write deployments:write]" {
			t.Errorf("%s %s with %v", w.Method, w.Path, w.Permissions)
		}
	}
}

func TestADeletedCheckRunStartsANewOne(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseBuilding)
	r, c, fake, _ := githubReporter(t, app)
	_, got := reconcileReport(t, r, app)
	fake.DeleteCheckRun(reportOf(got).CheckRun)
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
		st.Phase, st.Releases = shpyrdv1.PhaseDeploying, []shpyrdv1.Release{release(1, testCommit, "Push 0123456 by alice: fix login")}
	})
	_, got = reconcileReport(t, r, app)
	runs := fake.CheckRunsOn(testCommit)
	if len(runs) != 1 || runs[0].Title != "Deploying" || reportOf(got).CheckRun != runs[0].ID {
		t.Errorf("%+v, annotation %+v", runs, reportOf(got))
	}
}

func TestNothingIsReportedForASuspendedInstallation(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseBuilding)
	r, _, fake, st := githubReporter(t, app)
	if err := st.SetGitHubInstallationState(context.Background(), 42, true, false); err != nil {
		t.Fatal(err)
	}
	res, got := reconcileReport(t, r, app)
	if res.RequeueAfter != 0 || len(fake.Calls()) != 0 || reportOf(got) != (githubReport{}) {
		t.Errorf("requeue %v, calls %v, annotation %+v", res.RequeueAfter, fake.Calls(), reportOf(got))
	}
}

func TestOnlyAppsFollowingABranchOnGitHubAreReported(t *testing.T) {
	for _, app := range []*shpyrdv1.App{
		pinGit(gitApp("lab", "https://gitlab.example.test/acme/lab"), testCommit),
		func() *shpyrdv1.App {
			a := gitApp("fixed", "https://github.com/acme/shop")
			a.Spec.Source.Git.Revision = testCommit
			return a
		}(),
	} {
		if reportsToGitHub(app) {
			t.Errorf("%s is reported", app.Name)
		}
	}
	if !reportsToGitHub(shopAt(testCommit, shpyrdv1.PhaseRunning)) {
		t.Error("a branch on github.com is reported")
	}
}

// Ruling F2: a cover that cannot be told is not "not covered": the
// reporter waits through its backoff and keeps what it sent.
func TestAnUnknownCoverWaitsAndGivesUpNoReport(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseBuilding)
	r, c, fake, st := githubReporter(t, app)
	_, got := reconcileReport(t, r, app)
	sent := reportOf(got)
	writes := len(fake.Writes())
	r.GitHub.Installations = unreadableInstallations{st}
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
		st.Phase, st.Releases = shpyrdv1.PhaseRunning, []shpyrdv1.Release{release(1, testCommit, "Push 0123456 by alice: fix login")}
	})
	res, got := reconcileReport(t, r, app)
	if res.RequeueAfter != 30*time.Second || reportOf(got) != sent || len(fake.Writes()) != writes {
		t.Fatalf("unknown cover: requeue %v, annotation %+v (was %+v), %d writes", res.RequeueAfter, reportOf(got), sent, len(fake.Writes())-writes)
	}
	r.GitHub.Installations = st
	advance(fake, 30*time.Second)
	if _, got = reconcileReport(t, r, app); reportOf(got).Check != "completed/success" {
		t.Errorf("once known: %+v", reportOf(got))
	}
}

// Ruling F2: not covered (suspended, removed, no App) stops reporting for
// the App: nothing is sent and nothing is queued.
func TestNothingIsReportedWhenTheAppNoLongerCoversTheRepository(t *testing.T) {
	for name, uncover := range map[string]func(*GitHubReporter, store.Store) error{
		"removed": func(_ *GitHubReporter, st store.Store) error {
			return st.SetGitHubInstallationState(context.Background(), 42, false, true)
		},
		"no App": func(r *GitHubReporter, _ store.Store) error { r.GitHub = &GitHubApp{}; return nil },
	} {
		t.Run(name, func(t *testing.T) {
			app := shopAt(testCommit, shpyrdv1.PhaseBuilding)
			r, c, fake, st := githubReporter(t, app)
			_, got := reconcileReport(t, r, app)
			sent, calls := reportOf(got), len(fake.Calls())
			if err := uncover(r, st); err != nil {
				t.Fatal(err)
			}
			setStatus(t, c, app, func(st *shpyrdv1.AppStatus) { st.Phase = shpyrdv1.PhaseFailed })
			res, got := reconcileReport(t, r, app)
			if res.RequeueAfter != 0 || len(fake.Calls()) != calls || reportOf(got) != sent {
				t.Errorf("requeue %v, %d calls, annotation %+v", res.RequeueAfter, len(fake.Calls())-calls, reportOf(got))
			}
		})
	}
}

// Ruling F3: a release of config or sizes, or a rollback, never changes
// the commit's check, whether it rolls out, runs or fails.
func TestAConfigReleaseOrARollbackLeavesTheCommitsCheck(t *testing.T) {
	code := release(1, testCommit, "Push 0123456 by alice: fix login")
	config := release(2, testCommit, "Config change")
	t.Run("config", func(t *testing.T) {
		app := shopAt(testCommit, shpyrdv1.PhaseRunning, code)
		r, c, fake, _ := githubReporter(t, app)
		reconcileReport(t, r, app)
		writes := checkWrites(fake)
		for _, phase := range []string{shpyrdv1.PhaseDeploying, shpyrdv1.PhaseFailed, shpyrdv1.PhaseRunning} {
			setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
				st.Phase, st.Releases, st.Message = phase, []shpyrdv1.Release{code, config}, "the config release failed"
			})
			_, got := reconcileReport(t, r, app)
			if runs := fake.CheckRunsOn(testCommit); checkWrites(fake) != writes || len(runs) != 1 || runs[0].Conclusion != "success" || reportOf(got).Check != "completed/success" {
				t.Errorf("%s: %d writes, %+v", phase, checkWrites(fake)-writes, runs)
			}
		}
	})
	t.Run("rollback", func(t *testing.T) {
		older := release(1, otherCommit, "Push 89abcde by alice: before")
		newest := release(2, testCommit, "Push 0123456 by alice: fix login")
		app := shopAt(testCommit, shpyrdv1.PhaseRunning, older, newest)
		r, c, fake, _ := githubReporter(t, app)
		reconcileReport(t, r, app)
		writes := checkWrites(fake)
		back := release(3, otherCommit, "Rollback to v1")
		for _, phase := range []string{shpyrdv1.PhaseDeploying, shpyrdv1.PhaseRunning, shpyrdv1.PhaseFailed} {
			setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
				st.Phase, st.Releases, st.Message = phase, []shpyrdv1.Release{older, newest, back}, "the rollback failed"
			})
			reconcileReport(t, r, app)
			if runs := fake.CheckRunsOn(testCommit); checkWrites(fake) != writes || len(runs) != 1 || runs[0].Conclusion != "success" {
				t.Errorf("%s: %d writes, %+v", phase, checkWrites(fake)-writes, runs)
			}
		}
		// The rollback is its own release: one new Deployment, through
		// in progress, success and failure; release 2's is left alone.
		d := fake.Deployments()
		if len(d) != 2 || d[0].Release != 2 || len(d[0].Statuses) != 1 || d[0].Statuses[0].State != "success" {
			t.Errorf("release 2's Deployment: %+v", d)
		}
		if len(d) != 2 || d[1].Release != 3 || len(d[1].Statuses) != 3 || d[1].Statuses[0].State != "in_progress" ||
			d[1].Statuses[1].State != "success" || d[1].Statuses[2].State != "failure" {
			t.Errorf("the rollback's Deployment: %+v", d)
		}
	})
}

func TestANotDeployedCommitsCheckSaysWhyInWords(t *testing.T) {
	for _, reason := range []string{gitauth.ReasonNoWatchedChanges, gitauth.ReasonCIFailedPrefix + "test", gitauth.ReasonCITimeout, gitauth.ReasonCINotCovered} {
		app := shopAt(testCommit, shpyrdv1.PhaseRunning, release(1, testCommit, "Push 0123456 by alice: fix login"))
		withRecord(app, gitauth.SourceRecord{Ref: "main", Latest: otherCommit, Message: "fix it", Author: "alice", Via: "push", NotDeployed: otherCommit, NotDeployedReason: reason})
		w, ok := wantLatest(app)
		if !ok || w.commit != otherCommit || w.conclusion != "neutral" || w.title != reason || !strings.Contains(w.summary, "Not deployed: "+reason+".") {
			t.Errorf("%s: %+v", reason, w)
		}
	}
}

func TestPermissionsNotGrantedBackOffTheReporterAndAreLoggedOnceAMinute(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseBuilding)
	r, _, fake, _ := githubReporter(t, app)
	fake.RefusePermission("checks")
	var buf strings.Builder
	ctx := log.IntoContext(context.Background(), funcr.New(func(prefix, args string) { buf.WriteString(prefix + args + "\n") }, funcr.Options{}))
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(app)}
	for i, want := range []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute} {
		res, err := r.Reconcile(ctx, req)
		if err != nil || res.RequeueAfter != want {
			t.Fatalf("failure %d: %v, %v", i+1, res.RequeueAfter, err)
		}
		advance(fake, want)
	}
	// Logged at 0 and at 90 s, not at 30 s.
	if n := strings.Count(buf.String(), "could not report to GitHub"); n != 2 || !strings.Contains(buf.String(), "accept the App's updated permissions") {
		t.Errorf("%d log lines:\n%s", n, buf.String())
	}
	got := &shpyrdv1.App{}
	if err := r.Get(ctx, req.NamespacedName, got); err != nil || reportOf(got) != (githubReport{}) {
		t.Errorf("annotation %+v, %v", reportOf(got), err)
	}
	// The clone's caches are the clone's: no backoff, no refusal.
	if r.GitHub.backingOff(42) {
		t.Error("the clone backs off")
	}
	if why, ok := r.GitHub.refusedLately(42, "shop"); ok {
		t.Errorf("the clone is refused: %s", why)
	}
}

// checkWrites are the writes of check runs; the Deployments' are not
// counted.
func checkWrites(fake *githubtest.Server) int {
	n := 0
	for _, w := range fake.Writes() {
		if strings.Contains(w.Path, "/check-runs") {
			n++
		}
	}
	return n
}

// creates counts the check runs made through the API.
func creates(fake *githubtest.Server) int {
	n := 0
	for _, w := range fake.Writes() {
		if w.Method == "POST" && strings.HasSuffix(w.Path, "/check-runs") {
			n++
		}
	}
	return n
}

func TestAnUnknownCoverIsReportedInTheLogOnceAMinute(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseBuilding)
	r, _, fake, st := githubReporter(t, app)
	r.GitHub.Installations = unreadableInstallations{st}
	var buf strings.Builder
	ctx := log.IntoContext(context.Background(), funcr.New(func(prefix, args string) { buf.WriteString(prefix + args + "\n") }, funcr.Options{}))
	for _, wait := range []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute} {
		if res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(app)}); err != nil || res.RequeueAfter != wait {
			t.Fatalf("%v, %v", res.RequeueAfter, err)
		}
		advance(fake, wait)
	}
	if n := strings.Count(buf.String(), "\n"); n != 2 || strings.Contains(buf.String(), "could not tell whether") {
		t.Errorf("%d log lines:\n%s", n, buf.String())
	}
}

// Review 1: a push held, not deployed or skipped while the pin builds is
// not the pin moving: the pin's check goes on.
func TestAPushHeldWhileThePinBuildsLeavesThePinsCheck(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseBuilding)
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	setAnnotations(t, c, app, func(a *shpyrdv1.App) {
		withRecord(a, gitauth.SourceRecord{Ref: "main", Latest: otherCommit, Message: "fix it", Author: "alice", Via: "push", Held: otherCommit, HeldSince: at(sourceNow)})
	})
	reconcileReport(t, r, app)
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
		st.Phase, st.Releases = shpyrdv1.PhaseRunning, []shpyrdv1.Release{release(1, testCommit, "Push 0123456 by alice: fix login")}
	})
	_, got := reconcileReport(t, r, app)
	if runs := fake.CheckRunsOn(testCommit); len(runs) != 1 || runs[0].Conclusion != "success" {
		t.Errorf("the pin: %+v", runs)
	}
	if runs := fake.CheckRunsOn(otherCommit); len(runs) != 1 || runs[0].Status != "queued" || runs[0].Title != "Waiting for CI" {
		t.Errorf("the held push: %+v", runs)
	}
	if rep := reportOf(got); rep.Commit != testCommit || rep.Check != "completed/success" || rep.Latest != otherCommit || rep.LatestCheck != "queued/waiting" {
		t.Errorf("annotation %+v", rep)
	}
}

// Ruling (b): a held commit pinned while the older release runs is
// Building at once, on its own run.
func TestAHeldCommitPinnedIsBuildingOnTheSameCheckRun(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseRunning, release(1, testCommit, "Push 0123456 by alice: fix login"))
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	held := gitauth.SourceRecord{Ref: "main", Latest: otherCommit, Message: "fix it", Author: "alice", Via: "push", Held: otherCommit, HeldSince: at(sourceNow)}
	setAnnotations(t, c, app, func(a *shpyrdv1.App) { withRecord(a, held) })
	reconcileReport(t, r, app)
	queued := fake.CheckRunsOn(otherCommit)
	passed := held
	passed.Held, passed.HeldSince = "", nil
	setAnnotations(t, c, app, func(a *shpyrdv1.App) { pinGit(a, otherCommit); withRecord(a, passed) })
	_, got := reconcileReport(t, r, app)
	runs := fake.CheckRunsOn(otherCommit)
	if len(queued) != 1 || len(runs) != 1 || runs[0].ID != queued[0].ID || runs[0].Status != "in_progress" || runs[0].Title != "Building" {
		t.Fatalf("%+v", runs)
	}
	if first := fake.CheckRunsOn(testCommit); len(first) != 1 || first[0].Conclusion != "success" {
		t.Errorf("the finished check of the pin before: %+v", first)
	}
	if rep := reportOf(got); rep.Commit != otherCommit || rep.CheckRun != runs[0].ID || rep.Check != "in_progress/building" || rep.Latest != "" {
		t.Errorf("annotation %+v", rep)
	}
}

// Ruling (b): a new pin with no finished check is Building while the
// older release runs; a finished one is never reopened.
func TestANewPinsCheckIsBuildingWhileTheOlderReleaseRuns(t *testing.T) {
	older := release(1, otherCommit, "Push 89abcde by alice: before")
	app := shopAt(testCommit, shpyrdv1.PhaseRunning, older)
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	if runs := fake.CheckRunsOn(testCommit); len(runs) != 1 || runs[0].Status != "in_progress" || runs[0].Title != "Building" {
		t.Fatalf("new pin: %+v", runs)
	}
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) { st.Phase, st.Message = shpyrdv1.PhaseFailed, "boom" })
	reconcileReport(t, r, app)
	writes := checkWrites(fake)
	for _, phase := range []string{shpyrdv1.PhaseRunning, shpyrdv1.PhaseBuilding, shpyrdv1.PhaseDeploying} {
		setStatus(t, c, app, func(st *shpyrdv1.AppStatus) { st.Phase = phase })
		_, got := reconcileReport(t, r, app)
		if runs := fake.CheckRunsOn(testCommit); checkWrites(fake) != writes || len(runs) != 1 || runs[0].Conclusion != "failure" || reportOf(got).Check != "completed/failure" {
			t.Errorf("%s: %d writes, %+v", phase, checkWrites(fake)-writes, runs)
		}
	}
}

// Ruling (a): a release of config or sizes running, or failed, while the
// commit's code runs, finishes an unfinished check, or a missing one, as
// a success; a finished one stays.
func TestAConfigReleaseFinishesAnUnfinishedCheckAsASuccess(t *testing.T) {
	code := release(1, testCommit, "Push 0123456 by alice: fix login")
	config := release(2, testCommit, "Config change")
	for _, phase := range []string{shpyrdv1.PhaseRunning, shpyrdv1.PhaseFailed} {
		t.Run(phase+" after deploying", func(t *testing.T) {
			app := shopAt(testCommit, shpyrdv1.PhaseDeploying, code)
			r, c, fake, _ := githubReporter(t, app)
			_, got := reconcileReport(t, r, app)
			if reportOf(got).Check != "in_progress/deploying" {
				t.Fatalf("%+v", reportOf(got))
			}
			setStatus(t, c, app, func(st *shpyrdv1.AppStatus) { st.Phase, st.Releases = phase, []shpyrdv1.Release{code, config} })
			_, got = reconcileReport(t, r, app)
			if runs := fake.CheckRunsOn(testCommit); len(runs) != 1 || runs[0].Conclusion != "success" || reportOf(got).Check != "completed/success" {
				t.Errorf("%+v", runs)
			}
		})
		t.Run(phase+" with no check", func(t *testing.T) {
			app := shopAt(testCommit, phase, code, config)
			r, _, fake, _ := githubReporter(t, app)
			reconcileReport(t, r, app)
			if runs := fake.CheckRunsOn(testCommit); len(runs) != 1 || runs[0].Conclusion != "success" {
				t.Errorf("%+v", runs)
			}
		})
	}
	t.Run("finished", func(t *testing.T) {
		app := shopAt(testCommit, shpyrdv1.PhaseFailed, code)
		r, c, fake, _ := githubReporter(t, app)
		reconcileReport(t, r, app)
		writes := checkWrites(fake)
		setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
			st.Phase, st.Releases = shpyrdv1.PhaseRunning, []shpyrdv1.Release{code, config}
		})
		reconcileReport(t, r, app)
		if runs := fake.CheckRunsOn(testCommit); checkWrites(fake) != writes || len(runs) != 1 || runs[0].Conclusion != "failure" {
			t.Errorf("%d writes, %+v", checkWrites(fake)-writes, runs)
		}
	})
}

// listings counts the reporter's listings of a commit's check runs.
func listings(fake *githubtest.Server) int {
	n := 0
	for _, c := range fake.Calls() {
		if strings.HasPrefix(c, "GET ") && strings.HasSuffix(c, "/check-runs") {
			n++
		}
	}
	return n
}

// Review 2: a run made whose annotation patch failed is never made twice.
func TestAFailedReportPatchNeverPostsACheckRunTwice(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseBuilding)
	r, c, fake, _ := githubReporter(t, app)
	inner := r.Client
	r.Client = interceptor.NewClient(inner.(client.WithWatch), interceptor.Funcs{
		Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
			return errors.New("the API server is away")
		},
	})
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(app)}
	if _, err := r.Reconcile(context.Background(), req); err == nil {
		t.Fatal("the failed patch is not an error")
	}
	if _, err := r.Reconcile(context.Background(), req); err == nil {
		t.Fatal("the failed patch is not an error")
	}
	r.Client = inner
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
		st.Phase, st.Releases = shpyrdv1.PhaseDeploying, []shpyrdv1.Release{release(1, testCommit, "Push 0123456 by alice: fix login")}
	})
	_, got := reconcileReport(t, r, app)
	// Memory knows the run: it is not looked for again.
	if runs := fake.CheckRunsOn(testCommit); creates(fake) != 1 || listings(fake) != 1 || len(runs) != 1 || runs[0].Title != "Deploying" || reportOf(got).CheckRun != runs[0].ID {
		t.Errorf("%d creates, %+v, annotation %+v", creates(fake), runs, reportOf(got))
	}
}

// Review 2: an annotation older than what was sent (a stale cache) is not
// taken for the truth.
func TestAStaleReportNeverPostsACheckRunTwice(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseBuilding)
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	setAnnotations(t, c, app, func(a *shpyrdv1.App) { delete(a.Annotations, shpyrdv1.AnnotationGitHubReport) })
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
		st.Phase, st.Releases = shpyrdv1.PhaseDeploying, []shpyrdv1.Release{release(1, testCommit, "Push 0123456 by alice: fix login")}
	})
	_, got := reconcileReport(t, r, app)
	if runs := fake.CheckRunsOn(testCommit); creates(fake) != 1 || listings(fake) != 1 || len(runs) != 1 || runs[0].Title != "Deploying" || reportOf(got).Check != "in_progress/deploying" {
		t.Errorf("%d creates, %+v, annotation %+v", creates(fake), runs, reportOf(got))
	}
}

// Review 2: a run shpyrd made for the App, unknown to the annotation and
// to memory (a restart after a failed patch), is found and used.
func TestARunAlreadyOnGitHubIsAdopted(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseDeploying, release(1, testCommit, "Push 0123456 by alice: fix login"))
	r, _, fake, _ := githubReporter(t, app)
	fake.SetCheckRuns(testCommit,
		githubtest.CheckRun{Name: "shpyrd / shop", Status: "in_progress", ExternalID: "someone else", AppID: githubtest.AppID},
		githubtest.CheckRun{Name: "shpyrd / shop", Status: "in_progress", ExternalID: string(app.UID), AppID: githubtest.AppID},
		githubtest.CheckRun{Name: "test", Status: "completed", Conclusion: "success"})
	_, got := reconcileReport(t, r, app)
	runs := fake.CheckRunsOn(testCommit)
	var ours []githubtest.CheckRun
	for _, run := range runs {
		if run.ExternalID == string(app.UID) {
			ours = append(ours, run)
		}
	}
	if creates(fake) != 0 || len(ours) != 1 || ours[0].Title != "Deploying" || reportOf(got).CheckRun != ours[0].ID {
		t.Errorf("%d creates, %+v, annotation %+v", creates(fake), runs, reportOf(got))
	}
}

// Review 3: only a build's failure, in words and masked, reaches GitHub.
func TestAFailedCheckSaysOnlyThatTheDeployFailedUnlessTheBuildFailed(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseFailed)
	app.Status.Message = "render env: secret shop-env: password=hunter2"
	setCondition(app, shpyrdv1.ConditionReady, metav1.ConditionFalse, "ReconcileError", app.Status.Message)
	r, _, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	runs := fake.CheckRunsOn(testCommit)
	if len(runs) != 1 || runs[0].Conclusion != "failure" || strings.Contains(runs[0].Summary, "hunter2") ||
		!strings.Contains(runs[0].Summary, "The deploy failed: see the project on shpyrd") {
		t.Errorf("%+v", runs)
	}
}

func TestACheckSummaryIsCutAtACharacter(t *testing.T) {
	msg := strings.Repeat("é", 3000) // 6000 bytes
	got := lastLines(msg, 20, 4001)
	if !utf8.ValidString(got) || len(got) != 4000 {
		t.Errorf("%d bytes, valid %v", len(got), utf8.ValidString(got))
	}
}

func TestTheCheckLinksToTheProjectOnItsWorkspacesDashboard(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseBuilding)
	r, _, fake, _ := githubReporter(t, app)
	r.Config.WorkspaceDomain = func(string) string { return "acme.example.test" }
	reconcileReport(t, r, app)
	if runs := fake.CheckRunsOn(testCommit); len(runs) != 1 || runs[0].DetailsURL != "https://acme.example.test/projects/shop" {
		t.Errorf("%+v", runs)
	}
}

func TestAReleaseOfANewCommitIsAGitHubDeployment(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseDeploying, release(1, testCommit, "Push 0123456 by alice: fix login"))
	r, c, fake, _ := githubReporter(t, app)
	_, got := reconcileReport(t, r, app)
	d := fake.Deployments()
	if len(d) != 1 || d[0].Ref != testCommit || d[0].Environment != "shop" || d[0].AutoMerge || d[0].Transient ||
		d[0].RequiredContexts == nil || len(d[0].RequiredContexts) != 0 || len(d[0].Statuses) != 1 || d[0].Statuses[0].State != "in_progress" {
		t.Fatalf("%+v", d)
	}
	if rep := reportOf(got); rep.Release != 1 || rep.Deployment != d[0].ID || rep.DeploymentState != "in_progress" {
		t.Errorf("annotation %+v", rep)
	}
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) { st.Phase = shpyrdv1.PhaseRunning })
	reconcileReport(t, r, app)
	d = fake.Deployments()
	if len(d) != 1 || len(d[0].Statuses) != 2 || d[0].Statuses[1].State != "success" || d[0].Statuses[1].EnvironmentURL != "https://shop.example.test" ||
		d[0].Statuses[1].LogURL != "https://acme.shpyrd.test/projects/shop" {
		t.Errorf("%+v", d)
	}
}

func TestAReleaseThatOnlyChangesConfigMakesNoDeployment(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseRunning, release(1, testCommit, "Push 0123456 by alice: fix login"))
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
		st.Phase = shpyrdv1.PhaseDeploying
		st.Releases = append(st.Releases, release(2, testCommit, "Set LOG_LEVEL"))
	})
	reconcileReport(t, r, app)
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) { st.Phase = shpyrdv1.PhaseRunning })
	reconcileReport(t, r, app)
	d := fake.Deployments()
	if len(d) != 1 || len(d[0].Statuses) != 1 || d[0].Statuses[0].State != "success" {
		t.Errorf("a config release neither makes a Deployment nor takes one back to in progress: %+v", d)
	}
	if runs := fake.CheckRunsOn(testCommit); len(runs) != 1 || runs[0].Conclusion != "success" {
		t.Errorf("a config release leaves the commit's check as it was: %+v", runs)
	}
}

func TestARollbackKeepsTheCheckAndIsADeployment(t *testing.T) {
	app := shopAt(otherCommit, shpyrdv1.PhaseRunning, release(1, testCommit, "Push 0123456"), release(2, otherCommit, "Push 89abcde by alice: fix login"))
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
		st.Releases = append(st.Releases, release(3, testCommit, "Rollback to v1"))
	})
	_, got := reconcileReport(t, r, app)
	if runs := fake.CheckRunsOn(otherCommit); len(runs) != 1 || runs[0].Conclusion != "success" {
		t.Errorf("the newest commit's check stays: %+v", runs)
	}
	if runs := fake.CheckRunsOn(testCommit); len(runs) != 0 {
		t.Errorf("no check for the rolled-back commit: %+v", runs)
	}
	d := fake.Deployments()
	if len(d) != 2 || d[1].Ref != short(testCommit) || d[1].Description != "Rollback to v1" || d[1].Statuses[0].State != "success" || reportOf(got).Release != 3 {
		t.Errorf("%+v, annotation %+v", d, reportOf(got))
	}
}

func TestAFailedRolloutFailsTheDeploymentAndAFailedBuildLeavesIt(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseRunning, release(1, testCommit, "Push 0123456 by alice: fix login"))
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	// The next commit's build fails: the running release's Deployment stays.
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
		st.Phase, st.Message = shpyrdv1.PhaseFailed, "The build failed."
		meta.SetStatusCondition(&st.Conditions, metav1.Condition{Type: shpyrdv1.ConditionBuilt, Status: metav1.ConditionFalse, Reason: "BuildFailed", Message: "The build failed."})
	})
	reconcileReport(t, r, app)
	if d := fake.Deployments(); len(d[0].Statuses) != 1 {
		t.Errorf("a failed build is not a failed rollout: %+v", d)
	}
	// The rollout of the release fails.
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
		st.Message = "web: CrashLoopBackOff"
		meta.SetStatusCondition(&st.Conditions, metav1.Condition{Type: shpyrdv1.ConditionBuilt, Status: metav1.ConditionTrue, Reason: "Built", Message: "built"})
	})
	reconcileReport(t, r, app)
	if d := fake.Deployments(); len(d[0].Statuses) != 2 || d[0].Statuses[1].State != "failure" {
		t.Errorf("%+v", d)
	}
}

func TestADeletedDeploymentStartsANewOne(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseDeploying, release(1, testCommit, "Push 0123456 by alice: fix login"))
	r, c, fake, _ := githubReporter(t, app)
	_, got := reconcileReport(t, r, app)
	fake.DeleteDeployment(reportOf(got).Deployment)
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) { st.Phase = shpyrdv1.PhaseRunning })
	_, got = reconcileReport(t, r, app)
	d := fake.Deployments()
	if len(d) != 1 || d[0].Ref != testCommit || len(d[0].Statuses) != 1 || d[0].Statuses[0].State != "success" || reportOf(got).Deployment != d[0].ID {
		t.Errorf("%+v, annotation %+v", d, reportOf(got))
	}
}

// A Deployment made whose annotation patch failed is never made twice:
// memory knows it, and after a restart it is found on GitHub by its
// release, never another App's.
func TestAFailedReportPatchAndARestartNeverPostADeploymentTwice(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseDeploying, release(1, testCommit, "Push 0123456 by alice: fix login"))
	r, c, fake, _ := githubReporter(t, app)
	fake.AddDeployment(githubtest.Deployment{Ref: testCommit, Environment: "shop", AppID: 999, App: string(app.UID), Release: 1})
	inner := r.Client
	r.Client = interceptor.NewClient(inner.(client.WithWatch), interceptor.Funcs{
		Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
			return errors.New("the API server is away")
		},
	})
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(app)}
	for range 2 {
		if _, err := r.Reconcile(context.Background(), req); err == nil {
			t.Fatal("the failed patch is not an error")
		}
	}
	ours := func() []githubtest.Deployment {
		var out []githubtest.Deployment
		for _, d := range fake.Deployments() {
			if d.AppID == githubtest.AppID {
				out = append(out, d)
			}
		}
		return out
	}
	if d := ours(); len(d) != 1 || len(d[0].Statuses) != 1 || d[0].Statuses[0].State != "in_progress" || d[0].App != string(app.UID) || d[0].Release != 1 {
		t.Fatalf("memory: %+v", fake.Deployments())
	}
	// A restart: nothing in memory, and no annotation.
	again := &GitHubReporter{Client: inner, GitHub: &GitHubApp{Source: fake.Source(inner, "shpyrd-system"), Installations: r.GitHub.Installations}, Config: r.Config, Now: fake.Time}
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) { st.Phase = shpyrdv1.PhaseRunning })
	_, got := reconcileReport(t, again, app)
	d := ours()
	if len(d) != 1 || len(d[0].Statuses) != 2 || d[0].Statuses[1].State != "success" {
		t.Errorf("after a restart: %+v", fake.Deployments())
	}
	if rep := reportOf(got); len(d) != 1 || rep.Release != 1 || rep.Deployment != d[0].ID || rep.DeploymentState != "success" {
		t.Errorf("annotation %+v", rep)
	}
	if foreign := fake.Deployments()[0]; foreign.AppID != 999 || len(foreign.Statuses) != 0 {
		t.Errorf("another App's Deployment was used: %+v", foreign)
	}
}

// Re-review c: a run of the same name and external_id made by another
// App is not shpyrd's.
func TestARunOfAnotherAppIsNeverAdopted(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseDeploying, release(1, testCommit, "Push 0123456 by alice: fix login"))
	r, _, fake, _ := githubReporter(t, app)
	fake.SetCheckRuns(testCommit, githubtest.CheckRun{Name: "shpyrd / shop", Status: "in_progress", ExternalID: string(app.UID), AppID: 999})
	_, got := reconcileReport(t, r, app)
	runs := fake.CheckRunsOn(testCommit)
	if creates(fake) != 1 || len(runs) != 2 || runs[0].Title != "" || runs[1].AppID != githubtest.AppID || runs[1].Title != "Deploying" || reportOf(got).CheckRun != runs[1].ID {
		t.Errorf("%d creates, %+v, annotation %+v", creates(fake), runs, reportOf(got))
	}
}

// Re-review a: a held commit a newer one overtook is superseded by it.
func TestAHeldCommitOvertakenByANewerOneIsSuperseded(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseRunning, release(1, testCommit, "Push 0123456 by alice: fix login"))
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	setAnnotations(t, c, app, func(a *shpyrdv1.App) {
		withRecord(a, gitauth.SourceRecord{Ref: "main", Latest: otherCommit, Via: "push", Held: otherCommit, HeldSince: at(sourceNow)})
	})
	reconcileReport(t, r, app)
	setAnnotations(t, c, app, func(a *shpyrdv1.App) {
		withRecord(a, gitauth.SourceRecord{Ref: "main", Latest: thirdCommit, Via: "push", Held: thirdCommit, HeldSince: at(sourceNow)})
	})
	_, got := reconcileReport(t, r, app)
	if runs := fake.CheckRunsOn(otherCommit); len(runs) != 1 || runs[0].Status != "completed" || runs[0].Conclusion != "neutral" || runs[0].Title != "Superseded by fedcba9" {
		t.Errorf("the overtaken commit: %+v", runs)
	}
	if runs := fake.CheckRunsOn(thirdCommit); len(runs) != 1 || runs[0].Status != "queued" || reportOf(got).Latest != thirdCommit {
		t.Errorf("the newer one: %+v, annotation %+v", runs, reportOf(got))
	}
}

// Re-review a: a held commit shpyrd stops waiting for, with no newer
// commit, is closed with why, never left queued, never "superseded".
func TestAHeldCommitNoLongerWaitedForIsClosedWithWhy(t *testing.T) {
	held := gitauth.SourceRecord{Ref: "main", Latest: otherCommit, Via: "push", Held: otherCommit, HeldSince: at(sourceNow)}
	released := held
	released.Held, released.HeldSince = "", nil
	for name, tc := range map[string]struct {
		change func(*shpyrdv1.App)
		title  string
	}{
		"auto-deploy off": {func(a *shpyrdv1.App) {
			off := false
			a.Spec.Source.Git.AutoDeploy = &off
			withRecord(a, released)
		}, "No longer waiting: auto-deploy is off"},
		"wait for CI off": {func(a *shpyrdv1.App) {
			a.Spec.Source.Git.WaitForChecks = false
			withRecord(a, released)
		}, "No longer waiting: wait for CI is off"},
		"another branch, no pin": {func(a *shpyrdv1.App) { a.Spec.Source.Git.Revision = "develop" }, "No longer waiting: the project follows develop now"},
		"anything else":          {func(a *shpyrdv1.App) { withRecord(a, released) }, "No longer waiting for CI"},
	} {
		t.Run(name, func(t *testing.T) {
			app := shopAt(testCommit, shpyrdv1.PhaseRunning, release(1, testCommit, "Push 0123456 by alice: fix login"))
			app.Spec.Source.Git.WaitForChecks = true
			r, c, fake, _ := githubReporter(t, app)
			reconcileReport(t, r, app)
			setAnnotations(t, c, app, func(a *shpyrdv1.App) { withRecord(a, held) })
			reconcileReport(t, r, app)
			setAnnotations(t, c, app, tc.change)
			_, got := reconcileReport(t, r, app)
			if runs := fake.CheckRunsOn(otherCommit); len(runs) != 1 || runs[0].Status != "completed" || runs[0].Conclusion != "neutral" || runs[0].Title != tc.title {
				t.Errorf("%+v", runs)
			}
			if rep := reportOf(got); rep.Latest != "" || rep.LatestRun != 0 {
				t.Errorf("annotation %+v", rep)
			}
		})
	}
}

// Re-review b, ruling (a): a rollback never fails the pinned commit's
// check, also when that commit never ran.
func TestAFailedRollbackNeverFailsThePinsCheck(t *testing.T) {
	v1, v2 := release(1, otherCommit, "Push 89abcde by alice: before"), release(2, thirdCommit, "Push fedcba9 by alice: later")
	back := release(3, otherCommit, "Rollback to v1")
	back.Image = v1.Image
	for name, tc := range map[string]struct {
		rels     []shpyrdv1.Release
		annotate bool
	}{
		"rolling out": {[]shpyrdv1.Release{v1, v2}, true},
		"rolled back": {[]shpyrdv1.Release{v1, v2, back}, false},
	} {
		t.Run(name, func(t *testing.T) {
			app := shopAt(testCommit, shpyrdv1.PhaseFailed, tc.rels...)
			app.Status.Message = "web: CrashLoopBackOff"
			app.Spec.Image = v1.Image
			if tc.annotate {
				app.Annotations[shpyrdv1.AnnotationRollbackTo] = "1"
			}
			r, _, fake, _ := githubReporter(t, app)
			reconcileReport(t, r, app)
			if runs := fake.CheckRunsOn(testCommit); len(runs) != 0 {
				t.Errorf("the pin's check: %+v", runs)
			}
		})
	}
}

// Ruling on concern 3: a release of config or sizes that fails never
// changes the code release's finished Deployment, as with the check.
func TestAFailedConfigReleaseLeavesTheDeployment(t *testing.T) {
	code := release(1, testCommit, "Push 0123456 by alice: fix login")
	app := shopAt(testCommit, shpyrdv1.PhaseRunning, code)
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
		st.Phase, st.Message, st.Releases = shpyrdv1.PhaseFailed, "web: CrashLoopBackOff", []shpyrdv1.Release{code, release(2, testCommit, "Set LOG_LEVEL")}
	})
	_, got := reconcileReport(t, r, app)
	if d := fake.Deployments(); len(d) != 1 || len(d[0].Statuses) != 1 || d[0].Statuses[0].State != "success" || reportOf(got).DeploymentState != "success" {
		t.Errorf("%+v, annotation %+v", d, reportOf(got))
	}
}

// Ruling on concern 4: a newer release that fails before it has its own
// Deployment (here a rollback still pending) never touches an older one.
func TestAPendingRollbackThatFailsLeavesThePreviousDeployment(t *testing.T) {
	v1, v2 := release(1, testCommit, "Push 0123456 by alice: before"), release(2, otherCommit, "Push 89abcde by alice: fix login")
	app := shopAt(otherCommit, shpyrdv1.PhaseRunning, v1, v2)
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	setAnnotations(t, c, app, func(a *shpyrdv1.App) {
		a.Spec.Image = v1.Image
		a.Annotations[shpyrdv1.AnnotationRollbackTo] = "1"
	})
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) { st.Phase, st.Message = shpyrdv1.PhaseFailed, "web: CrashLoopBackOff" })
	_, got := reconcileReport(t, r, app)
	if d := fake.Deployments(); len(d) != 1 || len(d[0].Statuses) != 1 || d[0].Statuses[0].State != "success" || reportOf(got).Release != 2 {
		t.Errorf("%+v, annotation %+v", d, reportOf(got))
	}
	if runs := fake.CheckRunsOn(otherCommit); len(runs) != 1 || runs[0].Conclusion != "success" {
		t.Errorf("the pin's check: %+v", runs)
	}
}

// Ruling on concern 5: a git App deployed by image gets no Deployment,
// and is no rollback.
func TestAGitAppDeployedByImageGetsNoDeployment(t *testing.T) {
	code := release(1, testCommit, "Push 0123456 by alice: fix login")
	app := shopAt(testCommit, shpyrdv1.PhaseRunning, code)
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	byImage := release(2, otherCommit, "Deploy")
	byImage.Image = "registry.example.test/shop:2"
	setAnnotations(t, c, app, func(a *shpyrdv1.App) { a.Spec.Image = byImage.Image })
	for _, phase := range []string{shpyrdv1.PhaseDeploying, shpyrdv1.PhaseFailed, shpyrdv1.PhaseRunning} {
		setStatus(t, c, app, func(st *shpyrdv1.AppStatus) { st.Phase, st.Releases = phase, []shpyrdv1.Release{code, byImage} })
		_, got := reconcileReport(t, r, app)
		if d := fake.Deployments(); len(d) != 1 || len(d[0].Statuses) != 1 || d[0].Statuses[0].State != "success" {
			t.Errorf("%s: %+v", phase, d)
		}
		if rolledBack(got) {
			t.Errorf("%s: a deploy by image is taken for a rollback", phase)
		}
	}
}

// states lists a Deployment's states in order.
func states(d githubtest.Deployment) []string {
	var out []string
	for _, s := range d.Statuses {
		out = append(out, s.State)
	}
	return out
}

// Fix round 1: a push during a rollout never leaves the replaced release's
// Deployment in progress: it is inactive once the newer release appears,
// and no Deployment is made while the newer one is to come.
func TestAPushDuringARolloutLeavesTheReplacedDeploymentInactive(t *testing.T) {
	b := release(1, testCommit, "Push 0123456 by alice: fix login")
	app := shopAt(testCommit, shpyrdv1.PhaseDeploying, b)
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	setAnnotations(t, c, app, func(a *shpyrdv1.App) {
		pinGit(a, otherCommit)
		withRecord(a, gitauth.SourceRecord{Ref: "main", Latest: otherCommit, Message: "again", Author: "alice", Via: "push"})
	})
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) { st.Phase = shpyrdv1.PhaseBuilding })
	reconcileReport(t, r, app)
	if d := fake.Deployments(); len(d) != 1 || fmt.Sprint(states(d[0])) != "[in_progress]" {
		t.Fatalf("while C builds: %+v", d)
	}
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
		st.Phase, st.Releases = shpyrdv1.PhaseDeploying, []shpyrdv1.Release{b, release(2, otherCommit, "Push 89abcde by alice: again")}
	})
	reconcileReport(t, r, app)
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) { st.Phase = shpyrdv1.PhaseRunning })
	_, got := reconcileReport(t, r, app)
	d := fake.Deployments()
	if len(d) != 2 || fmt.Sprint(states(d[0])) != "[in_progress inactive]" || d[0].Statuses[1].Description != "Replaced by release 2" {
		t.Errorf("B: %+v", d)
	}
	if len(d) != 2 || d[1].Release != 2 || fmt.Sprint(states(d[1])) != "[in_progress success]" || reportOf(got).Deployment != d[1].ID {
		t.Errorf("C: %+v, annotation %+v", d, reportOf(got))
	}
}

// Fix round 1: an outage that skips past a release in progress closes it
// once GitHub answers again.
func TestAnOutageThatSkipsAReleaseInProgressClosesIt(t *testing.T) {
	b := release(1, testCommit, "Push 0123456 by alice: fix login")
	app := shopAt(testCommit, shpyrdv1.PhaseDeploying, b)
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	fake.SetDown(true)
	setAnnotations(t, c, app, func(a *shpyrdv1.App) {
		pinGit(a, otherCommit)
		withRecord(a, gitauth.SourceRecord{Ref: "main", Latest: otherCommit, Message: "again", Author: "alice", Via: "push"})
	})
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
		st.Phase, st.Releases = shpyrdv1.PhaseRunning, []shpyrdv1.Release{b, release(2, otherCommit, "Push 89abcde by alice: again")}
	})
	if res, _ := reconcileReport(t, r, app); res.RequeueAfter == 0 {
		t.Fatal("the outage is not waited out")
	}
	fake.SetDown(false)
	advance(fake, time.Minute)
	reconcileReport(t, r, app)
	d := fake.Deployments()
	if len(d) != 2 || fmt.Sprint(states(d[0])) != "[in_progress inactive]" || d[0].Statuses[1].Description != "Replaced by release 2" ||
		fmt.Sprint(states(d[1])) != "[success]" {
		t.Errorf("%+v", d)
	}
}

// Fix round 1: no Deployment, not even one without a status, is made for
// an older release while a newer one is to come.
func TestNoDeploymentForAnOlderReleaseWhileANewerOneIsToCome(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseBuilding, release(1, otherCommit, "Push 89abcde by alice: before"))
	r, _, fake, _ := githubReporter(t, app)
	_, got := reconcileReport(t, r, app)
	if d := fake.Deployments(); len(d) != 0 || reportOf(got).Deployment != 0 {
		t.Errorf("%+v, annotation %+v", d, reportOf(got))
	}
}

// Fix round 1: a project shpyrd stops reporting on (a deploy of a fixed
// commit) has its Deployment in progress closed, and its report dropped.
func TestAnAppNoLongerReportedClosesItsDeploymentInProgress(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseDeploying, release(1, testCommit, "Push 0123456 by alice: fix login"))
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	setAnnotations(t, c, app, func(a *shpyrdv1.App) { a.Spec.Source.Git.Revision = testCommit })
	before := &shpyrdv1.App{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(app), before); err != nil {
		t.Fatal(err)
	}
	if !githubReportWatched(before) {
		t.Error("an App with a report is not watched")
	}
	_, got := reconcileReport(t, r, app)
	d := fake.Deployments()
	if len(d) != 1 || fmt.Sprint(states(d[0])) != "[in_progress inactive]" || d[0].Statuses[1].Description != "shpyrd stopped reporting on this project" {
		t.Errorf("%+v", d)
	}
	if _, ok := got.Annotations[shpyrdv1.AnnotationGitHubReport]; ok {
		t.Errorf("the report is kept: %+v", reportOf(got))
	}
	if githubReportWatched(got) {
		t.Error("an App with no report, not on a branch, is watched")
	}
}

// Fix round 1: a held commit's check after a branch switch the watcher
// already recorded is not "superseded" by the new branch's commit.
func TestAHeldCommitAfterABranchSwitchIsNotSuperseded(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseRunning, release(1, testCommit, "Push 0123456 by alice: fix login"))
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	setAnnotations(t, c, app, func(a *shpyrdv1.App) {
		withRecord(a, gitauth.SourceRecord{Ref: "main", Latest: otherCommit, Via: "push", Held: otherCommit, HeldSince: at(sourceNow)})
	})
	reconcileReport(t, r, app)
	setAnnotations(t, c, app, func(a *shpyrdv1.App) {
		a.Spec.Source.Git.Revision = "develop"
		withRecord(a, gitauth.SourceRecord{Ref: "develop", Latest: thirdCommit, Via: "poll"})
	})
	reconcileReport(t, r, app)
	if runs := fake.CheckRunsOn(otherCommit); len(runs) != 1 || runs[0].Status != "completed" || runs[0].Title != "No longer waiting: the project follows develop now" {
		t.Errorf("%+v", runs)
	}
}

// Fix round 1: the same commit redeployed and running repairs a failed
// Deployment; a release of config or sizes never fails a successful one.
func TestARedeployThatRunsRepairsAFailedDeployment(t *testing.T) {
	code := release(1, testCommit, "Push 0123456 by alice: fix login")
	app := shopAt(testCommit, shpyrdv1.PhaseFailed, code)
	app.Status.Message = "web: CrashLoopBackOff"
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	again := release(2, testCommit, "Restart")
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
		st.Phase, st.Releases = shpyrdv1.PhaseDeploying, []shpyrdv1.Release{code, again}
	})
	reconcileReport(t, r, app)
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) { st.Phase = shpyrdv1.PhaseRunning })
	reconcileReport(t, r, app)
	if d := fake.Deployments(); len(d) != 1 || fmt.Sprint(states(d[0])) != "[failure success]" {
		t.Errorf("repaired: %+v", d)
	}
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
		st.Phase, st.Releases = shpyrdv1.PhaseFailed, []shpyrdv1.Release{code, again, release(3, testCommit, "Set LOG_LEVEL")}
	})
	reconcileReport(t, r, app)
	if d := fake.Deployments(); len(d) != 1 || fmt.Sprint(states(d[0])) != "[failure success]" {
		t.Errorf("a config release failed a success: %+v", d)
	}
}

// inactives counts the inactive statuses posted.
func inactives(fake *githubtest.Server) int {
	n := 0
	for _, d := range fake.Deployments() {
		for _, s := range d.Statuses {
			if s.State == "inactive" {
				n++
			}
		}
	}
	return n
}

// Fix round 2: a project shpyrd stops reporting on has its unfinished
// check runs closed: a held commit's queued run here.
func TestAnAppNoLongerReportedClosesItsUnfinishedChecks(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseRunning, release(1, testCommit, "Push 0123456 by alice: fix login"))
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	setAnnotations(t, c, app, func(a *shpyrdv1.App) {
		withRecord(a, gitauth.SourceRecord{Ref: "main", Latest: otherCommit, Via: "push", Held: otherCommit, HeldSince: at(sourceNow)})
	})
	reconcileReport(t, r, app)
	setAnnotations(t, c, app, func(a *shpyrdv1.App) { a.Spec.Source.Git.Revision = testCommit })
	reconcileReport(t, r, app)
	if runs := fake.CheckRunsOn(otherCommit); len(runs) != 1 || runs[0].Status != "completed" || runs[0].Conclusion != "neutral" ||
		runs[0].Title != "shpyrd stopped reporting on this project" {
		t.Errorf("the held commit: %+v", runs)
	}
	if runs := fake.CheckRunsOn(testCommit); len(runs) != 1 || runs[0].Conclusion != "success" {
		t.Errorf("the finished check: %+v", runs)
	}
}

// Fix round 2: an App deleted mid-deploy has its check run and its
// Deployment closed, once, however many updates its finalization brings.
func TestAnAppDeletedMidDeployClosesItsReportsOnce(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseDeploying, release(1, testCommit, "Push 0123456 by alice: fix login"))
	app.Finalizers = []string{"shpyrd.io/test"}
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	if err := c.Delete(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		reconcileReport(t, r, app)
	}
	if runs := fake.CheckRunsOn(testCommit); len(runs) != 1 || runs[0].Status != "completed" || runs[0].Conclusion != "neutral" ||
		runs[0].Title != "shpyrd stopped reporting on this project" {
		t.Errorf("the check: %+v", runs)
	}
	if d := fake.Deployments(); len(d) != 1 || fmt.Sprint(states(d[0])) != "[in_progress inactive]" || inactives(fake) != 1 {
		t.Errorf("%+v", d)
	}
	got := &shpyrdv1.App{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(app), got); err != nil || got.DeletionTimestamp.IsZero() {
		t.Fatalf("%v", err)
	}
	if _, ok := got.Annotations[shpyrdv1.AnnotationGitHubReport]; ok {
		t.Errorf("the report is kept on a terminating App: %+v", reportOf(got))
	}
}

// Fix round 2: a failed patch of the dropped report never posts inactive
// twice.
func TestAFailedPatchWhenReportingStopsNeverClosesTwice(t *testing.T) {
	app := shopAt(testCommit, shpyrdv1.PhaseDeploying, release(1, testCommit, "Push 0123456 by alice: fix login"))
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	setAnnotations(t, c, app, func(a *shpyrdv1.App) { a.Spec.Source.Git.Revision = testCommit })
	inner := r.Client
	r.Client = interceptor.NewClient(inner.(client.WithWatch), interceptor.Funcs{
		Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
			return errors.New("the API server is away")
		},
	})
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(app)}); err == nil {
		t.Fatal("the failed patch is not an error")
	}
	r.Client = inner
	_, got := reconcileReport(t, r, app)
	if inactives(fake) != 1 {
		t.Errorf("%d inactive posts: %+v", inactives(fake), fake.Deployments())
	}
	if _, ok := got.Annotations[shpyrdv1.AnnotationGitHubReport]; ok {
		t.Errorf("the report is kept: %+v", reportOf(got))
	}
}

// Fix round 2: B runs on while the pinned commit's build failed: B's
// Deployment finishes from Ready, never stays in progress.
func TestAReleaseThatRunsWhileTheNextBuildFailedFinishesItsDeployment(t *testing.T) {
	b := release(1, testCommit, "Push 0123456 by alice: fix login")
	app := shopAt(testCommit, shpyrdv1.PhaseDeploying, b)
	r, c, fake, _ := githubReporter(t, app)
	reconcileReport(t, r, app)
	setAnnotations(t, c, app, func(a *shpyrdv1.App) {
		pinGit(a, otherCommit)
		withRecord(a, gitauth.SourceRecord{Ref: "main", Latest: otherCommit, Message: "again", Author: "alice", Via: "push"})
	})
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
		st.Phase, st.Message = shpyrdv1.PhaseFailed, "The build failed."
		meta.SetStatusCondition(&st.Conditions, metav1.Condition{Type: shpyrdv1.ConditionBuilt, Status: metav1.ConditionFalse, Reason: "BuildFailed", Message: "The build failed."})
	})
	reconcileReport(t, r, app)
	if d := fake.Deployments(); len(d) != 1 || fmt.Sprint(states(d[0])) != "[in_progress]" {
		t.Fatalf("while B rolls out: %+v", d)
	}
	setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
		meta.SetStatusCondition(&st.Conditions, metav1.Condition{Type: shpyrdv1.ConditionReady, Status: metav1.ConditionTrue, Reason: "Running", Message: "web: 1/1"})
	})
	_, got := reconcileReport(t, r, app)
	if d := fake.Deployments(); len(d) != 1 || fmt.Sprint(states(d[0])) != "[in_progress success]" || reportOf(got).DeploymentState != "success" {
		t.Errorf("%+v, annotation %+v", d, reportOf(got))
	}
}
