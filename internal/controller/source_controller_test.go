package controller

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/audit"
	"github.com/shpyrd-io/shpyrd/pkg/configvars"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
	"github.com/shpyrd-io/shpyrd/pkg/github/githubtest"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

const (
	testCommit  = "0123456789abcdef0123456789abcdef01234567"
	otherCommit = "89abcdef0123456789abcdef0123456789abcdef"
)

var sourceNow = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

// pinGit pins sha for the revision app follows, as the source watcher does.
func pinGit(app *shpyrdv1.App, sha string) *shpyrdv1.App {
	if app.Annotations == nil {
		app.Annotations = map[string]string{}
	}
	app.Annotations[shpyrdv1.AnnotationGitCommit] = sha
	app.Annotations[shpyrdv1.AnnotationGitCommitRef] = app.GitRevision()
	return app
}

func withPush(app *shpyrdv1.App, p gitauth.Push) *shpyrdv1.App {
	if app.Annotations == nil {
		app.Annotations = map[string]string{}
	}
	raw, _ := json.Marshal(p)
	app.Annotations[shpyrdv1.AnnotationGitPush] = string(raw)
	return app
}

func withRecord(app *shpyrdv1.App, rec gitauth.SourceRecord) *shpyrdv1.App {
	if app.Annotations == nil {
		app.Annotations = map[string]string{}
	}
	if rec.Watch == "" {
		// A record that has been through a pass knows how its pushes arrive:
		// what the App's record said, else a timer (nothing covers it).
		if rec.Watch = sourceRecord(app).Watch; rec.Watch == "" {
			rec.Watch = WatchMode(false, rec)
		}
	}
	raw, _ := json.Marshal(rec)
	app.Annotations[shpyrdv1.AnnotationGitSource] = string(raw)
	return app
}

func sourceRecord(app *shpyrdv1.App) gitauth.SourceRecord {
	return gitauth.ReadSourceRecord(app.Annotations[shpyrdv1.AnnotationGitSource])
}

func at(t time.Time) *time.Time { return &t }

// fakeLsRemote answers every listing with refs and err, and counts them.
type fakeLsRemote struct {
	refs  map[string]string
	err   error
	calls int
	url   string
	kind  string
}

func (f *fakeLsRemote) list(_ context.Context, url string, b gitauth.Build) (map[string]string, error) {
	f.calls++
	f.url, f.kind = url, b.Kind
	return f.refs, f.err
}

func newSourceReconciler(t *testing.T, ls *fakeLsRemote, objs ...client.Object) (*SourceReconciler, client.Client) {
	t.Helper()
	// The fake client gives objects no UID; the watcher keeps its checks
	// by UID.
	for _, o := range objs {
		if o.GetUID() == "" {
			o.SetUID(types.UID(o.GetNamespace() + "/" + o.GetName()))
		}
	}
	_, c := newTestReconciler(t, objs...)
	return &SourceReconciler{
		Client: c, Kube: k8sfake.NewSimpleClientset(), SystemNamespace: "shpyrd-system",
		PollInterval: DefaultPollInterval, LsRemote: ls.list,
		Now:    func() time.Time { return sourceNow },
		Jitter: func(d time.Duration) time.Duration { return d },
	}, c
}

func reconcileSource(t *testing.T, r *SourceReconciler, app *shpyrdv1.App) (ctrl.Result, *shpyrdv1.App) {
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

func heardAudits(t *testing.T) *[]audit.Entry {
	t.Helper()
	var heard []audit.Entry
	audit.Sink = func(_ audit.Ref, e audit.Entry) { heard = append(heard, e) }
	t.Cleanup(func() { audit.Sink = nil })
	return &heard
}

func TestSourcePinsTheBranchOfANewApp(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit, "refs/heads/dev": otherCommit}}
	app := gitApp("shop", "https://github.com/acme/shop")
	r, _ := newSourceReconciler(t, ls, app)
	heard := heardAudits(t)
	res, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != testCommit || got.Annotations[shpyrdv1.AnnotationGitCommitRef] != "main" {
		t.Fatalf("pin %q ref %q", got.Annotations[shpyrdv1.AnnotationGitCommit], got.Annotations[shpyrdv1.AnnotationGitCommitRef])
	}
	if ls.url != "https://github.com/acme/shop" {
		t.Errorf("listed %q", ls.url)
	}
	if note := got.Annotations[shpyrdv1.AnnotationReleaseNote]; note != "Commit 0123456 (polled)" {
		t.Errorf("note %q", note)
	}
	if got.Annotations[shpyrdv1.AnnotationDeployedBy] != "git" || got.Annotations[shpyrdv1.AnnotationDeployedClient] != "poll" {
		t.Errorf("attribution %q via %q", got.Annotations[shpyrdv1.AnnotationDeployedBy], got.Annotations[shpyrdv1.AnnotationDeployedClient])
	}
	rec := sourceRecord(got)
	if rec.Ref != "main" || rec.Latest != testCommit || rec.Via != "poll" || rec.CheckedAt == nil || !rec.CheckedAt.Equal(sourceNow) || rec.Error != "" {
		t.Errorf("record %+v", rec)
	}
	if res.RequeueAfter != DefaultPollInterval {
		t.Errorf("requeue %v", res.RequeueAfter)
	}
	if len(*heard) != 1 || (*heard)[0].Action != "git.push" || (*heard)[0].Target != "0123456" || (*heard)[0].Actor != "git" {
		t.Errorf("audit %+v", *heard)
	}
}

