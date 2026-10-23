package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/pages"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// The CLI's browser sign-in (RFC-0052), after the device flow of RFC 8628:
// `shpyrd login` asks the workspace for a code, shows the person a short
// one and opens the browser at /cli/activate, where the dashboard session
// approves it; the CLI polls until the approval has turned into a token.
// The token is the person's own (kind session, RFC-0031): it acts with
// their roles as they are, and it is listed and revoked like any other.

// cliDeviceTTL is how long a code may wait for its approval.
const cliDeviceTTL = 10 * time.Minute

// cliDeviceInterval is how often the CLI may poll, in seconds.
const cliDeviceInterval = 5

// cliSessionTTL is how long the credential a browser approval mints
// lasts before `shpyrd login` is asked again.
const cliSessionTTL = 30 * 24 * time.Hour

// maxCLIDevices caps codes waiting for approval; a backstop against a
// client minting codes in a loop, far above real use.
const maxCLIDevices = 1024

// userCodeAlphabet leaves out the letters that read like one another and
// every vowel, so a code is typed right and spells nothing.
const userCodeAlphabet = "BCDFGHJKLMNPQRSTVWXZ"

// cliDeviceStore holds codes waiting for approval. They live in the
// control-plane database, so the CLI's polls and the person's approval may
// land on any replica of the server; a server with no store keeps them in
// the store's memory implementation, which one replica is enough for.
type cliDeviceStore struct {
	backing store.Store
	now     func() time.Time
}

func newCLIDeviceStore(st store.Store) *cliDeviceStore {
	if st == nil {
		st = store.NewMemory()
	}
	return &cliDeviceStore{backing: st, now: time.Now}
}

// cliDeviceStoreTimeout bounds the store's part of a code's step.
const cliDeviceStoreTimeout = 5 * time.Second

// start registers a new code pair for a workspace.
func (st *cliDeviceStore) start(ctx context.Context, workspaceID, name string) (deviceCode, userCode string, err error) {
	ctx, cancel := context.WithTimeout(ctx, cliDeviceStoreTimeout)
	defer cancel()
	deviceCode = oauthRandom(32)
	now := st.now()
	for range 8 { // a user code already waiting in the workspace: draw again
		userCode = newUserCode()
		err = st.backing.PutCLIDevice(ctx, store.CLIDevice{DeviceHash: hashToken(deviceCode), WorkspaceID: workspaceID, UserCode: userCode, Name: name, Expires: now.Add(cliDeviceTTL)}, maxCLIDevices, now)
		if !errors.Is(err, store.ErrConflict) {
			break
		}
	}
	switch {
	case errors.Is(err, store.ErrFull):
		return "", "", errors.New("too many sign-ins are waiting for approval; try again in a moment")
	case err != nil:
		return "", "", err
	}
	return deviceCode, userCode, nil
}

// newUserCode is eight letters shown as XXXX-XXXX.
func newUserCode() string {
	raw := make([]byte, 8)
	_, _ = rand.Read(raw)
	b := make([]byte, 0, 9)
	for i, r := range raw {
		if i == 4 {
			b = append(b, '-')
		}
		b = append(b, userCodeAlphabet[int(r)%len(userCodeAlphabet)])
	}
	return string(b)
}

// normaliseUserCode accepts what a person typed: any case, with or
// without the dash or spaces.
func normaliseUserCode(s string) string {
	s = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(s)))
	if len(s) != 8 {
		return ""
	}
	return s[:4] + "-" + s[4:]
}

// lookup finds a code still waiting by what the person typed, at its
// workspace.
func (st *cliDeviceStore) lookup(ctx context.Context, workspaceID, userCode string) (*store.CLIDevice, bool) {
	if userCode == "" {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, cliDeviceStoreTimeout)
	defer cancel()
	d, err := st.backing.CLIDeviceByUserCode(ctx, workspaceID, userCode, st.now())
	return d, err == nil
}

