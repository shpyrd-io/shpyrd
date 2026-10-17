package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"regexp"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/audit"
	"github.com/shpyrd-io/shpyrd/pkg/configvars"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

// The source watcher (RFC-0018). For each App that follows a git branch
// or tag it decides which commit runs: from a push the webhook left on the
// App, else from a listing of the repository when a check is due. It pins
// the commit in two annotations, with the release note and attribution a
// deploy uses, and records what it saw in AnnotationGitSource. It never
// writes status: the App controller copies the record there.

const (
	// DefaultPollInterval is how often a branch with no recent webhook is
	// read (SHPYRD_GIT_POLL_INTERVAL).
	DefaultPollInterval = 2 * time.Minute
	minPollInterval     = 10 * time.Second
	// webhookInterval is how often a branch whose webhook delivered in
	// the last day is read: the webhook is the signal, the listing the
	// net under it.
	webhookInterval = time.Hour
	webhookRecent   = 24 * time.Hour
	// recordRefresh is how stale the record's checkedAt may get: a check
	// that finds nothing new writes nothing (each write of the App wakes
	// the App controller) until the written time is this old.
	recordRefresh = time.Hour
	// forgetChecks drops a remembered check this old: its App is gone, or
	// the written record has caught up with it.
	forgetChecks = 2 * time.Hour
	// errorBackoff is the first wait after a failed listing; it doubles
	// up to the interval.
	errorBackoff = 30 * time.Second
	// maxFailures caps the failures counted: the backoff has reached the
	// interval by then, and a record that stops changing is not written on
	// each attempt.
	maxFailures = 6
)

// sourceUnreadable is what a person reads when the branch could not be
// read for a reason without words of its own; the error itself is logged.
const sourceUnreadable = "shpyrd could not read the branch; it tries again by itself."

// PollInterval reads SHPYRD_GIT_POLL_INTERVAL ("2m", "30s"): empty or
// unreadable is the default, and nothing below 10 s.
func PollInterval(v string) time.Duration {
	d, err := time.ParseDuration(strings.TrimSpace(v))
	if err != nil || d <= 0 {
		return DefaultPollInterval
	}
	return max(d, minPollInterval)
}

// SourceReconciler pins the commit each watched App builds.
type SourceReconciler struct {
	client.Client
	// Kube writes the git.push and git.skip audit entries; nil records
	// nothing.
	Kube            kubernetes.Interface
	SystemNamespace string
	// GitHub is the GitHub App (RFC-0054): listings use its token as
	// builds do. Nil: never.
	GitHub *GitHubApp
	// PollInterval is the wait between listings; zero is the default.
	PollInterval time.Duration
	// LsRemote lists a repository; nil is gitauth.LsRemote. Tests inject one.
	LsRemote func(ctx context.Context, cloneURL string, b gitauth.Build) (map[string]string, error)
	// Now and Jitter are the clock and the spread of requeues; nil is
	// time.Now and ±10 %.
	Now    func() time.Time
	Jitter func(time.Duration) time.Duration

	// checked is when each App's branch was last read, kept here because
	// a check that finds nothing new is not written on the App. After a
	// restart it is empty, and the record's checkedAt stands in.
	mu      sync.Mutex
	checked map[types.UID]sourceCheck
	// ciChecked is when each App's held commit's checks were last read.
	ciChecked map[types.UID]time.Time
	// ciLogged is when each App's unreadable checks were last logged.
	ciLogged map[types.UID]time.Time
}

// sourceCheck is a check remembered: when, and of which revision.
type sourceCheck struct {
	at  time.Time
	ref string
	// push is the last push annotation consumed, as written: read again
	// from a stale App, it is only dropped.
	push string
	// pinned is the last commit this watcher pinned for ref: a stale App
	// that does not show it yet is not pinned again.
	pinned string
}

// SetupWithManager registers the watcher on the Apps that follow a branch.
func (r *SourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("source").
		For(&shpyrdv1.App{}, builder.WithPredicates(predicate.NewPredicateFuncs(func(o client.Object) bool {
			app, ok := o.(*shpyrdv1.App)
			return ok && watchesGit(app)
		}))).
		// A listing may take up to 20 s; a few at a time keep hundreds of
		// apps on their schedule.
		WithOptions(controller.Options{MaxConcurrentReconciles: 4}).
		Complete(r)
}

