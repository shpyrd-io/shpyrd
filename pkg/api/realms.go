package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/shpyrd-io/shpyrd/pkg/edge"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Realms decides which login methods a workspace offers (RFC-0033's
// RealmProvider). DefaultRealms serves the open-source platform and the
// cloud: a workspace shows its own methods and the platform's (unless it
// hides them); a console pool as a realm of its own comes with
// collaborators.
type Realms interface {
	// Methods filters the platform's login methods for a workspace. The
	// password form (auth-local) is listed among them by id when enabled.
	Methods(ctx context.Context, ws *store.Workspace, all AuthConfig) AuthConfig
}

// DefaultRealms is the Realms the core ships and the cloud uses too: a
// workspace's login page shows the methods the workspace configured
// itself (RFC-0033 per-workspace SSO) and the platform's, unless the
// workspace hides the platform's (settings.ownMethodsOnly). The implicit
// workspace is the platform: it shows everything.
type DefaultRealms struct{}

// Methods implements Realms.
func (DefaultRealms) Methods(_ context.Context, ws *store.Workspace, all AuthConfig) AuthConfig {
	if ws == nil || ws.Implicit() {
		return all
	}
	offered := func(owner string) bool {
		return owner == ws.Slug || (owner == "" && !ws.Settings.OwnMethodsOnly)
	}
	out := AuthConfig{Token: all.Token, Providers: []ProviderInfo{}}
	for _, p := range all.Providers {
		if offered(p.Workspace) {
			out.Providers = append(out.Providers, p)
		}
	}
	if all.Password != nil && offered(all.Password.Workspace) {
		out.Password = all.Password
	}
	return out
}

// allMethods is DefaultRealms under its old name.
type allMethods = DefaultRealms

// authConfigFor is the sign-in configuration the request's workspace shows.
// The admin token is the operator's break-glass: it is never offered at an
// explicit workspace's host (it still authenticates there, for the CLI).
func (s *Server) authConfigFor(c *gin.Context) AuthConfig {
	all := s.authConfig()
	ws, err := s.tenant(c)
	if err != nil {
		return all
	}
	cfg := s.realms.Methods(c.Request.Context(), ws, all)
	if !ws.Implicit() {
		cfg.Token = false
	}
	if claims, err := s.store.ListDomainClaims(c.Request.Context(), ws.Slug); err == nil {
		for _, d := range claims {
			if d.VerifiedAt != nil && d.Connector != "" && s.offersIn(cfg, d.Connector) {
				cfg.CompanyDomains = true
			}
		}
	}
	return cfg
}

// offersIn reports whether a sign-in configuration lists a method.
func (s *Server) offersIn(cfg AuthConfig, providerID string) bool {
	if cfg.Password != nil && cfg.Password.ID == providerID {
		return true
	}
	for _, p := range cfg.Providers {
		if p.ID == providerID {
			return true
		}
	}
	return false
}

// authRoute is GET /api/auth/route?email=: the method a claimed email
// domain routes to, so the login page skips the chooser (RFC-0033). It
// answers an empty provider for everything else, and says nothing about
// whether an account exists.
func (s *Server) authRoute(c *gin.Context) {
	email := strings.ToLower(strings.TrimSpace(c.Query("email")))
	out := gin.H{"provider": ""}
	i := strings.LastIndex(email, "@")
	if i <= 0 || i == len(email)-1 {
		c.JSON(http.StatusOK, out)
		return
	}
	domain := email[i+1:]
	ws, err := s.tenant(c)
	if err != nil {
		c.JSON(http.StatusOK, out)
		return
	}
	cfg := s.authConfigFor(c)
	if claims, err := s.store.ListDomainClaims(c.Request.Context(), ws.Slug); err == nil {
		for _, d := range claims {
			if d.Domain == domain && d.VerifiedAt != nil && d.Connector != "" && s.offersIn(cfg, d.Connector) {
				out["provider"] = d.Connector
				if p := s.rp.provider(d.Connector); p != nil {
					out["label"] = p.Label
				}
			}
		}
	}
	c.JSON(http.StatusOK, out)
}

// offers reports whether a workspace lists a login method ("" means the
// only one, when there is exactly one).
func (s *Server) offers(ctx context.Context, ws *store.Workspace, providerID string) bool {
	cfg := s.realms.Methods(ctx, ws, s.authConfig())
	if providerID == "" {
		return len(cfg.Providers) == 1 || cfg.Password != nil
	}
	if cfg.Password != nil && cfg.Password.ID == providerID {
		return true
	}
	for _, p := range cfg.Providers {
		if p.ID == providerID {
			return true
		}
	}
	return false
}

// ---- console-hosted sign-in (RFC-0033 phase 6, option 1) ------------------------
//
// The bundled issuer has one relying party: the console, which is the
// implicit workspace's dashboard. A workspace at its own host cannot
// complete an OpenID Connect flow itself, so its /api/auth/login sends the
// browser to the console's, naming the workspace; the console signs the
// person in, applies the *workspace's* admission (join policy, claimed
// domains, suspension), and hands a one-time code back to the workspace
// host, which mints the workspace's session. No console session is
// opened for the person: the console only vouches.

