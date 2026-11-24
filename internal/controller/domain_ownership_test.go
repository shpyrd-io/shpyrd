package controller

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"github.com/shpyrd-io/shpyrd/pkg/tenancy"
)

// The ownership rule at the controller: whatever wrote a host into a
// project's spec.domains, a host that another workspace owns, or that is a
// name of the platform, is not served.

const (
	ownedHost  = "a.b.example.com"      // under a domain alpha verified
	freeHost   = "www.free.example.org" // nobody's
	consoleFor = "console.example.org"
)

var errStoreDown = errors.New("store down")

// downStore makes the host lookups of a store fail while down is set.
type downStore struct {
	store.Store
	down atomic.Bool
}

func (d *downStore) WorkspaceByAddress(ctx context.Context, a string) (*store.Workspace, error) {
	if d.down.Load() {
		return nil, errStoreDown
	}
	return d.Store.WorkspaceByAddress(ctx, a)
}

func (d *downStore) WorkspaceByHost(ctx context.Context, h string) (*store.Workspace, *store.WorkspaceHost, error) {
	if d.down.Load() {
		return nil, nil, errStoreDown
	}
	return d.Store.WorkspaceByHost(ctx, h)
}

// ownershipWorld is alpha, with example.com verified, and beta, which owns
// nothing yet; the project under test is beta's.
type ownershipWorld struct {
	st     *downStore
	owners *tenancy.Owners
	ids    map[string]string
}

func newOwnershipWorld(t *testing.T) *ownershipWorld {
	t.Helper()
	ctx := context.Background()
	mem := store.NewMemory()
	w := &ownershipWorld{st: &downStore{Store: mem}, ids: map[string]string{}}
	for _, slug := range []string{"alpha", "beta"} {
		ws, err := mem.CreateWorkspace(ctx, store.Workspace{Slug: slug, Name: slug, Address: slug + ".ws.example.org"})
		if err != nil {
			t.Fatal(err)
		}
		w.ids[slug] = ws.ID
	}
	w.verify(t, "alpha", "example.com")
	w.owners = &tenancy.Owners{
		Store:       w.st,
		Layout:      tenancy.Layout{WorkspacesDomain: "ws.example.org", AppsDomain: "apps.example.org"},
		TTL:         time.Nanosecond, // a change is seen by the next reconcile
		ConsoleHost: consoleFor,
		Domain:      "example.test",
		Reserved:    []string{"auth.example.test"},
	}
	return w
}

func (w *ownershipWorld) verify(t *testing.T, slug, host string) {
	t.Helper()
	now := time.Now()
	if _, err := w.st.PutWorkspaceHost(context.Background(), slug, store.WorkspaceHost{Host: host, Kind: store.HostCustom, VerifiedAt: &now}); err != nil {
		t.Fatal(err)
	}
}

// wire gives the reconciler the rule, as the server does.
func (w *ownershipWorld) wire(r *AppReconciler) {
	r.Config.HostAvailability = func(workspace, host string) tenancy.Availability {
		id := w.ids[workspace]
		if id == "" {
			return tenancy.HostUnknown
		}
		return w.owners.Check(id, host)
	}
}

func betaApp(domains ...string) *shpyrdv1.App {
	app := domainsApp()
	app.Labels = map[string]string{shpyrdv1.LabelWorkspace: "beta"}
	app.Spec.Domains = domains
	return app
}

func ingressHosts(t *testing.T, c client.Client, ns string) map[string][]string {
	t.Helper()
	var list networkingv1.IngressList
	if err := c.List(context.Background(), &list, client.InNamespace(ns)); err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	for _, ing := range list.Items {
		for _, rule := range ing.Spec.Rules {
			out[ing.Name] = append(out[ing.Name], rule.Host)
		}
		for _, tls := range ing.Spec.TLS {
			out[ing.Name] = append(out[ing.Name], tls.Hosts...)
		}
		for k, v := range ing.Annotations {
			if strings.Contains(k, "external-dns") {
				out[ing.Name] = append(out[ing.Name], strings.Split(v, ",")...)
			}
		}
	}
	return out
}

