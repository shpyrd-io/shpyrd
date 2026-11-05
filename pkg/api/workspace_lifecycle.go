package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// The rest of the lifecycle at the API (#274): workspaces made for someone
// who has not arrived yet (an agent's), claimed by the first sign-in or on
// purpose; the hold that keeps policies off a workspace; an owner deleting
// their own workspace; and the email owners get when a deletion is
// scheduled.

// parseGrace reads a duration where "d" means days; "now" and "0" are
// zero; "" is def.
func parseGrace(s string, def time.Duration) (time.Duration, error) {
	s = strings.TrimSpace(s)
	switch s {
	case "":
		return def, nil
	case "now", "0":
		return 0, nil
	}
	if strings.HasSuffix(s, "d") {
		var days int
		if _, err := fmt.Sscanf(s, "%dd", &days); err != nil || days < 0 {
			return 0, fmt.Errorf("%q is not a number of days", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("%q is not a duration (7d, 24h, now)", s)
	}
	return d, nil
}

// DefaultAgentExpiry is how long a workspace an agent creates stays
// unclaimed before it expires, when the request names no time.
const DefaultAgentExpiry = 24 * time.Hour

// ParseExpiry reads a CreateWorkspaceRequest's ExpiresIn: nil for "" (no
// expiry), else now plus the duration.
func ParseExpiry(expiresIn string) (*time.Time, error) {
	if strings.TrimSpace(expiresIn) == "" {
		return nil, nil
	}
	d, err := parseGrace(expiresIn, 0)
	if err != nil {
		return nil, fmt.Errorf("expiresIn: %w", err)
	}
	if d == 0 {
		return nil, errors.New("expiresIn: must be a positive duration")
	}
	t := time.Now().Add(d).UTC()
	return &t, nil
}

// UnclaimedQuota bounds how many workspaces may wait to be claimed at
// once: per creator (an agent, a token) and on the whole platform. Past
// it, creation answers 429 until some are claimed or expire.
type UnclaimedQuota struct {
	PerCreator int
	Total      int
}

// DefaultUnclaimedQuota is the platform's: ten per creator, a hundred in
// all.
var DefaultUnclaimedQuota = UnclaimedQuota{PerCreator: 10, Total: 100}

// CheckUnclaimedQuota counts the live workspaces that wait to be claimed
// (ExpiresAt set) and refuses one more for createdBy when a bound is
// reached; the error is an *apiError (429) saying the counts.
func CheckUnclaimedQuota(ctx context.Context, st store.Store, createdBy string, q UnclaimedQuota) error {
	all, err := st.ListWorkspaces(ctx)
	if err != nil {
		return err
	}
	total, mine := 0, 0
	for i := range all {
		w := &all[i]
		if w.ExpiresAt == nil || w.Gone() {
			continue
		}
		total++
		if createdBy != "" && w.CreatedBy == createdBy {
			mine++
		}
	}
	if q.Total > 0 && total >= q.Total {
		return &apiError{http.StatusTooManyRequests, fmt.Sprintf("%d workspaces wait to be claimed on this platform, the most allowed; claim or delete some first", total)}
	}
	if q.PerCreator > 0 && createdBy != "" && mine >= q.PerCreator {
		return &apiError{http.StatusTooManyRequests, fmt.Sprintf("%s has %d workspaces waiting to be claimed, the most allowed; claim or delete some first", createdBy, mine)}
	}
	return nil
}

// ClaimWorkspace clears a workspace's expiry: someone arrived. Nothing to
// do on one that has none.
func ClaimWorkspace(ctx context.Context, st store.Store, ws *store.Workspace) (*store.Workspace, error) {
	if ws.ExpiresAt == nil {
		return ws, nil
	}
	return st.SetWorkspaceExpiry(ctx, ws.Slug, nil)
}

// claimOnSignIn clears the expiry of a workspace a person just signed in
// to (#274): a human owner arrived, the workspace is theirs to keep.
func (s *Server) claimOnSignIn(ctx context.Context, slug string) {
	if slug == "" {
		return
	}
	ws, err := s.store.Workspace(ctx, slug)
	if err != nil || ws.ExpiresAt == nil || ws.Gone() {
		return
	}
	if _, err := s.store.SetWorkspaceExpiry(ctx, ws.Slug, nil); err != nil {
		s.log.Warn("could not claim the workspace at sign-in", "workspace", slug, "err", err.Error())
		return
	}
	s.log.Info("workspace claimed by a sign-in", "workspace", slug, "origin", ws.Origin)
}

// deleteOwnWorkspace is DELETE /api/workspace at the workspace's own door,
// by an owner (RFC-0033's owner promise): the workspace is suspended and
// scheduled DefaultDeleteGrace ahead; any owner cancels by activating.
// The platform may refuse first (Options.WorkspaceDeleteAllowed: in the
// cloud, a paid subscription must be cancelled before).
func (s *Server) deleteOwnWorkspace(c *gin.Context) {
	ws, err := s.tenant(c)
	if err != nil || ws == nil {
		abort(c, http.StatusNotFound, errConsoleNotWorkspace)
		return
	}
	var req DeleteWorkspaceRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			abort(c, http.StatusBadRequest, err)
			return
		}
	}
	after, err := parseGrace(req.After, DefaultDeleteGrace)
	if err != nil {
		abort(c, http.StatusBadRequest, fmt.Errorf("after: %w", err))
		return
	}
	ctx := c.Request.Context()
	if s.opts.WorkspaceDeleteAllowed != nil {
		if err := s.opts.WorkspaceDeleteAllowed(ctx, ws); err != nil {
			abort(c, http.StatusConflict, err)
			return
		}
	}
	out, err := ScheduleWorkspaceDeletion(ctx, s.store, ws, after, store.DeleteReasonOwner)
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
	s.audit(c, "", "workspace.delete", ws.Slug, fmt.Sprintf("by an owner; after %s", out.DeleteAfter.UTC().Format(time.RFC3339)))
	s.mailOwnersScheduled(ctx, out)
	c.JSON(http.StatusAccepted, s.workspaceSummaryCore(ctx, out))
}

