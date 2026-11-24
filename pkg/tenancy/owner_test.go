package tenancy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/store"
)

var errBoom = errors.New("store down")

// failing makes the host lookups of a store fail on demand.
type failing struct {
	store.Store
	addr, host bool
	calls      int
}

func (f *failing) WorkspaceByAddress(ctx context.Context, a string) (*store.Workspace, error) {
	f.calls++
	if f.addr {
		return nil, errBoom
	}
	return f.Store.WorkspaceByAddress(ctx, a)
}

func (f *failing) WorkspaceByHost(ctx context.Context, h string) (*store.Workspace, *store.WorkspaceHost, error) {
	f.calls++
	if f.host {
		return nil, nil, errBoom
	}
	return f.Store.WorkspaceByHost(ctx, h)
}

// ownershipFixture: alpha has verified example.com, an unexpired moved
// address, and an app host; beta has verified example.net.
func ownershipFixture(t *testing.T) (store.Store, Layout) {
	t.Helper()
	ctx := context.Background()
	st := store.NewMemory()
	layout := Layout{WorkspacesDomain: "ws.example.org", AppsDomain: "apps.example.org"}
	for slug, addr := range map[string]string{"alpha": "alpha.ws.example.org", "beta": "beta.ws.example.org"} {
		if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: slug, Name: slug, Address: addr}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	later, earlier := now.Add(time.Hour), now.Add(-time.Hour)
	for ws, hosts := range map[string][]store.WorkspaceHost{
		"alpha": {
			{Host: "example.com", Kind: store.HostCustom, VerifiedAt: &now},
			{Host: "pending.example.com", Kind: store.HostCustom},
			{Host: "old.example.org", Kind: store.HostMoved, ExpiresAt: &later},
			{Host: "gone.example.org", Kind: store.HostMoved, ExpiresAt: &earlier},
			{Host: "unverified.example.net", Kind: store.HostCustom},
		},
		"beta": {
			{Host: "example.net", Kind: store.HostCustom, VerifiedAt: &now},
			{Host: "sub.example.com", Kind: store.HostCustom, VerifiedAt: &now},
		},
	} {
		for _, h := range hosts {
			if _, err := st.PutWorkspaceHost(ctx, ws, h); err != nil {
				t.Fatal(err)
			}
		}
	}
	return st, layout
}

func TestHostOwner(t *testing.T) {
	ctx := context.Background()
	st, layout := ownershipFixture(t)
	for host, want := range map[string]string{
		// exact, one, two and five labels under a verified custom host
		"example.com":           "alpha",
		"shop.example.com":      "alpha",
		"a.b.example.com":       "alpha",
		"a.b.c.d.e.example.com": "alpha",
		"SHOP.Example.com:8443": "alpha",
		"a.b.c.d.e.example.net": "beta",
		"example.net":           "beta",
		// the most specific verified record wins over its parent
		"sub.example.com":     "beta",
		"x.y.sub.example.com": "beta",
		"other.example.com":   "alpha",
		// an unverified record owns nothing: the parent decides
		"pending.example.com":    "alpha",
		"x.pending.example.com":  "alpha",
		"unverified.example.net": "beta",
		// workspace addresses at any depth, and apps of the layout
		"alpha.ws.example.org":         "alpha",
		"a.b.alpha.ws.example.org":     "alpha",
		"alpha-crm.apps.example.org":   "alpha",
		"x.alpha-crm.apps.example.org": "alpha",
		// a moved address that has not expired
		"old.example.org":     "alpha",
		"a.b.old.example.org": "alpha",
		// nobody's
		"gone.example.org":           "",
		"a.b.gone.example.org":       "",
		"example.org":                "",
		"stranger.example.org":       "",
		"nolabel.apps.example.org":   "",
		"ghost-crm.apps.example.org": "",
	} {
		ws, err := HostOwner(ctx, st, layout, host)
		if err != nil {
			t.Errorf("HostOwner(%q): %v", host, err)
			continue
		}
		got := ""
		if ws != nil {
			got = ws.Slug
		}
		if got != want {
			t.Errorf("HostOwner(%q) = %q, want %q", host, got, want)
		}
	}
}

func TestHostOwnerMovedAddressApps(t *testing.T) {
	ctx := context.Background()
	st, layout := ownershipFixture(t)
	later := time.Now().Add(time.Hour)
	if _, err := st.PutWorkspaceHost(ctx, "beta", store.WorkspaceHost{Host: "formerly.ws.example.org", Kind: store.HostMoved, ExpiresAt: &later}); err != nil {
		t.Fatal(err)
	}
	ws, err := HostOwner(ctx, st, layout, "formerly-crm.apps.example.org")
	if err != nil || ws == nil || ws.Slug != "beta" {
		t.Errorf("app of a moved address = %v %v, want beta", ws, err)
	}
}

func TestHostOwnerErrors(t *testing.T) {
	ctx := context.Background()
	base, layout := ownershipFixture(t)
	for name, f := range map[string]*failing{
		"address": {Store: base, addr: true},
		"host":    {Store: base, host: true},
	} {
		// "x.stranger.example.org" is nobody's on a healthy store: an
		// error must not turn into that answer.
		for _, host := range []string{"x.stranger.example.org", "shop.example.com"} {
			ws, err := HostOwner(ctx, f, layout, host)
			if !errors.Is(err, errBoom) || ws != nil {
				t.Errorf("%s down, HostOwner(%q) = %v %v, want the store error", name, host, ws, err)
			}
		}
	}
	for _, host := range []string{"", ":8443", "a..example.com", "bad host.example.com", "ex/ample.com", "http://example.com"} {
		if ws, err := HostOwner(ctx, base, layout, host); !errors.Is(err, ErrInvalidHost) || ws != nil {
			t.Errorf("HostOwner(%q) = %v %v, want ErrInvalidHost", host, ws, err)
		}
	}
}

