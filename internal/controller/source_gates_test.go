package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr/funcr"
	corev1 "k8s.io/api/core/v1"
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

// gatedWatcher is the source watcher on a platform with the fake's App,
// its installation 42 on Acme connected; its clock is moved with advance.
func gatedWatcher(t *testing.T, ls *fakeLsRemote, app *shpyrdv1.App) (*SourceReconciler, *githubtest.Server) {
	t.Helper()
	fake, st := connectedGitHub(t)
	r, c := newSourceReconciler(t, ls, fake.Secret("shpyrd-system"), app)
	r.GitHub = &GitHubApp{Source: fake.Source(c, "shpyrd-system"), Installations: st}
	r.Now = fake.Time
	return r, fake
}

// advance moves the watcher's and the fake's clock.
func advance(fake *githubtest.Server, d time.Duration) { fake.SetNow(fake.Time().Add(d)) }

// shopWith is the shop app on github.com/acme/shop, its branch main
// pinned at testCommit, last seen through a webhook a minute ago.
func shopWith(git shpyrdv1.GitSource) *shpyrdv1.App {
	app := gitApp("shop", "https://github.com/acme/shop")
	git.URL = "https://github.com/acme/shop"
	app.Spec.Source.Git = &git
	pinGit(app, testCommit)
	return withRecord(app, gitauth.SourceRecord{Ref: "main", Latest: testCommit, Via: "push",
		CheckedAt: at(sourceNow.Add(-time.Minute)), LastWebhookAt: at(sourceNow.Add(-time.Minute))})
}

// pushed leaves a push of sha after before on app, as the webhook does.
func pushed(app *shpyrdv1.App, sha, before, message string, when time.Time) *shpyrdv1.App {
	return withPush(app, gitauth.Push{Ref: "refs/heads/main", SHA: sha, Before: before, Message: message, Author: "alice", At: when})
}

