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
	"strings"

	"github.com/shpyrd-io/shpyrd/pkg/authz"
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

var _ = strings.ToLower // used by the routes of Task 3
