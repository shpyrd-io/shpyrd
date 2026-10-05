package tenancy

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/store"
)

var cloud = Layout{WorkspacesDomain: "shpyrd.cloud", AppsDomain: "shpyrd.app"}

func TestLayoutParseAppHost(t *testing.T) {
	for host, want := range map[string][2]string{
		"acme-crm.shpyrd.app":         {"acme", "crm"},
		"acme-my-app.shpyrd.app":      {"acme", "my-app"}, // project slugs keep their hyphens
		"ACME-CRM.shpyrd.app:8443":    {"acme", "crm"},
		"a-b.shpyrd.app":              {"a", "b"},
		"io-hr-tools.shpyrd.app":      {"io", "hr-tools"},
		"acme-crm.shpyrd.app.":        {"acme", "crm"},
		"platform-billing.shpyrd.app": {"platform", "billing"},
	} {
		label, app, ok := cloud.ParseAppHost(host)
		if !ok || label != want[0] || app != want[1] {
			t.Errorf("ParseAppHost(%q) = %q %q %v, want %v", host, label, app, ok, want)
		}
	}
	// No hyphen: the platform's (the apex, www); two labels: an address from
	// before the layout; another domain: not ours.
	for _, host := range []string{"shpyrd.app", "www.shpyrd.app", "crm.acme.shpyrd.app", "acme-crm.shpyrd.cloud", "acme-crm.example.com", "-crm.shpyrd.app", "acme-.shpyrd.app"} {
		if label, app, ok := cloud.ParseAppHost(host); ok {
			t.Errorf("ParseAppHost(%q) = %q %q, want none", host, label, app)
		}
	}
	if _, _, ok := (Layout{}).ParseAppHost("acme-crm.shpyrd.app"); ok {
		t.Error("the zero layout parses nothing")
	}
}

func TestLayoutLabel(t *testing.T) {
	if l, ok := cloud.Label("acme.shpyrd.cloud"); !ok || l != "acme" {
		t.Errorf("Label = %q %v", l, ok)
	}
	// A hyphen left from before the rule, another domain, two labels: the
	// layout does not cover them, so their apps stay one label under.
	for _, a := range []string{"team-tests.shpyrd.cloud", "acme.shpyrd.app", "x.acme.shpyrd.cloud", "shpyrd.cloud", "intranet.acme.com"} {
		if l, ok := cloud.Label(a); ok {
			t.Errorf("Label(%q) = %q, want none", a, l)
		}
	}
}

func TestLayoutAppHosts(t *testing.T) {
	now := time.Now()
	custom := func(host string, primary bool) store.WorkspaceHost {
		return store.WorkspaceHost{Host: host, Kind: store.HostCustom, Primary: primary, VerifiedAt: &now}
	}
	moved := store.WorkspaceHost{Host: "old.shpyrd.cloud", Kind: store.HostMoved}
	unverified := store.WorkspaceHost{Host: "wiki.acme.com", Kind: store.HostCustom}
	cases := []struct {
		name    string
		layout  Layout
		address string
		hosts   []store.WorkspaceHost
		want    []string
	}{
		{"layout", cloud, "acme.shpyrd.cloud", nil, []string{"acme-crm.shpyrd.app"}},
		{"moved and unverified hosts add nothing", cloud, "acme.shpyrd.cloud", []store.WorkspaceHost{moved, unverified}, []string{"acme-crm.shpyrd.app"}},
		{"own domain first, the shared name kept", cloud, "acme.shpyrd.cloud", []store.WorkspaceHost{custom("intranet.acme.com", true)}, []string{"crm.intranet.acme.com", "acme-crm.shpyrd.app"}},
		{"another verified domain", cloud, "acme.shpyrd.cloud", []store.WorkspaceHost{custom("intranet.acme.com", false)}, []string{"acme-crm.shpyrd.app", "crm.intranet.acme.com"}},
		{"address from before the layout", cloud, "acme.shpyrd.app", nil, []string{"crm.acme.shpyrd.app"}},
		{"hyphen left from before the rule", cloud, "team-tests.shpyrd.cloud", nil, []string{"crm.team-tests.shpyrd.cloud"}},
		{"no layout", Layout{}, "acme.shpyrd.app", []store.WorkspaceHost{custom("intranet.acme.com", true)}, []string{"crm.intranet.acme.com", "crm.acme.shpyrd.app"}},
	}
	for _, c := range cases {
		if got := c.layout.AppHosts(c.address, c.hosts, "", "crm"); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: AppHosts = %v, want %v", c.name, got, c.want)
		}
	}
	if got := (Layout{}).AppHosts("", nil, "example.com", "crm"); !reflect.DeepEqual(got, []string{"crm.example.com"}) {
		t.Errorf("no address: %v", got)
	}
}

