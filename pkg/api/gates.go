package api

// Gates (RFC-0083): a project of one workspace at a host of its own, which
// people enter from their own workspace. The edge carries their session
// there with a one-time code bound to the browser that asked for it, keeps
// a cookie that holds a pass standing for the session (never the session
// itself), and checks it, and their access, on every request. People of the gate's own workspace
// enter by the project's access; people of any other workspace by the
// gate's rule.

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/shpyrd-io/shpyrd/pkg/authz"
	"github.com/shpyrd-io/shpyrd/pkg/edge"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// gateSet is the gates the extensions declared, by name and by host.
type gateSet struct {
	byName map[string]ext.Gate
	byHost map[string]ext.Gate
	// suspended names the gate that receives suspended workspaces.
	suspended string
}

// collectGates asks the extensions for their gates and links, after their
// routes are mounted (their deps are set by then).
func (s *Server) collectGates(deps ext.Deps) error {
	for _, x := range s.opts.Extensions {
		if gp, ok := x.(ext.GateProvider); ok {
			if err := s.addGates(gp.Gates(deps)); err != nil {
				return fmt.Errorf("extension %s: %w", x.Name(), err)
			}
		}
		if lp, ok := x.(ext.LinkProvider); ok {
			s.linkProviders = append(s.linkProviders, lp)
		}
	}
	return nil
}

// addGates records gates, refusing an invalid name, a name or a host
// already taken.
func (s *Server) addGates(gates []ext.Gate) error {
	if s.gates.byName == nil {
		s.gates = gateSet{byName: map[string]ext.Gate{}, byHost: map[string]ext.Gate{}}
	}
	for _, g := range gates {
		g.Host = hostOnly(g.Host)
		if !project.ValidSlug(g.Name) || g.Host == "" || !project.ValidSlug(g.Project) || g.Workspace == "" {
			return fmt.Errorf("gate %q: a name, a host, a workspace and a project are required", g.Name)
		}
		if _, taken := s.gates.byName[g.Name]; taken {
			return fmt.Errorf("gate %q is declared twice", g.Name)
		}
		if _, taken := s.gates.byHost[g.Host]; taken {
			return fmt.Errorf("gate %q: host %s is another gate's", g.Name, g.Host)
		}
		if g.OpenWhenSuspended {
			if s.gates.suspended != "" {
				return fmt.Errorf("gate %q: gate %q already receives suspended workspaces", g.Name, s.gates.suspended)
			}
			s.gates.suspended = g.Name
		}
		s.gates.byName[g.Name] = g
		s.gates.byHost[g.Host] = g
	}
	return nil
}

func (s *Server) gateByName(name string) (ext.Gate, bool) {
	g, ok := s.gates.byName[name]
	return g, ok
}

// suspendedGate is the gate a suspended workspace's people are sent to,
// if there is one.
func (s *Server) suspendedGate() (ext.Gate, bool) {
	if s.gates.suspended == "" {
		return ext.Gate{}, false
	}
	return s.gateByName(s.gates.suspended)
}

// gateByHost finds the gate a request's host is, port or not.
func (s *Server) gateByHost(host string) (ext.Gate, bool) {
	g, ok := s.gates.byHost[hostOnly(host)]
	return g, ok
}

// visitorIn is someone as a gate sees them: their roles and teams in the
// workspace they came from, now.
func (s *Server) visitorIn(ctx context.Context, ws *store.Workspace, id ext.Identity) (ext.Visitor, authz.Roles, error) {
	roles, err := s.rolesInWorkspace(ctx, ws, id)
	if err != nil {
		return ext.Visitor{}, roles, err
	}
	snap, err := s.authz.SnapshotFor(ctx, ws.Slug)
	if err != nil {
		return ext.Visitor{}, roles, err
	}
	teams := snap.TeamNames(id)
	if teams == nil {
		teams = []string{}
	}
	return ext.Visitor{Identity: id, Workspace: *ws, WorkspaceRole: roles.Workspace, PlatformRole: roles.Platform, Teams: teams}, roles, nil
}

// gateAdmits decides whether a visitor may pass a gate: in the gate's own
// workspace by the project's access (and the project role it gives),
// elsewhere by the gate's rule.
func (s *Server) gateAdmits(ctx context.Context, g ext.Gate, v ext.Visitor, roles authz.Roles) (string, bool) {
	if roles.Suspended {
		return "", false
	}
	if v.Workspace.Slug == g.Workspace {
		app, err := s.findApp(ctx, g.Workspace, g.Project)
		if err != nil {
			return "", false
		}
		key := projectGrantKey(app)
		if !roles.Can(authz.ProjectOpen, key) {
			return "", false
		}
		return roles.ProjectRole(key), true
	}
	if g.Admit == nil {
		return "", false
	}
	return "", g.Admit(ctx, v)
}

