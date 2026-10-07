package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"github.com/shpyrd-io/shpyrd/pkg/tenancy"
)

var cloudLayout = tenancy.Layout{WorkspacesDomain: "shpyrd.cloud", AppsDomain: "shpyrd.app"}

// In the shared layout two doors and two wildcards serve every workspace:
// none of its own for a workspace of the layout, the old shape for an
// address from before it, and a redirecting door only for an old name
// outside the layout.
func TestSharedFrontDoors(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	for _, w := range []store.Workspace{
		{Slug: "acme", Name: "Acme", Address: "acme.shpyrd.cloud"},
		{Slug: "older", Name: "Older", Address: "older.shpyrd.app"}, // not moved yet
	} {
		if _, err := st.CreateWorkspace(ctx, w); err != nil {
			t.Fatal(err)
		}
	}
	exp := time.Now().Add(time.Hour)
	for _, h := range []store.WorkspaceHost{
		{Host: "acme.shpyrd.app", Kind: store.HostMoved, ExpiresAt: &exp},      // the address before the layout
		{Host: "oldacme.shpyrd.cloud", Kind: store.HostMoved, ExpiresAt: &exp}, // a rename inside it
	} {
		if _, err := st.PutWorkspaceHost(ctx, "acme", h); err != nil {
			t.Fatal(err)
		}
	}
	base, c := newTestReconciler(t)
	r := &WorkspaceReconciler{Client: c, Scheme: base.Scheme, Store: st, Config: Config{
		Domain: "operator.shpyrd.io", SystemNamespace: "shpyrd-system", ClusterIssuer: "letsencrypt",
		WorkspaceCertIssuer: "letsencrypt-dns01", IngressClassExternal: "nginx", WildcardTLS: true, Layout: cloudLayout,
	}}
	if _, err := r.Reconcile(ctx, ctrl.Request{}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	get := func(name string) *networkingv1.Ingress {
		ing := &networkingv1.Ingress{}
		if err := c.Get(ctx, types.NamespacedName{Namespace: "shpyrd-system", Name: name}, ing); err != nil {
			return nil
		}
		return ing
	}
	hosts := func(ing *networkingv1.Ingress) []string {
		var out []string
		for _, rule := range ing.Spec.Rules {
			out = append(out, rule.Host)
		}
		return out
	}

	ws := get(workspacesFrontDoorName)
	if ws == nil || len(hosts(ws)) != 2 || hosts(ws)[0] != "shpyrd.cloud" || hosts(ws)[1] != "*.shpyrd.cloud" || ws.Spec.TLS[0].SecretName != WorkspacesWildcardSecretName {
		t.Fatalf("workspaces front door = %+v", ws)
	}
	if ws.Labels["shpyrd.io/workspace-front-door"] != "" || ws.Annotations[ExternalDNSExclude] != "" {
		t.Errorf("the shared door is nobody's and publishes its wildcard: %v %v", ws.Labels, ws.Annotations)
	}
	if src := get(sourcesFrontDoorName(workspacesFrontDoorName)); src == nil || src.Spec.Rules[0].Host != "*.shpyrd.cloud" {
		t.Errorf("uploads at every workspace host: %+v", src)
	}
	apps := get(appsFrontDoorName)
	if apps == nil || len(hosts(apps)) != 2 || hosts(apps)[1] != "*.shpyrd.app" || apps.Spec.TLS[0].SecretName != AppsWildcardSecretName {
		t.Fatalf("apps front door = %+v", apps)
	}
	for name, want := range map[string][]string{
		WorkspacesWildcardSecretName: {"shpyrd.cloud", "*.shpyrd.cloud"},
		AppsWildcardSecretName:       {"shpyrd.app", "*.shpyrd.app"},
	} {
		cert := &unstructured.Unstructured{}
		cert.SetGroupVersionKind(CertificateGVK)
		if err := c.Get(ctx, types.NamespacedName{Namespace: "shpyrd-system", Name: name}, cert); err != nil {
			t.Fatalf("certificate %s: %v", name, err)
		}
		names, _, _ := unstructured.NestedStringSlice(cert.Object, "spec", "dnsNames")
		issuer, _, _ := unstructured.NestedString(cert.Object, "spec", "issuerRef", "name")
		if len(names) != 2 || names[0] != want[0] || names[1] != want[1] || issuer != "letsencrypt-dns01" {
			t.Errorf("certificate %s = %v by %s", name, names, issuer)
		}
	}

	// acme: nothing of its own. Its address from before the layout keeps a
	// door (and wildcard) so old links redirect; its rename inside the
	// layout needs none.
	if get("workspace-acme") != nil {
		t.Error("a workspace of the layout got a front door of its own")
	}
	if old := get(workspaceHostFrontDoorName("acme", "acme.shpyrd.app")); old == nil || hosts(old)[1] != "*.acme.shpyrd.app" {
		t.Errorf("old address door = %+v", old)
	}
	if get(workspaceHostFrontDoorName("acme", "oldacme.shpyrd.cloud")) != nil {
		t.Error("a rename inside the layout got a door")
	}
	// older, not moved yet: the door it had.
	if g := get("workspace-older"); g == nil || hosts(g)[1] != "*.older.shpyrd.app" {
		t.Errorf("older door = %+v", g)
	}

	// Without the layout, the shared doors go.
	r.Config.Layout = tenancy.Layout{}
	if _, err := r.Reconcile(ctx, ctrl.Request{}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if get(workspacesFrontDoorName) != nil || get(appsFrontDoorName) != nil {
		t.Error("shared doors outlived the layout")
	}
}

// An app of a workspace of the layout answers at <label>-<app>.<apps
// domain>, with the apps' wildcard copied into its namespace, and asks
// ExternalDNS for nothing: the wildcard record answers.
func TestAppInSharedLayout(t *testing.T) {
	ctx := context.Background()
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-acme-shop", Labels: project.NamespaceLabels("acme", "shop")}, Spec: shpyrdv1.AppSpec{Domains: []string{"www.acme.com"}}}
	src := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: AppsWildcardSecretName, Namespace: "shpyrd-system"}, Type: corev1.SecretTypeTLS, Data: map[string][]byte{"tls.crt": []byte("CERT"), "tls.key": []byte("KEY")}}
	r, c := newTestReconciler(t, app, src)
	r.Config.SystemNamespace = "shpyrd-system"
	r.Config.Domain = "operator.shpyrd.io"
	r.Config.HTTPSPort = ""
	r.Config.Layout = cloudLayout
	r.Config.WorkspaceAppHosts = func(ws, slug string) []string {
		if ws == "acme" {
			return cloudLayout.AppHosts("acme.shpyrd.cloud", nil, "", slug)
		}
		return nil
	}

	if got := r.Config.defaultHost(app); got != "acme-shop.shpyrd.app" {
		t.Errorf("default host = %q", got)
	}
	if got := r.Config.url(app); got != "https://acme-shop.shpyrd.app" {
		t.Errorf("url = %q", got)
	}
	tls := map[string]string{}
	for _, entry := range r.Config.ingressTLS(app) {
		tls[entry.Hosts[0]] = entry.SecretName
	}
	if tls["acme-shop.shpyrd.app"] != AppsTLSSecretName || tls["www.acme.com"] == AppsTLSSecretName || tls["www.acme.com"] == "" {
		t.Errorf("tls = %v", tls)
	}
	if err := r.reconcileAppsTLS(ctx, app); err != nil {
		t.Fatal(err)
	}
	cp := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-acme-shop", Name: AppsTLSSecretName}, cp); err != nil || string(cp.Data["tls.key"]) != "KEY" {
		t.Fatalf("copy = %+v %v", cp, err)
	}
	if reqs := r.workspaceTLSToApps(ctx, src); len(reqs) != 1 || reqs[0].Name != "shop" {
		t.Errorf("a renewal of the apps' wildcard requeues %v", reqs)
	}

	ing := &networkingv1.Ingress{}
	r.Config.mutateIngress(app, ing, false)
	if ing.Annotations[ExternalDNSExclude] != "true" || ing.Spec.Rules[0].Host != "acme-shop.shpyrd.app" {
		t.Errorf("ingress = %v %+v", ing.Annotations, ing.Spec.Rules)
	}
	// An internal app needs its own record, at the private front door.
	internal := app.DeepCopy()
	internal.Spec.Exposure = "internal"
	ing = &networkingv1.Ingress{}
	r.Config.mutateIngress(internal, ing, false)
	if _, ok := ing.Annotations[ExternalDNSExclude]; ok {
		t.Error("internal app excluded from ExternalDNS")
	}
	// A host the platform publishes one by one (an address from before the
	// layout) keeps its record.
	if r.Config.wildcardPublishes([]string{"shop.acme.shpyrd.app"}) {
		t.Error("an old two-label host has no wildcard record of the shared layout")
	}
}

