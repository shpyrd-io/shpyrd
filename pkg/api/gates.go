package api

// Gates (RFC-0083): a project of one workspace at a host of its own, which
// people enter from their own workspace. The edge carries their session
// there with a one-time code, keeps a cookie that names it, and checks it,
// and their access, on every request. People of the gate's own workspace
// enter by the project's access; people of any other workspace by the
// gate's rule.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
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
		s.gates.byName[g.Name] = g
		s.gates.byHost[g.Host] = g
	}
	return nil
}

func (s *Server) gateByName(name string) (ext.Gate, bool) {
	g, ok := s.gates.byName[name]
	return g, ok
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
	gateCookieSecure   = "__Host-shpyrd_gate"
	gateCookieInsecure = "shpyrd_gate"
)

// gateCookieName depends on HTTPS: the __Host- prefix needs Secure.
func (s *Server) gateCookieName() string {
	if s.platformHTTPS() {
		return gateCookieSecure
	}
	return gateCookieInsecure
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

// gateOpen is GET /.shpyrd/gate?name=<gate> on a workspace's host: the
// workspace vouches for its signed-in visitor with a one-time code for the
// gate's host.
func (s *Server) gateOpen(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	g, ok := s.gateByName(c.Query("name"))
	if !ok {
		s.edgePage(c, http.StatusNotFound, "Nothing here", "No app answers by that name.", nil)
		return
	}
	if ok, _ := s.sessionAuth(c); !ok {
		self := edgePathPrefix + "gate?" + url.Values{"name": {g.Name}}.Encode()
		c.Redirect(http.StatusFound, "/?next="+url.QueryEscape(self))
		return
	}
	ctx := c.Request.Context()
	ws, err := s.tenant(c)
	if err != nil {
		s.edgePage(c, http.StatusNotFound, "Nothing here", "Open this from your workspace.", nil)
		return
	}
	id, _ := ext.IdentityFrom(c)
	v, roles, err := s.visitorIn(ctx, ws, id)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return
	}
	if _, ok := s.gateAdmits(ctx, g, v, roles); !ok {
		s.edgePage(c, http.StatusForbidden, "Not open to you", "Ask the people who run it for access.", nil)
		return
	}
	sid, _ := c.Cookie(sessionCookie)
	code, err := s.edgeCodes.MintJSON(ctx, g.Host, edge.KindGate, edge.GateClaims{SessionID: sid, Workspace: ws.Slug, WorkspaceID: ws.ID, Gate: g.Name})
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return
	}
	c.Redirect(http.StatusFound, "https://"+g.Host+edgePathPrefix+"callback?"+url.Values{"code": {code}}.Encode())
}

// gateCallback is GET /.shpyrd/callback?code= on a gate's host: the code
// becomes the gate cookie.
func (s *Server) gateCallback(c *gin.Context, g ext.Gate) {
	c.Header("Cache-Control", "no-store")
	var claims edge.GateClaims
	if err := s.edgeCodes.RedeemJSON(c.Request.Context(), c.Query("code"), hostOnly(c.Request.Host), edge.KindGate, &claims); err != nil || claims.Gate != g.Name {
		s.edgePage(c, http.StatusBadRequest, "This link expired", "Open it again from your workspace.", nil)
		return
	}
	claims.IssuedAt, claims.ExpiresAt = 0, 0 // the cookie's own lifetime
	value, err := s.edgeKeys.SignGateCookie(claims)
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

// gateLogout is GET /.shpyrd/logout on a gate's host: the gate cookie goes,
// the workspace's session stays.
func (s *Server) gateLogout(c *gin.Context, g ext.Gate) {
	http.SetCookie(c.Writer, &http.Cookie{Name: s.gateCookieName(), Value: "", Path: "/", HttpOnly: true, Secure: s.platformHTTPS(), SameSite: http.SameSiteLaxMode, MaxAge: -1})
	s.edgePage(c, http.StatusOK, "Signed out of "+g.Name, "Open it again from your workspace.", nil)
}

// gateAuth is GET /edge/auth?gate=<gate>: the gate cookie's session, still
// alive in exactly its workspace, and the visitor still admitted, now.
func (s *Server) gateAuth(c *gin.Context, name string) {
	g, ok := s.gateByName(name)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no such gate"})
		return
	}
	for _, h := range []string{"X-Shpyrd-User", "X-Shpyrd-Email", "X-Shpyrd-Name", "X-Shpyrd-Teams", "X-Shpyrd-Roles", "Authorization"} {
		c.Header(h, "")
	}
	ctx := c.Request.Context()
	signIn := func() { c.JSON(http.StatusUnauthorized, gin.H{"error": "open this from your workspace"}) }
	raw, err := c.Cookie(s.gateCookieName())
	if err != nil || raw == "" || s.rp == nil {
		signIn()
		return
	}
	claims, err := s.edgeKeys.VerifyGateCookie(raw, g.Name)
	if err != nil {
		signIn()
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
	if ws.Status == store.WorkspaceSuspended {
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
		Issuer: s.dashboardURLOf(ws), Subject: sess.Identity.Subject, Audience: g.Name,
		IssuedAt: now.Unix(), ExpiresAt: now.Add(edge.TokenTTL).Unix(),
		Email: sess.Identity.Email, Name: sess.Identity.Name, Workspace: ws.Slug, Project: g.Project,
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
	c.Header("Authorization", "Bearer "+jwt)
	c.Status(http.StatusOK)
}

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