// decide records the approval (with the token minted for it) or the
// refusal, once: false when the code no longer waits (answered meanwhile,
// perhaps on another replica, or expired).
func (st *cliDeviceStore) decide(ctx context.Context, d *store.CLIDevice, state, token, email string, tokenExp *time.Time) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, cliDeviceStoreTimeout)
	defer cancel()
	err := st.backing.DecideCLIDevice(ctx, d.WorkspaceID, d.UserCode, state, token, email, tokenExp, st.now())
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// poll is what the CLI asks: the token once the code is approved, else
// why not yet. An approved code is consumed by the poll that takes it.
func (st *cliDeviceStore) poll(ctx context.Context, workspaceID, deviceCode string) (*store.CLIDevice, string) {
	ctx, cancel := context.WithTimeout(ctx, cliDeviceStoreTimeout)
	defer cancel()
	d, slowDown, err := st.backing.PollCLIDevice(ctx, workspaceID, hashToken(deviceCode), st.now(), (cliDeviceInterval-1)*time.Second)
	switch {
	case err != nil:
		return nil, "expired_token"
	case slowDown:
		return nil, "slow_down"
	case d.State == "approved":
		return d, ""
	case d.State == "denied":
		return nil, "access_denied"
	}
	return nil, "authorization_pending"
}