// mailOwnersScheduled tells every owner a deletion is scheduled: when,
// why, and that activating cancels it. Nothing without mail, or owners.
func (s *Server) mailOwnersScheduled(ctx context.Context, ws *store.Workspace) {
	if s.mailer == nil || !s.mailer.Configured(ctx) || ws.DeleteAfter == nil {
		return
	}
	roles, err := s.store.ListMemberships(ctx, ws.Slug)
	if err != nil {
		return
	}
	var to []string
	for _, m := range roles {
		if m.Role == store.WorkspaceRoleOwner {
			to = append(to, m.Email)
		}
	}
	if len(to) == 0 {
		return
	}
	if err := s.mailer.Send(ctx, scheduledDeletionMail(ws, to, s.dashboardURLOf(ws))); err != nil {
		s.log.Warn("could not mail the owners about the scheduled deletion", "workspace", ws.Slug, "err", err.Error())
	}
}

// scheduledDeletionMail is the email owners get when their workspace is
// scheduled for deletion.
func scheduledDeletionMail(ws *store.Workspace, to []string, dashboard string) ext.Message {
	name := firstNonEmpty(ws.Name, ws.Slug)
	when := ws.DeleteAfter.UTC().Format("Jan 2, 2006 at 15:04 UTC")
	why := map[string]string{
		store.DeleteReasonOwner:    "one of its owners asked for it",
		store.DeleteReasonOperator: "the platform operator scheduled it",
		store.DeleteReasonDunning:  "its invoice stayed unpaid",
		store.DeleteReasonIdle:     "nobody used it for a long time",
		store.DeleteReasonExpired:  "nobody claimed it",
	}[ws.DeleteReason]
	if why == "" {
		why = "it was scheduled"
	}
	text := fmt.Sprintf(`The workspace %s will be deleted on %s because %s.

Until then it is suspended: its apps and databases are stopped, nothing is
removed. To keep it, activate it again before that date (%s) or write to
the people who run the platform.

When it is deleted, its projects, databases, volumes, buckets, images and
people go; its name stays unavailable for thirty days.
`, name, when, why, dashboard)
	return ext.Message{To: to, Subject: fmt.Sprintf("%s will be deleted on %s", name, ws.DeleteAfter.UTC().Format("Jan 2")), Text: text}
}
