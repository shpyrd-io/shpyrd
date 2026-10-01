package api

// RFC-0014: password reset and invite / set-password flows.
//
// Routes (all public — no session required):
//
//	POST /api/auth/reset          {email}  → send reset link (always 200)
//	GET  /account/reset?token=…           → show set-password form
//	POST /account/reset?token=…   {password, confirm} → complete reset
//	GET  /account/set-password?token=…   → show set-password form (invite)
//	POST /account/set-password?token=…  → complete invite activation
//
// Tokens are 32-byte random values, SHA-256 hashed in a Secret, 1h for
// reset and 24h for invite (pkg/ext/authlocal.tokens.go).

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/shpyrd-io/shpyrd/pkg/audit"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/ext/authlocal"
)

// resetRequestRateLimit caps requests per IP per 10 minutes.
const resetRequestsPerMinute = 3

// ResetRequest is the body of POST /api/auth/reset.
type ResetRequest struct {
	Email string `json:"email" form:"email"`
}

// requestReset handles POST /api/auth/reset: sends a reset email if the
// account exists. Always responds 200 to avoid leaking whether an account
// exists (RFC-0014 §"Reset").
func (s *Server) requestReset(c *gin.Context) {
	if !s.resetRateLimit.allow(c.ClientIP()) {
		// Still 200 — the rate limit itself should not reveal information.
		c.JSON(http.StatusOK, gin.H{"message": "if an account with that email exists, a reset link was sent"})
		return
	}
	var req ResetRequest
	_ = c.ShouldBindJSON(&req)
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		abort(c, http.StatusBadRequest, fmt.Errorf("email is required"))
		return
	}

	ctx := c.Request.Context()
	// Only send if the account exists and auth-local is enabled.
	if s.localAccounts == nil || s.kube == nil {
		c.JSON(http.StatusOK, gin.H{"message": "if an account with that email exists, a reset link was sent"})
		return
	}
	code, err := authlocal.MintAccountToken(ctx, s.kube.Kube, s.deps().SystemNamespace, email, authlocal.TokenKindReset)
	if err != nil {
		// Don't leak the error; log it.
		s.log.Warn("reset: could not mint token", "email", email, "err", err)
		c.JSON(http.StatusOK, gin.H{"message": "if an account with that email exists, a reset link was sent"})
		return
	}
	if s.deps().Mail != nil {
		link := s.resetLink(c, code)
		_ = s.deps().Mail.Send(ctx, ext.Message{
			To:      []string{email},
			Subject: "Reset your password",
			Text:    "Open this link to set a new password (expires in 1 hour):\n\n" + link + "\n\nIf you did not request this, ignore this email.",
			HTML:    fmt.Sprintf(`<p>Open this link to set a new password (expires in 1 hour):</p><p><a href="%s">Reset password</a></p><p>If you did not request this, ignore this email.</p>`, link),
		})
	}
	audit.Record(ctx, s.kube.Kube, audit.ClusterRef(s.deps().SystemNamespace),
		audit.Entry{Actor: email, Action: "user.reset_requested", Target: email, Via: "api", From: c.ClientIP()}) //nolint:errcheck
	c.JSON(http.StatusOK, gin.H{"message": "if an account with that email exists, a reset link was sent"})
}

// doorURL is the door this request came in on: a person who asks for a
// reset at acme.shpyrd.app is sent back there, never to another host
// (RFC-0080: the platform has several doors, and none is "the" dashboard).
func doorURL(c *gin.Context) string {
	return requestScheme(c) + "://" + c.Request.Host
}

func (s *Server) resetLink(c *gin.Context, code string) string {
	return doorURL(c) + "/account/reset?token=" + code
}

// setPasswordLink is where an invited person chooses a password: on the
// door they were invited to.
func setPasswordLink(door, code string) string {
	return strings.TrimSuffix(door, "/") + "/account/set-password?token=" + code
}