func certificateNames(t *testing.T, c client.Client, ns string) map[string]bool {
	t.Helper()
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{Group: "cert-manager.io", Version: "v1", Kind: "CertificateList"})
	if err := c.List(context.Background(), list, client.InNamespace(ns)); err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, it := range list.Items {
		out[it.GetName()] = true
	}
	return out
}

func mentions(hosts map[string][]string, host string) []string {
	var in []string
	for ing, hs := range hosts {
		for _, h := range hs {
			if h == host {
				in = append(in, ing)
				break
			}
		}
	}
	return in
}

func statusOf(app *shpyrdv1.App, host string) *shpyrdv1.DomainStatus {
	for i := range app.Status.Domains {
		if app.Status.Domains[i].Host == host {
			return &app.Status.Domains[i]
		}
	}
	return nil
}

func reconcileResult(t *testing.T, r *AppReconciler, app *shpyrdv1.App) ctrl.Result {
	t.Helper()
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: app.Namespace, Name: app.Name}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return res
}

// C12: a host another workspace owns gets no rule, no TLS entry and no
// Certificate, in any Ingress; a host nobody owns is served as before; the
// refused one has a status entry that says why, without naming anyone.
func TestDomainOwnershipRefusedHostIsNotServed(t *testing.T) {
	w := newOwnershipWorld(t)
	app := betaApp(ownedHost, freeHost)
	r, c := newTestReconciler(t, app)
	w.wire(r)
	r.Config.ClusterIssuer = "letsencrypt"

	got := runReconcile(t, r, app)

	hosts := ingressHosts(t, c, app.Namespace)
	if in := mentions(hosts, ownedHost); len(in) != 0 {
		t.Errorf("refused host appears in %v", in)
	}
	if in := mentions(hosts, freeHost); len(in) == 0 {
		t.Errorf("free host is not served: %v", hosts)
	}
	certs := certificateNames(t, c, app.Namespace)
	if certs[certificateSecretName(app, ownedHost)] {
		t.Errorf("refused host has a certificate: %v", certs)
	}
	if !certs[certificateSecretName(app, freeHost)] {
		t.Errorf("free host has no certificate: %v", certs)
	}
	st := statusOf(got, ownedHost)
	if st == nil {
		t.Fatalf("no status for the refused host: %+v", got.Status.Domains)
	}
	if st.Certificate != CertFailed || st.DNS != DNSUnknown || !strings.Contains(st.Message, "another workspace") {
		t.Errorf("refused status = %+v", st)
	}
	for _, d := range got.Status.Domains {
		if strings.Contains(d.Message, "alpha") || strings.Contains(d.Message, "beta") {
			t.Errorf("message names a workspace: %q", d.Message)
		}
	}
	if st := statusOf(got, freeHost); st == nil || st.Certificate == CertFailed {
		t.Errorf("free host status = %+v", st)
	}
}

// C13: what a refused host already had goes: its Ingress rule, its
// Certificate and its TLS Secret.
func TestDomainOwnershipRemovesWhatExists(t *testing.T) {
	w := newOwnershipWorld(t)
	app := betaApp(ownedHost, freeHost)
	r, c := newTestReconciler(t, app)
	r.Config.ClusterIssuer = "letsencrypt"

	// Before the rule is known, the host is served.
	runReconcile(t, r, app)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: certificateSecretName(app, ownedHost), Namespace: app.Namespace},
		Type:       corev1.SecretTypeTLS,
	}
	if err := c.Create(context.Background(), secret); err != nil {
		t.Fatal(err)
	}
	if len(mentions(ingressHosts(t, c, app.Namespace), ownedHost)) == 0 || !certificateNames(t, c, app.Namespace)[certificateSecretName(app, ownedHost)] {
		t.Fatal("setup: the host should be served with a certificate")
	}

	w.wire(r)
	runReconcile(t, r, app)

	if in := mentions(ingressHosts(t, c, app.Namespace), ownedHost); len(in) != 0 {
		t.Errorf("rule remains in %v", in)
	}
	if certificateNames(t, c, app.Namespace)[certificateSecretName(app, ownedHost)] {
		t.Error("certificate remains")
	}
	err := c.Get(context.Background(), client.ObjectKeyFromObject(secret), &corev1.Secret{})
	if err == nil {
		t.Error("TLS secret remains")
	}
	if !certificateNames(t, c, app.Namespace)[certificateSecretName(app, freeHost)] {
		t.Error("the free host lost its certificate")
	}
}