// cliDeviceStart is POST /api/cli/device: a code pair for this workspace.
func (s *Server) cliDeviceStart(c *gin.Context) {
	ws, err := s.tenant(c)
	if err != nil {
		abort(c, http.StatusNotFound, err)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	_ = c.ShouldBindJSON(&req)
	req.Name = strings.TrimSpace(req.Name)
	if len(req.Name) > 60 {
		req.Name = req.Name[:60]
	}
	deviceCode, userCode, err := s.cliDevices.start(c.Request.Context(), ws.ID, req.Name)
	if err != nil {
		c.Header("Retry-After", "60")
		abort(c, http.StatusServiceUnavailable, err)
		return
	}
	base := s.dashboardURLOf(ws)
	c.JSON(http.StatusOK, gin.H{
		"device_code":               deviceCode,
		"user_code":                 userCode,
		"verification_uri":          base + "/cli/activate",
		"verification_uri_complete": base + "/cli/activate?code=" + url.QueryEscape(userCode),
		"expires_in":                int(cliDeviceTTL / time.Second),
		"interval":                  cliDeviceInterval,
	})
}

// cliDevicePoll is POST /api/cli/device/token: the token, or why not yet
// (RFC 8628 §3.5: authorization_pending, slow_down, access_denied,
// expired_token).
func (s *Server) cliDevicePoll(c *gin.Context) {
	ws, err := s.tenant(c)
	if err != nil {
		abort(c, http.StatusNotFound, err)
		return
	}
	var req struct {
		DeviceCode string `json:"device_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.DeviceCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "error_description": "device_code is required"})
		return
	}
	d, status := s.cliDevices.poll(c.Request.Context(), ws.ID, req.DeviceCode)
	if status != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": status})
		return
	}
	out := gin.H{"token": d.Token, "email": d.Email}
	if d.TokenExp != nil {
		out["expiresAt"] = d.TokenExp
	}
	c.JSON(http.StatusOK, out)
}

// cliActivatePage is GET /cli/activate[?code=XXXX-XXXX]: with a dashboard
// session, the approval page; without one, the sign-in page first, coming
// back here.
func (s *Server) cliActivatePage(c *gin.Context) {
	ok, _ := s.sessionAuth(c)
	if !ok {
		c.Redirect(http.StatusFound, "/?next="+url.QueryEscape(c.Request.URL.RequestURI()))
		return
	}
	id, _ := ext.IdentityFrom(c)
	sess, _ := s.rp.sessions.get(sessionIDOf(c))
	csrf := ""
	if sess != nil {
		csrf = sess.CSRF
	}
	s.cliActivateForm(c, id, csrf, normaliseUserCode(c.Query("code")), "")
}

// deviceNameOf is what the CLI behind a pending code called its machine,
// so the person can tell their own terminal's code from a link they were
// sent; "" when the code is unknown.
func (s *Server) deviceNameOf(c *gin.Context, code string) string {
	ws, err := s.tenant(c)
	if err != nil {
		return ""
	}
	if d, ok := s.cliDevices.lookup(c.Request.Context(), ws.ID, code); ok {
		return d.Name
	}
	return ""
}

// cliActivateForm asks the person to approve the code shown on their
// terminal: a form, like the OAuth consent page (RFC-0032).
func (s *Server) cliActivateForm(c *gin.Context, id ext.Identity, csrf, code, problem string) {
	ws, _ := s.tenant(c)
	page := pages.CLIPage{
		Title: "Authorize the shpyrd CLI", Workspace: firstNonEmpty(ws.Name, "this workspace"),
		Account: firstNonEmpty(id.Email, id.Name, "you"), Device: firstNonEmpty(s.deviceNameOf(c, code), "Your terminal"),
		Code: code, CSRF: csrf, Problem: problem,
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(pages.HTML(pages.CLIActivate, page)))
}

// cliDonePage reports the result without changing the authorization decision.
func (s *Server) cliDonePage(c *gin.Context, status int, title, text string) {
	outcome, hint := "stopped", "You can close this tab."
	if title == "The CLI is signed in" {
		outcome, hint = "success", "You can close this tab."
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'")
	c.Data(status, "text/html; charset=utf-8", []byte(pages.HTML(pages.CLIDone, pages.CLIPage{Title: title, Text: text, Outcome: outcome, Hint: hint})))
}

// cliActivateDecide is POST /cli/activate: the form's answer. Approval
// mints the person's CLI credential and hands it to the waiting CLI.
func (s *Server) cliActivateDecide(c *gin.Context) {
	if s.rp == nil {
		abort(c, http.StatusForbidden, errors.New("no sign-in on this server"))
		return
	}
	sess, found := s.rp.sessions.getIn(sessionIDOf(c), s.realmAt(c), s.workspaceID(c))
	if !found {
		next := "/cli/activate"
		if code := normaliseUserCode(c.PostForm("code")); code != "" {
			next += "?code=" + url.QueryEscape(code)
		}
		c.Redirect(http.StatusFound, "/?next="+url.QueryEscape(next))
		return
	}
	if subtle.ConstantTimeCompare([]byte(sess.CSRF), []byte(c.PostForm("csrf"))) != 1 {
		s.cliDonePage(c, http.StatusForbidden, "The form expired", "Open the link from your terminal again.")
		return
	}
	ext.SetIdentity(c, sess.Identity)
	id := sess.Identity
	ws, err := s.tenant(c)
	if err != nil {
		abort(c, http.StatusNotFound, err)
		return
	}
	code := normaliseUserCode(c.PostForm("code"))
	d, ok := s.cliDevices.lookup(c.Request.Context(), ws.ID, code)
	if code == "" || !ok {
		s.cliActivateForm(c, id, sess.CSRF, code, "That code is not waiting for approval: check it against your terminal, or run `shpyrd login` again (a code lasts ten minutes).")
		return
	}
	if c.PostForm("decision") != "allow" {
		if _, err := s.cliDevices.decide(c.Request.Context(), d, "denied", "", "", nil); err != nil {
			abort(c, http.StatusBadGateway, err)
			return
		}
		s.audit(c, "", "cli.login.denied", d.Name, "")
		s.cliDonePage(c, http.StatusOK, "Sign-in refused", "The CLI was told no; nothing was changed.")
		return
	}
	name := "CLI"
	if d.Name != "" {
		name = "CLI on " + d.Name
	}
	// One credential per machine: approving again from the same host
	// replaces the previous one rather than piling up tokens.
	if existing, err := s.store.ListTokens(c.Request.Context(), ws.Slug, id.Subject); err == nil {
		for _, t := range existing {
			if t.Kind == store.TokenKindSession && t.Name == name && t.PrincipalID == id.Subject {
				_ = s.store.DeleteToken(c.Request.Context(), ws.Slug, t.ID)
			}
		}
	}
	value, _, hash, err := GenerateToken()
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return
	}
	exp := time.Now().Add(cliSessionTTL)
	created, err := s.store.CreateToken(c.Request.Context(), ws.Slug, store.APIToken{Name: name, OwnerEmail: id.Email, PrincipalID: id.Subject, Kind: store.TokenKindSession, ExpiresAt: &exp}, hash)
	if err != nil {
		storeErr(c, err, "token")
		return
	}
	decided, err := s.cliDevices.decide(c.Request.Context(), d, "approved", value, id.Email, created.ExpiresAt)
	if err != nil || !decided {
		// Answered meanwhile (another tab, another replica) or expired:
		// the credential minted for this answer is nobody's.
		_ = s.store.DeleteToken(context.WithoutCancel(c.Request.Context()), ws.Slug, created.ID)
		if err != nil {
			abort(c, http.StatusBadGateway, err)
			return
		}
		s.cliActivateForm(c, id, sess.CSRF, code, "That code is not waiting for approval: check it against your terminal, or run `shpyrd login` again (a code lasts ten minutes).")
		return
	}
	s.audit(c, "", "cli.login", name, "")
	who := firstNonEmpty(id.Email, id.Name, "you")
	s.cliDonePage(c, http.StatusOK, "The CLI is signed in", "Your terminal is signed in to "+firstNonEmpty(ws.Name, "the workspace")+" as "+who+".")
}