// accountPage renders one of the two password-setting pages.
func accountPage(title, action, submitLabel, tokenKey, token, errorMsg string) string {
	// Inline template — no build step, matches the platform's dark style.
	const tpl = `<!doctype html><html lang="en"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}}</title>
<style>
*{box-sizing:border-box}body{margin:0;font:15px/1.5 system-ui,sans-serif;background:#0f172a;color:#e2e8f0;display:flex;min-height:100vh;align-items:center;justify-content:center}
main{width:100%;max-width:26rem;padding:2rem;background:#1e293b;border-radius:12px;border-top:4px solid #ff4f00}
h1{font-size:1.1rem;margin:0 0 1.25rem;color:#f1f5f9}
label{display:block;font-size:.85rem;color:#94a3b8;margin-bottom:.3rem}
input[type=password]{width:100%;padding:.5rem .75rem;background:#0f172a;border:1px solid #334155;border-radius:6px;color:#e2e8f0;font-size:.95rem;margin-bottom:1rem}
input[type=password]:focus{outline:none;border-color:#ff4f00}
button{width:100%;padding:.6rem;background:#ff4f00;color:#fff;border:none;border-radius:6px;font-size:.95rem;cursor:pointer}
button:hover{background:#e64500}
.err{background:#7f1d1d;color:#fca5a5;padding:.5rem .75rem;border-radius:6px;margin-bottom:1rem;font-size:.875rem}
</style></head><body><main>
<h1>{{.Title}}</h1>
{{if .Error}}<div class="err">{{.Error}}</div>{{end}}
<form method="post" action="{{.Action}}">
<input type="hidden" name="{{.TokenKey}}" value="{{.Token}}">
<label>New password</label>
<input type="password" name="password" required autofocus minlength="8">
<label>Confirm password</label>
<input type="password" name="confirm" required minlength="8">
<button type="submit">{{.Submit}}</button>
</form></main></body></html>`
	t := template.Must(template.New("").Parse(tpl))
	var b strings.Builder
	_ = t.Execute(&b, map[string]string{
		"Title":    title,
		"Action":   action,
		"TokenKey": tokenKey,
		"Token":    token,
		"Submit":   submitLabel,
		"Error":    errorMsg,
	})
	return b.String()
}

// showResetPage serves GET /account/reset?token=…
func (s *Server) showResetPage(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.Redirect(http.StatusFound, "/")
		return
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, accountPage("Reset your password", "/account/reset", "Set new password", "token", token, ""))
}

// handleReset handles POST /account/reset (form submission).
func (s *Server) handleReset(c *gin.Context) {
	token := c.PostForm("token")
	password := c.PostForm("password")
	confirm := c.PostForm("confirm")

	renderErr := func(msg string) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, accountPage("Reset your password", "/account/reset", "Set new password", "token", token, msg))
	}

	if password != confirm {
		renderErr("Passwords do not match.")
		return
	}
	if s.localAccounts == nil || s.kube == nil {
		renderErr("Password reset is not available on this cluster.")
		return
	}
	ctx := c.Request.Context()
	email, kind, err := authlocal.RedeemAccountToken(ctx, s.kube.Kube, s.deps().SystemNamespace, token)
	if err != nil || kind != authlocal.TokenKindReset {
		renderErr("This link is invalid or has expired. Request a new one from the sign-in page.")
		return
	}
	if err := s.localAccounts.SetPasswordAndVerify(ctx, email, password); err != nil {
		renderErr("Could not set password: " + err.Error())
		return
	}
	// End existing sessions of this user so they sign in fresh.
	s.rp.sessions.deleteByEmail(ctx, email)
	audit.Record(ctx, s.kube.Kube, audit.ClusterRef(s.deps().SystemNamespace),
		audit.Entry{Actor: email, Action: "user.reset", Target: email, Via: "web", From: c.ClientIP()}) //nolint:errcheck
	if s.deps().Mail != nil {
		_ = s.deps().Mail.Send(ctx, ext.Message{
			To:      []string{email},
			Subject: "Your password was changed",
			Text:    "Your password was changed. If you did not do this, contact your administrator immediately.",
			HTML:    "<p>Your password was changed.</p><p>If you did not do this, contact your administrator immediately.</p>",
		})
	}
	c.Redirect(http.StatusFound, "/?reset=ok")
}

