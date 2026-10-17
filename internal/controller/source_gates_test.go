package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
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
		{"300 files", &githubtest.Compare{Status: "ahead", Files: many}, "more than 300 files changed"},
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