// gateAdmitsHook is Deps.GateAdmits.
func (s *Server) gateAdmitsHook(ctx context.Context, gate string, v ext.Visitor) bool {
	g, ok := s.gateByName(gate)
	if !ok {
		return false
	}
	roles, err := s.rolesInWorkspace(ctx, &v.Workspace, v.Identity)
	if err != nil {
		return false
	}
	_, ok = s.gateAdmits(ctx, g, v, roles)
	return ok
}

const (
	gateCookieSecure        = "__Host-shpyrd_gate"
	gateCookieInsecure      = "shpyrd_gate"
	gateNonceCookieSecure   = "__Host-shpyrd_gate_nonce"
	gateNonceCookieInsecure = "shpyrd_gate_nonce"
	// gateNonceBytes is the size of a way in's nonce, before base64url.
	gateNonceBytes = 24
	// gateNonceTTL is how long a browser has to come back from its
	// workspace with a code, in seconds.
	gateNonceTTL = 300
)

// gateCookieName depends on HTTPS: the __Host- prefix needs Secure.
func (s *Server) gateCookieName() string {
	if s.platformHTTPS() {
		return gateCookieSecure
	}
	return gateCookieInsecure
}

// gateNonceCookieName is the cookie that binds a way in to the browser that
// began it, at the gate's host; like the gate cookie, it depends on HTTPS.
func (s *Server) gateNonceCookieName() string {
	if s.platformHTTPS() {
		return gateNonceCookieSecure
	}
	return gateNonceCookieInsecure
}

// validGateNonce says whether a value can be a nonce begin made: base64url
// of exactly gateNonceBytes.
func validGateNonce(n string) bool {
	b, err := base64.RawURLEncoding.DecodeString(n)
	return err == nil && len(b) == gateNonceBytes
}

// onGateHost answers a /.shpyrd/ route itself when the host is a gate's,
// before the tenant middleware, to which a gate's host is unknown.
func (s *Server) onGateHost(h func(*gin.Context, ext.Gate)) gin.HandlerFunc {
	return func(c *gin.Context) {
		if g, ok := s.gateByHost(c.Request.Host); ok {
			h(c, g)
			c.Abort()
			return
		}
		c.Next()
	}
}

// notAGate answers a route only a gate's host has, at any other host.
func (s *Server) notAGate(c *gin.Context) {
	s.edgePage(c, http.StatusNotFound, "Nothing here", "There is nothing at this address.", nil)
}

// gateOpen is GET /.shpyrd/gate?name=<gate> on a workspace's host. Without
// a nonce it sends an admitted visitor to the gate's host to begin, which
// gives their browser a nonce and sends it back here; with one, the
// workspace vouches for its signed-in visitor with a one-time code for the
// gate's host, bound to that nonce. Who would be refused is refused here,
// before going anywhere.
func (s *Server) gateOpen(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	g, ok := s.gateByName(c.Query("name"))
	if !ok {
		s.edgePage(c, http.StatusNotFound, "Nothing here", "No app answers by that name.", nil)
		return
	}
	signIn := func() {
		self := edgePathPrefix + "gate?" + url.Values{"name": {g.Name}}.Encode()
		c.Redirect(http.StatusFound, "/?next="+url.QueryEscape(self))
	}
	if ok, _ := s.sessionAuth(c); !ok {
		signIn()
		return
	}
	ctx := c.Request.Context()
	ws, err := s.tenant(c)
	if err != nil {
		s.edgePage(c, http.StatusNotFound, "Nothing here", "Open this from your workspace.", nil)
		return
	}
	// sessionAuth lets a session without a workspace through; a gate takes
	// only a session of exactly this workspace.
	sid, _ := c.Cookie(sessionCookie)
	if sess, ok := s.rp.sessions.getIn(sid, store.RealmWorkspace, ws.ID); !ok || sess.WorkspaceID != ws.ID {
		signIn()
		return
	}
	id, _ := ext.IdentityFrom(c)
	v, roles, err := s.visitorIn(ctx, ws, id)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	suspended := ws.Status == store.WorkspaceSuspended
	if suspended && !g.OpenWhenSuspended {
		s.edgePage(c, http.StatusForbidden, "Workspace suspended", "This workspace is suspended.", nil)
		return
	}
	if _, ok := s.gateAdmits(ctx, g, v, roles); !ok {
		if suspended {
			s.edgePage(c, http.StatusForbidden, "Workspace suspended", "This workspace is suspended. Talk to its owner to bring it back.", nil)
			return
		}
		s.edgePage(c, http.StatusForbidden, "Not open to you", "Ask the people who run it for access.", nil)
		return
	}
	// The way in runs only at the workspace's platform address: begin sends
	// the browser back there, never to a host a request named.
	home := s.gateHome(ws)
	atHome := hostOnly(c.Request.Host) == hostOnly(home)
	nonce, asked := c.GetQuery("nonce")
	if !asked {
		if !atHome {
			c.Redirect(http.StatusFound, "https://"+home+edgePathPrefix+"gate?"+url.Values{"name": {g.Name}}.Encode())
			return
		}
		ticket, err := s.edgeKeys.SignGateBegin(edge.GateBegin{Workspace: ws.Slug, WorkspaceID: ws.ID, Gate: g.Name})
		if err != nil {
			abort(c, http.StatusInternalServerError, err)
			return
		}
		c.Redirect(http.StatusFound, "https://"+s.withPort(g.Host)+edgePathPrefix+"begin?"+url.Values{"t": {ticket}}.Encode())
		return
	}
	// begin sent no nonce to any other host: one arriving there is not a
	// way in.
	if !atHome || !validGateNonce(nonce) {
		s.edgePage(c, http.StatusBadRequest, "This link is not valid", "Open it again from your workspace.", nil)
		return
	}
	code, err := s.edgeCodes.MintJSON(ctx, g.Host, edge.KindGate, edge.GateClaims{SessionID: sid, Workspace: ws.Slug, WorkspaceID: ws.ID, Gate: g.Name, Nonce: nonce})
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return
	}
	c.Redirect(http.StatusFound, "https://"+s.withPort(g.Host)+edgePathPrefix+"callback?"+url.Values{"code": {code}}.Encode())
}