func calledGitHub(fake *githubtest.Server, prefix string) int {
	n := 0
	for _, c := range fake.Calls() {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func TestWatchPathsIgnoreACommitAndCountItInTheNextCompare(t *testing.T) {
	app := pushed(shopWith(shpyrdv1.GitSource{WatchPaths: []string{"services/api/**"}}), otherCommit, testCommit, "docs: typo", sourceNow)
	r, fake := gatedWatcher(t, &fakeLsRemote{}, app)
	heard := heardAudits(t)
	fake.SetCompare("acme/shop", testCommit, otherCommit, githubtest.Compare{Status: "ahead", Files: []githubtest.File{{Name: "README.md"}}})
	_, got := reconcileSource(t, r, app)
	rec := sourceRecord(got)
	if got.PinnedCommit() != testCommit || rec.NotDeployed != otherCommit || rec.NotDeployedReason != gitauth.ReasonNoWatchedChanges || rec.Latest != otherCommit {
		t.Fatalf("pin %s, record %+v", got.PinnedCommit(), rec)
	}
	if len(*heard) != 1 || (*heard)[0].Action != "git.skip" || (*heard)[0].Target != "89abcde" || (*heard)[0].Detail != gitauth.ReasonNoWatchedChanges {
		t.Errorf("audited %+v", *heard)
	}

	// The next push changes the API: the compare runs from the commit that
	// runs, so the README change counts with it.
	fake.SetCompare("acme/shop", testCommit, thirdCommit, githubtest.Compare{Status: "ahead", Files: []githubtest.File{{Name: "README.md"}, {Name: "services/api/main.go"}}})
	got = pushed(got, thirdCommit, otherCommit, "fix the api", sourceNow.Add(time.Minute))
	if err := r.Update(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	advance(fake, time.Minute)
	_, got = reconcileSource(t, r, got)
	rec = sourceRecord(got)
	if got.PinnedCommit() != thirdCommit || rec.NotDeployed != "" || got.Annotations[shpyrdv1.AnnotationReleaseNote] != "Push fedcba9 by alice: fix the api" {
		t.Fatalf("pin %s, note %q, record %+v", got.PinnedCommit(), got.Annotations[shpyrdv1.AnnotationReleaseNote], rec)
	}
	if calledGitHub(fake, "GET /repos/acme/shop/compare/"+testCommit+"..."+thirdCommit) != 1 {
		t.Errorf("calls %v", fake.Calls())
	}
}

func TestWatchPathsAreNotCheckedWhenGitHubsListCannotBeTrusted(t *testing.T) {
	var many []githubtest.File
	for i := range 300 {
		many = append(many, githubtest.File{Name: fmt.Sprintf("docs/%03d.md", i)})
	}
	for _, c := range []struct {
		name    string
		compare *githubtest.Compare
		note    string
	}{
		{"a commit GitHub does not know", nil, "GitHub's compare failed"},
		{"300 files", &githubtest.Compare{Status: "ahead", Files: many}, "300 or more files changed"},
		{"a rewritten history", &githubtest.Compare{Status: "diverged", Files: []githubtest.File{{Name: "README.md"}}}, "the branch's history was rewritten"},
		{"a branch moved back", &githubtest.Compare{Status: "behind"}, "the branch's history was rewritten"},
	} {
		app := pushed(shopWith(shpyrdv1.GitSource{WatchPaths: []string{"services/api/**"}}), otherCommit, testCommit, "fix login", sourceNow)
		r, fake := gatedWatcher(t, &fakeLsRemote{}, app)
		if c.compare != nil {
			fake.SetCompare("acme/shop", testCommit, otherCommit, *c.compare)
		}
		_, got := reconcileSource(t, r, app)
		want := "Push 89abcde by alice: fix login (watch paths not checked: " + c.note + ")"
		if got.PinnedCommit() != otherCommit || got.Annotations[shpyrdv1.AnnotationReleaseNote] != want {
			t.Errorf("%s: pin %s, note %q", c.name, got.PinnedCommit(), got.Annotations[shpyrdv1.AnnotationReleaseNote])
		}
	}
}

func TestAPersonsDeployIgnoresWatchPathsAndWaitForCI(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	app.Spec.Source.Git.WatchPaths, app.Spec.Source.Git.WaitForChecks = []string{"services/api/**"}, true
	app.Annotations = map[string]string{shpyrdv1.AnnotationDeployedBy: "ana@acme.test"}
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}
	r, fake := gatedWatcher(t, ls, app)
	_, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != otherCommit || sourceRecord(got).Held != "" {
		t.Errorf("pin %s, record %+v", got.PinnedCommit(), sourceRecord(got))
	}
	if n := calledGitHub(fake, "GET /repos/"); n != 0 {
		t.Errorf("a person's deploy asks GitHub nothing about the commit: %v", fake.Calls())
	}
}

func TestTheFirstCommitOfABranchIgnoresWatchPaths(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	app.Spec.Source.Git.WatchPaths = []string{"services/api/**"}
	r, fake := gatedWatcher(t, &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}, app)
	if _, got := reconcileSource(t, r, app); got.PinnedCommit() != otherCommit || calledGitHub(fake, "GET /repos/") != 0 {
		t.Errorf("pin %s, calls %v", got.PinnedCommit(), fake.Calls())
	}
}

func TestWatchPathsAreIgnoredWhereTheGitHubAppDoesNotReach(t *testing.T) {
	app := pushed(shopWith(shpyrdv1.GitSource{WatchPaths: []string{"services/api/**"}}), otherCommit, testCommit, "docs", sourceNow)
	r, _ := newSourceReconciler(t, &fakeLsRemote{}, app)
	if _, got := reconcileSource(t, r, app); got.PinnedCommit() != otherCommit || got.Annotations[shpyrdv1.AnnotationReleaseNote] != "Push 89abcde by alice: docs" {
		t.Errorf("without the App every push deploys: pin %s, note %q", got.PinnedCommit(), got.Annotations[shpyrdv1.AnnotationReleaseNote])
	}
}

func TestACommitNotDeployedStaysSoWhenTheBranchIsReadAgain(t *testing.T) {
	app := shopWith(shpyrdv1.GitSource{WatchPaths: []string{"services/api/**"}})
	withRecord(app, gitauth.SourceRecord{Ref: "main", Latest: otherCommit, Via: "push", NotDeployed: otherCommit,
		NotDeployedReason: gitauth.ReasonNoWatchedChanges, CheckedAt: at(sourceNow.Add(-3 * time.Hour))})
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}
	r, fake := gatedWatcher(t, ls, app)
	_, got := reconcileSource(t, r, app)
	if ls.calls != 1 || got.PinnedCommit() != testCommit || sourceRecord(got).NotDeployed != otherCommit || calledGitHub(fake, "GET /repos/") != 0 {
		t.Errorf("listed %d, pin %s, record %+v, calls %v", ls.calls, got.PinnedCommit(), sourceRecord(got), fake.Calls())
	}
}

// unreadableInstallations is a store whose GitHub connections cannot be
// read.
type unreadableInstallations struct{ store.GitHub }

func (unreadableInstallations) GitHubInstallations(context.Context, string) ([]store.GitHubInstallation, error) {
	return nil, errors.New("the database is down")
}

