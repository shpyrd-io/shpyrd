package tenancy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shpyrd-io/shpyrd/pkg/store"
)

func TestHost(t *testing.T) {
	for in, want := range map[string]string{
		"Shpyrd.Example.com:8443": "shpyrd.example.com",
		"shop.example.com":        "shop.example.com",
		"[::1]:8443":              "::1",
		"127.0.0.1:8080":          "127.0.0.1",
		"example.com.":            "example.com",
		"":                        "",
	} {
		if got := Host(in); got != want {
			t.Errorf("Host(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInternal(t *testing.T) {
	for _, h := range []string{"localhost", "10.0.0.5", "::1", "shpyrd-server.shpyrd-system.svc.cluster.local", "shpyrd-server.shpyrd-system.svc", ""} {
		if !Internal(h) {
			t.Errorf("Internal(%q) = false", h)
		}
	}
	for _, h := range []string{"shop.example.com", "acme.shpyrd.app", "svc.example.com"} {
		if Internal(h) {
			t.Errorf("Internal(%q) = true", h)
		}
	}
}

func TestSingle(t *testing.T) {
	st := store.NewMemory()
	r := &Single{Store: st, ConsoleHost: "shpyrd.example.com:8443"}
	// The console host is the console: no workspace (RFC-0080).
	if tn, err := r.Resolve(context.Background(), "shpyrd.example.com:8443"); err != nil || !tn.AtConsole() || tn.Workspace != nil {
		t.Errorf("console host = %+v %v", tn, err)
	}
	// Internal hosts are the operator's door with the default workspace as
	// tenant, so project commands over a kubeconfig keep working.
	if tn, err := r.Resolve(context.Background(), "10.0.0.1"); err != nil || !tn.AtConsole() || tn.ConsoleHost() || !tn.Internal || tn.Workspace == nil || tn.Workspace.Slug != store.DefaultWorkspace {
		t.Errorf("internal host = %+v %v", tn, err)
	}
	// Every other host is the one workspace.
	for _, h := range []string{"example.com", "anything.at.all", "hello.example.com"} {
		tn, err := r.Resolve(context.Background(), h)
		if err != nil || tn.AtConsole() || tn.Workspace == nil || tn.Workspace.Slug != store.DefaultWorkspace {
			t.Errorf("Single(%q) = %+v %v", h, tn, err)
		}
	}
	// No console host configured (development, tests): one door, the
	// operator's, with the workspace as tenant.
	one := &Single{Store: st}
	if tn, err := one.Resolve(context.Background(), "anything.example.com"); err != nil || !tn.AtConsole() || tn.ConsoleHost() || tn.Workspace == nil {
		t.Errorf("one door = %+v %v", tn, err)
	}
}

func TestByAddress(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "acme", Name: "Acme", Address: "acme.shpyrd.app"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "intranet", Name: "Acme intranet", Address: "intranet.acme.com"}); err != nil {
		t.Fatal(err)
	}
	// The operator's default workspace has an address like any other
	// (RFC-0080): here the platform domain itself, the open-source layout.
	if _, err := st.UpdateWorkspaceAddress(ctx, store.DefaultWorkspace, "shpyrd.app"); err != nil {
		t.Fatal(err)
	}
	r := &ByAddress{Store: st, Domain: "shpyrd.app", ConsoleHost: "console.shpyrd.io", Reserved: []string{"auth.shpyrd.app", "grafana.shpyrd.app"}, TTL: time.Minute}

	cases := map[string]string{
		// workspace addresses, exact and one label under
		"acme.shpyrd.app":        "acme",
		"ACME.shpyrd.app:443":    "acme",
		"shop.acme.shpyrd.app":   "acme",
		"intranet.acme.com":      "intranet",
		"wiki.intranet.acme.com": "intranet",
		// the default workspace: its address and one label under
		"shpyrd.app":       store.DefaultWorkspace,
		"hello.shpyrd.app": store.DefaultWorkspace,
	}
	for host, want := range cases {
		tn, err := r.Resolve(ctx, host)
		if err != nil || tn.Workspace == nil || tn.Workspace.Slug != want || tn.AtConsole() {
			t.Errorf("Resolve(%q) = %v %v, want workspace %s", host, tn, err, want)
		}
	}
	// The console host is the console, whatever addresses exist; internal
	// hosts are the console with the default workspace as tenant.
	for _, host := range []string{"console.shpyrd.io", "CONSOLE.shpyrd.io:8443"} {
		if tn, err := r.Resolve(ctx, host); err != nil || !tn.ConsoleHost() {
			t.Errorf("Resolve(%q) = %v %v, want the console", host, tn, err)
		}
	}
	for _, host := range []string{"localhost:8080", "10.244.0.7", "shpyrd-server.shpyrd-system.svc.cluster.local"} {
		if tn, err := r.Resolve(ctx, host); err != nil || !tn.AtConsole() || tn.ConsoleHost() || !tn.Internal || tn.Workspace == nil || tn.Workspace.Slug != store.DefaultWorkspace {
			t.Errorf("Resolve(%q) = %v %v, want internal console", host, tn, err)
		}
	}
	// Reserved platform names are nobody's, even one label under the
	// default workspace's address.
	for _, host := range []string{"acme.com", "deep.shop.acme.shpyrd.app", "evil.example.com", "shpyrd.io", "app.shpyrd.io", "auth.shpyrd.app", "grafana.shpyrd.app"} {
		if _, err := r.Resolve(ctx, host); !errors.Is(err, ErrUnknownHost) {
			t.Errorf("Resolve(%q) = %v, want ErrUnknownHost", host, err)
		}
	}

	// Lookups are cached for the TTL, and Forget drops one.
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "beta", Name: "Beta", Address: "evil.example.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Resolve(ctx, "evil.example.com"); !errors.Is(err, ErrUnknownHost) {
		t.Errorf("negative result not cached: %v", err)
	}
	r.Forget("evil.example.com")
	if tn, err := r.Resolve(ctx, "evil.example.com"); err != nil || tn.Workspace == nil || tn.Workspace.Slug != "beta" {
		t.Errorf("after Forget: %v %v", tn, err)
	}
}
