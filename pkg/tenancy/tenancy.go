// Package tenancy maps the host a request arrived at to the door it came
// through (RFC-0080): the console, the operator's, at the console host; or
// a workspace, at its address, one label under it, or one of its hosts.
// The open-source platform has one workspace and resolves every other host
// to it; the cloud layer's resolver knows many. The interface is the seam;
// both implementations live here so they are tested together.
package tenancy

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// ErrUnknownHost says no workspace answers at the host.
var ErrUnknownHost = errors.New("no workspace answers at this host")

// Realm is the door a request came through (RFC-0080).
type Realm = string

// Realms.
const (
	RealmConsole   Realm = store.RealmConsole
	RealmWorkspace Realm = store.RealmWorkspace
)

// Tenant is what a host resolves to.
type Tenant struct {
	Realm Realm
	// Workspace is the workspace at a workspace host. At the console host it
	// is nil: the console is not a workspace. At an internal host (the
	// kubeconfig proxy, in-cluster callers, a server with no console host
	// configured) it is the default workspace, in the workspace realm, with
	// the operator's rights: project commands over a kubeconfig keep
	// working and console routes answer there.
	Workspace *store.Workspace
	// Internal marks an in-cluster caller (or the one-door fallback).
	Internal bool
}

// AtConsole says the request may reach the operator's routes: the console
// host, or an internal caller.
func (t *Tenant) AtConsole() bool { return t != nil && (t.Realm == RealmConsole || t.Internal) }

// ConsoleHost says the request arrived at the console host proper: no
// workspace, the console's own sign-in methods and sessions.
func (t *Tenant) ConsoleHost() bool { return t != nil && t.Realm == RealmConsole && t.Workspace == nil }

// Resolver finds the tenant for a request host. Implementations must be
// safe for concurrent use and cheap: they run on every request.
type Resolver interface {
	Resolve(ctx context.Context, host string) (*Tenant, error)
}

// DefaultSlugFunc answers the default workspace's slug (RFC-0078: a
// setting), so resolvers follow a change of default without a restart.
type DefaultSlugFunc func(ctx context.Context) string

// Host strips the port and lowercases a Host header value.
func Host(hostport string) string {
	h := strings.ToLower(strings.TrimSpace(hostport))
	if h == "" {
		return ""
	}
	if strings.HasPrefix(h, "[") { // [::1]:8443
		if i := strings.LastIndex(h, "]"); i > 0 {
			return h[1:i]
		}
	}
	if host, _, err := net.SplitHostPort(h); err == nil {
		return host
	}
	return strings.TrimSuffix(h, ".")
}

// Internal reports whether a host is one in-cluster callers use rather
// than a public name: an IP literal, localhost, or a Kubernetes service
// name. Such requests belong to the operator: the console realm, with the
// default workspace as the tenant of project commands.
func Internal(host string) bool {
	switch {
	case host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost"):
		return true
	case net.ParseIP(host) != nil:
		return true
	case strings.HasSuffix(host, ".svc") || strings.Contains(host, ".svc."):
		return true
	}
	return false
}

// Single is the open-source resolver: the console host is the console,
// every other host is the one workspace. The row is re-read every ten
// seconds so its settings (branding, join policy) are seen.
type Single struct {
	Store store.Store
	// ConsoleHost is where the console answers (shpyrd.<domain>, or the
	// apex with SHPYRD_CONSOLE_NAME=apex).
	ConsoleHost string
	// DefaultSlug names the workspace; nil means store.DefaultWorkspace.
	DefaultSlug DefaultSlugFunc

	mu      sync.Mutex
	ws      *store.Workspace
	fetched time.Time
}

// Resolve implements Resolver. Without a ConsoleHost (a server started
// with no dashboard URL: development, tests) there is one door: every
// host is the operator's, with the workspace as tenant, as an internal
// host is.
func (r *Single) Resolve(ctx context.Context, hostport string) (*Tenant, error) {
	host := Host(hostport)
	console := Host(r.ConsoleHost)
	if console != "" && host == console {
		return &Tenant{Realm: RealmConsole}, nil
	}
	ws, err := r.workspace(ctx)
	if err != nil {
		return nil, err
	}
	if console == "" || Internal(host) {
		return &Tenant{Realm: RealmWorkspace, Workspace: ws, Internal: true}, nil
	}
	return &Tenant{Realm: RealmWorkspace, Workspace: ws}, nil
}