// watchesGit says the App follows a branch or tag of a repository shpyrd
// can read: the webhook's predicate too (gitauth.Watched).
func watchesGit(app *shpyrdv1.App) bool { return gitauth.Watched(app) }

// sourceEvent is what one reconcile learnt: a push, or the branch's
// commit from a listing, or why the listing failed.
type sourceEvent struct {
	push   *gitauth.Push
	latest string
	err    error
	// gates are GitHub's answers for watch paths and wait for CI.
	gates sourceGates
	// recheck: a held or CI-failed commit's checks are read again; a
	// commit CI failed is reopened.
	recheck bool
	// resume: auto-deploy switched back on, the commit that waited is
	// decided, through watch paths and wait for CI.
	resume bool
	// fromRecord: the commit is the record's, the branch was not read (a
	// recheck or a resume without a listing).
	fromRecord bool
	// ciRead is the commit whose checks gather read (GitHub answered).
	ciRead string
}

// readBranch says the event is a check of the branch: a push, or a
// listing (failed or not); not a commit decided again from the record.
func (ev sourceEvent) readBranch() bool { return !ev.fromRecord }

// sourceUpdate is what one reconcile writes on the App.
type sourceUpdate struct {
	record gitauth.SourceRecord
	// pin is a commit to pin; "" keeps the pin.
	pin string
	// note, deployedBy and client go with a pin made for a push or a poll.
	note, deployedBy, client string
	// audit is "git.push" or "git.skip", in the name of actor.
	audit, actor string
	// target and reason are the commit and the words of a git.skip for a
	// commit not deployed.
	target, reason string
	// ask is what decideSource needs from GitHub before it decides.
	ask githubAsk
}

var errNoSuchRevision = errors.New("no such branch or tag")