func TestSourceLeavesAPersonsDeployToThem(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit}}
	app := gitApp("shop", "https://github.com/acme/shop")
	app.Annotations = map[string]string{shpyrdv1.AnnotationDeployedBy: "ana@acme.test", shpyrdv1.AnnotationReleaseNote: "Deploy from the dashboard"}
	r, _ := newSourceReconciler(t, ls, app)
	heard := heardAudits(t)
	_, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != testCommit {
		t.Fatalf("pin %q", got.PinnedCommit())
	}
	if got.Annotations[shpyrdv1.AnnotationDeployedBy] != "ana@acme.test" || got.Annotations[shpyrdv1.AnnotationReleaseNote] != "Deploy from the dashboard" {
		t.Errorf("a person's deploy keeps their name and note: %v", got.Annotations)
	}
	if sourceRecord(got).Via != "deploy" || len(*heard) != 0 {
		t.Errorf("via %q, audits %+v", sourceRecord(got).Via, *heard)
	}
}

func TestSourceDeploysAPushToTheBranch(t *testing.T) {
	ls := &fakeLsRemote{}
	app := withPush(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit), gitauth.Push{
		Ref: "refs/heads/main", SHA: otherCommit, Message: "fix login\n\nThe redirect lost the session.", Author: "alice", Delivery: "d-1", At: sourceNow,
	})
	r, _ := newSourceReconciler(t, ls, app)
	heard := heardAudits(t)
	res, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != otherCommit {
		t.Fatalf("pin %q", got.PinnedCommit())
	}
	if note := got.Annotations[shpyrdv1.AnnotationReleaseNote]; note != "Push 89abcde by alice: fix login" {
		t.Errorf("note %q", note)
	}
	if got.Annotations[shpyrdv1.AnnotationDeployedBy] != "git:alice" || got.Annotations[shpyrdv1.AnnotationDeployedClient] != "push" {
		t.Errorf("attribution %v", got.Annotations)
	}
	if _, ok := got.Annotations[shpyrdv1.AnnotationGitPush]; ok || ls.calls != 0 {
		t.Errorf("the push is consumed, and nothing is listed: %d listings", ls.calls)
	}
	rec := sourceRecord(got)
	if rec.Via != "push" || rec.Delivery != "d-1" || rec.Author != "alice" || rec.LastWebhookAt == nil || !rec.LastWebhookAt.Equal(sourceNow) {
		t.Errorf("record %+v", rec)
	}
	if res.RequeueAfter != pushDrivenRecheck {
		t.Errorf("a project whose webhook delivered is never listed on a timer; its next pass looks at its cover: %v", res.RequeueAfter)
	}
	if !rec.ProjectHook {
		t.Error("the project's own webhook delivered")
	}
	if len(*heard) != 1 || (*heard)[0].Actor != "git:alice" || (*heard)[0].Detail != "fix login" {
		t.Errorf("audit %+v", *heard)
	}
}

func TestSourceDropsAPushToAnotherBranch(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit}}
	app := withPush(withRecord(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit),
		gitauth.SourceRecord{Ref: "main", Latest: testCommit, CheckedAt: at(sourceNow)}),
		gitauth.Push{Ref: "refs/heads/dev", SHA: otherCommit})
	r, _ := newSourceReconciler(t, ls, app)
	_, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != testCommit || got.Annotations[shpyrdv1.AnnotationGitPush] != "" || ls.calls != 0 {
		t.Errorf("pin %q push %q listings %d", got.PinnedCommit(), got.Annotations[shpyrdv1.AnnotationGitPush], ls.calls)
	}
}

// Review Focus 2.
func TestSourceKeepsASkippedCommitSkipped(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}
	app := withPush(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit), gitauth.Push{
		Ref: "refs/heads/main", SHA: otherCommit, Message: "Fix a typo [Skip Deploy]", Author: "bruno", Delivery: "d-2", App: true,
	})
	r, _ := newSourceReconciler(t, ls, app)
	heard := heardAudits(t)
	_, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != testCommit || sourceRecord(got).Skipped != otherCommit {
		t.Fatalf("pin %q skipped %q", got.PinnedCommit(), sourceRecord(got).Skipped)
	}
	if len(*heard) != 1 || (*heard)[0].Action != "git.skip" || (*heard)[0].Actor != "git:bruno" {
		t.Errorf("audit %+v", *heard)
	}
	// Three hours on, a poll finds the skipped commit at the head.
	r.Now = func() time.Time { return sourceNow.Add(3 * time.Hour) }
	_, got = reconcileSource(t, r, got)
	if ls.calls != 1 || got.PinnedCommit() != testCommit {
		t.Errorf("listings %d, pin %q: a poll must not deploy a skipped commit", ls.calls, got.PinnedCommit())
	}
}

func TestSourceWithAutoDeployOffRecordsTheCommitOnly(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}
	off := false
	app := pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit)
	app.Spec.Source.Git.AutoDeploy = &off
	fresh := gitApp("fresh", "https://github.com/acme/fresh")
	fresh.Spec.Source.Git.AutoDeploy = &off
	r, _ := newSourceReconciler(t, ls, app, fresh)
	_, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != testCommit || sourceRecord(got).Latest != otherCommit || got.Annotations[shpyrdv1.AnnotationReleaseNote] != "" {
		t.Errorf("pin %q latest %q note %q", got.PinnedCommit(), sourceRecord(got).Latest, got.Annotations[shpyrdv1.AnnotationReleaseNote])
	}
	// A new app, with nothing pinned yet, still gets its first commit.
	if _, got := reconcileSource(t, r, fresh); got.PinnedCommit() != otherCommit {
		t.Errorf("a new app builds once: pin %q", got.PinnedCommit())
	}
}