// C14: a refused host is never the redirect target, even when redirectTo
// names it and its status once said ready.
func TestDomainOwnershipRefusedHostIsNeverTheRedirectTarget(t *testing.T) {
	w := newOwnershipWorld(t)
	app := betaApp(ownedHost, freeHost)
	app.Spec.RedirectTo = ownedHost
	r, c := newTestReconciler(t, app)
	w.wire(r)
	ready := []shpyrdv1.DomainStatus{{Host: ownedHost, DNS: DNSOK, Certificate: CertReady}}

	if got := r.Config.requestedRedirect(app); got != "" {
		t.Errorf("requestedRedirect = %q", got)
	}
	probe := app.DeepCopy()
	probe.Status.Domains = ready
	if got := r.Config.redirectTarget(probe); got != "" {
		t.Errorf("redirectTarget = %q", got)
	}

	got := runReconcile(t, r, app)
	if strings.Contains(got.Status.URL, ownedHost) {
		t.Errorf("url = %q", got.Status.URL)
	}
	for ing, hs := range ingressHosts(t, c, app.Namespace) {
		for _, h := range hs {
			if h == ownedHost {
				t.Errorf("%s routes %s", ing, h)
			}
		}
		if strings.HasSuffix(ing, "-domain-redirect") {
			t.Errorf("redirect ingress %s exists", ing)
		}
	}

	// A host the project may serve still is one.
	app.Spec.RedirectTo = freeHost
	if got := r.Config.requestedRedirect(app); got != freeHost {
		t.Errorf("requestedRedirect of a free host = %q", got)
	}
}