// A workspace's apps follow its names: a change of address (or of its
// domains) is told once, the first look tells nothing.
func TestWorkspaceNamesChangeRequeuesItsApps(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "acme", Name: "Acme", Address: "acme.shpyrd.app"}); err != nil {
		t.Fatal(err)
	}
	base, c := newTestReconciler(t)
	var told []string
	r := &WorkspaceReconciler{Client: c, Scheme: base.Scheme, Store: st, Config: Config{
		Domain: "operator.shpyrd.io", SystemNamespace: "shpyrd-system", ClusterIssuer: "letsencrypt", Layout: cloudLayout,
	}, NamesChanged: func(_ context.Context, slug string) { told = append(told, slug) }}
	reconcile := func() {
		if _, err := r.Reconcile(ctx, ctrl.Request{}); err != nil {
			t.Fatal(err)
		}
	}
	reconcile()
	reconcile()
	if len(told) != 0 {
		t.Fatalf("nothing changed, told %v", told)
	}
	if _, err := st.UpdateWorkspaceAddress(ctx, "acme", "acme.shpyrd.cloud"); err != nil {
		t.Fatal(err)
	}
	reconcile()
	reconcile()
	if len(told) != 1 || told[0] != "acme" {
		t.Errorf("after a move, told %v", told)
	}
}

