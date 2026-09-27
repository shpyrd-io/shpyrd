package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shpyrd-io/shpyrd/pkg/authz"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Invitations (RFC-0033): an owner or admin invites a person by email
// with a workspace role (and, optionally, a team). The link carries a
// random token shown once; the store keeps its hash. Signing in with the
// invited email accepts the invitation, link or no link: the person gets
// the role and the invitation goes. When the mail extension (RFC-0013) is
// configured the link is emailed too.

// InvitationTTL is how long an invitation can be accepted.
const InvitationTTL = 7 * 24 * time.Hour

// InvitationView is one pending invitation, as owners and admins see it.
type InvitationView struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	Team      string    `json:"team,omitempty"`
	InvitedBy string    `json:"invitedBy,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Expired   bool      `json:"expired"`
}

func invitationView(inv store.Invitation) InvitationView {
	return InvitationView{ID: inv.ID, Email: inv.Email, Role: inv.Role, Team: inv.Team, InvitedBy: inv.InvitedBy, CreatedAt: inv.CreatedAt, ExpiresAt: inv.ExpiresAt, Expired: inv.Expired(time.Now())}
}

// InviteRequest is POST /api/workspace/invitations.
type InviteRequest struct {
	Email string `json:"email" binding:"required"`
	Role  string `json:"role"` // member when empty
	Team  string `json:"team"`
}

// InviteResult is what an invitation produced: a link (shown once, sent by
// email when mail is configured), or, for a person who has signed in
// before, the role applied at once.
type InviteResult struct {
	Email string `json:"email"`
	Role  string `json:"role"`
	Team  string `json:"team,omitempty"`
	// Applied says the person was known already and holds the role now;
	// there is no link.
	Applied    bool            `json:"applied"`
	Invitation *InvitationView `json:"invitation,omitempty"`
	Link       string          `json:"link,omitempty"`
	// Emailed says the link went out by email; MailError says why not
	// (nothing when mail is not configured: the inviter sends the link).
	Emailed   bool   `json:"emailed"`
	MailError string `json:"mailError,omitempty"`
}

// InvitationPublicView is GET /api/invitations/:token: what the holder of
// a link sees before signing in.
type InvitationPublicView struct {
	Workspace WorkspaceRef `json:"workspace"`
	Email     string       `json:"email"`
	Role      string       `json:"role"`
	Team      string       `json:"team,omitempty"`
	InvitedBy string       `json:"invitedBy,omitempty"`
	ExpiresAt time.Time    `json:"expiresAt"`
	Expired   bool         `json:"expired"`
	// URL is where the workspace's dashboard (and sign-in) answers.
	URL string `json:"url"`
}

// newInvitationToken mints the link's secret and the hash the store keeps.
func newInvitationToken() (token, hash string, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, invitationHash(token), nil
}

func invitationHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// invitationLink is the URL the invited person opens.
func (s *Server) invitationLink(ws *store.Workspace, token string) string {
	return strings.TrimSuffix(s.dashboardURLOf(ws), "/") + "/invite/" + token
}

// pendingInvitation is the live invitation of an email in a workspace,
// nil when there is none (or the store is down: a sign-in must not
// depend on it).
func (s *Server) pendingInvitation(ctx context.Context, ws, email string) *store.Invitation {
	list, err := s.store.ListInvitations(ctx, ws)
	if err != nil {
		return nil
	}
	email = strings.ToLower(strings.TrimSpace(email))
	now := time.Now()
	for i := range list {
		if list[i].Email == email && !list[i].Expired(now) {
			return &list[i]
		}
	}
	return nil
}

func (s *Server) listInvitations(c *gin.Context) {
	list, err := s.store.ListInvitations(c.Request.Context(), s.workspace(c))
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	out := make([]InvitationView, 0, len(list))
	for _, inv := range list {
		out = append(out, invitationView(inv))
	}
	c.JSON(http.StatusOK, out)
}

// createInvitation is POST /api/workspace/invitations. Inviting again
// replaces the pending invitation (a new link); inviting someone who has
// signed in before applies the role at once. Inviting as owner takes an
// owner.
func (s *Server) createInvitation(c *gin.Context) {
	var req InviteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, err)
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !validEmail(email) {
		abort(c, http.StatusBadRequest, errors.New("that is not an email address"))
		return
	}
	role := firstNonEmpty(strings.TrimSpace(req.Role), store.WorkspaceRoleMember)
	if !store.ValidWorkspaceRole(role) {
		abort(c, http.StatusBadRequest, errors.New("role must be owner, admin or member"))
		return
	}
	team := strings.TrimSpace(req.Team)
	if team == store.TeamEveryone {
		abort(c, http.StatusBadRequest, fmt.Errorf("everyone who signs in is in the %s team already", store.TeamEveryone))
		return
	}
	ctx := c.Request.Context()
	s.claimOwnershipInBootstrap(c)
	roles, err := s.rolesOf(c)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	if role == store.WorkspaceRoleOwner && !roles.Can(authz.WorkspaceOwner, "") {
		abort(c, http.StatusForbidden, denial(roles, authz.WorkspaceOwner, ""))
		return
	}
	me, _ := ext.IdentityFrom(c)
	if strings.EqualFold(me.Email, email) {
		abort(c, http.StatusBadRequest, errors.New("you cannot invite yourself"))
		return
	}
	ws, err := s.store.Workspace(ctx, s.workspace(c))
	if err != nil {
		storeErr(c, err, "workspace")
		return
	}
	if team != "" {
		if _, err := s.store.GetTeam(ctx, ws.Slug, team); err != nil {
			storeErr(c, err, "team")
			return
		}
	}
	result := InviteResult{Email: email, Role: role, Team: team}

	// Someone the workspace knows (and has not suspended) gets the role now.
	if known, err := s.store.GetIdentity(ctx, ws.Slug, email); err == nil && known.Status != store.StatusSuspended {
		snap, _ := s.authz.SnapshotFor(ctx, ws.Slug)
		if snap != nil && snap.WorkspaceRole(email) == store.WorkspaceRoleOwner && !roles.Can(authz.WorkspaceOwner, "") {
			abort(c, http.StatusForbidden, denial(roles, authz.WorkspaceOwner, ""))
			return
		}
		if err := s.applyRole(c, ws.Slug, email, role, team, "invited"); err != nil {
			storeErr(c, err, "person")
			return
		}
		result.Applied = true
		c.JSON(http.StatusOK, result)
		return
	}

	token, hash, err := newInvitationToken()
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return
	}
	inv, err := s.store.CreateInvitation(ctx, ws.Slug, store.Invitation{
		Email: email, Role: role, Team: team, InvitedBy: strings.ToLower(me.Email), ExpiresAt: time.Now().Add(InvitationTTL),
	}, hash)
	if err != nil {
		storeErr(c, err, "invitation")
		return
	}
	view := invitationView(*inv)
	result.Invitation = &view
	result.Link = s.invitationLink(ws, token)
	s.audit(c, "", "workspace.invitation.create", email, "role "+role+teamDetail(team))

	if s.mailer != nil && s.mailer.Configured(ctx) {
		if err := s.mailer.Send(ctx, s.invitationMail(ws, *inv, me, result.Link)); err != nil {
			result.MailError = err.Error()
			s.log.Warn("invitation email failed", "to", email, "err", err.Error())
			s.audit(c, "", "workspace.invitation.mail_failed", email, err.Error())
		} else {
			result.Emailed = true
		}
	}
	c.JSON(http.StatusCreated, result)
}

func teamDetail(team string) string {
	if team == "" {
		return ""
	}
	return ", team " + team
}

// applyRole gives a person a workspace role and, when asked, a team; how
// says what brought it (an invitation accepted, or applied at once).
func (s *Server) applyRole(c *gin.Context, ws, email, role, team, how string) error {
	ctx := c.Request.Context()
	if _, err := s.store.PutMembership(ctx, ws, email, role); err != nil {
		return err
	}
	if team != "" {
		t, err := s.store.GetTeam(ctx, ws, team)
		if err == nil && !t.Everyone {
			present := false
			for _, m := range t.Members {
				if m == email {
					present = true
				}
			}
			if !present {
				t.Members = append(t.Members, email)
				if _, _, err := s.store.PutTeam(ctx, ws, *t); err != nil {
					return err
				}
			}
		}
	}
	s.membershipChanged()
	s.audit(c, "", "workspace.role", email, "-> "+role+teamDetail(team)+" ("+how+")")
	return nil
}

func (s *Server) deleteInvitation(c *gin.Context) {
	ctx := c.Request.Context()
	list, err := s.store.ListInvitations(ctx, s.workspace(c))
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	id := c.Param("id")
	email := ""
	for _, inv := range list {
		if inv.ID == id {
			email = inv.Email
		}
	}
	if email == "" {
		abort(c, http.StatusNotFound, errors.New("invitation not found"))
		return
	}
	if err := s.store.DeleteInvitation(ctx, s.workspace(c), id); err != nil {
		storeErr(c, err, "invitation")
		return
	}
	s.audit(c, "", "workspace.invitation.revoke", email, "")
	c.Status(http.StatusNoContent)
}

// invitationOf resolves the token in the URL to a live invitation of the
// request's workspace; it answers the request itself when it cannot.
func (s *Server) invitationOf(c *gin.Context) (*store.Invitation, *store.Workspace, bool) {
	ctx := c.Request.Context()
	token := c.Param("token")
	if len(token) < 16 || len(token) > 128 {
		abort(c, http.StatusNotFound, errors.New("this invitation link is not valid"))
		return nil, nil, false
	}
	inv, err := s.store.InvitationByToken(ctx, invitationHash(token))
	if errors.Is(err, store.ErrNotFound) {
		abort(c, http.StatusNotFound, errors.New("this invitation was used, revoked, or never existed"))
		return nil, nil, false
	}
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return nil, nil, false
	}
	ws, err := s.store.Workspace(ctx, s.workspace(c))
	if err != nil || ws.ID != inv.WorkspaceID {
		// The link belongs to another workspace's dashboard.
		abort(c, http.StatusNotFound, errors.New("this invitation is for another workspace: open the link you were sent"))
		return nil, nil, false
	}
	return inv, ws, true
}

// getInvitation is GET /api/invitations/:token (public): what the link
// holder sees before signing in.
func (s *Server) getInvitation(c *gin.Context) {
	inv, ws, ok := s.invitationOf(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, InvitationPublicView{
		Workspace: WorkspaceRef{Slug: ws.Slug, Name: ws.Name, Implicit: ws.Implicit()},
		Email:     inv.Email, Role: inv.Role, Team: inv.Team, InvitedBy: inv.InvitedBy,
		ExpiresAt: inv.ExpiresAt, Expired: inv.Expired(time.Now()), URL: s.dashboardURLOf(ws),
	})
}

// acceptInvitation is POST /api/invitations/:token/accept: the signed-in
// person takes the role. Their email must be the invited one.
func (s *Server) acceptInvitation(c *gin.Context) {
	inv, ws, ok := s.invitationOf(c)
	if !ok {
		return
	}
	me, ok := ext.IdentityFrom(c)
	if !ok || me.Email == "" {
		abort(c, http.StatusForbidden, errors.New("sign in with the invited address to accept"))
		return
	}
	if !strings.EqualFold(me.Email, inv.Email) {
		abort(c, http.StatusForbidden, fmt.Errorf("this invitation is for %s; you are signed in as %s", inv.Email, strings.ToLower(me.Email)))
		return
	}
	if inv.Expired(time.Now()) {
		abort(c, http.StatusGone, errors.New("this invitation has expired: ask to be invited again"))
		return
	}
	if err := s.accept(c, ws.Slug, *inv); err != nil {
		storeErr(c, err, "invitation")
		return
	}
	c.JSON(http.StatusOK, gin.H{"workspace": ws.Slug, "role": inv.Role, "team": inv.Team, "next": "/"})
}

// accept turns an invitation into a membership and removes it.
func (s *Server) accept(c *gin.Context, ws string, inv store.Invitation) error {
	if err := s.applyRole(c, ws, inv.Email, inv.Role, inv.Team, "invitation accepted"); err != nil {
		return err
	}
	if err := s.store.DeleteInvitation(c.Request.Context(), ws, inv.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	s.audit(c, "", "workspace.invitation.accept", inv.Email, "role "+inv.Role+teamDetail(inv.Team)+", invited by "+firstNonEmpty(inv.InvitedBy, "-"))
	return nil
}

// acceptPendingInvitation is called at sign-in: the person's pending
// invitation, if any, is accepted without the link. Failures are logged,
// never surfaced: a sign-in does not depend on it.
func (s *Server) acceptPendingInvitation(c *gin.Context, id ext.Identity) {
	if id.Email == "" || id.Provider == "token" || id.Provider == "kubeconfig" || id.Subject == "admin-token" {
		return
	}
	ws := s.workspace(c)
	inv := s.pendingInvitation(c.Request.Context(), ws, id.Email)
	if inv == nil {
		return
	}
	if err := s.accept(c, ws, *inv); err != nil {
		s.log.Warn("could not accept invitation at sign-in", "email", id.Email, "err", err.Error())
	}
}

// invitationMail is the email carrying the link (RFC-0013).
func (s *Server) invitationMail(ws *store.Workspace, inv store.Invitation, by ext.Identity, link string) ext.Message {
	inviter := firstNonEmpty(by.Name, by.Email, "An administrator")
	if by.Name != "" && by.Email != "" {
		inviter = by.Name + " (" + by.Email + ")"
	}
	name := firstNonEmpty(ws.Name, ws.Slug)
	what := "as " + article(inv.Role) + " " + inv.Role
	if inv.Team != "" {
		what += ", in team " + inv.Team
	}
	until := inv.ExpiresAt.UTC().Format("Jan 2, 2006")
	text := fmt.Sprintf(`%s invited you to join %s %s.

