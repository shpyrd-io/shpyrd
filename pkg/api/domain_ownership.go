package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	networkingv1 "k8s.io/api/networking/v1"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"github.com/shpyrd-io/shpyrd/pkg/tenancy"
)

// The write funnel for a project's custom domains. Every write that changes
// an App's spec.domains passes through vetDomains: createProject before
// anything is created, mutateApp after the mutation and before the update.
// Handlers do not judge hosts themselves, and only this file assigns
// Spec.Domains (TestSpecDomainsAreAssignedOnlyInTheFunnel).
//
// Only the hosts a write adds are judged. A host the App already holds
// stays, whatever the rules say now, until it is removed.

// maxDomainsAdded bounds the hosts one write may add: each is looked up.
const maxDomainsAdded = 100

// domainRefusal is why a domain write is refused, with its status.
type domainRefusal struct {
	status int
	err    error
}

// canonicalHost is the form hosts are compared in: trimmed, lower-case,
// without a trailing dot.
func canonicalHost(raw string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
}

// setSpecDomains, appendSpecDomain and dropSpecDomain are how handlers
// change spec.domains inside a mutation; the funnel judges the result.
func setSpecDomains(a *shpyrdv1.App, hosts []string) { a.Spec.Domains = hosts }

func appendSpecDomain(a *shpyrdv1.App, host string) {
	a.Spec.Domains = append(a.Spec.Domains, host)
}

// dropSpecDomain removes host and says whether it was there.
func dropSpecDomain(a *shpyrdv1.App, host string) bool {
	kept := a.Spec.Domains[:0:0]
	found := false
	for _, d := range a.Spec.Domains {
		if canonicalHost(d) == host {
			found = true
			continue
		}
		kept = append(kept, d)
	}
	a.Spec.Domains = kept
	return found
}

// vetDomains judges a write that leaves an App with the hosts in after,
// when it held before. ws is the request's workspace, self the App being
// changed (nil when it is being created) and slug its slug. It returns the
// list to store: the hosts normalised and without duplicates, or before
// itself when the write holds the same hosts.
func (s *Server) vetDomains(ctx context.Context, ws *store.Workspace, self *shpyrdv1.App, slug string, before, after []string) ([]string, *domainRefusal) {
	held := map[string]bool{}
	for _, d := range before {
		held[canonicalHost(d)] = true
	}
	out := make([]string, 0, len(after))
	seen := map[string]bool{}
	var added []string
	for _, raw := range after {
		host, err := normalizeHost(raw)
		if err != nil {
			// A host the App holds from before is not judged again.
			if c := canonicalHost(raw); held[c] {
				host = c
			} else {
				return nil, &domainRefusal{http.StatusBadRequest, err}
			}
		}
		if seen[host] {
			continue
		}
		seen[host] = true
		out = append(out, host)
		if !held[host] {
			added = append(added, host)
		}
	}
	if len(added) == 0 {
		if len(out) == len(held) {
			return before, nil // the same hosts: the list stays as it is
		}
		return out, nil
	}
	if len(added) > maxDomainsAdded {
		return nil, &domainRefusal{http.StatusBadRequest, fmt.Errorf("add at most %d domains at once", maxDomainsAdded)}
	}
	if ws == nil {
		return nil, &domainRefusal{http.StatusBadGateway, errors.New("the workspace is not known")}
	}
	var holders map[string]domainHolder
	for _, host := range added {
		if r := s.vetDomainHost(ctx, ws, slug, host); r != nil {
			return nil, r
		}
		if holders == nil {
			var r *domainRefusal
			if holders, r = s.domainHolders(ctx, self); r != nil {
				return nil, r
			}
		}
		if h, ok := holders[host]; ok {
			if h.workspace == ws.Slug && h.slug != "" {
				return nil, &domainRefusal{http.StatusConflict, fmt.Errorf("%s is already used by project %s", host, h.slug)}
			}
			return nil, &domainRefusal{http.StatusConflict, fmt.Errorf("%s is already in use by another project", host)}
		}
	}
	return out, nil
}

