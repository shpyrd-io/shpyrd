package server

import (
	"context"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/shpyrd-io/shpyrd/pkg/kube"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"github.com/shpyrd-io/shpyrd/pkg/tenancy"
)

// The App controller the server builds judges the hosts of a project's
// spec.domains: it has the ownership lookup, over the store, and the lookup
// refuses another workspace's hosts and the platform's own names.
func TestDomainOwnershipReconcilerHasTheLookup(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	ids := map[string]string{}
	for _, slug := range []string{"alpha", "beta"} {
		ws, err := st.CreateWorkspace(ctx, store.Workspace{Slug: slug, Name: slug, Address: slug + ".example.org"})
		if err != nil {
			t.Fatal(err)
		}
		ids[slug] = ws.ID
	}
	now := time.Now()
	if _, err := st.PutWorkspaceHost(ctx, "alpha", store.WorkspaceHost{Host: "example.com", Kind: store.HostCustom, VerifiedAt: &now}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHPYRD_DOMAIN", "example.net")
	t.Setenv("SHPYRD_DASHBOARD_URL", "https://console.example.net")

	rec := newAppReconciler(appReconcilerParts{
		kube:       &kube.Client{Kube: fake.NewSimpleClientset(), Namespace: "shpyrd-system"},
		client:     ctrlfake.NewClientBuilder().Build(),
		recorder:   record.NewFakeRecorder(1),
		workspaces: &tenancy.Addresses{Store: st},
		owners:     newHostOwners(st),
		store:      st,
	})

	lookup := rec.Config.HostAvailability
	if lookup == nil {
		t.Fatal("the reconciler the server builds has no ownership lookup")
	}
	for _, c := range []struct {
		workspace, host string
		want            tenancy.Availability
	}{
		{"beta", "shop.example.com", tenancy.HostTaken},
		{"alpha", "shop.example.com", tenancy.HostFree},
		{"beta", "www.nobody.example.org", tenancy.HostFree},
		{"beta", "console.example.net", tenancy.HostPlatform},
		{"beta", "auth.example.net", tenancy.HostPlatform},
		{"gamma", "www.nobody.example.org", tenancy.HostUnknown},
	} {
		if got := lookup(c.workspace, c.host); got != c.want {
			t.Errorf("%s asking for %s: %v, want %v", c.workspace, c.host, got, c.want)
		}
	}
}