// showSetPasswordPage serves GET /account/set-password?token=… (invite flow).
func (s *Server) showSetPasswordPage(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.Redirect(http.StatusFound, "/")
		return
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, accountPage("Set your password", "/account/set-password", "Set password and sign in", "token", token, ""))
}

// handleSetPassword handles POST /account/set-password (invite activation).
func (s *Server) handleSetPassword(c *gin.Context) {
	token := c.PostForm("token")
	password := c.PostForm("password")
	confirm := c.PostForm("confirm")

	renderErr := func(msg string) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, accountPage("Set your password", "/account/set-password", "Set password and sign in", "token", token, msg))
	}

	if password != confirm {
		renderErr("Passwords do not match.")
		return
	}
	if s.localAccounts == nil || s.kube == nil {
		renderErr("Local accounts are not enabled on this cluster.")
		return
	}
	ctx := c.Request.Context()
	email, kind, err := authlocal.RedeemAccountToken(ctx, s.kube.Kube, s.deps().SystemNamespace, token)
	if err != nil || kind != authlocal.TokenKindInvite {
		renderErr("This link is invalid or has expired. Ask an administrator to re-send the invitation.")
		return
	}
	if err := s.localAccounts.ActivateFromInvite(ctx, email, password); err != nil {
		renderErr("Could not activate account: " + err.Error())
		return
	}
	audit.Record(ctx, s.kube.Kube, audit.ClusterRef(s.deps().SystemNamespace),
		audit.Entry{Actor: email, Action: "user.activated", Target: email, Via: "web", From: c.ClientIP()}) //nolint:errcheck
	c.Redirect(http.StatusFound, "/?activated=ok")
}

// setSignupPasswordHook is ext.Deps.SetSignupPassword: the password a
// person chose at signup, applied to the local sign-in at once. The email
// was proved by the signup's code, which is what an invitation link proves
// too, so the account is activated the way an invite activates it.
func (s *Server) setSignupPasswordHook(ctx context.Context, email, name, password string) error {
	if s.localAccounts == nil {
		return errors.New("the platform has no password sign-in")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	status, err := s.localAccounts.Status(ctx, email)
	if err != nil {
		return err
	}
	switch status {
	case "":
		if err := s.localAccounts.CreatePending(ctx, email, name); err != nil {
			return err
		}
	case ext.AccountPending:
	case ext.AccountLocked:
		return errors.New("the account is locked; try again later")
	default:
		return ext.ErrAccountHasPassword
	}
	return s.localAccounts.ActivateFromInvite(ctx, email, password)
}

// passwordWayIn gives an invited person without a password a way to
// choose one (RFC-0014): a pending account when they have none, a fresh
// token when they are still pending; a link on the door they were
// invited to. "" when they hold a password already (they sign in with
// it) or auth-local is off (the door's other methods carry the sign-in).
func (s *Server) passwordWayIn(ctx context.Context, email, name, door string) (string, error) {
	if s.localAccounts == nil || s.kube == nil || s.kube.Kube == nil {
		return "", nil
	}
	status, err := s.localAccounts.Status(ctx, email)
	if err != nil {
		return "", err
	}
	switch status {
	case "":
		if err := s.localAccounts.CreatePending(ctx, email, name); err != nil {
			return "", err
		}
	case ext.AccountPending:
	default:
		return "", nil
	}
	code, err := authlocal.MintAccountToken(ctx, s.kube.Kube, s.deps().SystemNamespace, email, authlocal.TokenKindInvite)
	if err != nil {
		return "", err
	}
	return setPasswordLink(door, code), nil
}