// gateBegin is GET /.shpyrd/begin?t=<ticket> on a gate's host: the
// browser gets a nonce here, at the gate's host, and goes back to its
// workspace to ask for a code bound to it. It goes back only to the
// platform address of the workspace a ticket for this gate names, still
// that workspace by ID; the ticket carries no host.
func (s *Server) gateBegin(c *gin.Context, g ext.Gate) {
	c.Header("Cache-Control", "no-store")
	invalid := func() {
		s.edgePage(c, http.StatusBadRequest, "This link is not valid", "Open it again from your workspace.", nil)
	}
	ticket, err := s.edgeKeys.VerifyGateBegin(c.Query("t"), g.Name)
	if err != nil {
		invalid()
		return
	}
	ws, err := s.store.Workspace(c.Request.Context(), ticket.Workspace)
	if err != nil || ws.ID != ticket.WorkspaceID {
		invalid() // gone, or another workspace under the same slug
		return
	}
	home := s.gateHome(ws)
	back := url.URL{Scheme: "https", Host: home, Path: edgePathPrefix + "gate"}
	if !validHostname(hostOnly(home)) || back.Host != home {
		invalid()
		return
	}
	nonce, err := randomToken(gateNonceBytes)
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return
	}
	back.RawQuery = url.Values{"name": {g.Name}, "nonce": {nonce}}.Encode()
	http.SetCookie(c.Writer, &http.Cookie{
		Name: s.gateNonceCookieName(), Value: nonce, Path: "/", HttpOnly: true, Secure: s.platformHTTPS(),
		SameSite: http.SameSiteLaxMode, MaxAge: gateNonceTTL,
	})
	c.Redirect(http.StatusFound, back.String())
}

// gateHome is where a gate sends a browser back to start: the workspace's
// platform address (the operator gives it; a customer's custom domain is
// DNS they control, so never that), or the dashboard's configured host for
// a workspace without one. Never the request's Host.
func (s *Server) gateHome(ws *store.Workspace) string {
	if ws.Address != "" {
		return s.withPort(ws.Address)
	}
	return s.dashboardHost()
}

// validHostname says whether a host is a plain DNS name: lowercase labels
// of letters, digits and inner hyphens, at least two of them, 253
// characters at most. Nothing that could bend a URL gets through.
func validHostname(host string) bool {
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return false
	}
	for _, l := range labels {
		if len(l) == 0 || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for i := 0; i < len(l); i++ {
			ch := l[i]
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
				return false
			}
		}
	}
	return true
}