func (r *Single) workspace(ctx context.Context) (*store.Workspace, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ws != nil && time.Since(r.fetched) < 10*time.Second {
		return r.ws, nil
	}
	slug := store.DefaultWorkspace
	if r.DefaultSlug != nil {
		slug = r.DefaultSlug(ctx)
	}
	ws, err := r.Store.Workspace(ctx, slug)
	if err != nil {
		if r.ws != nil {
			return r.ws, nil // stale beats down
		}
		return nil, err
	}
	r.ws, r.fetched = ws, time.Now()
	return ws, nil
}

// ForgetAll drops the remembered workspace: a settings change must be
// seen by the next request.
func (r *Single) ForgetAll() {
	r.mu.Lock()
	r.ws = nil
	r.mu.Unlock()
}

// ByAddress resolves hosts against workspace addresses: a host equal to a
// workspace's Address, or one label under it (<app>.<address>), is that
// workspace; so is one of its verified or moved hosts. The console host is
// the console; internal hosts are the console with the default workspace
// as tenant. The platform's reserved names (auth, grafana, the registry)
// are nobody's. Anything else is ErrUnknownHost, never a guess: a request
// at a host nobody claimed must not land in someone's workspace.
type ByAddress struct {
	Store store.Store
	// Domain is the platform domain. Reserved names live one label under it.
	Domain string
	// ConsoleHost is where the console answers.
	ConsoleHost string
	// Reserved are platform hosts that are never a workspace's, whatever
	// address a workspace has (auth.<domain>, grafana.<domain>, ...).
	Reserved []string
	// DefaultSlug names the default workspace; nil means store.DefaultWorkspace.
	DefaultSlug DefaultSlugFunc
	// TTL bounds how long a lookup, found or not, is remembered. Zero means
	// ten seconds.
	TTL time.Duration

	mu    sync.Mutex
	cache map[string]entry
}

type entry struct {
	t       *Tenant
	err     error
	expires time.Time
}

// ForgetAll drops every remembered lookup: a workspace's settings changed
// and the next request must see them.
func (r *ByAddress) ForgetAll() {
	r.mu.Lock()
	r.cache = nil
	r.mu.Unlock()
}

func (r *ByAddress) ttl() time.Duration {
	if r.TTL <= 0 {
		return 10 * time.Second
	}
	return r.TTL
}

// Resolve implements Resolver.
func (r *ByAddress) Resolve(ctx context.Context, hostport string) (*Tenant, error) {
	host := Host(hostport)
	now := time.Now()
	r.mu.Lock()
	if e, ok := r.cache[host]; ok && now.Before(e.expires) {
		r.mu.Unlock()
		return e.t, e.err
	}
	r.mu.Unlock()

	t, err := r.lookup(ctx, host)
	if err != nil && !errors.Is(err, ErrUnknownHost) {
		return nil, err // store trouble is not cached
	}
	r.mu.Lock()
	if r.cache == nil {
		r.cache = map[string]entry{}
	}
	if len(r.cache) > 4096 { // a flood of unknown hosts must not grow this without bound
		r.cache = map[string]entry{}
	}
	r.cache[host] = entry{t: t, err: err, expires: now.Add(r.ttl())}
	r.mu.Unlock()
	return t, err
}

func (r *ByAddress) defaultWorkspace(ctx context.Context) (*store.Workspace, error) {
	slug := store.DefaultWorkspace
	if r.DefaultSlug != nil {
		slug = r.DefaultSlug(ctx)
	}
	return r.Store.Workspace(ctx, slug)
}