// Review Focus 1: a push wins over a listing that still shows the commit
// before it, and the watcher's own write does not bring a listing.
func TestSourceWebhookBeatsAStalePoll(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit}}
	app := withPush(withRecord(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit),
		gitauth.SourceRecord{Ref: "main", Latest: testCommit, CheckedAt: at(sourceNow.Add(-time.Hour))}),
		gitauth.Push{Ref: "refs/heads/main", SHA: otherCommit, Before: testCommit, Author: "alice"})
	r, _ := newSourceReconciler(t, ls, app)
	_, got := reconcileSource(t, r, app)
	_, got = reconcileSource(t, r, got)
	if got.PinnedCommit() != otherCommit || ls.calls != 0 {
		t.Errorf("pin %q after %d listings", got.PinnedCommit(), ls.calls)
	}
}

// Review Focus 1: every status write of the App controller is an App
// event; none of them lists the repository before the check is due.
func TestSourcePollsOnlyWhenDue(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit}}
	app := withRecord(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit),
		gitauth.SourceRecord{Ref: "main", Latest: testCommit, CheckedAt: at(sourceNow.Add(-30 * time.Second))})
	failing := withRecord(gitApp("other", "https://github.com/acme/other"),
		gitauth.SourceRecord{Ref: "main", Error: "could not reach github.com", Failures: 3, CheckedAt: at(sourceNow.Add(-10 * time.Second))})
	r, c := newSourceReconciler(t, ls, app, failing)
	var res ctrl.Result
	for i := 0; i < 3; i++ {
		res, _ = reconcileSource(t, r, app)
	}
	if ls.calls != 0 || res.RequeueAfter != DefaultPollInterval-30*time.Second {
		t.Fatalf("listings %d, requeue %v", ls.calls, res.RequeueAfter)
	}
	// An app whose listings fail backs off (30 s, 1 m, 2 m: the third
	// failure waits two minutes)...
	if res, _ = reconcileSource(t, r, failing); ls.calls != 0 || res.RequeueAfter != 110*time.Second {
		t.Fatalf("listings %d, requeue %v", ls.calls, res.RequeueAfter)
	}
	// ...but a deploy asks for the branch at once.
	got := &shpyrdv1.App{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(failing), got); err != nil {
		t.Fatal(err)
	}
	got.Annotations[configvars.AnnotationDeployRequested] = sourceNow.Add(-time.Second).Format(time.RFC3339Nano)
	if err := c.Update(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	if _, got = reconcileSource(t, r, got); ls.calls != 1 || got.PinnedCommit() != testCommit {
		t.Errorf("listings %d, pin %q", ls.calls, got.PinnedCommit())
	}
}

func TestSourceFollowsAForcePushAndKeepsThePinWhenTheBranchGoes(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}
	app := withRecord(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit),
		gitauth.SourceRecord{Ref: "main", Latest: testCommit, CheckedAt: at(sourceNow.Add(-time.Hour))})
	r, _ := newSourceReconciler(t, ls, app)
	_, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != otherCommit {
		t.Fatalf("history rewritten: the branch's commit is what runs; pin %q", got.PinnedCommit())
	}
	ls.refs = map[string]string{"refs/heads/dev": testCommit}
	r.Now = func() time.Time { return sourceNow.Add(time.Hour) }
	res, got := reconcileSource(t, r, got)
	rec := sourceRecord(got)
	if got.PinnedCommit() != otherCommit {
		t.Errorf("a deleted branch keeps what runs: pin %q", got.PinnedCommit())
	}
	if !strings.Contains(rec.Error, "no branch or tag named main") || rec.Failures != 1 || res.RequeueAfter != 30*time.Second {
		t.Errorf("record %+v requeue %v", rec, res.RequeueAfter)
	}
}

func TestSourceSaysHowToGiveAccessAndBacksOff(t *testing.T) {
	ls := &fakeLsRemote{err: gitauth.ErrNoAccess}
	app := gitApp("shop", "https://github.com/acme/shop")
	r, _ := newSourceReconciler(t, ls, app)
	res, got := reconcileSource(t, r, app)
	rec := sourceRecord(got)
	if !strings.Contains(rec.Error, "shpyrd git-auth deploy-key") || !strings.Contains(rec.Error, "--host github.com") {
		t.Errorf("error %q", rec.Error)
	}
	if got.PinnedCommit() != "" || rec.Failures != 1 || res.RequeueAfter != 30*time.Second {
		t.Errorf("pin %q failures %d requeue %v", got.PinnedCommit(), rec.Failures, res.RequeueAfter)
	}
	r.Now = func() time.Time { return sourceNow.Add(31 * time.Second) }
	res, got = reconcileSource(t, r, got)
	if ls.calls != 2 || sourceRecord(got).Failures != 2 || res.RequeueAfter != time.Minute {
		t.Errorf("listings %d failures %d requeue %v", ls.calls, sourceRecord(got).Failures, res.RequeueAfter)
	}
}

// Review Focus 4.
func TestSourcePinsTheCommitATagPointsAt(t *testing.T) {
	tagObject := "f00df00df00df00df00df00df00df00df00df00d"
	ls := &fakeLsRemote{refs: map[string]string{"refs/tags/v1.2": tagObject, "refs/tags/v1.2^{}": testCommit}}
	app := gitApp("shop", "https://github.com/acme/shop")
	app.Spec.Source.Git.Revision = "v1.2"
	app = withPush(app, gitauth.Push{Ref: "refs/tags/v1.2", SHA: tagObject})
	r, _ := newSourceReconciler(t, ls, app)
	_, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != testCommit || got.Annotations[shpyrdv1.AnnotationGitCommitRef] != "v1.2" {
		t.Errorf("pin %q ref %q: the tag's commit, not the tag object", got.PinnedCommit(), got.Annotations[shpyrdv1.AnnotationGitCommitRef])
	}
	if got.Annotations[shpyrdv1.AnnotationGitPush] != "" {
		t.Error("a push to a tag is not a push to a branch: dropped")
	}
}