func (r *SourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	app := &shpyrdv1.App{}
	if err := r.Get(ctx, req.NamespacedName, app); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !app.DeletionTimestamp.IsZero() || !watchesGit(app) {
		r.forget(app.UID)
		return ctrl.Result{}, nil
	}
	now := r.now()
	rev := app.GitRevision()
	written := gitauth.ReadSourceRecord(app.Annotations[shpyrdv1.AnnotationGitSource])
	rec := written
	if rec.Ref != rev {
		// Another branch's record: only what says the webhook works
		// carries over.
		rec = gitauth.SourceRecord{LastWebhookAt: rec.LastWebhookAt, Delivery: rec.Delivery}
	}
	rawPush := app.Annotations[shpyrdv1.AnnotationGitPush]
	push, _ := gitauth.ReadPush(rawPush)
	if push != nil && push.Ref != "refs/heads/"+rev {
		push = nil // another branch's, or a tag's: dropped below
	}
	rawCI := app.Annotations[shpyrdv1.AnnotationGitCI]
	nudge := gitauth.ReadCINudge(rawCI)
	if rawPush != "" && rawPush == r.consumedPush(app.UID) {
		// Already applied: this App is a stale read.
		push = nil
	}
	// A push whose commit is not a commit name is not trusted: dropped,
	// and the branch read instead.
	badPush := push != nil && (!pushCommit.MatchString(push.SHA) || strings.Trim(push.SHA, "0") == "")
	if badPush {
		push = nil
	}
	// The last check: the one remembered, when it is later than the one
	// written. A check remembered but not yet seen in the App may be a
	// write the cache has not caught up with: a missing pin then is not a
	// reason to read the branch, and pin it, again.
	last := rec
	var remembered bool
	last.CheckedAt, remembered = r.lastChecked(app.UID, rev, rec)
	var ev sourceEvent
	switch {
	case push != nil && pushFollows(push, rec):
		ev.push = push
	case push != nil || badPush || (app.PinnedCommit() == "" && rec.Failures == 0 && !remembered) || r.due(app, last, now):
		// A push that does not follow the commit last seen may be late,
		// or delivered again: the branch says what its head is.
		ev.latest, ev.err = r.poll(ctx, app)
		if push != nil && ev.err == nil && ev.latest == strings.ToLower(push.SHA) {
			// The head is the push's commit: what the push says of it
			// (its words, author, a skip) holds.
			ev.push, ev.latest = push, ""
		}
		if ev.err == nil && ev.push == nil && nudge != nil && ev.latest == nudge.SHA && rec.WaitsOnCI(nudge.SHA) {
			// The head is a commit whose checks changed: read them again.
			ev.recheck = true
		}
	case rec.Held != "" && rec.Held == rec.Latest && rec.Held != r.lastPinned(app.UID, rev) &&
		((nudge != nil && nudge.SHA == rec.Held) || r.ciDue(app.UID, now)):
		// A held commit: its checks are read again. One this watcher
		// pinned already is a stale read of the App.
		ev.latest, ev.recheck, ev.fromRecord = rec.Latest, true, true
	case nudge != nil && nudge.SHA == rec.Latest && rec.WaitsOnCI(nudge.SHA) && rec.Latest != r.lastPinned(app.UID, rev):
		// A commit CI failed: its checks changed (a re-run), read again.
		ev.latest, ev.recheck, ev.fromRecord = rec.Latest, true, true
	case waitingCommit(app, rec) != "" && waitingCommit(app, rec) != r.lastPinned(app.UID, rev):
		// Auto-deploy switched back on: the commit that waited is decided
		// now, from the record, through its watch paths and wait for CI.
		ev.latest, ev.resume, ev.fromRecord = rec.Latest, true, true
	default:
		r.dropPush(ctx, app, rawPush)
		if nudge == nil || !rec.WaitsOnCI(nudge.SHA) {
			r.dropAnnotation(ctx, app, shpyrdv1.AnnotationGitCI, rawCI)
		}
		return ctrl.Result{RequeueAfter: r.requeue(app.UID, last, now)}, nil
	}
	u := decideSource(app, rec, ev, now)
	// Watch paths and wait for CI need GitHub's answers: asked, then the
	// commit is decided again.
	for i := 0; i < 2 && u.ask.any(); i++ {
		r.gather(ctx, app, &ev, u.ask, now)
		u = decideSource(app, rec, ev, now)
	}
	if ev.err != nil && u.record.Error == sourceUnreadable {
		log.FromContext(ctx).Info("could not read the branch", "revision", rev, "err", ev.err.Error())
	}
	if u.pin != "" || sourceChanged(written, u.record) || written.CheckedAt == nil || now.Sub(*written.CheckedAt) >= recordRefresh {
		if err := r.apply(ctx, app, u); err != nil {
			return ctrl.Result{}, err
		}
	}
	consumed := ""
	if push != nil {
		// Applied, or answered by the listing: read again from a stale
		// App, it is only dropped.
		consumed = rawPush
	}
	if ev.readBranch() {
		r.remember(app.UID, rev, now, consumed, u.pin)
	} else if u.pin != "" {
		r.notePinned(app.UID, rev, u.pin, last.CheckedAt)
	}
	r.dropPush(ctx, app, rawPush)
	if nudge == nil || nudge.SHA == ev.ciRead || !u.record.WaitsOnCI(nudge.SHA) {
		// Read, or of a commit no longer waiting; else kept for the next
		// pass.
		r.dropAnnotation(ctx, app, shpyrdv1.AnnotationGitCI, rawCI)
	}
	r.auditUpdate(ctx, app, u)
	return ctrl.Result{RequeueAfter: r.requeue(app.UID, u.record, now)}, nil
}

// pushFollows says a push applies as it is: it follows the commit last
// seen on the branch, or nothing was seen of the branch yet.
func pushFollows(p *gitauth.Push, rec gitauth.SourceRecord) bool {
	return rec.Latest == "" || strings.EqualFold(p.Before, rec.Latest) || rec.Ref != strings.TrimPrefix(p.Ref, "refs/heads/")
}

// waitingCommit is the commit the record holds that auto-deploy would
// have deployed: newer than the pin, not skipped, read without a failure
// since. "" when there is none, or auto-deploy is off.
func waitingCommit(app *shpyrdv1.App, rec gitauth.SourceRecord) string {
	pin := app.PinnedCommit()
	if !app.AutoDeploys() || rec.Ref != app.GitRevision() || rec.Failures != 0 || rec.Latest == "" || pin == "" ||
		rec.Latest == pin || rec.Latest == rec.Skipped || rec.Latest == rec.NotDeployed || rec.Latest == rec.Held {
		return ""
	}
	return rec.Latest
}

