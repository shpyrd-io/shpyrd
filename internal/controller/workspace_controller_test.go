package controller

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

func TestWorkspaceFrontDoors(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "acme", Name: "Acme", Address: "acme.shpyrd.test"}); err != nil {
		t.Fatal(err)
	}
	// Under the platform domain the wildcard covers the dashboard host.
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "beta", Name: "Beta", Address: "beta.example.test"}); err != nil {
		t.Fatal(err)
	}
	// No address yet: nothing to publish.
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "pending", Name: "Pending"}); err != nil {
		t.Fatal(err)
	}
	base, c := newTestReconciler(t)
	r := &WorkspaceReconciler{Client: c, Scheme: base.Scheme, Store: st, Config: Config{
		Domain: "example.test", SystemNamespace: "shpyrd-system", ClusterIssuer: "letsencrypt", IngressClassExternal: "nginx", WildcardTLS: true,
	}}
	if _, err := r.Reconcile(ctx, ctrl.Request{}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// acme: its own host, its own certificate, on the external front door.
	ing := &networkingv1.Ingress{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "shpyrd-system", Name: "workspace-acme"}, ing); err != nil {
		t.Fatalf("acme ingress: %v", err)
	}
	if ing.Spec.Rules[0].Host != "acme.shpyrd.test" || *ing.Spec.IngressClassName != "nginx" || ing.Spec.TLS[0].SecretName != "workspace-acme-tls" {
		t.Errorf("acme ingress = %+v", ing.Spec)
	}
	// The dashboard's Ingress keeps nginx's 1 MiB body default (a JSON API,
	// and nginx buffers before the server sees anything)...
	if got, ok := ing.Annotations["nginx.ingress.kubernetes.io/proxy-body-size"]; ok {
		t.Errorf("acme front door sets proxy-body-size %q; uploads have their own Ingress", got)
	}
	// ...and `shpyrd deploy` uploads through a companion for the one path,
	// streamed, up to the API's maximum (pkg/api maxSourceSize).
	src := &networkingv1.Ingress{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "shpyrd-system", Name: "workspace-acme-sources"}, src); err != nil {
		t.Fatalf("acme sources ingress: %v", err)
	}
	if got := src.Annotations["nginx.ingress.kubernetes.io/proxy-body-size"]; got != SourcesBodySize || SourcesBodySize != "512m" {
		t.Errorf("acme sources proxy-body-size = %q, want 512m", got)
	}
	if got := src.Annotations["nginx.ingress.kubernetes.io/proxy-request-buffering"]; got != "off" {
		t.Errorf("acme sources request buffering = %q, want off", got)
	}
	if len(src.Spec.Rules) != 1 || src.Spec.Rules[0].Host != "acme.shpyrd.test" || src.Spec.TLS[0].SecretName != "workspace-acme-tls" {
		t.Errorf("acme sources ingress = %+v", src.Spec)
	} else if p := src.Spec.Rules[0].HTTP.Paths[0]; p.Path != SourcesPath || *p.PathType != networkingv1.PathTypeExact || p.Backend.Service.Name != "shpyrd-server" {
		t.Errorf("acme sources path = %+v", p)
	}
	if src.Labels["shpyrd.io/workspace-front-door"] != "true" || src.Labels[shpyrdv1.LabelWorkspace] != "acme" {
		t.Errorf("acme sources labels = %v (must be collected with the workspace)", src.Labels)
	}
	if svc := ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service; svc.Name != "shpyrd-server" || svc.Port.Name != "http" {
		t.Errorf("acme backend = %+v", svc)
	}
	if ing.Labels[shpyrdv1.LabelWorkspace] != "acme" {
		t.Errorf("acme labels = %v", ing.Labels)
	}
	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(CertificateGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: "shpyrd-system", Name: "workspace-acme-tls"}, cert); err != nil {
		t.Fatalf("acme certificate: %v", err)
	}
	names, _, _ := unstructured.NestedStringSlice(cert.Object, "spec", "dnsNames")
	if len(names) != 2 || names[0] != "acme.shpyrd.test" || names[1] != "*.acme.shpyrd.test" {
		t.Errorf("acme certificate names = %v", names)
	}

	// beta: covered by the platform wildcard, no certificate of its own.
	if err := c.Get(ctx, types.NamespacedName{Namespace: "shpyrd-system", Name: "workspace-beta"}, ing); err != nil {
		t.Fatalf("beta ingress: %v", err)
	}
	if ing.Spec.TLS[0].SecretName != "" {
		t.Errorf("beta must use the wildcard: %+v", ing.Spec.TLS)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "shpyrd-system", Name: "workspace-beta-tls"}, cert); err == nil {
		t.Error("beta got a certificate although the wildcard covers it")
	}
	// pending: nothing.
	if err := c.Get(ctx, types.NamespacedName{Namespace: "shpyrd-system", Name: "workspace-pending"}, ing); err == nil {
		t.Error("a workspace without address got a front door")
	}

	// A workspace that disappears from the store loses its front door.
	m2 := store.NewMemory()
	if _, err := m2.CreateWorkspace(ctx, store.Workspace{Slug: "beta", Name: "Beta", Address: "beta.example.test"}); err != nil {
		t.Fatal(err)
	}
	r.Store = m2
	if _, err := r.Reconcile(ctx, ctrl.Request{}); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "shpyrd-system", Name: "workspace-acme"}, ing); err == nil {
		t.Error("acme's front door survived its workspace")
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "shpyrd-system", Name: "workspace-acme-sources"}, ing); err == nil {
		t.Error("acme's sources front door survived its workspace")
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "shpyrd-system", Name: "workspace-beta"}, ing); err != nil {
		t.Errorf("beta's front door must stay: %v", err)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "shpyrd-system", Name: "workspace-beta-sources"}, ing); err != nil {
		t.Errorf("beta's sources front door must stay: %v", err)
	}
}