// Review Focus 5.
func TestSourcePinsFromATruncatedListing(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit}, err: gitauth.ErrTooManyRefs}
	app := gitApp("shop", "https://github.com/acme/shop")
	tagged := gitApp("tagged", "https://github.com/acme/monorepo")
	tagged.Spec.Source.Git.Revision = "v9"
	r, _ := newSourceReconciler(t, ls, app, tagged)
	if _, got := reconcileSource(t, r, app); got.PinnedCommit() != testCommit || sourceRecord(got).Error != "" {
		t.Errorf("pin %q error %q", got.PinnedCommit(), sourceRecord(got).Error)
	}
	if _, got := reconcileSource(t, r, tagged); !strings.Contains(sourceRecord(got).Error, "more refs") {
		t.Errorf("error %q", sourceRecord(got).Error)
	}
}

func TestSourceLeavesCommitsAndUnreadableURLsAlone(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit}}
	commit := gitApp("pinned", "https://github.com/acme/shop")
	commit.Spec.Source.Git.Revision = "a1b2c3d"
	raw := gitApp("raw", "git://example.test/acme/shop")
	r, _ := newSourceReconciler(t, ls, commit, raw)
	for _, a := range []*shpyrdv1.App{commit, raw} {
		if res, got := reconcileSource(t, r, a); res.RequeueAfter != 0 || got.Annotations[shpyrdv1.AnnotationGitSource] != "" {
			t.Errorf("%s: requeue %v annotations %v", a.Name, res.RequeueAfter, got.Annotations)
		}
	}
	if ls.calls != 0 {
		t.Errorf("%d listings", ls.calls)
	}
}

// Review Focus 3: a newer value written meanwhile is kept.
func TestRemoveAnnotationIfUnchangedKeepsANewerValue(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	app.Annotations = map[string]string{shpyrdv1.AnnotationGitPush: `{"sha":"b"}`}
	_, c := newTestReconciler(t, app)
	ctx := context.Background()
	stale := app.DeepCopy()
	if removed, err := removeAnnotationIfUnchanged(ctx, c, stale, shpyrdv1.AnnotationGitPush, `{"sha":"a"}`); removed || err == nil {
		t.Fatalf("removed %v err %v", removed, err)
	}
	got := &shpyrdv1.App{}
	_ = c.Get(ctx, client.ObjectKeyFromObject(app), got)
	if got.Annotations[shpyrdv1.AnnotationGitPush] != `{"sha":"b"}` {
		t.Fatalf("the newer push is gone: %v", got.Annotations)
	}
	if removed, err := removeAnnotationIfUnchanged(ctx, c, got, shpyrdv1.AnnotationGitPush, `{"sha":"b"}`); !removed || err != nil {
		t.Fatalf("removed %v err %v", removed, err)
	}
	_ = c.Get(ctx, client.ObjectKeyFromObject(app), got)
	if _, ok := got.Annotations[shpyrdv1.AnnotationGitPush]; ok {
		t.Error("an unchanged value is removed")
	}
}

func TestLookupGitBuildIsWhatTheBuildUses(t *testing.T) {
	app := gitApp("shop", "https://github.com/acme/shop")
	_, c := newTestReconciler(t, app, workspaceToken("default", "github.com", "ghp_ours"))
	b, err := lookupGitBuild(context.Background(), c, "shpyrd-system", nil, app)
	if err != nil || b.Kind != gitauth.KindToken || b.Secret == nil || string(b.Secret.Data["password"]) != "ghp_ours" {
		t.Fatalf("build %+v err %v", b, err)
	}
	if _, err := lookupGitBuild(context.Background(), c, "shpyrd-system", nil, gitApp("raw", "git://example.test/x")); err != errGitURL {
		t.Errorf("err = %v, want errGitURL", err)
	}
}

func TestPollIntervalAndJitter(t *testing.T) {
	for in, want := range map[string]time.Duration{"": 5 * time.Minute, "30s": 30 * time.Second, "nonsense": 5 * time.Minute, "1s": 30 * time.Second, "10m": 10 * time.Minute} {
		if got := PollInterval(in); got != want {
			t.Errorf("PollInterval(%q) = %v, want %v", in, got, want)
		}
	}
	for i := 0; i < 200; i++ {
		if d := jitter(100 * time.Second); d < 90*time.Second || d > 110*time.Second {
			t.Fatalf("jitter(100s) = %v", d)
		}
	}
}

