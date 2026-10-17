package controller

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr/funcr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
func TestAnUnknownCoverWaitsAndGivesUpNothing(t *testing.T) {
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
		writes := len(fake.Writes())
		for _, phase := range []string{shpyrdv1.PhaseDeploying, shpyrdv1.PhaseFailed, shpyrdv1.PhaseRunning} {
			setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
				st.Phase, st.Releases, st.Message = phase, []shpyrdv1.Release{code, config}, "the config release failed"
			})
			_, got := reconcileReport(t, r, app)
			if runs := fake.CheckRunsOn(testCommit); len(fake.Writes()) != writes || len(runs) != 1 || runs[0].Conclusion != "success" || reportOf(got).Check != "completed/success" {
				t.Errorf("%s: %d writes, %+v", phase, len(fake.Writes())-writes, runs)
			}
		}
	})
	t.Run("rollback", func(t *testing.T) {
		older := release(1, otherCommit, "Push 89abcde by alice: before")
		newest := release(2, testCommit, "Push 0123456 by alice: fix login")
		app := shopAt(testCommit, shpyrdv1.PhaseRunning, older, newest)
		r, c, fake, _ := githubReporter(t, app)
		reconcileReport(t, r, app)
		writes := len(fake.Writes())
		back := release(3, otherCommit, "Rollback to v1")
		for _, phase := range []string{shpyrdv1.PhaseDeploying, shpyrdv1.PhaseRunning, shpyrdv1.PhaseFailed} {
			setStatus(t, c, app, func(st *shpyrdv1.AppStatus) {
				st.Phase, st.Releases, st.Message = phase, []shpyrdv1.Release{older, newest, back}, "the rollback failed"
			})
			reconcileReport(t, r, app)
			if runs := fake.CheckRunsOn(testCommit); len(fake.Writes()) != writes || len(runs) != 1 || runs[0].Conclusion != "success" {
				t.Errorf("%s: %d writes, %+v", phase, len(fake.Writes())-writes, runs)
			}
		}
	})
}

func TestANotDeployedCommitsCheckSaysWhyInWords(t *testing.T) {
	for _, reason := range []string{gitauth.ReasonNoWatchedChanges, gitauth.ReasonCIFailedPrefix + "test", gitauth.ReasonCITimeout, gitauth.ReasonCINotCovered} {
		app := shopAt(testCommit, shpyrdv1.PhaseRunning, release(1, testCommit, "Push 0123456 by alice: fix login"))
		withRecord(app, gitauth.SourceRecord{Ref: "main", Latest: otherCommit, Message: "fix it", Author: "alice", Via: "push", NotDeployed: otherCommit, NotDeployedReason: reason})
		w, ok := wantCheck(app)
		if !ok || w.commit != otherCommit || w.conclusion != "neutral" || w.title != reason || !strings.Contains(w.summary, "Not deployed: "+reason+".") {
			t.Errorf("%s: %+v", reason, w)
		}
	}
}

func TestPermissionsNotGrantedBackOffAndAreLoggedOnceAMinute(t *testing.T) {
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