// sourceChanged says the record shows a person something new: all but
// the time of the check.
func sourceChanged(written, next gitauth.SourceRecord) bool {
	written.CheckedAt, next.CheckedAt = nil, nil
	a, errA := json.Marshal(written)
	b, errB := json.Marshal(next)
	return errA != nil || errB != nil || string(a) != string(b)
}

// lastChecked is when the branch was last read: the check remembered, or
// the record's when it is later or nothing is remembered (after a
// restart). A check of another revision does not count. It says whether
// the remembered check is the later one.
func (r *SourceReconciler) lastChecked(uid types.UID, rev string, rec gitauth.SourceRecord) (*time.Time, bool) {
	r.mu.Lock()
	seen, ok := r.checked[uid]
	r.mu.Unlock()
	if !ok || seen.ref != rev || (rec.CheckedAt != nil && !seen.at.After(*rec.CheckedAt)) {
		return rec.CheckedAt, false
	}
	return &seen.at, true
}

// remember keeps the time of a check, and drops the checks of Apps not
// seen for a while.
// A push consumed by this check is kept with it; without one, the last
// one consumed stays. So does the last commit pinned, for the revision.
func (r *SourceReconciler) remember(uid types.UID, rev string, now time.Time, push, pinned string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.checked == nil {
		r.checked = map[types.UID]sourceCheck{}
	}
	for k, c := range r.checked {
		if now.Sub(c.at) > forgetChecks {
			delete(r.checked, k)
		}
	}
	prev := r.checked[uid]
	if push == "" {
		push = prev.push
	}
	if pinned == "" && prev.ref == rev {
		pinned = prev.pinned
	}
	r.checked[uid] = sourceCheck{at: now, ref: rev, push: push, pinned: pinned}
}

// notePinned keeps a commit pinned without a check: the check remembered
// stays as it was.
func (r *SourceReconciler) notePinned(uid types.UID, rev, sha string, checked *time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.checked == nil {
		r.checked = map[types.UID]sourceCheck{}
	}
	c, ok := r.checked[uid]
	if !ok || c.ref != rev {
		c = sourceCheck{ref: rev}
		if checked != nil {
			c.at = *checked
		}
	}
	c.pinned = sha
	r.checked[uid] = c
}

// lastPinned is the last commit this watcher pinned for rev.
func (r *SourceReconciler) lastPinned(uid types.UID, rev string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c := r.checked[uid]; c.ref == rev {
		return c.pinned
	}
	return ""
}

// consumedPush is the last push annotation applied to the App.
func (r *SourceReconciler) consumedPush(uid types.UID) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.checked[uid].push
}

// pushCommit is a commit name as a push must carry it: 40 or 64
// lowercase hex digits (and, checked apart, not all zeros: a deleted ref).
var pushCommit = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

func (r *SourceReconciler) forget(uid types.UID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.checked, uid)
	delete(r.ciChecked, uid)
	delete(r.ciLogged, uid)
}