func TestWatchPathsAreNotCheckedWhenShpyrdCannotTellWhetherTheAppCoversTheRepository(t *testing.T) {
	app := pushed(shopWith(shpyrdv1.GitSource{WatchPaths: []string{"services/api/**"}}), otherCommit, testCommit, "fix login", sourceNow)
	r, fake := gatedWatcher(t, &fakeLsRemote{}, app)
	r.GitHub.Installations = unreadableInstallations{r.GitHub.Installations}
	fake.SetCompare("acme/shop", testCommit, otherCommit, githubtest.Compare{Status: "ahead", Files: []githubtest.File{{Name: "README.md"}}})
	_, got := reconcileSource(t, r, app)
	want := "Push 89abcde by alice: fix login (watch paths not checked: shpyrd could not read the workspace's GitHub connections)"
	if got.PinnedCommit() != otherCommit || got.Annotations[shpyrdv1.AnnotationReleaseNote] != want || sourceRecord(got).NotDeployed != "" {
		t.Errorf("an unknown cover is not \"not covered\": pin %s, note %q, record %+v", got.PinnedCommit(), got.Annotations[shpyrdv1.AnnotationReleaseNote], sourceRecord(got))
	}
	if calledGitHub(fake, "GET /repos/") != 0 {
		t.Errorf("calls %v", fake.Calls())
	}
}

func TestTheWatchersFailuresLeaveTheClonesBackoffAndRefusalsAlone(t *testing.T) {
	app := pushed(shopWith(shpyrdv1.GitSource{WatchPaths: []string{"services/api/**"}}), otherCommit, testCommit, "fix login", sourceNow)
	r, fake := gatedWatcher(t, &fakeLsRemote{}, app)
	fake.SetDown(true)
	_, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != otherCommit || !strings.HasSuffix(got.Annotations[shpyrdv1.AnnotationReleaseNote], "(watch paths not checked: GitHub's compare failed)") {
		t.Fatalf("pin %s, note %q", got.PinnedCommit(), got.Annotations[shpyrdv1.AnnotationReleaseNote])
	}
	if r.GitHub.backingOff(42) {
		t.Error("a failed compare backs the clone off")
	}
	if _, refused := r.GitHub.refusedLately(42, "shop"); refused {
		t.Error("a failed compare records a refusal")
	}
}

func TestACloneFallsBackToTheOtherCredentialsWhenTheConnectionsCannotBeRead(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	r, c, _, st := githubReconciler(t, app, workspaceToken("default", "github.com", "ghp_ours"))
	r.Config.GitHub.Installations = unreadableInstallations{st}
	b, err := lookupGitBuild(context.Background(), c, "shpyrd-system", r.Config.GitHub, app)
	if err != nil || b.Kind != gitauth.KindToken || b.AppRefused != "" {
		t.Errorf("build = %+v, %v", b, err)
	}
}

// nudgeCI leaves on app what the webhook leaves when sha's checks change.
func nudgeCI(t *testing.T, c client.Client, app *shpyrdv1.App, sha string, when time.Time) {
	t.Helper()
	raw, _ := json.Marshal(gitauth.CINudge{SHA: sha, Delivery: "d-ci", At: when})
	patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{shpyrdv1.AnnotationGitCI: string(raw)}}})
	if err := c.Patch(context.Background(), app, client.RawPatch(types.MergePatchType, patch)); err != nil {
		t.Fatal(err)
	}
}

func ciRun(name, status, conclusion string) githubtest.CheckRun {
	return githubtest.CheckRun{Name: name, Status: status, Conclusion: conclusion, AppID: 99}
}

func waitingShop() *shpyrdv1.App {
	return pushed(shopWith(shpyrdv1.GitSource{WaitForChecks: true}), otherCommit, testCommit, "fix login", sourceNow)
}

func TestAHeldCommitDeploysOnceItsCIPasses(t *testing.T) {
	app := waitingShop()
	r, fake := gatedWatcher(t, &fakeLsRemote{}, app)
	// shpyrd's own queued check is on the commit too: it is left out.
	fake.SetCheckRuns(otherCommit, ciRun("test", "in_progress", ""), githubtest.CheckRun{Name: "shpyrd / shop", Status: "queued", AppID: githubtest.AppID})
	res, got := reconcileSource(t, r, app)
	rec := sourceRecord(got)
	if got.PinnedCommit() != testCommit || rec.Held != otherCommit || rec.HeldSince == nil || !rec.HeldSince.Equal(sourceNow) {
		t.Fatalf("pin %s, record %+v", got.PinnedCommit(), rec)
	}
	if res.RequeueAfter <= 0 || res.RequeueAfter > heldRecheck {
		t.Errorf("a held commit is read again within 30 s: %v", res.RequeueAfter)
	}
	// Too soon: GitHub is not asked again.
	reads := calledGitHub(fake, "GET /repos/acme/shop/commits/")
	advance(fake, 5*time.Second)
	if _, again := reconcileSource(t, r, got); again.PinnedCommit() != testCommit || calledGitHub(fake, "GET /repos/acme/shop/commits/") != reads {
		t.Errorf("read again after 5 s: %v", fake.Calls())
	}
	fake.SetCheckRuns(otherCommit, ciRun("test", "completed", "success"), githubtest.CheckRun{Name: "shpyrd / shop", Status: "queued", AppID: githubtest.AppID})
	advance(fake, 25*time.Second)
	_, got = reconcileSource(t, r, got)
	rec = sourceRecord(got)
	if got.PinnedCommit() != otherCommit || rec.Held != "" || got.Annotations[shpyrdv1.AnnotationReleaseNote] != "Push 89abcde by alice: fix login (CI passed)" {
		t.Fatalf("pin %s, note %q, record %+v", got.PinnedCommit(), got.Annotations[shpyrdv1.AnnotationReleaseNote], rec)
	}
	if rec.CheckedAt == nil || !rec.CheckedAt.Equal(sourceNow) {
		t.Errorf("reading the checks is no check of the branch: %v", rec.CheckedAt)
	}
}