func TestByAddressLayout(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	for _, w := range []store.Workspace{
		{Slug: "acme", Name: "Acme", Address: "acme.shpyrd.cloud"},
		// Not moved yet: its address and apps are where they were.
		{Slug: "older", Name: "Older", Address: "older.shpyrd.app"},
	} {
		if _, err := st.CreateWorkspace(ctx, w); err != nil {
			t.Fatal(err)
		}
	}
	// A workspace from before the hyphen rule, not moved yet: created with
	// a valid slug, given its old address by hand.
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "team", Name: "Team", Address: "teamtmp.shpyrd.cloud"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateWorkspaceAddress(ctx, "team", "team-tests.shpyrd.app"); err != nil {
		t.Fatal(err)
	}
	// Beta renamed itself from old to beta: the old address redirects.
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "beta", Name: "Beta", Address: "beta.shpyrd.cloud"}); err != nil {
		t.Fatal(err)
	}
	exp := time.Now().Add(time.Hour)
	if _, err := st.PutWorkspaceHost(ctx, "beta", store.WorkspaceHost{Host: "old.shpyrd.cloud", Kind: store.HostMoved, ExpiresAt: &exp}); err != nil {
		t.Fatal(err)
	}
	r := &ByAddress{Store: st, Domain: "operator.shpyrd.io", ConsoleHost: "operator.shpyrd.io", Layout: cloud, TTL: time.Minute}
	for host, want := range map[string]string{
		"acme.shpyrd.cloud":         "acme",
		"acme-crm.shpyrd.app":       "acme",
		"acme-my-app.shpyrd.app":    "acme",
		"older.shpyrd.app":          "older",
		"lessons.older.shpyrd.app":  "older",
		"team-tests.shpyrd.app":     "team",
		"crm.team-tests.shpyrd.app": "team",
		"old.shpyrd.cloud":          "beta",
		"old-crm.shpyrd.app":        "beta",
		"beta-crm.shpyrd.app":       "beta",
	} {
		tn, err := r.Resolve(ctx, host)
		if err != nil || tn.Workspace == nil || tn.Workspace.Slug != want {
			t.Errorf("Resolve(%q) = %v %v, want %s", host, tn, err, want)
		}
	}
	for _, host := range []string{"nobody-crm.shpyrd.app", "shpyrd.app", "www.shpyrd.app", "nobody.shpyrd.cloud"} {
		if _, err := r.Resolve(ctx, host); !errors.Is(err, ErrUnknownHost) {
			t.Errorf("Resolve(%q) = %v, want ErrUnknownHost", host, err)
		}
	}
}

func TestAddressesAppHosts(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "acme", Name: "Acme", Address: "acme.shpyrd.cloud"}); err != nil {
		t.Fatal(err)
	}
	a := &Addresses{Store: st, Layout: cloud}
	if got := a.AppHosts("acme", "crm"); !reflect.DeepEqual(got, []string{"acme-crm.shpyrd.app"}) {
		t.Errorf("AppHosts = %v", got)
	}
	if got := a.AppHosts("nobody", "crm"); got != nil {
		t.Errorf("unknown workspace: %v", got)
	}
}