// Ruling A: a check that finds what the record already says writes
// nothing, so the App controller is not woken every interval; the time of
// the check is kept in memory.
func TestSourceSameCommitWithinTheHourWritesNothing(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit}}
	app := withRecord(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit),
		gitauth.SourceRecord{Ref: "main", Latest: testCommit, LatestAt: at(sourceNow.Add(-time.Hour)), Via: "poll", CheckedAt: at(sourceNow.Add(-5 * time.Minute))})
	r, c := newSourceReconciler(t, ls, app)
	before := &shpyrdv1.App{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(app), before); err != nil {
		t.Fatal(err)
	}
	res, got := reconcileSource(t, r, app)
	if ls.calls != 1 {
		t.Fatalf("the check was due: %d listings", ls.calls)
	}
	if got.ResourceVersion != before.ResourceVersion || got.Annotations[shpyrdv1.AnnotationGitSource] != before.Annotations[shpyrdv1.AnnotationGitSource] {
		t.Errorf("a check that found nothing new wrote the App: %q -> %q", before.Annotations[shpyrdv1.AnnotationGitSource], got.Annotations[shpyrdv1.AnnotationGitSource])
	}
	if res.RequeueAfter != DefaultPollInterval {
		t.Errorf("requeue %v: the next check counts from this one", res.RequeueAfter)
	}
	// The App controller's writes wake the watcher: the check made a
	// moment ago, kept in memory, is not repeated.
	r.Now = func() time.Time { return sourceNow.Add(10 * time.Second) }
	if res, _ = reconcileSource(t, r, got); ls.calls != 1 || res.RequeueAfter != DefaultPollInterval-10*time.Second {
		t.Errorf("listings %d requeue %v", ls.calls, res.RequeueAfter)
	}
	// A new commit is written.
	ls.refs = map[string]string{"refs/heads/main": otherCommit}
	r.Now = func() time.Time { return sourceNow.Add(6 * time.Minute) }
	if _, got = reconcileSource(t, r, got); ls.calls != 2 || got.PinnedCommit() != otherCommit || sourceRecord(got).Latest != otherCommit {
		t.Errorf("listings %d pin %q record %+v", ls.calls, got.PinnedCommit(), sourceRecord(got))
	}
}

func TestSourceWritesAnErrorAndClearsIt(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit}, err: gitauth.ErrNoAccess}
	app := withRecord(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit),
		gitauth.SourceRecord{Ref: "main", Latest: testCommit, Via: "poll", CheckedAt: at(sourceNow.Add(-5 * time.Minute))})
	r, _ := newSourceReconciler(t, ls, app)
	_, got := reconcileSource(t, r, app)
	if rec := sourceRecord(got); rec.Error == "" || rec.Failures != 1 || !rec.CheckedAt.Equal(sourceNow) {
		t.Fatalf("record %+v", rec)
	}
	ls.err = nil
	r.Now = func() time.Time { return sourceNow.Add(time.Minute) }
	if _, got = reconcileSource(t, r, got); sourceRecord(got).Error != "" || sourceRecord(got).Failures != 0 {
		t.Errorf("record %+v", sourceRecord(got))
	}
}

func TestSourceRefreshesCheckedAtHourly(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit}}
	app := withRecord(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit),
		gitauth.SourceRecord{Ref: "main", Latest: testCommit, Via: "poll", CheckedAt: at(sourceNow.Add(-61 * time.Minute))})
	r, _ := newSourceReconciler(t, ls, app)
	_, got := reconcileSource(t, r, app)
	rec := sourceRecord(got)
	if ls.calls != 1 || rec.CheckedAt == nil || !rec.CheckedAt.Equal(sourceNow) {
		t.Errorf("listings %d record %+v", ls.calls, rec)
	}
	if rec.Latest != testCommit || got.PinnedCommit() != testCommit {
		t.Errorf("record %+v pin %q", rec, got.PinnedCommit())
	}
}

// Ruling B: the watcher lists with the URL the build clones: a deploy key
// reads an https URL over SSH.
func TestSourceListsWithTheBuildsCloneURL(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit}}
	app := gitApp("shop", "https://github.com/acme/shop")
	dk := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: gitauth.DeployKeySecretName, Namespace: "app-shop", Labels: map[string]string{gitauth.LabelCredential: gitauth.KindDeployKey},
			Annotations: map[string]string{gitauth.AnnotationHost: "github.com"}},
		Type: corev1.SecretTypeSSHAuth,
		Data: map[string][]byte{corev1.SSHAuthPrivateKey: []byte("K"), gitauth.KeyKnownHosts: []byte("github.com ssh-ed25519 A")},
	}
	r, _ := newSourceReconciler(t, ls, app, dk)
	if _, got := reconcileSource(t, r, app); got.PinnedCommit() != testCommit {
		t.Fatalf("pin %q", got.PinnedCommit())
	}
	if ls.url != "git@github.com:acme/shop.git" || ls.kind != gitauth.KindDeployKey {
		t.Errorf("listed %q with %q", ls.url, ls.kind)
	}
}

// A push without a time is recorded at the time it is read; with one, at
// its own.
func TestSourceRecordsThePushTime(t *testing.T) {
	pushed := sourceNow.Add(-2 * time.Minute)
	app := withPush(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit), gitauth.Push{Ref: "refs/heads/main", SHA: otherCommit, At: pushed})
	untimed := withPush(pinGit(gitApp("other", "https://github.com/acme/other"), testCommit), gitauth.Push{Ref: "refs/heads/main", SHA: otherCommit})
	r, _ := newSourceReconciler(t, &fakeLsRemote{}, app, untimed)
	if _, got := reconcileSource(t, r, app); !sourceRecord(got).LastWebhookAt.Equal(pushed) || !sourceRecord(got).LatestAt.Equal(pushed) {
		t.Errorf("record %+v", sourceRecord(got))
	}
	if _, got := reconcileSource(t, r, untimed); !sourceRecord(got).LastWebhookAt.Equal(sourceNow) {
		t.Errorf("record %+v", sourceRecord(got))
	}
}