// A suspended workspace's apps stop and are not served: their processes go
// to zero, their Ingresses go (the front door's default backend answers
// with the suspension page), and both come back on resume. The implicit
// workspace is never suspended.
func TestSuspendedWorkspaceIsNotServed(t *testing.T) {
	ctx := context.Background()
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-acme-shop", Labels: project.NamespaceLabels("acme", "shop"), Generation: 1},
		Spec:       shpyrdv1.AppSpec{Image: "ghcr.io/acme/shop@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Access: shpyrdv1.AccessAuthenticated},
	}
	r, c := newTestReconciler(t, app)
	suspended := map[string]bool{}
	r.Config.WorkspaceSuspended = func(slug string) bool { return suspended[slug] }
	r.Config.WorkspaceDomain = func(slug string) string { return slug + ".shpyrd.test" }
	got := runReconcile(t, r, app)
	for _, name := range []string{"shop", edgeName(app)} {
		if err := c.Get(ctx, types.NamespacedName{Namespace: "app-acme-shop", Name: name}, &networkingv1.Ingress{}); err != nil {
			t.Fatalf("active workspace: ingress %s missing: %v", name, err)
		}
	}
	suspended["acme"] = true
	got = runReconcile(t, r, got)
	for _, name := range []string{"shop", edgeName(app)} {
		if err := c.Get(ctx, types.NamespacedName{Namespace: "app-acme-shop", Name: name}, &networkingv1.Ingress{}); !apierrors.IsNotFound(err) {
			t.Errorf("suspended workspace: ingress %s still there: %v", name, err)
		}
	}
	dep := &appsv1.Deployment{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-acme-shop", Name: "shop-web"}, dep); err != nil {
		t.Errorf("the app's Deployment must stay while suspended: %v", err)
	} else if *dep.Spec.Replicas != 0 {
		t.Errorf("a suspended workspace's processes must stop, got %d replicas", *dep.Spec.Replicas)
	}
	if !strings.Contains(got.Status.Message, "workspace suspended") {
		t.Errorf("status must say why: %q", got.Status.Message)
	}
	delete(suspended, "acme")
	runReconcile(t, r, got)
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-acme-shop", Name: "shop"}, &networkingv1.Ingress{}); err != nil {
		t.Errorf("resumed workspace: ingress missing: %v", err)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-acme-shop", Name: "shop-web"}, dep); err != nil || *dep.Spec.Replicas != 1 {
		t.Errorf("resumed workspace: the processes must come back: %v", err)
	}
}

