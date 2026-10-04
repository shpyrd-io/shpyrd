package api

import (
	"context"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"

	"github.com/shpyrd-io/shpyrd/pkg/authz"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// RootExtension is an extension that serves at the workspace's own root,
// beside /api, and takes part in how API requests are identified: the
// enterprise's MCP server and its OAuth 2.1 server (ee/mcp). The server
// mounts it with a Host once its routes are in place.
type RootExtension interface {
	MountRoot(h Host) error
}

// Host is what a RootExtension gets from the API server.
type Host interface {
	Store() store.Store
	// Root is the server's root; API its /api group (the request's
	// workspace resolved, the caller authenticated). Tenant resolves the
	// request's workspace and Login throttles sign-in, as middlewares.
	Root() gin.IRouter
	API() gin.IRouter
	Tenant() gin.HandlerFunc
	Login() gin.HandlerFunc

	// The request's workspace: itself, its slug and its id.
	TenantOf(c *gin.Context) (*store.Workspace, error)
	WorkspaceSlug(c *gin.Context) string
	WorkspaceID(c *gin.Context) string
	// DashboardURLOf is a workspace's dashboard on its primary domain.
	DashboardURLOf(ws *store.Workspace) string
	// RealmAt is the realm a session at this door is opened in.
	RealmAt(c *gin.Context) string

	// RolesOf are the caller's roles; RolesIn someone's in a workspace;
	// SetRoles makes roles the request's (an identifier narrowing them).
	RolesOf(c *gin.Context) (authz.Roles, error)
	RolesIn(ctx context.Context, ws string, id ext.Identity) (authz.Roles, error)
	SetRoles(c *gin.Context, roles authz.Roles)

	// SessionAuth identifies the request by its dashboard session.
	SessionAuth(c *gin.Context) (bool, error)
	// Session is the request's dashboard session at this door: its CSRF
	// token and identity.
	Session(c *gin.Context) (csrf string, id ext.Identity, ok bool)
	// OpenSession opens a dashboard session for someone at a door and
	// returns the cookie's value and the CSRF token.
	OpenSession(ctx context.Context, realm, workspace string, id ext.Identity) (cookie, csrf string, err error)
	// IdentifyWithToken accepts a personal API token (shp_...).
	IdentifyWithToken(c *gin.Context) bool
	// AddIdentifier adds a way to identify an API request by its bearer
	// token, tried after the personal tokens: it returns false for tokens
	// that are not its own, and aborts the request for its own that it
	// refuses.
	AddIdentifier(f func(c *gin.Context) bool)

	// Sign and Verify are the platform's EdDSA JWTs, by header type.
	Sign(typ string, claims any) (string, error)
	Verify(token, typ string, into any) error

	// EdgePage answers with one of the server's own pages.
	EdgePage(c *gin.Context, status int, title, message string)
	// ServeAPI performs a GET against the API in-process, as the same
	// caller (their Authorization travels along).
	ServeAPI(c *gin.Context, path string) (int, []byte)

	Audit(c *gin.Context, project, action, target, detail string)
	AuditActor(c *gin.Context, actor, action, target, detail string)
	AuditAnonymous(c *gin.Context, action, detail string)
}

// host is the Server as a Host.
type host struct {
	s             *Server
	api           gin.IRouter
	tenant, login gin.HandlerFunc
}

func (h *host) Store() store.Store            { return h.s.store }
func (h *host) Root() gin.IRouter             { return h.s.engine }
func (h *host) API() gin.IRouter              { return h.api }
func (h *host) Tenant() gin.HandlerFunc       { return h.tenant }
func (h *host) Login() gin.HandlerFunc        { return h.login }
func (h *host) RealmAt(c *gin.Context) string { return h.s.realmAt(c) }

func (h *host) TenantOf(c *gin.Context) (*store.Workspace, error) { return h.s.tenant(c) }
func (h *host) WorkspaceSlug(c *gin.Context) string               { return h.s.workspace(c) }
func (h *host) WorkspaceID(c *gin.Context) string                 { return h.s.workspaceID(c) }
func (h *host) DashboardURLOf(ws *store.Workspace) string         { return h.s.dashboardURLOf(ws) }

func (h *host) RolesOf(c *gin.Context) (authz.Roles, error) { return h.s.rolesOf(c) }
func (h *host) RolesIn(ctx context.Context, ws string, id ext.Identity) (authz.Roles, error) {
	return h.s.authz.RolesIn(ctx, ws, id)
}
func (h *host) SetRoles(c *gin.Context, roles authz.Roles) { c.Set(rolesKey, roles) }

func (h *host) SessionAuth(c *gin.Context) (bool, error) { return h.s.sessionAuth(c) }
func (h *host) Session(c *gin.Context) (string, ext.Identity, bool) {
	sid, _ := c.Cookie(sessionCookie)
	sess, ok := h.s.rp.sessions.getIn(sid, h.s.realmAt(c), h.s.workspaceID(c))
	if !ok || sess == nil {
		return "", ext.Identity{}, false
	}
	return sess.CSRF, sess.Identity, true
}
func (h *host) OpenSession(ctx context.Context, realm, workspace string, id ext.Identity) (string, string, error) {
	sess, err := h.s.rp.sessions.create(ctx, realm, workspace, id, "")
	if err != nil {
		return "", "", err
	}
	return sess.ID, sess.CSRF, nil
}
func (h *host) IdentifyWithToken(c *gin.Context) bool { return h.s.identifyWithToken(c) }
func (h *host) AddIdentifier(f func(c *gin.Context) bool) {
	h.s.identifiers = append(h.s.identifiers, f)
}

func (h *host) Sign(typ string, claims any) (string, error) { return h.s.edgeKeys.Sign(typ, claims) }
func (h *host) Verify(token, typ string, into any) error {
	return h.s.edgeKeys.Verify(token, typ, into)
}

func (h *host) EdgePage(c *gin.Context, status int, title, message string) {
	h.s.edgePage(c, status, title, message, nil)
}

func (h *host) ServeAPI(c *gin.Context, path string) (int, []byte) {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = c.Request.Host
	req.Header.Set("Authorization", c.GetHeader("Authorization"))
	req.Header.Set("X-Shpyrd-Token", c.GetHeader("X-Shpyrd-Token"))
	req.Header.Set("Accept", "application/json")
	req.RemoteAddr = c.Request.RemoteAddr
	if xff := c.GetHeader("X-Forwarded-For"); xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	rec := httptest.NewRecorder()
	h.s.engine.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func (h *host) Audit(c *gin.Context, project, action, target, detail string) {
	h.s.audit(c, project, action, target, detail)
}
func (h *host) AuditActor(c *gin.Context, actor, action, target, detail string) {
	h.s.auditActor(c, actor, action, target, detail)
}
func (h *host) AuditAnonymous(c *gin.Context, action, detail string) {
	h.s.auditAnonymous(c, action, detail)
}

// mountRoot gives every RootExtension the Host.
func (s *Server) mountRoot(api gin.IRouter, tenant, login gin.HandlerFunc) error {
	h := &host{s: s, api: api, tenant: tenant, login: login}
	for _, x := range s.opts.Extensions {
		if r, ok := x.(RootExtension); ok {
			if err := r.MountRoot(h); err != nil {
				return err
			}
		}
	}
	return nil
}
