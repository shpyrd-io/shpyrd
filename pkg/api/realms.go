package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/ids"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"github.com/shpyrd-io/shpyrd/pkg/tenancy"
)

// Realms decides which login methods a workspace offers (RFC-0033's
// RealmProvider). DefaultRealms serves the open-source platform and the
// cloud: a workspace shows its own methods and the platform's defaults
// (unless it hides them). The console's realm is not a workspace's and is
// decided here, not by Realms (RFC-0080).
type Realms interface {
	// Methods filters the login methods for a workspace. The password form
	// (auth-local) is listed among them by id when enabled.
	Methods(ctx context.Context, ws *store.Workspace, all AuthConfig) AuthConfig
}

// DefaultRealms is the Realms the core ships and the cloud uses too: a
// workspace's login page shows the methods the workspace configured
// itself (RFC-0033 per-workspace SSO) and the platform's defaults, unless
// the workspace hides them (settings.ownMethodsOnly). Console methods are
// never a workspace's.
type DefaultRealms struct{}

// Methods implements Realms.
func (DefaultRealms) Methods(_ context.Context, ws *store.Workspace, all AuthConfig) AuthConfig {
	if ws == nil {
		return AuthConfig{Providers: []ProviderInfo{}}
	}
	offered := func(realm, owner string) bool {
		switch realm {
		case ext.RealmWorkspace:
			return owner == ids.Short(ws.ID) || owner == ws.Slug // slug: connectors from before RFC-0080's rekey
		case ext.RealmConsole:
			return false
		default: // the platform's defaults
			return !ws.Settings.OwnMethodsOnly
		}
	}
	out := AuthConfig{Providers: []ProviderInfo{}}
	for _, p := range all.Providers {
		if offered(p.Realm, p.Workspace) {
			out.Providers = append(out.Providers, p)
		}
	}
	if all.Password != nil && offered(all.Password.Realm, all.Password.Workspace) {
		out.Password = all.Password
	}
	return out
}

// allMethods is DefaultRealms under its old name.
type allMethods = DefaultRealms

// consoleMethods are the methods the console's login page offers: the
// connectors of the console realm, the password form when the operator
// keeps it on (settings console.password_signin, default on so a fresh
// install is usable), and the admin token. Platform defaults and
// workspace methods are not among them (RFC-0080).
func (s *Server) consoleMethods(ctx context.Context, all AuthConfig) AuthConfig {
	out := AuthConfig{Token: all.Token, Providers: []ProviderInfo{}}
	for _, p := range all.Providers {
		if p.Realm == ext.RealmConsole {
			out.Providers = append(out.Providers, p)
		}
	}
	if all.Password != nil && s.consolePasswordSignIn(ctx) {
		out.Password = all.Password
	}
	return out
}

// SettingConsolePasswordSignIn is the cluster setting that keeps the
// password form on the console's login page ("true" unless the operator
// turned it off after adding an identity provider).
const SettingConsolePasswordSignIn = "console.password_signin"

// consolePasswordSignIn reads the setting; on by default.
func (s *Server) consolePasswordSignIn(ctx context.Context) bool {
	v, err := s.store.GetSetting(ctx, SettingConsolePasswordSignIn)
	if err != nil || v == "" {
		return true
	}
	return v != "false" && v != "0" && v != "off"
}

// authConfigFor is the sign-in configuration the request's door shows.
// The admin token is the operator's break-glass: offered at the console
// only (it still authenticates at internal hosts, for the CLI).
func (s *Server) authConfigFor(c *gin.Context) AuthConfig {
	all := s.authConfig()
	t, err := s.door(c)
	if err != nil {
		return AuthConfig{Providers: []ProviderInfo{}}
	}
	if t.ConsoleHost() {
		return s.consoleMethods(c.Request.Context(), all)
	}
	ws := t.Workspace
	cfg := s.realms.Methods(c.Request.Context(), ws, all)
	cfg.Token = t.Internal && all.Token // the operator's break-glass, at internal hosts only
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

// offersAt reports whether the request's door lists a login method (""
// means the only one, when there is exactly one).
func (s *Server) offersAt(c *gin.Context, providerID string) bool {
	cfg := s.authConfigFor(c)
	if providerID == "" {
		return len(cfg.Providers) == 1 || cfg.Password != nil
	}
	return s.offersIn(cfg, providerID)
}

// atConsole says the request came through the operator's door: the
// console host, or an internal host (the kubeconfig proxy).
func (s *Server) atConsole(c *gin.Context) bool {
	t, err := s.door(c)
	return err == nil && t.AtConsole()
}

// requireConsole keeps a route for the console: at a workspace's host it
// is not there.
func (s *Server) requireConsole() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !s.atConsole(c) {
			abort(c, http.StatusNotFound, errors.New("not available in this workspace: the platform operator manages this"))
			return
		}
		c.Next()
	}
}

// requireWorkspace keeps a route for workspace hosts: at the console it is
// not there (RFC-0080). Internal hosts pass with the default workspace.
func (s *Server) requireWorkspace() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, err := s.tenant(c); errors.Is(err, errConsoleNotWorkspace) {
			abort(c, http.StatusNotFound, err)
			return
		}
		c.Next()
	}
}

// realmAt is the realm a session at this door is opened in: the console's
// at the console host, a workspace's everywhere else (internal hosts
// included: their tenant is the default workspace).
func (s *Server) realmAt(c *gin.Context) string {
	if t, err := s.door(c); err == nil && t.Realm == tenancy.RealmConsole {
		return tenancy.RealmConsole
	}
	return tenancy.RealmWorkspace
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
// or changed, when this replica runs one, and refreshes the identity
// provider's redirect URIs (RFC-0080).
func (s *Server) workspacesChanged() {
	s.forgetTenants()
	s.forgetHosts()
	if s.opts.WorkspacesChanged != nil {
		s.opts.WorkspacesChanged()
	}
	s.oidcClientChanged()
}

// forgetTenants drops the host resolver's memory of workspaces, so a
// settings change is seen by the next request rather than after the TTL.
func (s *Server) forgetTenants() {
	if f, ok := s.tenancy.(interface{ ForgetAll() }); ok {
		f.ForgetAll()
	}
}