// A workspace that moved into the shared layout loses the copy of its old
// wildcard: the apps' wildcard covers its apps now.
func TestMovedWorkspaceDropsItsOldWildcardCopy(t *testing.T) {
	ctx := context.Background()
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-acme-shop", Labels: project.NamespaceLabels("acme", "shop")}}
	old := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "workspace-acme-tls", Namespace: "shpyrd-system"}, Type: corev1.SecretTypeTLS, Data: map[string][]byte{"tls.crt": []byte("OLD"), "tls.key": []byte("KEY")}}
	copied := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: WorkspaceTLSSecretName, Namespace: "app-acme-shop"}, Type: corev1.SecretTypeTLS, Data: map[string][]byte{"tls.crt": []byte("OLD"), "tls.key": []byte("KEY")}}
	r, c := newTestReconciler(t, app, old, copied)
	r.Config.SystemNamespace = "shpyrd-system"
	r.Config.Domain = "operator.shpyrd.io"
	r.Config.Layout = cloudLayout
	r.Config.WorkspaceAddress = func(string) string { return "acme.shpyrd.cloud" }
	if ok, err := r.reconcileWorkspaceTLS(ctx, app); err != nil || ok {
		t.Fatalf("copy = %v %v", ok, err)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-acme-shop", Name: WorkspaceTLSSecretName}, &corev1.Secret{}); err == nil {
		t.Error("the old wildcard's copy survived the move")
	}
}

// ingressNginx stands in for ingress-nginx's admission webhook: an Ingress
// may not claim a host and path another Ingress already holds.
func ingressNginx(t *testing.T) client.Client {
	t.Helper()
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	var c client.Client
	check := func(ctx context.Context, obj client.Object) error {
		ing, ok := obj.(*networkingv1.Ingress)
		if !ok {
			return nil
		}
		var all networkingv1.IngressList
		if err := c.List(ctx, &all); err != nil {
			return err
		}
		for _, other := range all.Items {
			if other.Namespace == ing.Namespace && other.Name == ing.Name {
				continue
			}
			for _, a := range other.Spec.Rules {
				for _, b := range ing.Spec.Rules {
					if a.Host != b.Host || a.HTTP == nil || b.HTTP == nil {
						continue
					}
					for _, pa := range a.HTTP.Paths {
						for _, pb := range b.HTTP.Paths {
							if pa.Path == pb.Path {
								return fmt.Errorf("host %q and path %q is already defined in ingress %s/%s", b.Host, pb.Path, other.Namespace, other.Name)
							}
						}
					}
				}
			}
		}
		return nil
	}
	c = fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if err := check(ctx, obj); err != nil {
				return err
			}
			return cl.Create(ctx, obj, opts...)
		},
		Update: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			if err := check(ctx, obj); err != nil {
				return err
			}
			return cl.Update(ctx, obj, opts...)
		},
	}).Build()
	return c
}