// A pass that reads the App before the cache holds the watcher's own pin
// does not read the branch and pin the commit again; a changed branch is
// read at once.
func TestSourcePinsACommitOnce(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit, "refs/heads/dev": otherCommit}}
	app := gitApp("shop", "https://github.com/acme/shop")
	r, c := newSourceReconciler(t, ls, app)
	heard := heardAudits(t)
	_, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != testCommit {
		t.Fatalf("pin %q", got.PinnedCommit())
	}
	// The App as a stale cache shows it: no pin, no record.
	stale := got.DeepCopy()
	delete(stale.Annotations, shpyrdv1.AnnotationGitCommit)
	delete(stale.Annotations, shpyrdv1.AnnotationGitCommitRef)
	delete(stale.Annotations, shpyrdv1.AnnotationGitSource)
	if err := c.Update(context.Background(), stale); err != nil {
		t.Fatal(err)
	}
	r.Now = func() time.Time { return sourceNow.Add(5 * time.Second) }
	reconcileSource(t, r, stale)
	if ls.calls != 1 || len(*heard) != 1 {
		t.Errorf("listings %d audits %d", ls.calls, len(*heard))
	}
	// Another branch.
	got = &shpyrdv1.App{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(app), got); err != nil {
		t.Fatal(err)
	}
	got.Spec.Source.Git.Revision = "dev"
	if err := c.Update(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	if _, got = reconcileSource(t, r, got); ls.calls != 2 || got.PinnedCommit() != otherCommit {
		t.Errorf("listings %d pin %q", ls.calls, got.PinnedCommit())
	}
}

// Fix round 1: a push read again from a stale App, after it was consumed,
// is only dropped: no second pin, no second audit.
func TestSourceAppliesAConsumedPushOnce(t *testing.T) {
	app := withPush(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit), gitauth.Push{
		Ref: "refs/heads/main", SHA: otherCommit, Message: "fix login", Author: "alice", Delivery: "d-1", At: sourceNow,
	})
	raw := app.Annotations[shpyrdv1.AnnotationGitPush]
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}
	r, c := newSourceReconciler(t, ls, app)
	heard := heardAudits(t)
	_, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != otherCommit || len(*heard) != 1 {
		t.Fatalf("pin %q audits %d", got.PinnedCommit(), len(*heard))
	}
	// The App as a stale cache shows it: the old pin, the old record, and
	// the push still there.
	stale := got.DeepCopy()
	stale.Annotations[shpyrdv1.AnnotationGitCommit] = testCommit
	stale.Annotations[shpyrdv1.AnnotationGitPush] = raw
	delete(stale.Annotations, shpyrdv1.AnnotationGitSource)
	delete(stale.Annotations, shpyrdv1.AnnotationReleaseNote)
	if err := c.Update(context.Background(), stale); err != nil {
		t.Fatal(err)
	}
	r.Now = func() time.Time { return sourceNow.Add(5 * time.Second) }
	_, got = reconcileSource(t, r, stale)
	if len(*heard) != 1 || ls.calls != 0 {
		t.Errorf("audits %+v listings %d", *heard, ls.calls)
	}
	if got.PinnedCommit() != testCommit || got.Annotations[shpyrdv1.AnnotationReleaseNote] != "" {
		t.Errorf("the consumed push was applied again: pin %q note %q", got.PinnedCommit(), got.Annotations[shpyrdv1.AnnotationReleaseNote])
	}
	if _, ok := got.Annotations[shpyrdv1.AnnotationGitPush]; ok {
		t.Error("the consumed push is dropped")
	}
}

func TestSourceAuditsASkipOnce(t *testing.T) {
	app := withPush(withRecord(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit),
		gitauth.SourceRecord{Ref: "main", Latest: otherCommit, Via: "push", Skipped: otherCommit, CheckedAt: at(sourceNow.Add(-time.Minute))}),
		gitauth.Push{Ref: "refs/heads/main", SHA: otherCommit, Before: testCommit, Message: "Fix a typo [skip deploy]", Author: "bruno", Delivery: "d-2"})
	// Delivered again: it does not follow the latest, so the branch is
	// read, and its head is the skipped commit.
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}
	r, _ := newSourceReconciler(t, ls, app)
	heard := heardAudits(t)
	_, got := reconcileSource(t, r, app)
	if ls.calls != 1 || sourceRecord(got).Error != "" {
		t.Errorf("listings %d record %+v", ls.calls, sourceRecord(got))
	}
	if len(*heard) != 0 || got.PinnedCommit() != testCommit || sourceRecord(got).Skipped != otherCommit {
		t.Errorf("audits %+v pin %q record %+v", *heard, got.PinnedCommit(), sourceRecord(got))
	}
}

func TestSourceSkipOfTheRunningCommitIsNoSkip(t *testing.T) {
	app := withPush(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit),
		gitauth.Push{Ref: "refs/heads/main", SHA: testCommit, Message: "Docs [skip deploy]", Author: "bruno", Delivery: "d-3"})
	r, _ := newSourceReconciler(t, &fakeLsRemote{}, app)
	heard := heardAudits(t)
	_, got := reconcileSource(t, r, app)
	if len(*heard) != 0 || got.PinnedCommit() != testCommit || sourceRecord(got).Skipped != "" {
		t.Errorf("audits %+v pin %q record %+v", *heard, got.PinnedCommit(), sourceRecord(got))
	}
}

func TestSourceDropsAPushWithoutACommitAndPolls(t *testing.T) {
	for _, sha := range []string{"nothex", "0123456", strings.ToUpper(otherCommit), strings.Repeat("0", 40), otherCommit + "zz"} {
		ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}
		app := withPush(withRecord(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit),
			gitauth.SourceRecord{Ref: "main", Latest: testCommit, Via: "poll", CheckedAt: at(sourceNow.Add(-5 * time.Second))}),
			gitauth.Push{Ref: "refs/heads/main", SHA: sha, Author: "mallory"})
		r, _ := newSourceReconciler(t, ls, app)
		_, got := reconcileSource(t, r, app)
		if ls.calls != 1 || got.PinnedCommit() != otherCommit || sourceRecord(got).Via != "poll" || got.Annotations[shpyrdv1.AnnotationGitPush] != "" {
			t.Errorf("%q: listings %d pin %q record %+v", sha, ls.calls, got.PinnedCommit(), sourceRecord(got))
		}
	}
}

