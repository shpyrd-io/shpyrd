package tenancy

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// ErrInvalidHost says a host given to the ownership rule is empty or not a
// name: nothing can be said about it, and it is never reported as nobody's.
var ErrInvalidHost = errors.New("not a valid host name")

// HostOwner is the ownership rule: which workspace a host belongs to, at
// any depth, as opposed to Resolver.Resolve, which answers which door a
// request enters and looks only one label under a name.
//
// The host and each of its parent names (down to two labels) are compared,
// most specific first, with:
//
//   - workspace addresses;
//   - app hosts of the layout (<label>-<app>.<apps domain>), which belong
//     to the workspace at <label>.<workspaces domain>, or to the one that
//     moved from that address while its redirect lasts;
//   - verified custom hosts and moved addresses that have not expired
//     (HostServes).
//
// The first name that matches decides, so a verified subdomain of one
// workspace's domain that belongs to another workspace is the other's. An
// unverified custom record and an expired moved record own nothing and the
// search goes on with the parent name.
//
// The result is the owning workspace, or nil with a nil error when nobody
// owns the host. Any store error is returned as an error with a nil
// workspace: a failed lookup is never "nobody's". An empty or malformed
// host returns ErrInvalidHost.
func HostOwner(ctx context.Context, st store.Store, layout Layout, host string) (*store.Workspace, error) {
	if !ValidHost(host) {
		return nil, ErrInvalidHost
	}
	host = Host(host)
	for _, name := range hostAndParents(host) {
		ws, err := ownerOfName(ctx, st, layout, name)
		if err != nil {
			return nil, err
		}
		if ws != nil {
			return ws, nil
		}
	}
	return nil, nil
}

// ValidHost says the ownership rule can judge host: a plain DNS-style name
// (a port is ignored), not empty, not a URL or a path, with no wildcard
// label. It is the check HostOwner makes before it looks anything up.
func ValidHost(host string) bool {
	if strings.ContainsAny(host, "/\\@?#% \t") {
		return false
	}
	return validHostName(Host(host))
}