// decideSource turns what was learnt into what is written: the record
// (Reconcile writes it when it changed), a pin when the branch moved and
// the App deploys it. The order: the branch, auto-deploy, [skip deploy],
// watch paths, wait for CI, the pin. A person's deploy and the first
// commit of a branch skip watch paths and wait for CI.
func decideSource(app *shpyrdv1.App, rec gitauth.SourceRecord, ev sourceEvent, now time.Time) sourceUpdate {
	u := sourceUpdate{record: rec}
	u.record.Ref = app.GitRevision()
	if ev.readBranch() {
		// Reading a held commit's checks is no check of the branch.
		u.record.CheckedAt = &now
	}
	if ev.err != nil {
		u.record.Error = describeSourceError(ev.err, app)
		u.record.Failures = min(u.record.Failures+1, maxFailures)
		return u
	}
	if ev.readBranch() {
		u.record.Error, u.record.Failures = "", 0
	}
	// A hold lasts while the gate holds the commit again.
	u.record.Held, u.record.HeldSince = "", nil
	sha, via, message, author := ev.latest, "poll", "", ""
	// seen is when the commit was seen: the push's own time, else now.
	seen := now
	if p := ev.push; p != nil {
		sha, via, message, author = strings.ToLower(p.SHA), "push", p.Message, p.Author
		if !p.At.IsZero() {
			seen = p.At.UTC()
		}
		u.record.LastWebhookAt = &seen
		u.record.Delivery = p.Delivery
	}
	if via == "push" || sha != rec.Latest {
		u.record.Latest, u.record.LatestAt = sha, &seen
		u.record.Message, u.record.Author, u.record.Via = message, author, via
	}
	u.actor = "git"
	if u.record.Author != "" {
		u.actor = "git:" + u.record.Author
	}
	pin := app.PinnedCommit()
	// A skip of the commit already running is no skip.
	skip := ev.push != nil && (ev.push.Skip || gitauth.SkipsDeploy(message)) && sha != pin
	if skip {
		if rec.Skipped != sha {
			u.audit = "git.skip"
		}
		u.record.Skipped = sha
	}
	suffix := ""
	switch {
	case pin == "":
		// Nothing pinned for this revision yet (a new app, a deploy, a
		// changed branch): its commit, whatever auto-deploy, watch paths
		// and wait for CI say. A person's deploy keeps their note and name.
		if by := app.Annotations[shpyrdv1.AnnotationDeployedBy]; by != "" && by != "git" && !strings.HasPrefix(by, "git:") {
			u.pin, u.record.Via, u.record.Skipped, u.audit = sha, "deploy", "", ""
			u.record.NotDeployed, u.record.NotDeployedReason = "", ""
			return u
		}
	case sha == pin, skip, sha == rec.Skipped, !app.AutoDeploys():
		// Nothing new, a commit that asked not to deploy (now, or when a
		// push said so before this poll found it), or pushes recorded only.
		return u
	case sha == rec.NotDeployed && !ev.recheck:
		// Decided before: no change in the watch paths, or its CI. Only a
		// change of its checks reopens it.
		return u
	default:
		var stop bool
		if suffix, stop = gateCommit(app, &u, rec, sha, ev, now); stop {
			return u
		}
	}
	u.pin, u.record.Skipped, u.audit = sha, "", "git.push"
	u.record.NotDeployed, u.record.NotDeployedReason = "", ""
	u.deployedBy, u.client = u.actor, u.record.Via
	switch {
	case ev.resume:
		// The commit that waited while auto-deploy was off.
		u.actor, u.deployedBy, u.client = "git", "git", "auto-deploy"
		u.note = "Commit " + shortCommit(sha) + " (auto-deploy on)"
	case u.record.Via == "push":
		u.note = pushNote(sha, u.record.Author, u.record.Message)
	default:
		u.note = "Commit " + shortCommit(sha) + " (polled)"
	}
	u.note += suffix
	return u
}

// pushNote is the release's description: "Push a1b2c3d by alice: fix login".
func pushNote(sha, author, message string) string {
	note := "Push " + shortCommit(sha)
	if author != "" {
		note += " by " + author
	}
	if line := firstLine(message); line != "" {
		note += ": " + line
	}
	return note
}

func shortCommit(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// firstLine is a commit message's subject, at most 72 characters.
func firstLine(message string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(message), "\n")
	if r := []rune(strings.TrimSpace(line)); len(r) > 72 {
		return string(r[:71]) + "…"
	}
	return strings.TrimSpace(line)
}