// A workspace's door is looked at on every pass and what was seen is
// recorded on the workspace: the Ingress, the certificate (the Secret is
// the proof), and — where the platform publishes names — the name at the
// zone's own servers and the door over HTTPS. The pass runs again soon
// while a door does not answer, and the first time it does is kept.
func TestWorkspaceReadiness(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "acme", Name: "Acme", Address: "acme.shpyrd.test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "beta", Name: "Beta", Address: "beta.example.test"}); err != nil {
		t.Fatal(err)
	}
	base, c := newTestReconciler(t)
	r := &WorkspaceReconciler{Client: c, Scheme: base.Scheme, Store: st, Config: Config{
		Domain: "example.test", SystemNamespace: "shpyrd-system", ClusterIssuer: "letsencrypt", IngressClassExternal: "nginx", WildcardTLS: true,
	}}

	// No DNS provider: the two outside checks are not made, and say so.
	res, err := r.Reconcile(ctx, ctrl.Request{})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.RequeueAfter != readinessRetry {
		t.Errorf("a door not answering yet must be looked at again in %s, got %s", readinessRetry, res.RequeueAfter)
	}
	acme, _ := st.Workspace(ctx, "acme")
	if acme.Readiness == nil || acme.Readiness.Ready || acme.ReadyAt != nil {
		t.Fatalf("acme without its certificate must not be ready: %+v", acme.Readiness)
	}
	if got := checkByName(acme.Readiness, CheckCertificate); got == nil || got.OK || got.Detail != "certificate waiting for the certificate authority" {
		t.Errorf("acme certificate check = %+v", got)
	}
	if got := checkByName(acme.Readiness, CheckFrontDoor); got == nil || !got.OK {
		t.Errorf("acme front door check = %+v (the Ingress exists; no load balancer to wait for)", got)
	}
	if got := checkByName(acme.Readiness, CheckPublicName); got == nil || !got.OK || got.Detail != "not checked: no DNS provider" {
		t.Errorf("acme public name check = %+v", got)
	}
	beta, _ := st.Workspace(ctx, "beta")
	if beta.Readiness == nil || !beta.Readiness.Ready || beta.ReadyAt == nil {
		t.Fatalf("beta under the wildcard must be ready at once: %+v", beta.Readiness)
	}
	if got := checkByName(beta.Readiness, CheckCertificate); got == nil || got.Detail != "the platform's wildcard" {
		t.Errorf("beta certificate check = %+v", got)
	}

	// The certificate is issued: the Secret appears, and acme is ready.
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "shpyrd-system", Name: "workspace-acme-tls"}, Data: map[string][]byte{"tls.crt": []byte("pem"), "tls.key": []byte("pem")}}
	if err := c.Create(ctx, secret); err != nil {
		t.Fatal(err)
	}
	if res, err = r.Reconcile(ctx, ctrl.Request{}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.RequeueAfter != workspaceSync {
		t.Errorf("every door answers: the next pass is the usual one, got %s", res.RequeueAfter)
	}
	acme, _ = st.Workspace(ctx, "acme")
	if !acme.Readiness.Ready || acme.ReadyAt == nil {
		t.Fatalf("acme with its certificate must be ready: %+v", acme.Readiness)
	}
	readyAt := *acme.ReadyAt

	// With a DNS provider the name and the door are asked outside. The
	// name is asked at the zone's own servers; a "no such name" there is
	// the truth, and the door is not ready.
	r.Config.PublicChecks = true
	r.Config.ExternalLBAddress = "203.0.113.10"
	var asked []string
	r.checker.authoritative = func(_ context.Context, host string) ([]net.IP, error) {
		asked = append(asked, host)
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	r.checker.door = func(context.Context, string, string) error { return nil }
	// With a load balancer, the front door is published when its address
	// is on the Ingress; the test's front door never gets one by itself.
	ing := &networkingv1.Ingress{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "shpyrd-system", Name: "workspace-acme"}, ing); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Reconcile(ctx, ctrl.Request{}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	acme, _ = st.Workspace(ctx, "acme")
	if got := checkByName(acme.Readiness, CheckFrontDoor); got == nil || got.OK || got.Detail != "waiting for the front door's address" {
		t.Errorf("acme front door check without the balancer's address = %+v", got)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "shpyrd-system", Name: "workspace-acme"}, ing); err != nil {
		t.Fatal(err)
	}
	ing.Status.LoadBalancer.Ingress = []networkingv1.IngressLoadBalancerIngress{{IP: "203.0.113.10"}}
	if err := c.Status().Update(ctx, ing); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "shpyrd-system", Name: "workspace-acme"}, ing); err != nil || len(ing.Status.LoadBalancer.Ingress) == 0 {
		t.Fatalf("the test client did not keep the Ingress status: %+v %v", ing.Status, err)
	}
	if _, err = r.Reconcile(ctx, ctrl.Request{}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	acme, _ = st.Workspace(ctx, "acme")
	if acme.Readiness.Ready {
		t.Errorf("acme must not be ready while its name is not published: %+v", acme.Readiness)
	}
	if got := checkByName(acme.Readiness, CheckPublicName); got == nil || got.OK || got.Detail != "acme.shpyrd.test is not published yet" {
		t.Errorf("acme public name check = %+v", got)
	}
	if !acme.ReadyAt.Equal(readyAt) {
		t.Errorf("the first time the door answered is kept: %v, was %v", acme.ReadyAt, readyAt)
	}
	// The name points at the front door, for the address and a name under
	// it (the apps' wildcard); the door answers: ready. A door that does
	// not answer is not ready, whatever DNS says.
	r.checker.authoritative = func(_ context.Context, host string) ([]net.IP, error) {
		asked = append(asked, host)
		return []net.IP{net.ParseIP("203.0.113.10")}, nil
	}
	asked = nil
	if _, err = r.Reconcile(ctx, ctrl.Request{}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	acme, _ = st.Workspace(ctx, "acme")
	if !acme.Readiness.Ready {
		t.Errorf("acme published and answering must be ready: %+v", acme.Readiness)
	}
	if len(asked) < 2 || asked[0] != "acme.shpyrd.test" || !strings.HasSuffix(asked[1], ".acme.shpyrd.test") || !strings.HasPrefix(asked[1], "probe-") {
		t.Errorf("the address and a probe under it are asked, got %v", asked)
	}
	r.checker.door = func(context.Context, string, string) error {
		return errors.New("https://acme.shpyrd.test does not answer: connection refused")
	}
	if _, err = r.Reconcile(ctx, ctrl.Request{}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	acme, _ = st.Workspace(ctx, "acme")
	if got := checkByName(acme.Readiness, CheckDoorAnswers); acme.Readiness.Ready || got == nil || got.OK || !strings.Contains(got.Detail, "connection refused") {
		t.Errorf("acme door check = %+v ready=%v", got, acme.Readiness.Ready)
	}
	// A name pointing elsewhere is said so.
	r.checker.door = func(context.Context, string, string) error { return nil }
	r.checker.authoritative = func(_ context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("198.51.100.7")}, nil
	}
	if _, err = r.Reconcile(ctx, ctrl.Request{}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	acme, _ = st.Workspace(ctx, "acme")
	if got := checkByName(acme.Readiness, CheckPublicName); got == nil || got.OK || got.Detail != "acme.shpyrd.test points elsewhere: 198.51.100.7" {
		t.Errorf("acme public name check = %+v", got)
	}
}

func checkByName(r *store.WorkspaceReadiness, name string) *store.ReadinessCheck {
	if r == nil {
		return nil
	}
	for i := range r.Checks {
		if r.Checks[i].Name == name {
			return &r.Checks[i]
		}
	}
	return nil
}

// A workspace's ceilings reach the quotas of projects already running
// (#51): the workspace reconciler writes them on its pass, with no deploy;
// a workspace without ceilings has no quota.
func TestQuotasFollowTheWorkspace(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	for _, slug := range []string{"acme", "beta"} {
		if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: slug, Name: slug}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.UpdateWorkspaceSettings(ctx, "acme", store.WorkspaceSettings{Limits: &store.Limits{Memory: "256Mi"}}); err != nil {
		t.Fatal(err)
	}
	acme := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "p-shop", Labels: map[string]string{shpyrdv1.LabelWorkspace: "acme"}}}
	beta := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "wiki", Namespace: "p-wiki", Labels: map[string]string{shpyrdv1.LabelWorkspace: "beta"}}}
	base, c := newTestReconciler(t, acme, beta)
	r := &WorkspaceReconciler{Client: c, Scheme: base.Scheme, Store: st}
	memory := func(ns string) string {
		t.Helper()
		q := &corev1.ResourceQuota{}
		if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: QuotaName}, q); err != nil {
			return "none"
		}
		m := q.Spec.Hard[corev1.ResourceRequestsMemory]
		return m.String()
	}
	sync := func() {
		t.Helper()
		all, _ := st.ListWorkspaces(ctx)
		if err := r.syncQuotas(ctx, all); err != nil {
			t.Fatal(err)
		}
	}
	sync()
	if memory("p-shop") != "256Mi" || memory("p-wiki") != "none" {
		t.Fatalf("quotas = %s %s", memory("p-shop"), memory("p-wiki"))
	}
	// acme's ceilings are raised and beta gets some: both follow.
	if _, err := st.UpdateWorkspaceSettings(ctx, "acme", store.WorkspaceSettings{Limits: &store.Limits{Memory: "512Mi"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateWorkspaceSettings(ctx, "beta", store.WorkspaceSettings{Limits: &store.Limits{Memory: "2Gi"}}); err != nil {
		t.Fatal(err)
	}
	sync()
	if memory("p-shop") != "512Mi" || memory("p-wiki") != "2Gi" {
		t.Errorf("after the change = %s %s", memory("p-shop"), memory("p-wiki"))
	}
}