func (r *ByAddress) lookup(ctx context.Context, host string) (*Tenant, error) {
	// The console first: it is not a workspace, whatever addresses exist.
	if console := Host(r.ConsoleHost); console != "" && host == console {
		return &Tenant{Realm: RealmConsole}, nil
	}
	if Internal(host) {
		ws, err := r.defaultWorkspace(ctx)
		if err != nil {
			return nil, err
		}
		return &Tenant{Realm: RealmWorkspace, Workspace: ws, Internal: true}, nil
	}
	// Reserved platform names are nobody's, even one label under a
	// workspace whose address is the platform domain.
	for _, reserved := range r.Reserved {
		if host == Host(reserved) {
			return nil, ErrUnknownHost
		}
	}
	// Workspace addresses: acme.shpyrd.app, and <app>.acme.shpyrd.app.
	if ws, err := r.Store.WorkspaceByAddress(ctx, host); err == nil {
		return &Tenant{Realm: RealmWorkspace, Workspace: ws}, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if _, parent, ok := strings.Cut(host, "."); ok && parent != "" {
		if ws, err := r.Store.WorkspaceByAddress(ctx, parent); err == nil {
			return &Tenant{Realm: RealmWorkspace, Workspace: ws}, nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}
	// A workspace's other hosts (RFC-0033 names): a custom domain once
	// verified, or a previous address still redirecting; and one label
	// under either, where its apps answer.
	for _, candidate := range hostAndParent(host) {
		ws, rec, err := r.Store.WorkspaceByHost(ctx, candidate)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if HostServes(rec) {
			return &Tenant{Realm: RealmWorkspace, Workspace: ws}, nil
		}
	}
	return nil, ErrUnknownHost
}

// hostAndParent lists a host and, when it has one, its parent.
func hostAndParent(host string) []string {
	out := []string{host}
	if _, parent, ok := strings.Cut(host, "."); ok && parent != "" && strings.Contains(parent, ".") {
		out = append(out, parent)
	}
	return out
}

// HostServes says a workspace host record answers today: a verified custom
// domain, or a moved address whose redirect has not expired.
func HostServes(h *store.WorkspaceHost) bool {
	if h == nil {
		return false
	}
	switch h.Kind {
	case store.HostCustom:
		return h.VerifiedAt != nil
	case store.HostMoved:
		return h.ExpiresAt == nil || time.Now().Before(*h.ExpiresAt)
	}
	return false
}

// Forget drops a cached host, for callers that just changed an address.
func (r *ByAddress) Forget(host string) {
	r.mu.Lock()
	delete(r.cache, Host(host))
	r.mu.Unlock()
}

// oneLabelUnder reports whether host is <label>.<domain>, exactly one
// level down: where apps of a workspace live and what its wildcard
// certificate covers.
func oneLabelUnder(host, domain string) bool {
	label, ok := strings.CutSuffix(host, "."+domain)
	return ok && label != "" && !strings.Contains(label, ".")
}

// Addresses answers workspaces by slug with a short cache, for controllers
// that build hosts and quotas on every reconcile and have no request
// context to speak of.
type Addresses struct {
	Store store.Store
	TTL   time.Duration
	// DefaultSlug names the default workspace; nil means store.DefaultWorkspace.
	DefaultSlug DefaultSlugFunc

	mu    sync.Mutex
	cache map[string]wsEntry
	hosts map[string]hostsEntry
}

type wsEntry struct {
	ws      *store.Workspace // nil: unknown slug
	expires time.Time
}

type hostsEntry struct {
	hosts   []store.WorkspaceHost
	expires time.Time
}

// Workspace is the workspace of a slug, nil for an unknown one or when the
// store is unreachable. "" is the default workspace (apps from before
// RFC-0033 carry no workspace label). Safe for concurrent use.
func (a *Addresses) Workspace(slug string) *store.Workspace {
	if slug == "" {
		slug = a.defaultSlug()
	}
	ttl := a.TTL
	if ttl <= 0 {
		ttl = 10 * time.Second
	}
	now := time.Now()
	a.mu.Lock()
	if e, ok := a.cache[slug]; ok && now.Before(e.expires) {
		a.mu.Unlock()
		return e.ws
	}
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, err := a.Store.Workspace(ctx, slug)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil // store trouble: not cached
	}
	a.mu.Lock()
	if a.cache == nil {
		a.cache = map[string]wsEntry{}
	}
	a.cache[slug] = wsEntry{ws: ws, expires: now.Add(ttl)}
	a.mu.Unlock()
	return ws
}

// defaultSlug is the default workspace's slug, from the setting.
func (a *Addresses) defaultSlug() string {
	if a.DefaultSlug != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return a.DefaultSlug(ctx)
	}
	return store.DefaultWorkspace
}

