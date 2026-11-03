package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Deleting a workspace (#274). Every deletion is a date on the row that
// the controller's reaper executes; until then the workspace is suspended
// and `activate` cancels it. This file is the API's half: what may be
// scheduled, by whom, and the words at the door of a workspace that is
// gone. The cloud layer mounts the route and reaches the same function.

// DefaultDeleteGrace is how long a human workspace stays suspended before
// a scheduled deletion runs, when the request names no time: a week to
// change one's mind.
const DefaultDeleteGrace = 7 * 24 * time.Hour

// DeleteWorkspaceRequest is DELETE /api/workspaces/:slug.
type DeleteWorkspaceRequest struct {
	// After is how long to wait: a duration ("7d", "24h"), "now" for at
	// once, "" for DefaultDeleteGrace.
	After string `json:"after,omitempty"`
	// Reason is kept with the schedule and told in the audit: a
	// store.DeleteReason value; DeleteReasonOperator when empty.
	Reason string `json:"reason,omitempty"`
}

// ParseAfter reads an After value: "now", "" or a duration where "d" means
// days.
func (r DeleteWorkspaceRequest) ParseAfter() (time.Duration, error) {
	d, err := parseGrace(r.After, DefaultDeleteGrace)
	if err != nil {
		return 0, fmt.Errorf("after: %w", err)
	}
	return d, nil
}

// ErrWorkspaceProtected refuses to schedule a protected workspace.
var ErrWorkspaceProtected = errors.New("this workspace is protected: clear the hold first")

// ScheduleWorkspaceDeletion suspends the workspace when it is active and
// sets its deletion date. It refuses the default workspace, an
// operator-owned one, a protected one, and one already gone; the error is
// an *apiError with the status to answer. The caller audits and tells the
// front doors (workspacesChanged).
func ScheduleWorkspaceDeletion(ctx context.Context, st store.Store, ws *store.Workspace, after time.Duration, reason string) (*store.Workspace, error) {
	if reason == "" {
		reason = store.DeleteReasonOperator
	}
	switch {
	case ws.Gone():
		return nil, &apiError{http.StatusConflict, fmt.Sprintf("workspace %s is already %s", ws.Slug, ws.Status)}
	case ws.Slug == store.DefaultWorkspaceSlug(ctx, st):
		return nil, &apiError{http.StatusBadRequest, "the default workspace cannot be deleted"}
	case ws.OwnedByOperator():
		return nil, &apiError{http.StatusBadRequest, "an operator's workspace is not deleted this way"}
	case ws.Protected:
		return nil, &apiError{http.StatusConflict, ErrWorkspaceProtected.Error()}
	}
	if ws.Status == store.WorkspaceActive {
		suspend := store.SuspendReasonOperator
		if reason == store.DeleteReasonOwner {
			suspend = store.SuspendReasonOwner
		}
		if _, err := st.SuspendWorkspace(ctx, ws.Slug, suspend); err != nil {
			return nil, err
		}
	}
	out, err := st.ScheduleWorkspaceDeletion(ctx, ws.Slug, time.Now().Add(after), reason)
	if errors.Is(err, store.ErrConflict) {
		return nil, &apiError{http.StatusConflict, fmt.Sprintf("workspace %s cannot be scheduled now (%s)", ws.Slug, ws.Status)}
	}
	return out, err
}

// deleteWorkspace is DELETE /api/workspaces/:slug on the console
// (cluster admin): the open-source server's, where the one workspace
// refuses; the cloud layer serves its own with the same body.
func (s *Server) deleteWorkspace(c *gin.Context) {
	var req DeleteWorkspaceRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			abort(c, http.StatusBadRequest, err)
			return
		}
	}
	after, err := req.ParseAfter()
	if err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	ctx := c.Request.Context()
	ws, err := s.store.Workspace(ctx, c.Param("slug"))
	if err != nil {
		storeErr(c, err, "workspace")
		return
	}
	out, err := ScheduleWorkspaceDeletion(ctx, s.store, ws, after, req.Reason)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			abort(c, ae.status, ae)
			return
		}
		storeErr(c, err, "workspace")
		return
	}
	s.workspacesChanged()
	s.audit(c, "", "workspace.delete", ws.Slug, fmt.Sprintf("%s; after %s", out.DeleteReason, out.DeleteAfter.UTC().Format(time.RFC3339)))
	s.mailOwnersScheduled(ctx, out)
	c.JSON(http.StatusAccepted, s.workspaceSummaryCore(ctx, out))
}

// FillLifecycle copies a workspace's lifecycle onto its summary.
func FillLifecycle(sum *WorkspaceSummary, w *store.Workspace) {
	sum.SuspendedAt, sum.SuspendReason = w.SuspendedAt, w.SuspendReason
	sum.DeleteAfter, sum.DeleteReason = w.DeleteAfter, w.DeleteReason
	sum.ExpiresAt, sum.Origin, sum.CreatedBy, sum.Protected = w.ExpiresAt, w.Origin, w.CreatedBy, w.Protected
	sum.LastActivityAt, sum.DeletedAt, sum.Deletion = w.LastActivityAt, w.DeletedAt, w.Deletion
}

// OffPage says what the door of a workspace that is off tells a visitor:
// the title and the text, by its status.
func OffPage(ws *store.Workspace) (status int, title, text string) {
	switch ws.Status {
	case store.WorkspaceDeleting, store.WorkspaceDeleted:
		return http.StatusGone, "Workspace deleted", "This workspace was deleted; nothing answers here any more."
	}
	return http.StatusForbidden, "Workspace suspended", "This workspace is suspended."
}

// offError is the JSON answer at the door of a workspace that is off.
func offError(ws *store.Workspace) (int, error) {
	if ws.Gone() {
		return http.StatusGone, errors.New("this workspace was deleted")
	}
	return http.StatusForbidden, errors.New("this workspace is suspended")
}