func TestOwners(t *testing.T) {
	st, layout := ownershipFixture(t)
	f := &failing{Store: st}
	o := &Owners{
		Store: f, Layout: layout, TTL: time.Minute,
		ConsoleHost: "console.example.org", Domain: "example.org",
		Reserved: []string{"auth.example.org"},
	}
	alpha, err := st.Workspace(context.Background(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	beta, _ := st.Workspace(context.Background(), "beta")

	for _, tc := range []struct {
		host string
		ws   *store.Workspace
		want Availability
	}{
		{"shop.example.com", alpha, HostFree},   // its own
		{"shop.example.net", alpha, HostTaken},  // another's
		{"x.sub.example.com", alpha, HostTaken}, // another's, more specific
		{"x.sub.example.com", beta, HostFree},
		{"free.example.edu", alpha, HostFree},              // nobody's
		{"console.example.org", alpha, HostPlatform},       // the console
		{"auth.example.org", alpha, HostPlatform},          // reserved
		{"example.org", alpha, HostPlatform},               // the platform domain
		{"localhost", alpha, HostPlatform},                 // internal
		{"anything.apps.example.org", alpha, HostPlatform}, // under a layout domain, unowned
		{"alpha-crm.apps.example.org", alpha, HostFree},    // its own app host
		{"beta-crm.apps.example.org", alpha, HostTaken},    // another workspace's app host
		{"a b", alpha, HostUnknown},                        // not a name
	} {
		if got := o.Check(tc.ws.ID, tc.host); got != tc.want {
			t.Errorf("Check(%s, %q) = %d, want %d", tc.ws.Slug, tc.host, got, tc.want)
		}
	}

	// An answer is remembered for the TTL, found or not.
	o.Forget()
	f.calls = 0
	o.Owner("cached.example.net")
	n := f.calls
	o.Owner("cached.example.net")
	if f.calls != n {
		t.Errorf("a repeated lookup went to the store (%d calls, then %d)", n, f.calls)
	}
	if _, err := st.PutWorkspaceHost(context.Background(), "alpha", store.WorkspaceHost{Host: "fresh.example.edu", Kind: store.HostCustom, VerifiedAt: &time.Time{}}); err != nil {
		t.Fatal(err)
	}
	if ws, _ := o.Owner("fresh.example.edu"); ws == nil || ws.Slug != "alpha" {
		t.Fatalf("fresh host = %v", ws)
	}
	// Forget makes a change visible at once.
	if err := st.DeleteWorkspaceHost(context.Background(), "alpha", "fresh.example.edu"); err != nil {
		t.Fatal(err)
	}
	if ws, _ := o.Owner("fresh.example.edu"); ws == nil {
		t.Error("answer was not remembered")
	}
	o.Forget()
	if ws, err := o.Owner("fresh.example.edu"); err != nil || ws != nil {
		t.Errorf("after Forget = %v %v, want nobody", ws, err)
	}

	// Errors are reported, never remembered, and never read as free.
	f.addr, f.host = true, true
	o.Forget()
	if got := o.Check(alpha.ID, "down.example.edu"); got != HostUnknown {
		t.Errorf("store down: Check = %d, want HostUnknown", got)
	}
	f.addr, f.host = false, false
	if got := o.Check(alpha.ID, "down.example.edu"); got != HostFree {
		t.Errorf("store back: Check = %d, want HostFree (an error must not be cached)", got)
	}
}

// A request two labels under a verified custom host still resolves to no
// workspace: the ownership rule does not change request routing.
func TestResolveStaysOneLabelUnderCustomHost(t *testing.T) {
	st, layout := ownershipFixture(t)
	r := &ByAddress{Store: st, Domain: "example.org", ConsoleHost: "console.example.org", Layout: layout}
	ctx := context.Background()
	if tn, err := r.Resolve(ctx, "shop.example.com"); err != nil || tn.Workspace == nil || tn.Workspace.Slug != "alpha" {
		t.Errorf("one label under = %v %v, want alpha", tn, err)
	}
	for _, host := range []string{"a.b.example.com", "a.b.c.d.e.example.com", "x.shop.example.com"} {
		if _, err := r.Resolve(ctx, host); !errors.Is(err, ErrUnknownHost) {
			t.Errorf("Resolve(%q) = %v, want ErrUnknownHost", host, err)
		}
	}
}

func TestValidHost(t *testing.T) {
	for host, want := range map[string]bool{
		"www.example.org":       true,
		"WWW.Example.org:8443":  true,
		"*.wild.example.org":    false,
		"a..example.org":        false,
		"bad name.example.org":  false,
		"https://x.example.org": false,
		"x.example.org/p":       false,
		"":                      false,
	} {
		if got := ValidHost(host); got != want {
			t.Errorf("ValidHost(%q) = %v, want %v", host, got, want)
		}
	}
}

// A URL is not cut down to its scheme and judged free.
func TestCheckURLIsNotFree(t *testing.T) {
	st, layout := ownershipFixture(t)
	o := &Owners{Store: st, Layout: layout, ConsoleHost: "console.example.org", Domain: "example.org"}
	if got := o.Check("any", "https://x.example.edu/p"); got != HostUnknown {
		t.Errorf("Check(URL) = %d, want HostUnknown", got)
	}
}