func TestACommitCIFailedDeploysWhenARerunPasses(t *testing.T) {
	app := waitingShop()
	r, fake := gatedWatcher(t, &fakeLsRemote{}, app)
	heard := heardAudits(t)
	fake.SetCheckRuns(otherCommit, ciRun("lint", "completed", "success"), ciRun("test", "completed", "failure"))
	_, got := reconcileSource(t, r, app)
	rec := sourceRecord(got)
	if got.PinnedCommit() != testCommit || rec.Held != "" || rec.NotDeployed != otherCommit || rec.NotDeployedReason != "CI failed: test" {
		t.Fatalf("pin %s, record %+v", got.PinnedCommit(), rec)
	}
	if len(*heard) != 1 || (*heard)[0].Action != "git.skip" || (*heard)[0].Detail != "CI failed: test" {
		t.Errorf("audited %+v", *heard)
	}
	// Without a change of its checks, the failed commit is not read again.
	reads := calledGitHub(fake, "GET /repos/acme/shop/commits/")
	advance(fake, time.Minute)
	if _, got = reconcileSource(t, r, got); calledGitHub(fake, "GET /repos/acme/shop/commits/") != reads {
		t.Errorf("read again without a nudge: %v", fake.Calls())
	}
	// Re-run on GitHub: green; the webhook nudges.
	fake.SetCheckRuns(otherCommit, ciRun("lint", "completed", "success"), ciRun("test", "completed", "success"))
	nudgeCI(t, r.Client, got, otherCommit, fake.Time())
	_, got = reconcileSource(t, r, got)
	if got.PinnedCommit() != otherCommit || got.Annotations[shpyrdv1.AnnotationReleaseNote] != "Push 89abcde by alice: fix login (CI passed)" {
		t.Fatalf("pin %s, note %q, record %+v", got.PinnedCommit(), got.Annotations[shpyrdv1.AnnotationReleaseNote], sourceRecord(got))
	}
	if _, ok := got.Annotations[shpyrdv1.AnnotationGitCI]; ok {
		t.Error("the nudge is dropped once read")
	}
}

func TestAHeldCommitIsGivenUpAfterAnHour(t *testing.T) {
	app := waitingShop()
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}
	r, fake := gatedWatcher(t, ls, app)
	heard := heardAudits(t)
	fake.SetCheckRuns(otherCommit, ciRun("test", "in_progress", ""))
	_, got := reconcileSource(t, r, app)
	advance(fake, 59*time.Minute)
	if _, got = reconcileSource(t, r, got); sourceRecord(got).Held != otherCommit {
		t.Fatalf("59 minutes: %+v", sourceRecord(got))
	}
	advance(fake, time.Minute)
	_, got = reconcileSource(t, r, got)
	rec := sourceRecord(got)
	if got.PinnedCommit() != testCommit || rec.Held != "" || rec.NotDeployed != otherCommit || rec.NotDeployedReason != gitauth.ReasonCITimeout {
		t.Fatalf("pin %s, record %+v", got.PinnedCommit(), rec)
	}
	if n := len(*heard); n != 1 || (*heard)[0].Detail != gitauth.ReasonCITimeout {
		t.Errorf("audited %+v", *heard)
	}
}