// gateCallback is GET /.shpyrd/callback?code= on a gate's host: a code
// asked for by this browser (its nonce cookie says so) becomes a pass, and
// the gate cookie holds it.
func (s *Server) gateCallback(c *gin.Context, g ext.Gate) {
	c.Header("Cache-Control", "no-store")
	expired := func() {
		s.edgePage(c, http.StatusBadRequest, "This link expired", "Open it again from your workspace.", nil)
	}
	ctx := c.Request.Context()
	var claims edge.GateClaims
	if err := s.edgeCodes.RedeemJSON(ctx, c.Query("code"), hostOnly(c.Request.Host), edge.KindGate, &claims); err != nil || claims.Gate != g.Name {
		expired()
		return
	}
	// A code is worth something only in the browser that began the way in:
	// handed to anyone else, it would sign them in as someone else.
	nonce, err := c.Cookie(s.gateNonceCookieName())
	if err != nil || nonce == "" || claims.Nonce == "" || subtle.ConstantTimeCompare([]byte(nonce), []byte(claims.Nonce)) != 1 {
		expired()
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: s.gateNonceCookieName(), Value: "", Path: "/", HttpOnly: true, Secure: s.platformHTTPS(), SameSite: http.SameSiteLaxMode, MaxAge: -1})
	claims.Nonce, claims.IssuedAt, claims.ExpiresAt = "", 0, 0
	pass, err := s.edgeCodes.MintPass(ctx, g.Host, claims)
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return
	}
	value, err := s.edgeKeys.SignGateCookie(edge.GateCookie{Pass: pass, Gate: g.Name})
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name: s.gateCookieName(), Value: value, Path: "/", HttpOnly: true, Secure: s.platformHTTPS(),
		SameSite: http.SameSiteLaxMode, MaxAge: int(edge.CookieTTL.Seconds()),
	})
	c.Redirect(http.StatusFound, "/")
}

// gateLogout is GET /.shpyrd/logout on a gate's host: the gate's pass ends
// and its cookie goes; the workspace's session stays.
func (s *Server) gateLogout(c *gin.Context, g ext.Gate) {
	if raw, err := c.Cookie(s.gateCookieName()); err == nil && raw != "" {
		if cookie, err := s.edgeKeys.VerifyGateCookie(raw, g.Name); err == nil {
			if err := s.edgeCodes.DropPass(c.Request.Context(), cookie.Pass); err != nil {
				abort(c, http.StatusInternalServerError, err)
				return
			}
		}
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: s.gateCookieName(), Value: "", Path: "/", HttpOnly: true, Secure: s.platformHTTPS(), SameSite: http.SameSiteLaxMode, MaxAge: -1})
	s.edgePage(c, http.StatusOK, "Signed out of "+g.Name, "Open it again from your workspace.", nil)
}

