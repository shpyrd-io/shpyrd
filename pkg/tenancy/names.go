package tenancy

import (
	"strings"

	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// Layout is how the names of explicit workspaces are built (RFC-0033
// names, amended 2026-10-04). A workspace answers at its address,
// <label>.<WorkspacesDomain>; with AppsDomain set, its apps answer at
// <label>-<app>.<AppsDomain>, so one wildcard certificate per domain
// covers every workspace and every app, and nothing is issued or
// published per workspace. Workspace labels have no hyphen, so the first
// hyphen of an app's label ends the workspace's part; project slugs keep
// theirs. A workspace whose address is elsewhere (the open-source
// platform's domain, an address from before the layout) keeps its apps
// one label under its address. The zero Layout is that older shape for
// everyone.
type Layout struct {
	// WorkspacesDomain is where workspace addresses live (shpyrd.cloud).
	WorkspacesDomain string
	// AppsDomain is where the apps of those workspaces live (shpyrd.app).
	// Empty: apps live one label under their workspace's address.
	AppsDomain string
}

// Shared reports whether the layout is on: apps have a domain of their own.
func (l Layout) Shared() bool { return l.AppsDomain != "" && l.WorkspacesDomain != "" }

// Label is the label of an address in the layout: acme for
// acme.shpyrd.cloud. It is false for an address the layout does not
// cover: another domain, more than one label, a label with a hyphen
// (left from before the rule, until the workspace moves).
func (l Layout) Label(address string) (string, bool) {
	if !l.Shared() {
		return "", false
	}
	label, ok := strings.CutSuffix(Host(address), "."+l.WorkspacesDomain)
	if !ok || label == "" || strings.ContainsAny(label, ".-") {
		return "", false
	}
	return label, true
}

// Address is the address of the workspace whose label is given.
func (l Layout) Address(label string) string { return label + "." + l.WorkspacesDomain }

// AppHost is the host of an app of the workspace whose label is given.
func (l Layout) AppHost(label, app string) string { return label + "-" + app + "." + l.AppsDomain }

// ParseAppHost reads <label>-<app>.<AppsDomain>. A label without a hyphen
// under the apps domain is no app's: it is the platform's (its apex, www).
func (l Layout) ParseAppHost(host string) (label, app string, ok bool) {
	if !l.Shared() {
		return "", "", false
	}
	first, ok := strings.CutSuffix(Host(host), "."+l.AppsDomain)
	if !ok || first == "" || strings.Contains(first, ".") {
		return "", "", false
	}
	label, app, ok = strings.Cut(first, "-")
	if !ok || label == "" || app == "" {
		return "", "", false
	}
	return label, app, true
}

// UnderAppsDomain says the host is one label under the apps domain, where
// the apps' wildcard certificate answers.
func (l Layout) UnderAppsDomain(host string) bool {
	if l.AppsDomain == "" {
		return false
	}
	label, ok := strings.CutSuffix(Host(host), "."+l.AppsDomain)
	return ok && label != "" && !strings.Contains(label, ".")
}

// AppHosts lists the hosts an app answers at because of its workspace,
// the one its URL shows first:
//
//   - the primary custom domain, when one is verified: <app>.<domain>;
//   - in the layout, <label>-<app>.<apps domain>; otherwise one label
//     under the address (or under fallback, the platform's domain, for a
//     workspace without one);
//   - one label under every other verified custom domain.
//
// Moved addresses are not among them: they redirect.
func (l Layout) AppHosts(address string, hosts []store.WorkspaceHost, fallback, app string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(h string) {
		h = strings.ToLower(h)
		if h != "" && !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	primary := ""
	for _, h := range hosts {
		if h.Kind == store.HostCustom && h.Primary && h.VerifiedAt != nil {
			primary = h.Host
			add(app + "." + primary)
			break
		}
	}
	switch label, ok := l.Label(address); {
	case ok:
		add(l.AppHost(label, app))
	case address != "":
		add(app + "." + Host(address))
	case fallback != "":
		add(app + "." + fallback)
	}
	for _, h := range hosts {
		if h.Kind == store.HostCustom && h.VerifiedAt != nil && h.Host != primary {
			add(app + "." + h.Host)
		}
	}
	return out
}