// describeSourceError says why the branch could not be read, and what to
// do, in the words of PR 1's refused clone.
func describeSourceError(err error, app *shpyrdv1.App) string {
	rev := app.GitRevision()
	host := "<host>"
	if r, perr := gitauth.Parse(app.Spec.Source.Git.URL); perr == nil {
		host = r.Host
	}
	switch {
	case errors.Is(err, errNoSuchRevision):
		return fmt.Sprintf("The repository has no branch or tag named %s: it was deleted or renamed. What runs keeps running; deploy another branch with `shpyrd deploy --git <url> --ref <branch>`.", rev)
	case errors.Is(err, gitauth.ErrTooManyRefs):
		return fmt.Sprintf("The repository lists more refs than shpyrd reads (%d MiB) before it reaches %s: follow a branch, or pin a commit with `shpyrd deploy --git <url> --ref <commit>`.", gitauth.MaxAdvertisement>>20, rev)
	case errors.Is(err, gitauth.ErrHostKeyChanged):
		return "shpyrd could not check the git host's identity: its SSH host key is not the one recorded with the deploy key. If the host changed its key, make a new deploy key with `shpyrd git-auth deploy-key` and add it to the repository."
	case errors.Is(err, gitauth.ErrRefusedAddress):
		return fmt.Sprintf("%s is not an address shpyrd connects to: shpyrd does not read repositories at loopback, link-local or cloud metadata addresses.", host)
	case errors.Is(err, gitauth.ErrNoAccess):
		msg := fmt.Sprintf("shpyrd could not read the repository: it is private, or does not exist under that name. Give shpyrd access: `shpyrd git-auth deploy-key` prints a key to add to the repository as a read-only deploy key, or a workspace admin sets a token for the host with `shpyrd git-auth set --host %s --token -`. shpyrd tries again by itself.", host)
		var refused *appRefusedError
		if errors.As(err, &refused) {
			msg = refused.why + " " + msg
		}
		return msg
	}
	return sourceUnreadable
}

// poll lists the repository with the build's credential and finds the
// revision's commit. A listing cut at the cap still answers when the
// revision is among what was read.
func (r *SourceReconciler) poll(ctx context.Context, app *shpyrdv1.App) (string, error) {
	b, err := lookupGitBuild(ctx, r.Client, r.SystemNamespace, r.GitHub, app)
	if err != nil {
		return "", err
	}
	list := r.LsRemote
	if list == nil {
		list = gitauth.LsRemote
	}
	// The URL the build clones, in the form the credential fits: an
	// https URL with a deploy key is read over SSH.
	refs, err := list(ctx, firstNonEmpty(b.CloneURL, app.Spec.Source.Git.URL), b)
	if err != nil && !errors.Is(err, gitauth.ErrTooManyRefs) {
		if b.AppRefused != "" {
			return "", &appRefusedError{why: b.AppRefused, err: err}
		}
		return "", err
	}
	if sha, ok := gitauth.ResolveBranch(refs, app.GitRevision()); ok {
		return strings.ToLower(sha), nil
	}
	if err != nil {
		return "", err
	}
	return "", errNoSuchRevision
}

// apply writes the record, and the pin with its note, attribution and
// time, in one merge patch: the App controller's own writes are not
// touched.
func (r *SourceReconciler) apply(ctx context.Context, app *shpyrdv1.App, u sourceUpdate) error {
	if u.pin != "" {
		// When the pin was set: a build that failed before it is not the
		// pin's (the reporter reads it).
		at := r.now().Truncate(time.Second)
		u.record.PinnedAt = &at
	}
	raw, err := json.Marshal(u.record)
	if err != nil {
		return err
	}
	ann := map[string]any{shpyrdv1.AnnotationGitSource: string(raw)}
	if u.pin != "" {
		ann[shpyrdv1.AnnotationGitCommit] = u.pin
		ann[shpyrdv1.AnnotationGitCommitRef] = app.GitRevision()
		if u.note != "" {
			ann[shpyrdv1.AnnotationReleaseNote] = u.note
		}
		if u.deployedBy != "" {
			ann[shpyrdv1.AnnotationDeployedBy] = u.deployedBy
			ann[shpyrdv1.AnnotationDeployedClient] = u.client
			ann[shpyrdv1.AnnotationDeployedSubject] = nil
		}
	}
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": ann}})
	if err != nil {
		return err
	}
	return r.Patch(ctx, app, client.RawPatch(types.MergePatchType, patch))
}

// dropPush removes the push this reconcile read, unless a newer one
// arrived meanwhile: that one is read next.
func (r *SourceReconciler) dropPush(ctx context.Context, app *shpyrdv1.App, raw string) {
	if raw == "" {
		return
	}
	r.dropAnnotation(ctx, app, shpyrdv1.AnnotationGitPush, raw)
}

