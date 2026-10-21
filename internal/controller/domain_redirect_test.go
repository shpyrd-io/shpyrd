package controller

import (
	"regexp"
	"strings"
	"testing"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/tenancy"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestDomainRedirectRoutes(t *testing.T) {
	for _, access := range []string{shpyrdv1.AccessPublic, shpyrdv1.AccessAuthenticated} {
		for _, internal := range []bool{false, true} {
			t.Run(access+string(rune('0'+boolInt(internal))), func(t *testing.T) {
				app := domainsApp()
				app.Spec.Domains = []string{"www.myprod.com"}
				before := configHash(app, nil, nil, nil, nil)
				app.Spec.RedirectTo = "www.myprod.com"
				if configHash(app, nil, nil, nil, nil) != before {
					t.Fatal("redirect triggers a release")
				}
				app.Spec.Access = access
				if internal {
					app.Spec.Exposure = "internal"
				}
				app.Status.Domains = []shpyrdv1.DomainStatus{{Host: app.Spec.RedirectTo, DNS: DNSOK, Certificate: CertReady}}
				r, _ := newTestReconciler(t, app)
				r.Config.IngressClassInternal = "private"
				r.Config.InternalLBAddress = "10.0.0.1"
				r.Config.HTTPSPort = "8443"
				ing := &networkingv1.Ingress{}
				r.Config.mutateIngress(app, ing, true)
				if len(ing.Spec.Rules) != 1 || ing.Spec.Rules[0].Host != "www.myprod.com" || ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name != webSleepServiceName {
					t.Fatalf("app ingress: %+v", ing.Spec)
				}
				edge := &networkingv1.Ingress{}
				r.Config.mutateEdgeIngress(app, edge)
				if len(edge.Spec.Rules) != 2 || edge.Spec.Rules[0].HTTP.Paths[0].Path != "/.shpyrd/" {
					t.Fatalf("callbacks lost: %+v", edge.Spec)
				}
				if err := r.reconcileDomainRedirect(t.Context(), app, true); err != nil {
					t.Fatal(err)
				}
				redirect := &networkingv1.Ingress{}
				key := client.ObjectKey{Namespace: app.Namespace, Name: ingressName(app) + "-domain-redirect"}
				if err := r.Get(t.Context(), key, redirect); err != nil {
					t.Fatal(err)
				}
				if redirect.Spec.Rules[0].Host != "shop.example.test" || redirect.Annotations[AnnotationRedirectTo] != "https://www.myprod.com:8443" {
					t.Fatalf("redirect: %+v", redirect)
				}
				answeredByTheServer(t, r, redirect)
				if redirect.Annotations["nginx.ingress.kubernetes.io/auth-url"] != "" {
					t.Fatal("redirect requires authentication before reaching destination")
				}
				if internal && (*redirect.Spec.IngressClassName != "private" || redirect.Annotations["external-dns.kubernetes.io/target"] != "10.0.0.1") {
					t.Fatal("internal redirect became public")
				}
				if got := r.Config.url(app); got != "https://www.myprod.com:8443" {
					t.Fatal(got)
				}
				for _, change := range []string{"dns", "certificate", "removed", "disabled", "maintenance"} {
					copy := app.DeepCopy()
					switch change {
					case "dns":
						copy.Status.Domains[0].DNS = DNSWrong
					case "certificate":
						copy.Status.Domains[0].Certificate = CertIssuing
					case "removed":
						copy.Spec.Domains = nil
					case "disabled":
						copy.Spec.RedirectTo = ""
					case "maintenance":
						copy.Annotations = map[string]string{shpyrdv1.AnnotationMaintenance: "preparing"}
					}
					if got := r.Config.redirectTarget(copy); got != "" {
						t.Fatalf("%s still redirects: %s", change, got)
					}
				}
				app.Spec.RedirectTo = ""
				if err := r.reconcileDomainRedirect(t.Context(), app, true); err != nil {
					t.Fatal(err)
				}
				if err := r.Get(t.Context(), key, redirect); !apierrors.IsNotFound(err) {
					t.Fatalf("redirect not removed: %v", err)
				}
				r.Config.mutateIngress(app, ing, false)
				if ing.Spec.Rules[0].Host != "shop.example.test" {
					t.Fatal("platform host not restored")
				}
			})
		}
	}
}