func TestANewerPushReplacesAHeldCommitAtOnce(t *testing.T) {
	app := waitingShop()
	r, fake := gatedWatcher(t, &fakeLsRemote{}, app)
	fake.SetCheckRuns(otherCommit, ciRun("test", "in_progress", ""))
	_, got := reconcileSource(t, r, app)
	fake.SetCheckRuns(thirdCommit, ciRun("test", "completed", "success"))
	got = pushed(got, thirdCommit, otherCommit, "fix it again", sourceNow.Add(10*time.Second))
	if err := r.Update(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	advance(fake, 10*time.Second)
	_, got = reconcileSource(t, r, got)
	if rec := sourceRecord(got); got.PinnedCommit() != thirdCommit || rec.Held != "" || rec.Latest != thirdCommit {
		t.Errorf("pin %s, record %+v", got.PinnedCommit(), rec)
	}
}

func TestACommitWithNoCIDeploysAfterAMinute(t *testing.T) {
	app := waitingShop()
	r, fake := gatedWatcher(t, &fakeLsRemote{}, app)
	_, got := reconcileSource(t, r, app)
	if sourceRecord(got).Held != otherCommit {
		t.Fatalf("held first: %+v", sourceRecord(got))
	}
	advance(fake, 30*time.Second)
	if _, got = reconcileSource(t, r, got); got.PinnedCommit() != testCommit {
		t.Fatal("30 s: CI may still report")
	}
	advance(fake, 30*time.Second)
	_, got = reconcileSource(t, r, got)
	if got.PinnedCommit() != otherCommit || got.Annotations[shpyrdv1.AnnotationReleaseNote] != "Push 89abcde by alice: fix login (no CI)" {
		t.Errorf("pin %s, note %q", got.PinnedCommit(), got.Annotations[shpyrdv1.AnnotationReleaseNote])
	}
}

func TestAHeldCommitStaysHeldWhileGitHubIsDown(t *testing.T) {
	app := waitingShop()
	r, fake := gatedWatcher(t, &fakeLsRemote{}, app)
	fake.SetCheckRuns(otherCommit, ciRun("test", "in_progress", ""))
	_, got := reconcileSource(t, r, app)
	fake.SetDown(true)
	for range 4 {
		advance(fake, 30*time.Second)
		_, got = reconcileSource(t, r, got)
	}
	if rec := sourceRecord(got); got.PinnedCommit() != testCommit || rec.Held != otherCommit || rec.NotDeployed != "" {
		t.Fatalf("two minutes of GitHub down is not 'no CI': pin %s, record %+v", got.PinnedCommit(), rec)
	}
	fake.SetDown(false)
	fake.SetCheckRuns(otherCommit, ciRun("test", "completed", "success"))
	advance(fake, 2*time.Minute)
	if _, got = reconcileSource(t, r, got); got.PinnedCommit() != otherCommit {
		t.Errorf("green once GitHub answers: pin %s, record %+v", got.PinnedCommit(), sourceRecord(got))
	}
}

func TestWaitForCIComesAfterTheWatchPaths(t *testing.T) {
	app := pushed(shopWith(shpyrdv1.GitSource{WatchPaths: []string{"services/api/**"}, WaitForChecks: true}), otherCommit, testCommit, "docs", sourceNow)
	r, fake := gatedWatcher(t, &fakeLsRemote{}, app)
	fake.SetCompare("acme/shop", testCommit, otherCommit, githubtest.Compare{Status: "ahead", Files: []githubtest.File{{Name: "README.md"}}})
	_, got := reconcileSource(t, r, app)
	if rec := sourceRecord(got); rec.NotDeployedReason != gitauth.ReasonNoWatchedChanges || calledGitHub(fake, "GET /repos/acme/shop/commits/") != 0 {
		t.Errorf("record %+v, calls %v", rec, fake.Calls())
	}
}

func TestAHeldCommitStaysHeldWhenShpyrdCannotTellWhetherTheAppCoversTheRepository(t *testing.T) {
	// The workspace's connections cannot be read: a new commit is held, not
	// deployed as "no CI".
	app := waitingShop()
	r, fake := gatedWatcher(t, &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}, app)
	st := r.GitHub.Installations
	r.GitHub.Installations = unreadableInstallations{st}
	_, got := reconcileSource(t, r, app)
	for range 3 {
		advance(fake, 30*time.Second)
		_, got = reconcileSource(t, r, got)
	}
	if rec := sourceRecord(got); got.PinnedCommit() != testCommit || rec.Held != otherCommit || rec.NotDeployed != "" {
		t.Fatalf("an unreadable store is not 'no CI': pin %s, record %+v", got.PinnedCommit(), rec)
	}
	if calledGitHub(fake, "GET /repos/") != 0 {
		t.Errorf("calls %v", fake.Calls())
	}

	// The store answers again, then the App's Secret breaks: still held.
	r.GitHub.Installations = st
	fake.SetCheckRuns(otherCommit, ciRun("test", "in_progress", ""))
	advance(fake, 30*time.Second)
	if _, got = reconcileSource(t, r, got); sourceRecord(got).Held != otherCommit {
		t.Fatalf("pending: %+v", sourceRecord(got))
	}
	sec := &corev1.Secret{}
	key := client.ObjectKeyFromObject(fake.Secret("shpyrd-system"))
	if err := r.Get(context.Background(), key, sec); err != nil {
		t.Fatal(err)
	}
	sec.Data[github.KeyPrivateKey] = []byte("not a key")
	if err := r.Update(context.Background(), sec); err != nil {
		t.Fatal(err)
	}
	fake.SetCheckRuns(otherCommit, ciRun("test", "completed", "success"))
	advance(fake, github.Recheck+time.Second)
	if _, got = reconcileSource(t, r, got); got.PinnedCommit() != testCommit || sourceRecord(got).Held != otherCommit {
		t.Fatalf("a broken Secret is not 'CI passed': pin %s, record %+v", got.PinnedCommit(), sourceRecord(got))
	}

	// The hour still runs.
	advance(fake, time.Hour)
	_, got = reconcileSource(t, r, got)
	if rec := sourceRecord(got); got.PinnedCommit() != testCommit || rec.Held != "" || rec.NotDeployed != otherCommit || rec.NotDeployedReason != gitauth.ReasonCITimeout {
		t.Errorf("pin %s, record %+v", got.PinnedCommit(), rec)
	}
}