// C15 (D2): while the lookup fails, a reconcile changes no Ingress rule and
// no Certificate, adds nothing the spec newly asks for, and is retried.
func TestDomainOwnershipLookupFailureChangesNothing(t *testing.T) {
	w := newOwnershipWorld(t)
	app := betaApp(freeHost)
	r, c := newTestReconciler(t, app)
	w.wire(r)
	r.Config.ClusterIssuer = "letsencrypt"

	got := runReconcile(t, r, app)
	hostsBefore := ingressHosts(t, c, app.Namespace)
	certsBefore := certificateNames(t, c, app.Namespace)
	statusBefore := got.Status.Domains
	if len(mentions(hostsBefore, freeHost)) == 0 || !certsBefore[certificateSecretName(app, freeHost)] {
		t.Fatal("setup: the host should be served")
	}

	// The store goes down; the spec swaps the host for another.
	w.st.down.Store(true)
	got.Spec.Domains = []string{"www.other.example.org"}
	if err := c.Update(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	res := reconcileResult(t, r, got)

	hostsAfter := ingressHosts(t, c, app.Namespace)
	for name, before := range hostsBefore {
		if strings.Join(before, ",") != strings.Join(hostsAfter[name], ",") {
			t.Errorf("ingress %s changed: %v -> %v", name, before, hostsAfter[name])
		}
	}
	if len(hostsAfter) != len(hostsBefore) {
		t.Errorf("ingresses changed: %v -> %v", hostsBefore, hostsAfter)
	}
	certsAfter := certificateNames(t, c, app.Namespace)
	if len(certsAfter) != len(certsBefore) {
		t.Errorf("certificates changed: %v -> %v", certsBefore, certsAfter)
	}
	for name := range certsBefore {
		if !certsAfter[name] {
			t.Errorf("certificate %s removed", name)
		}
	}
	if res.RequeueAfter <= 0 || res.RequeueAfter > time.Minute {
		t.Errorf("not retried soon: %+v", res)
	}
	now := &shpyrdv1.App{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(app), now); err != nil {
		t.Fatal(err)
	}
	if len(now.Status.Domains) != len(statusBefore) {
		t.Errorf("status changed: %+v -> %+v", statusBefore, now.Status.Domains)
	}

	// Once the store answers, the spec is applied.
	w.st.down.Store(false)
	runReconcile(t, r, now)
	if len(mentions(ingressHosts(t, c, app.Namespace), "www.other.example.org")) == 0 {
		t.Error("the new host is not served once the lookup works")
	}
	if len(mentions(ingressHosts(t, c, app.Namespace), freeHost)) != 0 {
		t.Error("the old host is still served")
	}
}

// A workspace the store does not know cannot be answered for: the same as
// a failed lookup.
func TestDomainOwnershipUnknownWorkspaceChangesNothing(t *testing.T) {
	w := newOwnershipWorld(t)
	app := betaApp(freeHost)
	app.Labels[shpyrdv1.LabelWorkspace] = "gamma"
	r, c := newTestReconciler(t, app)
	w.wire(r)

	res := reconcileResult(t, r, app)
	if len(mentions(ingressHosts(t, c, app.Namespace), freeHost)) != 0 {
		t.Error("a host was added without an answer")
	}
	if res.RequeueAfter <= 0 || res.RequeueAfter > time.Minute {
		t.Errorf("not retried soon: %+v", res)
	}
}

// C16 (D5): the platform's own names in a spec are refused as another
// workspace's are.
func TestDomainOwnershipPlatformNamesAreRefused(t *testing.T) {
	w := newOwnershipWorld(t)
	names := []string{consoleFor, "auth.example.test", "someone.apps.example.org", "example.test"}
	app := betaApp(append([]string{freeHost}, names...)...)
	r, c := newTestReconciler(t, app)
	w.wire(r)

	got := runReconcile(t, r, app)

	hosts := ingressHosts(t, c, app.Namespace)
	certs := certificateNames(t, c, app.Namespace)
	for _, h := range names {
		if in := mentions(hosts, h); len(in) != 0 {
			t.Errorf("%s appears in %v", h, in)
		}
		if certs[certificateSecretName(app, h)] {
			t.Errorf("%s has a certificate", h)
		}
		st := statusOf(got, h)
		if st == nil || st.Certificate != CertFailed || st.Message == "" {
			t.Errorf("%s status = %+v", h, st)
		}
	}
	if len(mentions(hosts, freeHost)) == 0 {
		t.Error("the free host is not served")
	}
}

// C19 (D3): with no change to the App, a host that another workspace
// verifies afterwards stops being served, and the reconcile asks to be run
// again within ten minutes.
func TestDomainOwnershipIsCheckedAgain(t *testing.T) {
	w := newOwnershipWorld(t)
	app := betaApp("www.late.example.org")
	r, c := newTestReconciler(t, app)
	w.wire(r)

	res := reconcileResult(t, r, app)
	if res.RequeueAfter <= 0 || res.RequeueAfter > 10*time.Minute {
		t.Fatalf("a project with domains must be looked at again within ten minutes: %+v", res)
	}
	if len(mentions(ingressHosts(t, c, app.Namespace), "www.late.example.org")) == 0 {
		t.Fatal("setup: the host should be served")
	}

	w.verify(t, "alpha", "late.example.org")
	time.Sleep(time.Millisecond) // past the lookup's memory
	res = reconcileResult(t, r, app)
	if in := mentions(ingressHosts(t, c, app.Namespace), "www.late.example.org"); len(in) != 0 {
		t.Errorf("the host is still served in %v", in)
	}
	if res.RequeueAfter <= 0 || res.RequeueAfter > 10*time.Minute {
		t.Errorf("not looked at again: %+v", res)
	}

	// A project without domains needs no such visit.
	plain := betaApp()
	plain.Name, plain.Namespace = "plain", "app-plain"
	r2, _ := newTestReconciler(t, plain)
	w.wire(r2)
	if res := reconcileResult(t, r2, plain); res.RequeueAfter == domainOwnershipRecheck {
		t.Errorf("a project without domains is requeued: %+v", res)
	}
}

// The maintenance page is not a way round the rule: with the lookup
// failing, the Ingress keeps its hosts and answers 503 for them.
func TestDomainOwnershipMaintenanceKeepsHostsWhileUnknown(t *testing.T) {
	w := newOwnershipWorld(t)
	app := betaApp(freeHost)
	r, c := newTestReconciler(t, app)
	w.wire(r)
	runReconcile(t, r, app)
	before := ingressHosts(t, c, app.Namespace)

	got := &shpyrdv1.App{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(app), got); err != nil {
		t.Fatal(err)
	}
	got.Annotations = map[string]string{shpyrdv1.AnnotationMaintenance: "preparing"}
	got.Spec.Domains = []string{"www.other.example.org"}
	if err := c.Update(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	w.st.down.Store(true)
	reconcileResult(t, r, got)

	after := ingressHosts(t, c, app.Namespace)
	if strings.Join(before[ingressName(app)], ",") != strings.Join(after[ingressName(app)], ",") {
		t.Errorf("hosts changed: %v -> %v", before, after)
	}
}

// Whatever else a reconcile asks for, an App with domains is asked for again
// within ten minutes, and a sooner request is kept.
func TestDomainOwnershipRecheckBound(t *testing.T) {
	withDomains, without := betaApp(freeHost), betaApp()
	on := Config{HostAvailability: func(string, string) tenancy.Availability { return tenancy.HostFree }}
	for _, c := range []struct {
		cfg  Config
		app  *shpyrdv1.App
		in   time.Duration
		want time.Duration
	}{
		{on, withDomains, 0, domainOwnershipRecheck},
		{on, withDomains, time.Hour, domainOwnershipRecheck},
		{on, withDomains, 15 * time.Second, 15 * time.Second},
		{on, without, 0, 0},
		{Config{}, withDomains, 0, 0},
	} {
		got := c.cfg.withOwnershipRecheck(c.app, c.in)
		if got != c.want {
			t.Errorf("wait %v: got %v, want %v", c.in, got, c.want)
		}
		if c.want == domainOwnershipRecheck && got > 10*time.Minute {
			t.Errorf("recheck %v is beyond ten minutes", got)
		}
	}
}

// settleApp brings an app to rest: its deployment is ready, the DNS of each
// host is right and each host's certificate is issued, so that nothing but
// the ownership recheck asks for another reconcile.
func settleApp(t *testing.T, w *ownershipWorld, app *shpyrdv1.App) (*AppReconciler, ctrl.Result) {
	t.Helper()
	r, c := newTestReconciler(t, app)
	w.wire(r)
	r.Config.ClusterIssuer = "letsencrypt"
	r.Config.ExternalLBAddress = "147.15.59.84"
	cname := map[string]string{}
	for _, d := range app.Spec.Domains {
		cname[d] = "shop.example.test"
	}
	r.Resolver = fakeResolver{cname: cname, a: map[string][]string{"shop.example.test": {"147.15.59.84"}}}
	runReconcile(t, r, app)
	runReconcile(t, r, app)
	markDeploymentReady(t, c, app.Namespace, app.Name+"-web", 1)
	for _, d := range app.Spec.Domains {
		cert := &unstructured.Unstructured{}
		cert.SetGroupVersionKind(CertificateGVK)
		key := client.ObjectKey{Namespace: app.Namespace, Name: certificateSecretName(app, d)}
		if err := c.Get(context.Background(), key, cert); err != nil {
			t.Fatalf("certificate of %s: %v; have %v", d, err, certificateNames(t, c, app.Namespace))
		}
		cert.Object["status"] = map[string]interface{}{"conditions": []interface{}{
			map[string]interface{}{"type": "Ready", "status": "True"},
		}}
		if err := c.Update(context.Background(), cert); err != nil {
			t.Fatal(err)
		}
	}
	return r, reconcileResult(t, r, app)
}

// C19 at Reconcile, on an app that is settled, so that no other wait hides
// the recheck: with domains it is asked for again within ten minutes, and
// without domains it is not asked for at all.
func TestDomainOwnershipSettledAppIsCheckedAgain(t *testing.T) {
	w := newOwnershipWorld(t)
	withDomains := betaApp(freeHost)
	_, res := settleApp(t, w, withDomains)
	if res.RequeueAfter <= 0 || res.RequeueAfter > 10*time.Minute {
		t.Errorf("a settled project with domains must be looked at again within ten minutes: %+v", res)
	}

	plain := betaApp()
	_, res = settleApp(t, w, plain)
	if res.RequeueAfter != 0 {
		t.Errorf("a settled project without domains is requeued: %+v", res)
	}
}

// C15 (D2) on an app that is settled, so that no other wait can stand in for
// the retry: with the lookup failing the routing stays as it was and the
// reconcile is asked for again within 30 s, which the ownership recheck
// alone would not do.
func TestDomainOwnershipSettledLookupFailureIsRetried(t *testing.T) {
	w := newOwnershipWorld(t)
	app := betaApp(freeHost)
	r, res := settleApp(t, w, app)
	if res.RequeueAfter <= 30*time.Second {
		t.Fatalf("setup: a settled app should wait for the recheck, got %+v", res)
	}
	hostsBefore := ingressHosts(t, r.Client, app.Namespace)
	certsBefore := certificateNames(t, r.Client, app.Namespace)

	w.st.down.Store(true)
	time.Sleep(time.Millisecond) // past the lookup's memory
	res = reconcileResult(t, r, app)

	if res.RequeueAfter <= 0 || res.RequeueAfter > 30*time.Second {
		t.Errorf("a failed lookup on a settled app is not retried soon: %+v", res)
	}
	hostsAfter := ingressHosts(t, r.Client, app.Namespace)
	for name, before := range hostsBefore {
		if strings.Join(before, ",") != strings.Join(hostsAfter[name], ",") {
			t.Errorf("ingress %s changed: %v -> %v", name, before, hostsAfter[name])
		}
	}
	if len(hostsAfter) != len(hostsBefore) {
		t.Errorf("ingresses changed: %v -> %v", hostsBefore, hostsAfter)
	}
	certsAfter := certificateNames(t, r.Client, app.Namespace)
	if len(certsAfter) != len(certsBefore) {
		t.Errorf("certificates changed: %v -> %v", certsBefore, certsAfter)
	}
	for name := range certsBefore {
		if !certsAfter[name] {
			t.Errorf("certificate %s removed", name)
		}
	}

	// Once the store answers, the app is back to the ordinary recheck.
	w.st.down.Store(false)
	time.Sleep(time.Millisecond)
	if res = reconcileResult(t, r, app); res.RequeueAfter <= 30*time.Second {
		t.Errorf("after the lookup works the app should wait for the recheck: %+v", res)
	}
}

// The same retry when the store does not know the app's workspace.
func TestDomainOwnershipSettledUnknownWorkspaceIsRetried(t *testing.T) {
	w := newOwnershipWorld(t)
	app := betaApp(freeHost)
	r, _ := settleApp(t, w, app)
	hostsBefore := ingressHosts(t, r.Client, app.Namespace)
	certsBefore := certificateNames(t, r.Client, app.Namespace)

	delete(w.ids, "beta") // the store no longer answers for the workspace
	res := reconcileResult(t, r, app)

	if res.RequeueAfter <= 0 || res.RequeueAfter > 30*time.Second {
		t.Errorf("an unknown workspace on a settled app is not retried soon: %+v", res)
	}
	hostsAfter := ingressHosts(t, r.Client, app.Namespace)
	for name, before := range hostsBefore {
		if strings.Join(before, ",") != strings.Join(hostsAfter[name], ",") {
			t.Errorf("ingress %s changed: %v -> %v", name, before, hostsAfter[name])
		}
	}
	if len(hostsAfter) != len(hostsBefore) {
		t.Errorf("ingresses changed: %v -> %v", hostsBefore, hostsAfter)
	}
	certsAfter := certificateNames(t, r.Client, app.Namespace)
	if len(certsAfter) != len(certsBefore) {
		t.Errorf("certificates changed: %v -> %v", certsBefore, certsAfter)
	}
	for name := range certsBefore {
		if !certsAfter[name] {
			t.Errorf("certificate %s removed", name)
		}
	}
}