// ingress-nginx's admission webhook allows rewrite-target only these
// characters (parser.RegexPathWithCapture, controller v1.15); its
// permanent-redirect refuses the $request_uri a redirect needs (#133).
var nginxRewriteTarget = regexp.MustCompile(`^/?[\-\._~a-zA-Z0-9/:$]*$`)

// answeredByTheServer: a redirect Ingress sends every path to the server's
// redirect, through the server's alias in the namespace, and carries no
// annotation the webhook refuses.
func answeredByTheServer(t *testing.T, r *AppReconciler, ing *networkingv1.Ingress) {
	t.Helper()
	if _, ok := ing.Annotations[nginxPermanentRedirect]; ok {
		t.Fatalf("nginx redirects: %v", ing.Annotations)
	}
	if got := ing.Annotations["nginx.ingress.kubernetes.io/rewrite-target"]; got != RedirectPath+"/$1" || !nginxRewriteTarget.MatchString(got) || ing.Annotations["nginx.ingress.kubernetes.io/use-regex"] != "true" {
		t.Fatalf("rewrite-target %q", got)
	}
	if ing.Labels[LabelRedirect] != "true" {
		t.Fatalf("not found by the server: %v", ing.Labels)
	}
	for _, rule := range ing.Spec.Rules {
		for _, p := range rule.HTTP.Paths {
			if p.Path != "/(.*)" || *p.PathType != networkingv1.PathTypeImplementationSpecific || p.Backend.Service.Name != redirectServiceName {
				t.Fatalf("path %s goes to %s", p.Path, p.Backend.Service.Name)
			}
		}
	}
	svc := &corev1.Service{}
	if err := r.Get(t.Context(), client.ObjectKey{Namespace: ing.Namespace, Name: redirectServiceName}, svc); err != nil || svc.Spec.ExternalName == "" {
		t.Fatalf("the server's alias: %v %+v", err, svc.Spec)
	}
}

func TestAMovedHostIsRedirectedByTheServerAndLosesNginxsRedirect(t *testing.T) {
	app := domainsApp()
	r, _ := newTestReconciler(t, app)
	// One written by an earlier version, which the webhook now refuses.
	old := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: redirectIngressName(app, "old.example.test"), Namespace: app.Namespace,
		Annotations: map[string]string{nginxPermanentRedirect: "https://shop.example.test$request_uri"}}}
	if err := r.Create(t.Context(), old); err != nil {
		t.Fatal(err)
	}
	if err := r.ensureRedirectIngress(t.Context(), app, "old.example.test"); err != nil {
		t.Fatal(err)
	}
	ing := &networkingv1.Ingress{}
	if err := r.Get(t.Context(), client.ObjectKeyFromObject(old), ing); err != nil {
		t.Fatal(err)
	}
	if ing.Spec.Rules[0].Host != "old.example.test" || ing.Annotations[AnnotationRedirectTo] != "https://"+r.Config.defaultHost(app) {
		t.Fatalf("moved: %+v %v", ing.Spec.Rules, ing.Annotations)
	}
	answeredByTheServer(t, r, ing)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func TestWorkspaceDomainRedirectWaitsForEachCertificate(t *testing.T) {
	app := domainsApp()
	app.Spec.Domains = nil
	app.Labels = map[string]string{shpyrdv1.LabelWorkspace: "acme"}
	r, _ := newTestReconciler(t, app)
	r.Config.Layout = tenancy.Layout{AppsDomain: "shpyrd.app", WorkspacesDomain: "shpyrd.test"}
	r.Config.WorkspaceAddress = func(string) string { return "acme.shpyrd.test" }
	r.Config.WorkspaceAppHosts = func(string, string) []string { return []string{"shop.acme.com", "acme-shop.shpyrd.app"} }
	r.Config.WorkspaceRedirectDomain = func(string) string { return "acme.com" }
	r.Resolver = fakeResolver{cname: map[string]string{"shop.acme.com": "acme-shop.shpyrd.app"}}
	if err := r.reconcileCertificates(t.Context(), app); err != nil {
		t.Fatal(err)
	}
	app.Status.Domains, _ = r.domainStatuses(t.Context(), app)
	if got := r.Config.redirectTarget(app); got != "" {
		t.Fatal("redirect before certificate ready", got)
	}
	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(CertificateGVK)
	key := client.ObjectKey{Namespace: app.Namespace, Name: certificateSecretName(app, "shop.acme.com")}
	if err := r.Get(t.Context(), key, cert); err != nil {
		t.Fatal(err)
	}
	_ = unstructured.SetNestedSlice(cert.Object, []interface{}{map[string]interface{}{"type": "Ready", "status": "True"}}, "status", "conditions")
	if err := r.Update(t.Context(), cert); err != nil {
		t.Fatal(err)
	}
	app.Status.Domains, _ = r.domainStatuses(t.Context(), app)
	if got := r.Config.redirectTarget(app); got != "shop.acme.com" {
		t.Fatalf("target=%s domains=%+v", got, app.Status.Domains)
	}
	if r.Config.platformHost(app) != "acme-shop.shpyrd.app" {
		t.Fatal("lost shared DNS target")
	}
	r.Config.WorkspaceRedirectDomain = func(string) string { return "" }
	if r.Config.redirectTarget(app) != "" {
		t.Fatal("workspace disable ignored")
	}
}