Accept the invitation by opening this link and signing in as %s:

  %s

The link works until %s. If you were not expecting this, ignore it.
`, inviter, name, what, inv.Email, link, until)
	htmlBody := fmt.Sprintf(`<!doctype html><html><body style="margin:0;padding:32px 16px;background:#f6f6f6;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;color:#111">
<div style="max-width:520px;margin:0 auto;background:#fff;border-radius:8px;padding:32px;border:1px solid #e5e5e5">
<div style="font-weight:700;font-size:20px;letter-spacing:-0.02em;color:#ff4f00;margin-bottom:24px">shpyrd</div>
<p style="font-size:16px;line-height:1.5;margin:0 0 16px"><strong>%s</strong> invited you to join <strong>%s</strong> %s.</p>
<p style="font-size:14px;line-height:1.5;margin:0 0 24px;color:#444">Accept by opening the link and signing in as <strong>%s</strong>.</p>
<p style="margin:0 0 24px"><a href="%s" style="display:inline-block;background:#ff4f00;color:#fff;text-decoration:none;font-weight:600;padding:10px 18px;border-radius:6px">Accept invitation</a></p>
<p style="font-size:12px;line-height:1.5;color:#777;margin:0">Or copy this link: <a href="%s" style="color:#444">%s</a><br>The link works until %s. If you were not expecting this, ignore it.</p>
</div></body></html>`,
		html.EscapeString(inviter), html.EscapeString(name), html.EscapeString(what), html.EscapeString(inv.Email), link, link, link, until)
	subject := "You were invited to " + name
	if who := firstNonEmpty(by.Name, by.Email); who != "" {
		subject = who + " invited you to " + name
	}
	return ext.Message{To: []string{inv.Email}, Subject: subject, Text: text, HTML: htmlBody}
}

func article(word string) string {
	if strings.ContainsAny(word[:1], "aeiou") {
		return "an"
	}
	return "a"
}
