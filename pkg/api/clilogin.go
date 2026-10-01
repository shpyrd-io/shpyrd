package api

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"html"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
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

type cliDevice struct {
	userCode    string
	name        string // what the CLI called itself: the machine's host name
	workspaceID string
	expires     time.Time
	// state: "" while pending; approved once the token below is minted;
	// denied when the person said no.
	state    string
	token    string
	email    string
	tokenExp *time.Time
	lastPoll time.Time
}

// cliDeviceStore holds codes waiting for approval, in memory: they live
// ten minutes and the server runs a single replica.
type cliDeviceStore struct {
	mu  sync.Mutex
	m   map[string]*cliDevice // keyed by the device code's hash
	now func() time.Time
}

func newCLIDeviceStore() *cliDeviceStore {
	return &cliDeviceStore{m: map[string]*cliDevice{}, now: time.Now}
}

// start registers a new code pair for a workspace.
func (st *cliDeviceStore) start(workspaceID, name string) (deviceCode, userCode string, err error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.sweepLocked()
	if len(st.m) >= maxCLIDevices {
		return "", "", errors.New("too many sign-ins are waiting for approval; try again in a moment")
	}
	deviceCode = oauthRandom(32)
	for {
		userCode = newUserCode()
		if st.byUserCodeLocked(workspaceID, userCode) == nil {
			break
		}
	}
	st.m[hashToken(deviceCode)] = &cliDevice{userCode: userCode, name: name, workspaceID: workspaceID, expires: st.now().Add(cliDeviceTTL)}
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

func (st *cliDeviceStore) byUserCodeLocked(workspaceID, userCode string) *cliDevice {
	for _, d := range st.m {
		if d.workspaceID == workspaceID && d.userCode == userCode {
			return d
		}
	}
	return nil
}

// lookup finds a pending code by what the person typed, at its workspace.
func (st *cliDeviceStore) lookup(workspaceID, userCode string) (*cliDevice, bool) {
	if userCode == "" {
		return nil, false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.sweepLocked()
	d := st.byUserCodeLocked(workspaceID, userCode)
	if d == nil || d.state != "" {
		return nil, false
	}
	return d, true
}

// decide records the approval (with the token minted for it) or the
// refusal.
func (st *cliDeviceStore) decide(d *cliDevice, state, token, email string, tokenExp *time.Time) {
	st.mu.Lock()
	defer st.mu.Unlock()
	d.state, d.token, d.email, d.tokenExp = state, token, email, tokenExp
}

// poll is what the CLI asks: the token once the code is approved, else
// why not yet. An approved code is consumed by the poll that takes it.
func (st *cliDeviceStore) poll(workspaceID, deviceCode string) (d *cliDevice, status string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.sweepLocked()
	key := hashToken(deviceCode)
	d, ok := st.m[key]
	if !ok || d.workspaceID != workspaceID {
		return nil, "expired_token"
	}
	now := st.now()
	if !d.lastPoll.IsZero() && now.Sub(d.lastPoll) < (cliDeviceInterval-1)*time.Second {
		d.lastPoll = now
		return nil, "slow_down"
	}
	d.lastPoll = now
	switch d.state {
	case "approved":
		delete(st.m, key)
		return d, ""
	case "denied":
		delete(st.m, key)
		return nil, "access_denied"
	}
	return nil, "authorization_pending"
}

func (st *cliDeviceStore) sweepLocked() {
	now := st.now()
	for k, d := range st.m {
		if now.After(d.expires) {
			delete(st.m, k)
		}
	}
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
	deviceCode, userCode, err := s.cliDevices.start(ws.ID, req.Name)
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
	d, status := s.cliDevices.poll(ws.ID, req.DeviceCode)
	if status != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": status})
		return
	}
	out := gin.H{"token": d.token, "email": d.email}
	if d.tokenExp != nil {
		out["expiresAt"] = d.tokenExp
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
	if d, ok := s.cliDevices.lookup(ws.ID, code); ok {
		return d.name
	}
	return ""
}

// cliActivateForm asks the person to approve the code shown on their
// terminal: a form, like the OAuth consent page (RFC-0032).
func (s *Server) cliActivateForm(c *gin.Context, id ext.Identity, csrf, code, problem string) {
	ws, _ := s.tenant(c)
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Sign in the shpyrd CLI</title>`)
	b.WriteString(cliPageStyle)
	b.WriteString(`</head><body><main>`)
	b.WriteString(`<h1>Sign in the shpyrd CLI to ` + html.EscapeString(firstNonEmpty(ws.Name, "this workspace")) + `?</h1>`)
	b.WriteString(`<p>The CLI will act as <strong>` + html.EscapeString(firstNonEmpty(id.Email, id.Name, "you")) + `</strong>, with what your roles allow, for 30 days. The code is on your terminal.</p>`)
	if name := s.deviceNameOf(c, code); name != "" {
		b.WriteString(`<p>This code was asked for by <strong>` + html.EscapeString(name) + `</strong>.</p>`)
	}
	if problem != "" {
		b.WriteString(`<p class="problem">` + html.EscapeString(problem) + `</p>`)
	}
	b.WriteString(`<form method="post" action="/cli/activate">`)
	b.WriteString(`<input type="hidden" name="csrf" value="` + html.EscapeString(csrf) + `">`)
	b.WriteString(`<label>Code <input name="code" value="` + html.EscapeString(code) + `" autocomplete="off" spellcheck="false" placeholder="XXXX-XXXX" required></label>`)
	b.WriteString(`<p><button class="allow" name="decision" value="allow">Sign in</button><button name="decision" value="deny">Deny</button></p></form>`)
	b.WriteString(`<p><small>Only approve a code you asked for yourself, from a terminal you are at. This creates a session token, listed with your API tokens on the Workspace page; revoke it there at any time.</small></p></main></body></html>`)
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(b.String()))
}

// cliDonePage says how it ended, with nothing more to do in the browser.
func (s *Server) cliDonePage(c *gin.Context, status int, title, text string) {
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>` + html.EscapeString(title) + `</title>`)
	b.WriteString(cliPageStyle)
	b.WriteString(`</head><body><main><h1>` + html.EscapeString(title) + `</h1><p>` + html.EscapeString(text) + `</p><p><small>You can close this tab.</small></p></main></body></html>`)
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'")
	c.Data(status, "text/html; charset=utf-8", []byte(b.String()))
}

const cliPageStyle = `<style>body{margin:0;font:16px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;background:#f6f6f6;color:#111;display:flex;min-height:100vh;align-items:center;justify-content:center}main{max-width:26rem;width:100%;padding:2rem;background:#fff;border-radius:12px;border:1px solid #e5e5e5}h1{font-size:1.2rem;margin:0 0 .5rem}p{margin:0 0 1rem;color:#444}label{display:block;margin:0 0 1rem;color:#333}input{font:inherit;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;letter-spacing:.15em;text-transform:uppercase;padding:.5rem .7rem;border:1px solid #ddd;border-radius:8px;width:11rem;margin-left:.5rem}button{font:inherit;padding:.6rem 1.1rem;border-radius:8px;border:1px solid #ddd;background:#fff;cursor:pointer;margin-right:.5rem}button.allow{background:#ff4f00;border-color:#ff4f00;color:#fff;font-weight:600}small{color:#777}.problem{color:#b00020}</style>`

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
	d, ok := s.cliDevices.lookup(ws.ID, code)
	if code == "" || !ok {
		s.cliActivateForm(c, id, sess.CSRF, code, "That code is not waiting for approval: check it against your terminal, or run `shpyrd login` again (a code lasts ten minutes).")
		return
	}
	if c.PostForm("decision") != "allow" {
		s.cliDevices.decide(d, "denied", "", "", nil)
		s.audit(c, "", "cli.login.denied", d.name, "")
		s.cliDonePage(c, http.StatusOK, "Sign-in refused", "The CLI was told no; nothing was changed.")
		return
	}
	name := "CLI"
	if d.name != "" {
		name = "CLI on " + d.name
	}
	// One credential per machine: approving again from the same host
	// replaces the previous one rather than piling up tokens.
	if existing, err := s.store.ListTokens(c.Request.Context(), ws.Slug, id.Email); err == nil {
		for _, t := range existing {
			if t.Kind == store.TokenKindSession && t.Name == name && strings.EqualFold(t.OwnerEmail, id.Email) {
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
	created, err := s.store.CreateToken(c.Request.Context(), ws.Slug, store.APIToken{Name: name, OwnerEmail: id.Email, Kind: store.TokenKindSession, ExpiresAt: &exp}, hash)
	if err != nil {
		storeErr(c, err, "token")
		return
	}
	s.cliDevices.decide(d, "approved", value, id.Email, created.ExpiresAt)
	s.audit(c, "", "cli.login", name, "")
	who := firstNonEmpty(id.Email, id.Name, "you")
	s.cliDonePage(c, http.StatusOK, "The CLI is signed in", "Your terminal is signed in to "+firstNonEmpty(ws.Name, "the workspace")+" as "+who+".")
}