// ownerOfName looks one name up exactly; nil, nil when nothing owns it.
func ownerOfName(ctx context.Context, st store.Store, layout Layout, name string) (*store.Workspace, error) {
	ws, err := st.WorkspaceByAddress(ctx, name)
	if err == nil {
		return ws, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	// <label>-<app>.<apps domain> belongs to the workspace of <label>,
	// whether it still has that address or moved from it. A label no
	// workspace has falls through: the name may be a record of another.
	if label, _, ok := layout.ParseAppHost(name); ok {
		address := layout.Address(label)
		ws, err := st.WorkspaceByAddress(ctx, address)
		if err == nil {
			return ws, nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		ws, rec, err := st.WorkspaceByHost(ctx, address)
		if err == nil && HostServes(rec) {
			return ws, nil
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}
	ws, rec, err := st.WorkspaceByHost(ctx, name)
	if err == nil && HostServes(rec) {
		return ws, nil
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	return nil, nil
}

// hostAndParents lists a host and every parent name that still has two
// labels or more, longest first: a.b.example.com, b.example.com,
// example.com. A bare top-level label is never a candidate.
func hostAndParents(host string) []string {
	out := []string{host}
	for {
		_, parent, ok := strings.Cut(host, ".")
		if !ok || !strings.Contains(parent, ".") {
			return out
		}
		out = append(out, parent)
		host = parent
	}
}

// validHostName is a conservative syntax check on a lowercased name: the
// letters, digits, hyphens and dots of a DNS name, no empty label.
func validHostName(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
				return false
			}
		}
	}
	return true
}

// Availability is what the ownership rule says about a host for one
// workspace that wants to serve it.
type Availability int

// Availabilities. The zero value is HostUnknown, so a caller that forgets
// to set it fails closed.
const (
	// HostUnknown: the lookup failed or the host is not a name. Do not
	// treat it as free.
	HostUnknown Availability = iota
	// HostFree: nobody owns it, or the workspace itself does.
	HostFree
	// HostTaken: another workspace owns it.
	HostTaken
	// HostPlatform: a name of the platform (the console, sign-in and other
	// reserved hosts, an internal host, or a name at or under the layout's
	// domains that no workspace owns); no project may serve it.
	HostPlatform
	// HostInvalid: the host is not a valid name (a wildcard, a malformed
	// name); it can never be served, and asking again changes nothing.
	// Owners.Check does not return it: it answers HostUnknown for such a
	// name, and a caller tells the two apart with ValidHost.
	HostInvalid
)

// Owners is the cached form of HostOwner, for callers that ask on every
// reconcile and have no request context. Safe for concurrent use.
type Owners struct {
	Store store.Store
	// Layout says where workspaces and their apps answer.
	Layout Layout
	// TTL bounds how long an answer, owned or not, is remembered. Zero
	// means ten seconds. Errors are never remembered.
	TTL time.Duration
	// ConsoleHost, Domain and Reserved name the platform's own hosts, as in
	// ByAddress: the console, the platform domain and the hosts reserved
	// under it (auth, grafana, the registry).
	ConsoleHost string
	Domain      string
	Reserved    []string

	mu    sync.Mutex
	cache map[string]ownerEntry
}

type ownerEntry struct {
	ws      *store.Workspace // nil: nobody's
	expires time.Time
}

func (o *Owners) ttl() time.Duration {
	if o.TTL <= 0 {
		return 10 * time.Second
	}
	return o.TTL
}

// Owner is HostOwner with a cache: the workspace that owns host, nil with a
// nil error when nobody does, and an error, never remembered, when the
// lookup failed or the host is not a name. A change is seen within the TTL,
// or at once after Forget.
func (o *Owners) Owner(host string) (*store.Workspace, error) {
	host = Host(host)
	now := time.Now()
	o.mu.Lock()
	if e, ok := o.cache[host]; ok && now.Before(e.expires) {
		o.mu.Unlock()
		return e.ws, nil
	}
	o.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, err := HostOwner(ctx, o.Store, o.Layout, host)
	if err != nil {
		return nil, err
	}
	o.mu.Lock()
	if o.cache == nil || len(o.cache) > 4096 { // bounded: a flood of names must not grow it
		o.cache = map[string]ownerEntry{}
	}
	o.cache[host] = ownerEntry{ws: ws, expires: now.Add(o.ttl())}
	o.mu.Unlock()
	return ws, nil
}

// Check tells whether the workspace with the given id may serve host:
// HostFree (nobody's or its own), HostTaken (another workspace's),
// HostPlatform (a name of the platform) or HostUnknown (the lookup failed).
// A name another workspace owns is HostTaken even under a layout domain;
// the platform's own names win over everything.
func (o *Owners) Check(workspaceID, host string) Availability {
	if o.Platform(host) {
		return HostPlatform
	}
	if !ValidHost(host) { // before Host: it would cut a URL down to its scheme
		return HostUnknown
	}
	host = Host(host)
	ws, err := o.Owner(host)
	if err != nil {
		return HostUnknown
	}
	switch {
	case ws == nil:
		if o.Layout.Covers(host) {
			return HostPlatform
		}
		return HostFree
	case ws.ID == workspaceID:
		return HostFree
	}
	return HostTaken
}

// Platform says host is a name of the platform itself: the console host, a
// reserved host, an internal host (an IP literal, localhost, a service
// name), or the platform domain. It does not look at the store.
func (o *Owners) Platform(host string) bool {
	host = Host(host)
	if host == "" || Internal(host) {
		return true
	}
	if console := Host(o.ConsoleHost); console != "" && host == console {
		return true
	}
	if domain := Host(o.Domain); domain != "" && host == domain {
		return true
	}
	for _, r := range o.Reserved {
		if host == Host(r) {
			return true
		}
	}
	return false
}

// Forget drops what is remembered: a workspace, an address or a host
// record changed.
func (o *Owners) Forget() {
	o.mu.Lock()
	o.cache = nil
	o.mu.Unlock()
}

// Covers says host is one of the layout's domains or any name under one:
// names the platform hands out, never a project's own domain. False when
// the layout is off.
func (l Layout) Covers(host string) bool {
	host = Host(host)
	for _, d := range []string{l.WorkspacesDomain, l.AppsDomain} {
		if d != "" && (host == d || strings.HasSuffix(host, "."+d)) {
			return true
		}
	}
	return false
}