// Final review I1: a pin carries when it was set, to the second, so the
// reporter tells the pin's own build failure from an older one.
func TestSourceStampsWhenItPinsACommit(t *testing.T) {
	app := withPush(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit), gitauth.Push{
		Ref: "refs/heads/main", SHA: otherCommit, Message: "fix login", Author: "alice", At: sourceNow,
	})
	r, _ := newSourceReconciler(t, &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}, app)
	r.Now = func() time.Time { return sourceNow.Add(1500 * time.Millisecond) }
	_, got := reconcileSource(t, r, app)
	rec := sourceRecord(got)
	if got.PinnedCommit() != otherCommit || rec.PinnedAt == nil || !rec.PinnedAt.Equal(sourceNow.Add(time.Second)) {
		t.Fatalf("pin %s, record %+v", got.PinnedCommit(), rec)
	}
	// Nothing new: the time of the pin stays.
	r.Now = func() time.Time { return sourceNow.Add(3 * time.Hour) }
	_, got = reconcileSource(t, r, got)
	if rec := sourceRecord(got); rec.PinnedAt == nil || !rec.PinnedAt.Equal(sourceNow.Add(time.Second)) {
		t.Errorf("record %+v", rec)
	}
}

// An older push delivered again (the sweep resends what failed, in any
// order) pins nothing new: the branch says its head is the newer commit.
func TestAnOlderPushDeliveredAgainPinsNothing(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}
	app := withPush(withRecord(pinGit(gitApp("shop", "https://gitlab.com/acme/shop"), otherCommit),
		gitauth.SourceRecord{Ref: "main", Latest: otherCommit, Delivery: "d-2", CheckedAt: at(sourceNow.Add(-time.Minute))}),
		gitauth.Push{Ref: "refs/heads/main", SHA: testCommit, Before: "fedcba9876543210fedcba9876543210fedcba98", Delivery: "d-1", Author: "alice", At: sourceNow.Add(-10 * time.Minute)})
	r, _ := newSourceReconciler(t, ls, app)
	heard := heardAudits(t)
	_, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != otherCommit || ls.calls != 1 || len(*heard) != 0 {
		t.Errorf("pin %s after %d listings, audits %+v", got.PinnedCommit(), ls.calls, *heard)
	}
	if _, ok := got.Annotations[shpyrdv1.AnnotationGitPush]; ok {
		t.Error("the push stays on the App")
	}
}

func TestWatchModeSaysHowABranchsPushesArrive(t *testing.T) {
	for _, c := range []struct {
		covered bool
		rec     gitauth.SourceRecord
		want    string
	}{
		{true, gitauth.SourceRecord{}, WatchGitHubApp},
		{true, gitauth.SourceRecord{ProjectHook: true}, WatchGitHubApp},
		{false, gitauth.SourceRecord{ProjectHook: true}, WatchWebhook},
		{false, gitauth.SourceRecord{LastWebhookAt: at(sourceNow)}, WatchPoll},
		{false, gitauth.SourceRecord{}, WatchPoll},
	} {
		if got := WatchMode(c.covered, c.rec); got != c.want {
			t.Errorf("%v %+v: %s", c.covered, c.rec, got)
		}
	}
}

// Review Focus 7.
func TestACoveredRepositoryIsNeverListedOnATimer(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit}}
	app := shopWith(shpyrdv1.GitSource{Revision: "main"})
	r, fake := gatedWatcher(t, ls, app)
	got := app
	for i := 0; i < 6; i++ {
		var res ctrl.Result
		res, got = reconcileSource(t, r, got)
		if res.RequeueAfter != pushDrivenRecheck {
			t.Fatalf("pass %d: requeue %v", i, res.RequeueAfter)
		}
		advance(fake, 4*time.Hour)
	}
	if ls.calls != 0 {
		t.Fatalf("a repository the GitHub App covers was listed %d times on a timer", ls.calls)
	}
	// A person's deploy reads the branch, once.
	got.Annotations[configvars.AnnotationDeployRequested] = fake.Time().Format(time.RFC3339Nano)
	if err := r.Update(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	advance(fake, time.Second)
	if _, got = reconcileSource(t, r, got); ls.calls != 1 {
		t.Fatalf("the deploy: %d listings", ls.calls)
	}
	if _, got = reconcileSource(t, r, got); ls.calls != 1 {
		t.Fatalf("the deploy read twice: %d listings", ls.calls)
	}
	// Taken out of the installation: once its token ages, the App no longer
	// covers it, and the timer lists it again although App pushes arrived.
	fake.SetRepos(42, githubtest.Repo{FullName: "Acme/site", DefaultBranch: "trunk"})
	r.GitHub.Forget(store.DefaultWorkspace)
	advance(fake, 2*time.Hour)
	if _, _ = reconcileSource(t, r, got); ls.calls != 2 {
		t.Errorf("no longer covered: %d listings", ls.calls)
	}
}