func TestAHeldCommitStaysHeldWhenItsChecksAreTooManyToRead(t *testing.T) {
	app := waitingShop()
	r, fake := gatedWatcher(t, &fakeLsRemote{}, app)
	// GitHub says there are more runs than its pages give: the listing is
	// cut short.
	fake.SetCheckRuns(otherCommit, ciRun("test", "completed", "success"))
	fake.SetCheckRunsTotal(otherCommit, 5000)
	_, got := reconcileSource(t, r, app)
	advance(fake, 2*time.Minute)
	_, got = reconcileSource(t, r, got)
	if rec := sourceRecord(got); got.PinnedCommit() != testCommit || rec.Held != otherCommit {
		t.Errorf("a listing cut short is not 'CI passed': pin %s, record %+v", got.PinnedCommit(), rec)
	}
	if r.GitHub.backingOff(42) {
		t.Error("the watcher's failure backs the clone off")
	}
}

func TestAutoDeploySwitchedOnWaitsForTheCommitsCI(t *testing.T) {
	off := false
	app := waitingShop()
	app.Spec.Source.Git.AutoDeploy = &off
	r, fake := gatedWatcher(t, &fakeLsRemote{}, app)
	fake.SetCheckRuns(otherCommit, ciRun("test", "completed", "failure"))
	_, got := reconcileSource(t, r, app)
	if rec := sourceRecord(got); got.PinnedCommit() != testCommit || rec.Latest != otherCommit || rec.Held != "" {
		t.Fatalf("auto-deploy off: pin %s, record %+v", got.PinnedCommit(), rec)
	}
	on := true
	got.Spec.Source.Git.AutoDeploy = &on
	if err := r.Update(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	advance(fake, 10*time.Second)
	_, got = reconcileSource(t, r, got)
	if rec := sourceRecord(got); got.PinnedCommit() != testCommit || rec.NotDeployed != otherCommit || rec.NotDeployedReason != "CI failed: test" {
		t.Fatalf("CI red: pin %s, record %+v", got.PinnedCommit(), rec)
	}
	// Green after a re-run: it deploys, as auto-deploy switched on.
	fake.SetCheckRuns(otherCommit, ciRun("test", "completed", "success"))
	nudgeCI(t, r.Client, got, otherCommit, fake.Time())
	_, got = reconcileSource(t, r, got)
	if got.PinnedCommit() != otherCommit {
		t.Errorf("pin %s, record %+v", got.PinnedCommit(), sourceRecord(got))
	}
}

func TestAutoDeploySwitchedOnChecksTheWatchPaths(t *testing.T) {
	off := false
	app := pushed(shopWith(shpyrdv1.GitSource{WatchPaths: []string{"services/api/**"}}), otherCommit, testCommit, "docs", sourceNow)
	app.Spec.Source.Git.AutoDeploy = &off
	r, fake := gatedWatcher(t, &fakeLsRemote{}, app)
	fake.SetCompare("acme/shop", testCommit, otherCommit, githubtest.Compare{Status: "ahead", Files: []githubtest.File{{Name: "README.md"}}})
	_, got := reconcileSource(t, r, app)
	on := true
	got.Spec.Source.Git.AutoDeploy = &on
	if err := r.Update(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	_, got = reconcileSource(t, r, got)
	if rec := sourceRecord(got); got.PinnedCommit() != testCommit || rec.NotDeployed != otherCommit || rec.NotDeployedReason != gitauth.ReasonNoWatchedChanges {
		t.Errorf("pin %s, record %+v", got.PinnedCommit(), rec)
	}
}

// Fix round 1.

func TestARerunsNudgeIsReadWhenTheBranchIsDue(t *testing.T) {
	for _, c := range []struct {
		name string
		wait func(r *SourceReconciler, fake *githubtest.Server)
	}{
		{"the poll interval passed", func(_ *SourceReconciler, fake *githubtest.Server) { advance(fake, 2*time.Hour) }},
		{"the controller restarted", func(r *SourceReconciler, fake *githubtest.Server) {
			advance(fake, time.Minute)
			r.checked, r.ciChecked = nil, nil
		}},
	} {
		app := waitingShop()
		r, fake := gatedWatcher(t, &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}, app)
		fake.SetCheckRuns(otherCommit, ciRun("test", "completed", "failure"))
		_, got := reconcileSource(t, r, app)
		if rec := sourceRecord(got); rec.NotDeployed != otherCommit {
			t.Fatalf("%s: record %+v", c.name, rec)
		}
		fake.SetCheckRuns(otherCommit, ciRun("test", "completed", "success"))
		nudgeCI(t, r.Client, got, otherCommit, fake.Time())
		c.wait(r, fake)
		_, got = reconcileSource(t, r, got)
		if got.PinnedCommit() != otherCommit || got.Annotations[shpyrdv1.AnnotationReleaseNote] != "Push 89abcde by alice: fix login (CI passed)" {
			t.Errorf("%s: pin %s, note %q, record %+v", c.name, got.PinnedCommit(), got.Annotations[shpyrdv1.AnnotationReleaseNote], sourceRecord(got))
		}
		if _, ok := got.Annotations[shpyrdv1.AnnotationGitCI]; ok {
			t.Errorf("%s: the nudge is dropped once read", c.name)
		}
	}
}

func TestAHeldCommitKeepsItsWatchPathsNoteWhenItDeploys(t *testing.T) {
	var many []githubtest.File
	for i := range 300 {
		many = append(many, githubtest.File{Name: fmt.Sprintf("docs/%03d.md", i)})
	}
	app := pushed(shopWith(shpyrdv1.GitSource{WatchPaths: []string{"services/api/**"}, WaitForChecks: true}), otherCommit, testCommit, "fix login", sourceNow)
	r, fake := gatedWatcher(t, &fakeLsRemote{}, app)
	fake.SetCompare("acme/shop", testCommit, otherCommit, githubtest.Compare{Status: "ahead", Files: many})
	fake.SetCheckRuns(otherCommit, ciRun("test", "in_progress", ""))
	_, got := reconcileSource(t, r, app)
	if rec := sourceRecord(got); rec.Held != otherCommit {
		t.Fatalf("record %+v", rec)
	}
	fake.SetCheckRuns(otherCommit, ciRun("test", "completed", "success"))
	advance(fake, 30*time.Second)
	_, got = reconcileSource(t, r, got)
	want := "Push 89abcde by alice: fix login (watch paths not checked: 300 or more files changed) (CI passed)"
	if got.PinnedCommit() != otherCommit || got.Annotations[shpyrdv1.AnnotationReleaseNote] != want {
		t.Errorf("pin %s, note %q", got.PinnedCommit(), got.Annotations[shpyrdv1.AnnotationReleaseNote])
	}
}

func TestAHeldCommitsWatchPathsAreCheckedOnceTheCoverIsKnown(t *testing.T) {
	app := pushed(shopWith(shpyrdv1.GitSource{WatchPaths: []string{"services/api/**"}, WaitForChecks: true}), otherCommit, testCommit, "docs", sourceNow)
	r, fake := gatedWatcher(t, &fakeLsRemote{}, app)
	st := r.GitHub.Installations
	r.GitHub.Installations = unreadableInstallations{st}
	fake.SetCompare("acme/shop", testCommit, otherCommit, githubtest.Compare{Status: "ahead", Files: []githubtest.File{{Name: "README.md"}}})
	fake.SetCheckRuns(otherCommit, ciRun("test", "in_progress", ""))
	_, got := reconcileSource(t, r, app)
	if rec := sourceRecord(got); rec.Held != otherCommit {
		t.Fatalf("held while the cover is unknown: %+v", rec)
	}
	r.GitHub.Installations = st
	advance(fake, 30*time.Second)
	_, got = reconcileSource(t, r, got)
	if rec := sourceRecord(got); got.PinnedCommit() != testCommit || rec.Held != "" || rec.NotDeployed != otherCommit || rec.NotDeployedReason != gitauth.ReasonNoWatchedChanges {
		t.Errorf("pin %s, record %+v", got.PinnedCommit(), rec)
	}
}

func TestAHeldCommitStaysHeldWhenTheAppNoLongerCoversTheRepository(t *testing.T) {
	for _, c := range []struct {
		name string
		lose func(r *SourceReconciler, fake *githubtest.Server)
	}{
		{"the installation is suspended", func(r *SourceReconciler, _ *githubtest.Server) {
			if err := r.GitHub.Installations.SetGitHubInstallationState(context.Background(), 42, true, false); err != nil {
				t.Fatal(err)
			}
		}},
		{"the App's Secret is deleted", func(r *SourceReconciler, fake *githubtest.Server) {
			if err := r.Delete(context.Background(), fake.Secret("shpyrd-system")); err != nil {
				t.Fatal(err)
			}
			advance(fake, github.Recheck)
		}},
	} {
		app := waitingShop()
		r, fake := gatedWatcher(t, &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}, app)
		heard := heardAudits(t)
		fake.SetCheckRuns(otherCommit, ciRun("test", "in_progress", ""))
		_, got := reconcileSource(t, r, app)
		c.lose(r, fake)
		fake.SetCheckRuns(otherCommit, ciRun("test", "completed", "success"))
		advance(fake, 30*time.Second)
		if _, got = reconcileSource(t, r, got); got.PinnedCommit() != testCommit || sourceRecord(got).Held != otherCommit {
			t.Fatalf("%s: deployed unchecked: pin %s, record %+v", c.name, got.PinnedCommit(), sourceRecord(got))
		}
		advance(fake, time.Hour)
		_, got = reconcileSource(t, r, got)
		rec := sourceRecord(got)
		if got.PinnedCommit() != testCommit || rec.Held != "" || rec.NotDeployed != otherCommit || rec.NotDeployedReason != gitauth.ReasonCINotCovered {
			t.Fatalf("%s: pin %s, record %+v", c.name, got.PinnedCommit(), rec)
		}
		if n := len(*heard); n != 1 || (*heard)[0].Action != "git.skip" || (*heard)[0].Detail != gitauth.ReasonCINotCovered {
			t.Errorf("%s: audited %+v", c.name, *heard)
		}
		// A commit pushed after the App stopped covering the repository
		// deploys: the settings do nothing.
		got = pushed(got, thirdCommit, otherCommit, "next", fake.Time())
		if err := r.Update(context.Background(), got); err != nil {
			t.Fatal(err)
		}
		_, got = reconcileSource(t, r, got)
		if got.PinnedCommit() != thirdCommit || got.Annotations[shpyrdv1.AnnotationReleaseNote] != "Push fedcba9 by alice: next" {
			t.Errorf("%s: pin %s, note %q", c.name, got.PinnedCommit(), got.Annotations[shpyrdv1.AnnotationReleaseNote])
		}
	}
}