// gateAuth is GET /edge/auth?gate=<gate>: the session the gate cookie's
// pass stands for, still alive in exactly its workspace, and the visitor
// still admitted, now.
func (s *Server) gateAuth(c *gin.Context, name string) {
	g, ok := s.gateByName(name)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no such gate"})
		return
	}
	for _, h := range []string{"X-Shpyrd-User", "X-Shpyrd-Email", "X-Shpyrd-Name", "X-Shpyrd-Teams", "X-Shpyrd-Roles", "X-Shpyrd-Workspace", "X-Shpyrd-Operator", "Authorization"} {
		c.Header(h, "")
	}
	ctx := c.Request.Context()
	signIn := func() { c.JSON(http.StatusUnauthorized, gin.H{"error": "open this from your workspace"}) }
	raw, err := c.Cookie(s.gateCookieName())
	if err != nil || raw == "" || s.rp == nil {
		signIn()
		return
	}
	cookie, err := s.edgeKeys.VerifyGateCookie(raw, g.Name)
	if err != nil {
		signIn()
		return
	}
	claims, err := s.edgeCodes.Pass(ctx, cookie.Pass, g.Host)
	if err != nil || claims.Gate != g.Name || claims.SessionID == "" {
		signIn() // signed out of the gate, expired, or never a pass
		return
	}
	ws, err := s.store.Workspace(ctx, claims.Workspace)
	if err != nil || ws.ID != claims.WorkspaceID {
		signIn() // gone, or another workspace under the same slug
		return
	}
	sess, ok := s.rp.sessions.getIn(claims.SessionID, store.RealmWorkspace, ws.ID)
	if !ok || sess.WorkspaceID != ws.ID {
		signIn()
		return
	}
	if ws.Status == store.WorkspaceSuspended && !g.OpenWhenSuspended {
		c.JSON(http.StatusForbidden, gin.H{"error": "this workspace is suspended"})
		return
	}
	v, roles, err := s.visitorIn(ctx, ws, sess.Identity)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	projectRole, ok := s.gateAdmits(ctx, g, v, roles)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "you may not open this app"})
		return
	}
	if authz.ReadOnly(projectRole) && !safeMethod(originalMethod(c)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "your access to this app is read-only", "readOnly": true})
		return
	}
	roleList := []string{}
	if ws.Slug == g.Workspace {
		if projectRole != "" {
			roleList = append(roleList, projectRole)
		}
	} else if roles.Workspace != "" {
		roleList = append(roleList, roles.Workspace)
	}
	if roles.Platform != "" {
		roleList = append(roleList, roles.Platform)
	}
	now := time.Now()
	jwt, err := s.edgeKeys.Sign("JWT", edge.Claims{
		Issuer: s.dashboardURLOf(ws), Subject: sess.Identity.Subject, Audience: gateAudience(g.Name),
		IssuedAt: now.Unix(), ExpiresAt: now.Add(edge.TokenTTL).Unix(),
		Email: sess.Identity.Email, Name: sess.Identity.Name, Workspace: ws.Slug, Project: g.Project,
		WorkspaceID: ws.ID, WorkspaceURL: s.dashboardURLOf(ws),
		Roles: roleList, Teams: v.Teams, Realm: "workspace", Provider: sess.Identity.Provider,
		Operator: ws.OwnedByOperator(),
	})
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return
	}
	c.Header("X-Shpyrd-User", firstNonEmpty(sess.Identity.Email, sess.Identity.Subject))
	c.Header("X-Shpyrd-Email", sess.Identity.Email)
	c.Header("X-Shpyrd-Name", sess.Identity.Name)
	c.Header("X-Shpyrd-Teams", strings.Join(v.Teams, ","))
	c.Header("X-Shpyrd-Roles", strings.Join(roleList, ","))
	// Roles and team names mean something only in the workspace they come
	// from: an owner of any workspace is its platform-admin, and any
	// workspace can have a team called finance. The app behind a gate reads
	// where the visitor came from before it trusts either.
	c.Header("X-Shpyrd-Workspace", ws.Slug)
	c.Header("X-Shpyrd-Operator", strconv.FormatBool(ws.OwnedByOperator()))
	c.Header("Authorization", "Bearer "+jwt)
	c.Status(http.StatusOK)
}

// gateAudience is a gate's JWT audience: "gate:" and its name, which no
// app's audience can be, since a project's slug has no colon.
func gateAudience(name string) string { return "gate:" + name }

// gateErrorPage answers what nginx hands over for a gate's host: a 401 has
// no sign-in to go to (the gate cannot know the workspace), a 403 is no
// access. False when the host is no gate's.
func (s *Server) gateErrorPage(c *gin.Context) bool {
	g, ok := s.gateByHost(c.Request.Host)
	if !ok {
		return false
	}
	switch c.GetHeader("X-Code") {
	case "401":
		s.edgePage(c, http.StatusUnauthorized, "Open "+g.Name+" from your workspace", "Sign in to your workspace and open it from its sidebar.", nil)
	case "403":
		s.edgePage(c, http.StatusForbidden, "Not open to you", "Ask the people who run it for access.", nil)
	default:
		return false
	}
	return true
}

// links is GET /api/links: what the extensions add to this person's
// sidebar here (RFC-0083): the workspace's at a workspace's host, the
// console's at the console.
func (s *Server) links(c *gin.Context) {
	out := []ext.Link{}
	id, ok := ext.IdentityFrom(c)
	if !ok || (id.Subject == "" && id.Email == "") {
		c.JSON(http.StatusOK, out) // no identity: ask no provider for links
		return
	}
	ctx := c.Request.Context()
	area := ext.AreaWorkspace
	var v ext.Visitor
	if s.atConsole(c) {
		roles, err := s.rolesOf(c)
		if err != nil {
			abort(c, http.StatusBadGateway, err)
			return
		}
		area, v = ext.AreaConsole, ext.Visitor{Identity: id, PlatformRole: roles.Platform, Teams: []string{}}
	} else {
		ws, err := s.tenant(c)
		if err != nil {
			c.JSON(http.StatusOK, out)
			return
		}
		if v, _, err = s.visitorIn(ctx, ws, id); err != nil {
			abort(c, http.StatusBadGateway, err)
			return
		}
	}
	for _, p := range s.linkProviders {
		for _, l := range p.Links(ctx, v) {
			if firstNonEmpty(l.Area, ext.AreaWorkspace) == area {
				out = append(out, l)
			}
		}
	}
	c.JSON(http.StatusOK, out)
}
