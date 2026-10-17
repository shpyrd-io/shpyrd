package controller

import (
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

// The App controller's side of the source watcher (RFC-0018): a branch
// builds the commit pinned for it, and status.source shows what the
// watcher saw.

// reasonWaitingForCommit is the Built condition's reason while a branch
// has no commit pinned yet.
const reasonWaitingForCommit = "WaitingForCommit"

// reasonSourceUnreadable is the Built and Ready conditions' reason while
// the branch cannot be read and nothing is pinned: no build failed.
const reasonSourceUnreadable = "SourceUnreadable"

// failedReason is the reason of a Built or Ready condition that is False
// for the build.
func (b buildState) failedReason() string {
	if b.Reason == reasonSourceUnreadable {
		return reasonSourceUnreadable
	}
	return "BuildFailed"
}

// waitingForCommit says why a watched App builds nothing yet: the watcher
// has not pinned a commit (a few seconds), or it cannot read the branch
// (failed, with its words). "" when a commit is pinned or nothing is
// watched, and for an app built from its branch before the watcher, never
// pinned, whose branch cannot be read: it builds the branch as given, as
// before (BuildRevision), and kpack polls it.
func waitingForCommit(app *shpyrdv1.App) (string, bool) {
	if !watchesGit(app) || app.PinnedCommit() != "" {
		return "", false
	}
	if rec := gitauth.ReadSourceRecord(app.Annotations[shpyrdv1.AnnotationGitSource]); rec.Ref == app.GitRevision() && rec.Error != "" {
		if _, pinned := app.Annotations[shpyrdv1.AnnotationGitCommit]; len(app.Status.Releases) > 0 && !pinned {
			return "", false
		}
		return rec.Error, true
	}
	return "Reading the branch's latest commit.", false
}

// sourceStatus is status.source: the commit pinned, and what the watcher
// recorded of the revision the App follows. nil when nothing is watched.
func sourceStatus(app *shpyrdv1.App) *shpyrdv1.SourceStatus {
	if !watchesGit(app) {
		return nil
	}
	st := &shpyrdv1.SourceStatus{Commit: app.PinnedCommit()}
	rec := gitauth.ReadSourceRecord(app.Annotations[shpyrdv1.AnnotationGitSource])
	if rec.Ref != app.GitRevision() {
		return st
	}
	st.Latest, st.Message, st.Author, st.Via, st.Skipped, st.Error = rec.Latest, rec.Message, rec.Author, rec.Via, rec.Skipped, rec.Error
	st.LatestAt, st.LastWebhookAt, st.CheckedAt = metaTime(rec.LatestAt), metaTime(rec.LastWebhookAt), metaTime(rec.CheckedAt)
	st.Held, st.HeldSince = rec.Held, metaTime(rec.HeldSince)
	st.NotDeployed, st.NotDeployedReason = rec.NotDeployed, rec.NotDeployedReason
	return st
}

func metaTime(t *time.Time) *metav1.Time {
	if t == nil {
		return nil
	}
	m := metav1.NewTime(*t)
	return &m
}

// setSourceCondition mirrors status.source in SourceReady.
func setSourceCondition(app *shpyrdv1.App) {
	st := app.Status.Source
	switch {
	case st == nil:
		meta.RemoveStatusCondition(&app.Status.Conditions, shpyrdv1.ConditionSourceReady)
	case st.Error != "":
		setCondition(app, shpyrdv1.ConditionSourceReady, metav1.ConditionFalse, "Unreadable", st.Error)
	case st.Commit == "":
		setCondition(app, shpyrdv1.ConditionSourceReady, metav1.ConditionUnknown, "Reading", "Reading the branch's latest commit.")
	default:
		setCondition(app, shpyrdv1.ConditionSourceReady, metav1.ConditionTrue, "Pinned", "commit "+shortCommit(st.Commit))
	}
}