// consoleWorkspace is the slug of the workspace whose dashboard is the
// relying party.
const consoleWorkspace = store.DefaultWorkspace

// atConsole says the request arrived at the console (the implicit
// workspace's host) rather than at an explicit workspace's.
func (s *Server) atConsole(c *gin.Context) bool {
	ws, err := s.tenant(c)
	return err == nil && ws.Slug == consoleWorkspace
}

// requireConsole keeps a route for the console: at an explicit workspace's
// host it is not there.
func (s *Server) requireConsole() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !s.atConsole(c) {
			abort(c, http.StatusNotFound, errors.New("not available in this workspace: the platform operator manages this"))
			return
		}
		c.Next()
	}
}

// sessionHandoff is what the console hands a workspace host: who signed
// in and how.
type sessionHandoff struct {
	Identity ext.Identity `json:"identity"`
	IDToken  string       `json:"idToken,omitempty"`
	How      string       `json:"how"`
}

// consoleLoginURL is the console's login URL for a workspace: the browser
// comes back to next on the workspace host afterwards.
func (s *Server) consoleLoginURL(provider string, ws *store.Workspace, next string) string {
	q := url.Values{"workspace": {ws.Slug}, "next": {safeNext(next)}}
	if provider != "" {
		q.Set("provider", provider)
	}
	return s.opts.Public.DashboardURL + "/api/auth/login?" + q.Encode()
}

// handoffTarget resolves the workspace a console login is on behalf of.
func (s *Server) handoffTarget(ctx context.Context, slug string) (*store.Workspace, error) {
	ws, err := s.store.Workspace(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("unknown workspace %q", slug)
	}
	if ws.Address == "" {
		return nil, fmt.Errorf("workspace %q has no address of its own", slug)
	}
	if ws.Status == store.WorkspaceSuspended {
		return nil, errors.New("this workspace is suspended")
	}
	return ws, nil
}

// handoff completes a console login made on behalf of a workspace: the
// workspace's admission runs, then a one-time code travels to its host.
func (s *Server) handoff(c *gin.Context, target *store.Workspace, id ext.Identity, idToken, next string) {
	if err := s.admitSignIn(c.Request.Context(), target.Slug, id); err != nil {
		s.auditFailure(c, "auth.refused", id.Email, err.Error()+" (workspace "+target.Slug+")")
		s.loginFailedAt(c, target, err)
		return
	}
	host := s.dashboardHostOf(target)
	code, err := s.edgeCodes.MintJSON(c.Request.Context(), host, edge.KindSession, sessionHandoff{Identity: id, IDToken: idToken, How: "console"})
	if err != nil {
		abort(c, http.StatusInternalServerError, err)
		return
	}
	c.Redirect(http.StatusFound, "https://"+host+edgePathPrefix+"session?"+url.Values{"code": {code}, "rd": {safeNext(next)}}.Encode())
}

// edgeSession is GET /.shpyrd/session?code=&rd= at a workspace host: the
// console's code becomes this workspace's session.
func (s *Server) edgeSession(c *gin.Context) {
	if s.atConsole(c) {
		s.edgePage(c, http.StatusNotFound, "Nothing here", "Sign in from the dashboard.", nil)
		return
	}
	var h sessionHandoff
	if err := s.edgeCodes.RedeemJSON(c.Request.Context(), c.Query("code"), c.Request.Host, edge.KindSession, &h); err != nil {
		s.edgePage(c, http.StatusBadRequest, "Sign-in link expired", "Go back to the dashboard and sign in again.", map[string]string{"Dashboard": "/"})
		return
	}
	if h.Identity.Email == "" && h.Identity.Subject == "" {
		s.edgePage(c, http.StatusBadRequest, "Sign-in failed", "The sign-in carried no identity.", nil)
		return
	}
	if _, ok := s.openSession(c, h.Identity, h.IDToken, firstNonEmpty(h.How, "console")); !ok {
		return
	}
	c.Redirect(http.StatusFound, safeNext(c.Query("rd")))
}

// hasCapability says whether this server offers a capability beyond the
// core ("workspaces" on the cloud).
func (s *Server) hasCapability(name string) bool {
	for _, c := range s.opts.Public.Capabilities {
		if c == name {
			return true
		}
	}
	return false
}

// workspacesChanged tells the front-door reconciler a workspace appeared
// or changed, when this replica runs one.
func (s *Server) workspacesChanged() {
	s.forgetTenants()
	if s.opts.WorkspacesChanged != nil {
		s.opts.WorkspacesChanged()
	}
}

// forgetTenants drops the host resolver's memory of workspaces, so a
// settings change is seen by the next request rather than after the TTL.
func (s *Server) forgetTenants() {
	if f, ok := s.tenancy.(interface{ ForgetAll() }); ok {
		f.ForgetAll()
	}
}