// Address is the address of a workspace, "" for an unknown slug.
func (a *Addresses) Address(slug string) string {
	if ws := a.Workspace(slug); ws != nil {
		return ws.Address
	}
	return ""
}

// Hosts lists a workspace's host records, cached like the workspace.
func (a *Addresses) Hosts(slug string) []store.WorkspaceHost {
	if slug == "" {
		slug = a.defaultSlug()
	}
	ttl := a.TTL
	if ttl <= 0 {
		ttl = 10 * time.Second
	}
	now := time.Now()
	a.mu.Lock()
	if e, ok := a.hosts[slug]; ok && now.Before(e.expires) {
		a.mu.Unlock()
		return e.hosts
	}
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hosts, err := a.Store.ListWorkspaceHosts(ctx, slug)
	if err != nil {
		return nil
	}
	a.mu.Lock()
	if a.hosts == nil {
		a.hosts = map[string]hostsEntry{}
	}
	a.hosts[slug] = hostsEntry{hosts: hosts, expires: now.Add(ttl)}
	a.mu.Unlock()
	return hosts
}

// Domain is the domain a workspace's apps live one label under, as their
// URLs show it: the primary custom domain when one is verified and
// primary, the address otherwise.
func (a *Addresses) Domain(slug string) string {
	for _, h := range a.Hosts(slug) {
		if h.Kind == store.HostCustom && h.Primary && h.VerifiedAt != nil {
			return h.Host
		}
	}
	return a.Address(slug)
}

// ExtraDomains are the other domains a workspace's apps also answer under
// (one label under each): the address when a custom domain is primary,
// and every other verified custom domain. Moved addresses are not among
// them: they redirect.
func (a *Addresses) ExtraDomains(slug string) []string {
	primary := a.Domain(slug)
	var out []string
	if address := a.Address(slug); address != "" && address != primary {
		out = append(out, address)
	}
	for _, h := range a.Hosts(slug) {
		if h.Kind == store.HostCustom && h.VerifiedAt != nil && h.Host != primary {
			out = append(out, h.Host)
		}
	}
	return out
}

// ID is a workspace's id, "" for one the store does not know.
func (a *Addresses) ID(slug string) string {
	if ws := a.Workspace(slug); ws != nil {
		return ws.ID
	}
	return ""
}

// Suspended says whether a workspace is suspended; an unknown one is not.
func (a *Addresses) Suspended(slug string) bool {
	ws := a.Workspace(slug)
	return ws != nil && ws.Status == store.WorkspaceSuspended
}

// Limits are a workspace's ceilings; nil when it has none, or when the
// store is unreachable.
func (a *Addresses) Limits(slug string) *store.Limits {
	if ws := a.Workspace(slug); ws != nil {
		return ws.Settings.Limits
	}
	return nil
}

// SleepDefault is the workspace's default HTTP sleep policy (RFC-0075):
// after and resuming, or "" when it sets none. Cached like Workspace; a
// change shows within the TTL.
func (a *Addresses) SleepDefault(slug string) (after, resuming string) {
	if ws := a.Workspace(slug); ws != nil && ws.Settings.Sleep != nil {
		return ws.Settings.Sleep.AppsAfter, ws.Settings.Sleep.AppsResuming
	}
	return "", ""
}

// DatabaseSleepDefault is the workspace's default idle period before a
// database hibernates, "" when it sets none.
func (a *Addresses) DatabaseSleepDefault(slug string) string {
	if ws := a.Workspace(slug); ws != nil && ws.Settings.Sleep != nil {
		return ws.Settings.Sleep.DatabasesAfter
	}
	return ""
}

// Forget drops what is remembered: a workspace changed.
func (a *Addresses) Forget() {
	a.mu.Lock()
	a.cache, a.hosts = nil, nil
	a.mu.Unlock()
}