// dropAnnotation removes an annotation this reconcile read, unless it
// changed meanwhile: the newer one is read next.
func (r *SourceReconciler) dropAnnotation(ctx context.Context, app *shpyrdv1.App, key, raw string) {
	if raw == "" {
		return
	}
	if _, err := removeAnnotationIfUnchanged(ctx, r.Client, app, key, raw); err != nil {
		logRemoveAnnotation(ctx, err, "kept a newer one for the next pass", "an annotation", "annotation", key)
	}
}

// noteCI keeps when the App's held commit's checks were read.
func (r *SourceReconciler) noteCI(uid types.UID, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ciChecked == nil {
		r.ciChecked = map[types.UID]time.Time{}
	}
	r.ciChecked[uid] = now
}

// ciDue says a held commit's checks are to be read again: never read by
// this watcher, or 90 % of heldRecheck ago (requeues are spread).
func (r *SourceReconciler) ciDue(uid types.UID, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	last, ok := r.ciChecked[uid]
	return !ok || now.Sub(last) >= heldRecheck*9/10
}

// requeue is the wait until the next pass: the next check of the branch,
// or sooner, the next read of a held commit's checks.
func (r *SourceReconciler) requeue(uid types.UID, rec gitauth.SourceRecord, now time.Time) time.Duration {
	d := r.untilDue(rec, now)
	if rec.Held == "" || rec.Held != rec.Latest {
		return d
	}
	r.mu.Lock()
	last, ok := r.ciChecked[uid]
	r.mu.Unlock()
	wait := heldRecheck
	if ok {
		wait = max(heldRecheck-now.Sub(last), time.Second)
	}
	return min(d, wait)
}

func (r *SourceReconciler) auditUpdate(ctx context.Context, app *shpyrdv1.App, u sourceUpdate) {
	if r.Kube == nil || u.audit == "" {
		return
	}
	entry := audit.Entry{
		Actor: u.actor, Action: u.audit, Target: shortCommit(firstNonEmpty(u.target, u.pin, u.record.Skipped)),
		Detail: firstNonEmpty(u.reason, firstLine(u.record.Message)), Via: "controller", Client: u.record.Via, Realm: "workspace",
	}
	if err := audit.Record(ctx, r.Kube, audit.AppRefIn(app.Namespace, app.Name), entry); err != nil {
		log.FromContext(ctx).Info("could not audit the push", "err", err.Error())
	}
}

// due says the branch should be read now: never read, a deploy asked
// after the last check, or the wait since the last check has passed (90 %
// of it: requeues are spread ±10 %).
func (r *SourceReconciler) due(app *shpyrdv1.App, rec gitauth.SourceRecord, now time.Time) bool {
	if rec.CheckedAt == nil {
		return true
	}
	if asked, err := time.Parse(time.RFC3339Nano, app.Annotations[configvars.AnnotationDeployRequested]); err == nil && asked.After(*rec.CheckedAt) {
		return true
	}
	return now.Sub(*rec.CheckedAt) >= r.wait(rec, now)*9/10
}

// wait is the time between checks: the interval (an hour while webhooks
// arrive), or after failures a backoff from 30 s that doubles up to it.
func (r *SourceReconciler) wait(rec gitauth.SourceRecord, now time.Time) time.Duration {
	every := r.PollInterval
	if every <= 0 {
		every = DefaultPollInterval
	}
	if rec.LastWebhookAt != nil && now.Sub(*rec.LastWebhookAt) < webhookRecent {
		every = max(every, webhookInterval)
	}
	if rec.Failures > 0 {
		return min(every, errorBackoff<<min(rec.Failures-1, 10))
	}
	return every
}

func (r *SourceReconciler) untilDue(rec gitauth.SourceRecord, now time.Time) time.Duration {
	left := r.wait(rec, now)
	if rec.CheckedAt != nil {
		left -= now.Sub(*rec.CheckedAt)
	}
	spread := r.Jitter
	if spread == nil {
		spread = jitter
	}
	return spread(max(left, time.Second))
}

// jitter spreads d by ±10 %, so apps checked together drift apart.
func jitter(d time.Duration) time.Duration {
	return d + time.Duration((rand.Float64()*0.2-0.1)*float64(d))
}

func (r *SourceReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}