// A workspace moved out of the old layout (acme.shpyrd.app, its own door)
// into the shared one keeps a redirecting door for its old address. The
// door it had holds that host, so it goes first, in the same pass: else
// ingress-nginx refuses the new one and the pass stops there.
func TestMovingIntoTheLayoutReplacesTheOldDoor(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	for _, w := range []store.Workspace{
		{Slug: "acme", Name: "Acme", Address: "acme.shpyrd.app"},
		{Slug: "beta", Name: "Beta", Address: "beta.shpyrd.app"},
	} {
		if _, err := st.CreateWorkspace(ctx, w); err != nil {
			t.Fatal(err)
		}
	}
	c := ingressNginx(t)
	scheme, _ := kube.Scheme()
	r := &WorkspaceReconciler{Client: c, Scheme: scheme, Store: st, Config: Config{
		Domain: "operator.shpyrd.io", SystemNamespace: "shpyrd-system", ClusterIssuer: "letsencrypt",
		WorkspaceCertIssuer: "letsencrypt-dns01", IngressClassExternal: "nginx", Layout: cloudLayout,
	}}
	if _, err := r.Reconcile(ctx, ctrl.Request{}); err != nil {
		t.Fatalf("before the move: %v", err)
	}
	exists := func(name string) bool {
		return c.Get(ctx, types.NamespacedName{Namespace: "shpyrd-system", Name: name}, &networkingv1.Ingress{}) == nil
	}
	if !exists("workspace-acme") || !exists("workspace-beta") {
		t.Fatal("the old layout's doors are not there to begin with")
	}

	// The operator's move (shpyrd-cloud workspaces move acme acme).
	if _, err := st.UpdateWorkspaceAddress(ctx, "acme", "acme.shpyrd.cloud"); err != nil {
		t.Fatal(err)
	}
	exp := time.Now().Add(time.Hour)
	if _, err := st.PutWorkspaceHost(ctx, "acme", store.WorkspaceHost{Host: "acme.shpyrd.app", Kind: store.HostMoved, ExpiresAt: &exp}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{}); err != nil {
		t.Fatalf("after the move: %v", err)
	}
	for _, gone := range []string{"workspace-acme", "workspace-acme-sources", "workspace-acme-archives"} {
		if exists(gone) {
			t.Errorf("%s survived the move", gone)
		}
	}
	old := &networkingv1.Ingress{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "shpyrd-system", Name: workspaceHostFrontDoorName("acme", "acme.shpyrd.app")}, old); err != nil || old.Spec.Rules[0].Host != "acme.shpyrd.app" || old.Spec.Rules[1].Host != "*.acme.shpyrd.app" {
		t.Fatalf("the old address's door = %+v %v", old.Spec.Rules, err)
	}
	if !exists("workspace-beta") {
		t.Error("beta lost its door")
	}
}

// One workspace whose door cannot be kept does not keep the others from
// theirs: the pass goes on and reports it.
func TestOneWorkspacesDoorDoesNotStopTheOthers(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	for _, w := range []store.Workspace{
		{Slug: "acme", Name: "Acme", Address: "acme.shpyrd.app"},
		{Slug: "beta", Name: "Beta", Address: "beta.shpyrd.app"},
	} {
		if _, err := st.CreateWorkspace(ctx, w); err != nil {
			t.Fatal(err)
		}
	}
	c := ingressNginx(t)
	// Something outside the platform holds acme's host.
	pathType := networkingv1.PathTypePrefix
	squatter := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "squatter", Namespace: "elsewhere"}, Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{
		Host: "acme.shpyrd.app", IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{Path: "/", PathType: &pathType}}}},
	}}}}
	if err := c.Create(ctx, squatter); err != nil {
		t.Fatal(err)
	}
	scheme, _ := kube.Scheme()
	r := &WorkspaceReconciler{Client: c, Scheme: scheme, Store: st, Config: Config{
		Domain: "operator.shpyrd.io", SystemNamespace: "shpyrd-system", ClusterIssuer: "letsencrypt", IngressClassExternal: "nginx", Layout: cloudLayout,
	}}
	if _, err := r.Reconcile(ctx, ctrl.Request{}); err == nil {
		t.Error("acme's refusal is not reported")
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "shpyrd-system", Name: "workspace-beta"}, &networkingv1.Ingress{}); err != nil {
		t.Errorf("beta has no door: %v", err)
	}
}