func TestUnreadableChecksAreLoggedAtMostOnceAMinute(t *testing.T) {
	app := waitingShop()
	r, fake := gatedWatcher(t, &fakeLsRemote{}, app)
	fake.SetDown(true)
	var buf strings.Builder
	ctx := log.IntoContext(context.Background(), funcr.New(func(prefix, args string) { buf.WriteString(prefix + args + "\n") }, funcr.Options{}))
	for range 5 {
		if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(app)}); err != nil {
			t.Fatal(err)
		}
		advance(fake, 30*time.Second)
	}
	if n := strings.Count(buf.String(), "could not read the commit's checks"); n != 3 {
		t.Errorf("%d lines in two minutes:\n%s", n, buf.String())
	}
}

// Fix round 2.

func TestACommitCIFailedStaysNotDeployedWhileTheAppNoLongerCoversTheRepository(t *testing.T) {
	app := waitingShop()
	r, fake := gatedWatcher(t, &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}, app)
	heard := heardAudits(t)
	fake.SetCheckRuns(otherCommit, ciRun("test", "completed", "failure"))
	_, got := reconcileSource(t, r, app)
	if rec := sourceRecord(got); rec.NotDeployed != otherCommit {
		t.Fatalf("record %+v", rec)
	}
	ctx := context.Background()
	if err := r.GitHub.Installations.SetGitHubInstallationState(ctx, 42, true, false); err != nil {
		t.Fatal(err)
	}
	fake.SetCheckRuns(otherCommit, ciRun("test", "completed", "success"))
	nudgeCI(t, r.Client, got, otherCommit, fake.Time())
	check := func(when string) {
		t.Helper()
		rec := sourceRecord(got)
		if got.PinnedCommit() != testCommit || rec.NotDeployed != otherCommit || rec.NotDeployedReason != "CI failed: test" || rec.Held != "" {
			t.Fatalf("%s: deployed unchecked: pin %s, record %+v", when, got.PinnedCommit(), rec)
		}
		if len(*heard) != 1 {
			t.Errorf("%s: audited again %+v", when, *heard)
		}
	}
	// The nudge path.
	advance(fake, time.Minute)
	_, got = reconcileSource(t, r, got)
	check("nudged")
	if _, ok := got.Annotations[shpyrdv1.AnnotationGitCI]; !ok {
		t.Fatal("a nudge whose checks were not read is kept")
	}
	// The poll due, the nudge still there.
	advance(fake, 2*time.Hour)
	_, got = reconcileSource(t, r, got)
	check("polled")
	// Covered again: the kept nudge is read, and the green re-run deploys.
	if err := r.GitHub.Installations.SetGitHubInstallationState(ctx, 42, false, false); err != nil {
		t.Fatal(err)
	}
	advance(fake, time.Minute)
	_, got = reconcileSource(t, r, got)
	if got.PinnedCommit() != otherCommit {
		t.Errorf("covered again: pin %s, record %+v", got.PinnedCommit(), sourceRecord(got))
	}
	if _, ok := got.Annotations[shpyrdv1.AnnotationGitCI]; ok {
		t.Error("the nudge is dropped once read")
	}
}