// vetDomainHost is what is judged of one added host by itself: names of the
// platform, the workspace's own, the ownership rule.
func (s *Server) vetDomainHost(ctx context.Context, ws *store.Workspace, slug, host string) *domainRefusal {
	bad := func(format string, args ...any) *domainRefusal {
		return &domainRefusal{http.StatusBadRequest, fmt.Errorf(format, args...)}
	}
	if s.platformNames().Platform(host) {
		return bad("%s belongs to the platform", host)
	}
	if ws.Address != "" && (host == ws.Address || host == hostOnly(s.dashboardHostOf(ws))) {
		return bad("%s is the workspace's own address", host)
	}
	for _, h := range s.appHostsIn(ctx, ws, slug) {
		if host == h {
			return bad("%s is already the project's hostname", host)
		}
	}
	owner, err := tenancy.HostOwner(ctx, s.store, s.opts.Layout, host)
	switch {
	case errors.Is(err, tenancy.ErrInvalidHost):
		return bad("%q is not a valid hostname", host)
	case err != nil:
		return &domainRefusal{http.StatusBadGateway, fmt.Errorf("could not check who owns %s: try again", host)}
	case owner != nil && owner.ID != ws.ID:
		return &domainRefusal{http.StatusConflict, fmt.Errorf("%s belongs to another workspace", host)}
	}
	if d := s.platformDomainOf(host); d != "" {
		return bad("names under %s are the platform's: the project already has its address there", d)
	}
	return nil
}

// platformNames are the hosts the platform keeps for itself.
func (s *Server) platformNames() *tenancy.Owners {
	domain := s.opts.Public.Domain
	reserved := []string{"shpyrd." + domain, "auth." + domain, "grafana." + domain}
	if sign := hostOnly(s.opts.SignInHost); sign != "" {
		reserved = append(reserved, sign)
	}
	return &tenancy.Owners{
		Layout:      s.opts.Layout,
		ConsoleHost: hostOnly(s.dashboardHost()),
		Domain:      domain,
		Reserved:    reserved,
	}
}

// domainHolder is a project that holds a host.
type domainHolder struct {
	slug      string // "" when the holder is not an App this server knows
	workspace string
}

// domainHolders maps every host held in any App's spec or in any Ingress
// rule to its holder, leaving out self. A failed read refuses the write: a
// host is never taken to be free because the list could not be read.
func (s *Server) domainHolders(ctx context.Context, self *shpyrdv1.App) (map[string]domainHolder, *domainRefusal) {
	var apps shpyrdv1.AppList
	if err := s.apps.List(ctx, &apps); err != nil {
		return nil, &domainRefusal{http.StatusBadGateway, errors.New("could not check which projects hold the host: try again")}
	}
	var ingresses networkingv1.IngressList
	if err := s.apps.List(ctx, &ingresses); err != nil {
		return nil, &domainRefusal{http.StatusBadGateway, errors.New("could not check which projects hold the host: try again")}
	}
	isSelf := func(namespace string) bool { return self != nil && namespace == self.Namespace }
	holders := map[string]domainHolder{}
	byNamespace := map[string]domainHolder{}
	for i := range apps.Items {
		app := &apps.Items[i]
		h := domainHolder{slug: project.SlugOf(app), workspace: workspaceOf(app)}
		byNamespace[app.Namespace] = h
		if isSelf(app.Namespace) {
			continue
		}
		for _, d := range app.Spec.Domains {
			if _, ok := holders[canonicalHost(d)]; !ok {
				holders[canonicalHost(d)] = h
			}
		}
	}
	for i := range ingresses.Items {
		ing := &ingresses.Items[i]
		if isSelf(ing.Namespace) {
			continue
		}
		h := byNamespace[ing.Namespace] // zero: an Ingress of no App we know
		for _, rule := range ing.Spec.Rules {
			if _, ok := holders[canonicalHost(rule.Host)]; !ok {
				holders[canonicalHost(rule.Host)] = h
			}
		}
	}
	return holders, nil
}

// vetAppDomains is the funnel's step in mutateApp: when the mutation
// changed spec.domains, judge what it added and store the normalised list.
func (s *Server) vetAppDomains(c *gin.Context, app *shpyrdv1.App, before []string) error {
	if slices.Equal(before, app.Spec.Domains) {
		return nil
	}
	ws, err := s.tenant(c)
	if err != nil {
		abort(c, http.StatusBadGateway, err)
		return err
	}
	list, refusal := s.vetDomains(c.Request.Context(), ws, app, project.SlugOf(app), before, app.Spec.Domains)
	if refusal != nil {
		abort(c, refusal.status, refusal.err)
		return refusal.err
	}
	setSpecDomains(app, list)
	return nil
}
