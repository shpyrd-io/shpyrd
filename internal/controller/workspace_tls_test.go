package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/project"
)

func TestWorkspaceTLS(t *testing.T) {
	ctx := context.Background()
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-acme-shop", Labels: project.NamespaceLabels("acme", "shop")}, Spec: shpyrdv1.AppSpec{Domains: []string{"www.acme.com"}}}
	free := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "blog", Namespace: "app-blog"}}
	src := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "workspace-acme-tls", Namespace: "shpyrd-system"}, Type: corev1.SecretTypeTLS, Data: map[string][]byte{"tls.crt": []byte("CERT"), "tls.key": []byte("KEY")}}
	r, c := newTestReconciler(t, app, free, src)
	r.Config.SystemNamespace = "shpyrd-system"
	r.Config.Domain = "example.test"
	r.Config.WildcardTLS = true
	r.Config.WorkspaceDomain = func(slug string) string {
		if slug == "acme" {
			return "acme.shpyrd.test"
		}
		return ""
	}

	// The wildcard is copied into the workspace's project namespace.
	ok, err := r.reconcileWorkspaceTLS(ctx, app)
	if err != nil || !ok {
		t.Fatalf("copy: %v %v", ok, err)
	}
	cp := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-acme-shop", Name: WorkspaceTLSSecretName}, cp); err != nil || string(cp.Data["tls.crt"]) != "CERT" || cp.Type != corev1.SecretTypeTLS {
		t.Fatalf("copy = %+v %v", cp, err)
	}
	// The Ingress uses it for the default host; the custom domain keeps its own certificate.
	tls := r.Config.ingressTLS(app)
	got := map[string]string{}
	for _, t := range tls {
		got[t.Hosts[0]] = t.SecretName
	}
	if got["shop.acme.shpyrd.test"] != WorkspaceTLSSecretName {
		t.Errorf("default host tls = %q, want the workspace wildcard", got["shop.acme.shpyrd.test"])
	}
	if got["www.acme.com"] == WorkspaceTLSSecretName || got["www.acme.com"] == "" {
		t.Errorf("custom domain tls = %q, want its own certificate", got["www.acme.com"])
	}
	// The implicit workspace gets no copy; a stale one is removed.
	if ok, err := r.reconcileWorkspaceTLS(ctx, free); err != nil || ok {
		t.Errorf("implicit workspace copy = %v %v", ok, err)
	}
	// A renewal of the source maps to the workspace's apps.
	reqs := r.workspaceTLSToApps(ctx, src)
	if len(reqs) != 1 || reqs[0].Name != "shop" {
		t.Errorf("renewal requeues %v", reqs)
	}
	if reqs := r.workspaceTLSToApps(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "shpyrd-system"}}); len(reqs) != 0 {
		t.Errorf("unrelated secret requeues %v", reqs)
	}
}