func TestRedirectRefreshesReadinessBeforeRouting(t *testing.T) {
	app := domainsApp()
	app.Spec.Domains = []string{"www.myprod.com"}
	app.Spec.RedirectTo = "www.myprod.com"
	app.Status.Domains = []shpyrdv1.DomainStatus{{Host: app.Spec.RedirectTo, DNS: DNSOK, Certificate: CertReady}}
	r, _ := newTestReconciler(t, app)
	r.Resolver = fakeResolver{}
	got := runReconcile(t, r, app)
	ing := &networkingv1.Ingress{}
	if err := r.Get(t.Context(), client.ObjectKey{Namespace: app.Namespace, Name: ingressName(app)}, ing); err != nil {
		t.Fatal(err)
	}
	if ing.Spec.Rules[0].Host != "shop.example.test" || strings.Contains(got.Status.URL, "myprod.com") {
		t.Fatal("stale readiness redirected traffic")
	}
	// A directly authored CR cannot inject an nginx variable or annotation.
	app.Spec.RedirectTo = "evil.test$request_uri"
	app.Spec.Domains = []string{app.Spec.RedirectTo}
	app.Status.Domains = []shpyrdv1.DomainStatus{{Host: app.Spec.RedirectTo, DNS: DNSOK, Certificate: CertReady}}
	if r.Config.redirectTarget(app) != "" {
		t.Fatal("unsafe hostname accepted")
	}
}

func TestRedirectDNSNeedsEvidenceOfTheFrontDoor(t *testing.T) {
	r, _ := newTestReconciler(t)
	r.Resolver = fakeResolver{a: map[string][]string{"custom.example.com": {"203.0.113.9"}}}
	if got := r.dnsState(t.Context(), "custom.example.com", "shop.example.test", ""); got != DNSUnknown {
		t.Fatal(got)
	}
	r.Resolver = fakeResolver{a: map[string][]string{"custom.example.com": {"203.0.113.9"}, "shop.example.test": {"203.0.113.8"}}}
	if got := r.dnsState(t.Context(), "custom.example.com", "shop.example.test", ""); got != DNSWrong {
		t.Fatal(got)
	}
	r.Resolver = fakeResolver{a: map[string][]string{"custom.example.com": {"203.0.113.8"}, "shop.example.test": {"203.0.113.8"}}}
	if got := r.dnsState(t.Context(), "custom.example.com", "shop.example.test", ""); got != DNSOK {
		t.Fatal(got)
	}
}