func TestAProjectsOwnWebhookStopsItsChecksButTheAppsDoesNot(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}
	viaApp := withPush(pinGit(gitApp("shop", "https://github.com/acme/shop"), testCommit),
		gitauth.Push{Ref: "refs/heads/main", SHA: otherCommit, Delivery: "d-1", Author: "alice", At: sourceNow, App: true})
	viaHook := withPush(pinGit(gitApp("blog", "https://gitlab.com/acme/blog"), testCommit),
		gitauth.Push{Ref: "refs/heads/main", SHA: otherCommit, Delivery: "d-2", Author: "alice", At: sourceNow})
	r, _ := newSourceReconciler(t, ls, viaApp, viaHook)
	clock := sourceNow
	r.Now = func() time.Time { return clock }
	_, app := reconcileSource(t, r, viaApp)
	_, hook := reconcileSource(t, r, viaHook)
	if sourceRecord(app).ProjectHook || !sourceRecord(hook).ProjectHook || ls.calls != 0 {
		t.Fatalf("records %+v %+v, %d listings", sourceRecord(app), sourceRecord(hook), ls.calls)
	}
	clock = sourceNow.Add(time.Hour)
	reconcileSource(t, r, hook)
	if ls.calls != 0 {
		t.Errorf("a project whose webhook delivered was listed: %d", ls.calls)
	}
	// Not covered (no App here): a push through the App does not stop the
	// checks.
	reconcileSource(t, r, app)
	if ls.calls != 1 {
		t.Errorf("an uncovered repository with App pushes only: %d listings", ls.calls)
	}
}

// A change of cover is seen at the next pass: a repository that stops being
// covered is read once and goes back to the timer; one that becomes covered
// stops its timer.
func TestACoverThatChangesMovesTheBranchBetweenTheTimerAndTheApp(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit}}
	app := shopWith(shpyrdv1.GitSource{Revision: "main"})
	r, fake := gatedWatcher(t, ls, app)
	elsewhere := githubtest.Repo{FullName: "Acme/site", DefaultBranch: "trunk"}
	fake.SetRepos(42, elsewhere)
	r.GitHub.Forget(store.DefaultWorkspace)
	// Not covered: the timer lists it.
	advance(fake, 6*time.Minute)
	res, got := reconcileSource(t, r, app)
	if ls.calls != 1 || res.RequeueAfter > DefaultPollInterval {
		t.Fatalf("not covered: %d listings, requeue %v", ls.calls, res.RequeueAfter)
	}
	// Covered: no more listings, and the next pass is the hourly look at the cover.
	fake.SetRepos(42, elsewhere, githubtest.Repo{FullName: "Acme/shop", DefaultBranch: "main"})
	r.GitHub.Forget(store.DefaultWorkspace)
	advance(fake, 2*time.Hour)
	// A push missed while the repository joined the installation is found:
	// the flip reads the branch once.
	if res, got = reconcileSource(t, r, got); ls.calls != 2 || res.RequeueAfter != pushDrivenRecheck {
		t.Fatalf("became covered: %d listings, requeue %v", ls.calls, res.RequeueAfter)
	}
	advance(fake, 5*time.Hour)
	if res, got = reconcileSource(t, r, got); ls.calls != 2 || res.RequeueAfter != pushDrivenRecheck {
		t.Fatalf("covered, hours on: %d listings, requeue %v", ls.calls, res.RequeueAfter)
	}
	// Taken out again: one read, then the timer.
	fake.SetRepos(42, elsewhere)
	r.GitHub.Forget(store.DefaultWorkspace)
	advance(fake, 2*time.Hour)
	if res, got = reconcileSource(t, r, got); ls.calls != 3 || res.RequeueAfter > DefaultPollInterval {
		t.Fatalf("stopped being covered: %d listings, requeue %v", ls.calls, res.RequeueAfter)
	}
	advance(fake, 6*time.Minute)
	if _, _ = reconcileSource(t, r, got); ls.calls != 4 {
		t.Errorf("back on the timer: %d listings", ls.calls)
	}
}

// The first pass after an upgrade stores no mode yet: it reads the branch
// once, then a covered repository is left alone.
func TestARecordWithNoWatchModeReadsTheBranchOnceThenStaysQuiet(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": testCommit}}
	app := shopWith(shpyrdv1.GitSource{Revision: "main"})
	rec := sourceRecord(app)
	rec.Watch = ""
	raw, _ := json.Marshal(rec)
	app.Annotations[shpyrdv1.AnnotationGitSource] = string(raw)
	r, fake := gatedWatcher(t, ls, app)
	_, got := reconcileSource(t, r, app)
	if ls.calls != 1 || sourceRecord(got).Watch != WatchGitHubApp {
		t.Fatalf("%d listings, record %+v", ls.calls, sourceRecord(got))
	}
	advance(fake, 3*time.Hour)
	if _, _ = reconcileSource(t, r, got); ls.calls != 1 {
		t.Errorf("%d listings", ls.calls)
	}
}

// A listing a person's deploy asked for does not deploy a skipped head,
// also where the App covers the repository.
func TestACoveredRepositoryKeepsASkippedCommitSkipped(t *testing.T) {
	ls := &fakeLsRemote{refs: map[string]string{"refs/heads/main": otherCommit}}
	app := withPush(shopWith(shpyrdv1.GitSource{Revision: "main"}), gitauth.Push{
		Ref: "refs/heads/main", SHA: otherCommit, Before: testCommit, Message: "Fix a typo [Skip Deploy]", Author: "bruno", Delivery: "d-2", At: sourceNow, App: true,
	})
	r, fake := gatedWatcher(t, ls, app)
	_, got := reconcileSource(t, r, app)
	if got.PinnedCommit() != testCommit || sourceRecord(got).Skipped != otherCommit || ls.calls != 0 {
		t.Fatalf("pin %q record %+v listings %d", got.PinnedCommit(), sourceRecord(got), ls.calls)
	}
	advance(fake, 3*time.Hour)
	got.Annotations[configvars.AnnotationDeployRequested] = fake.Time().Add(-time.Second).Format(time.RFC3339Nano)
	if err := r.Update(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	_, got = reconcileSource(t, r, got)
	if ls.calls != 1 || got.PinnedCommit() != testCommit {
		t.Errorf("listings %d, pin %q: a listing must not deploy a skipped commit", ls.calls, got.PinnedCommit())
	}
}
