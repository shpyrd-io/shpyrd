package controller

import (
	"context"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"testing"
)

func TestPublicAPILifecycle(t *testing.T) {
	base, c := newTestReconciler(t)
	r := &WorkspaceReconciler{Client: c, Scheme: base.Scheme, Config: Config{SystemNamespace: "shpyrd-system", PublicAPIEnabled: true, PublicAPIHost: "api.example.test", PublicAPIIssuer: "letsencrypt", IngressClassExternal: "nginx"}}
	ctx := context.Background()
	if err := r.ensurePublicAPI(ctx); err != nil {
		t.Fatal(err)
	}
	key := types.NamespacedName{Namespace: "shpyrd-system", Name: "shpyrd-public-api"}
	ing := &networkingv1.Ingress{}
	if err := c.Get(ctx, key, ing); err != nil {
		t.Fatal(err)
	}
	if ing.Spec.Rules[0].Host != "api.example.test" || ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name != "shpyrd-public-api" || *ing.Spec.IngressClassName != "nginx" {
		t.Fatalf("wrong ingress: %+v", ing.Spec)
	}
	r.Config.PublicAPIHost = "api.other.test"
	if err := r.ensurePublicAPI(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, key, ing); err != nil || ing.Spec.TLS[0].Hosts[0] != "api.other.test" {
		t.Fatal("domain change failed", err)
	}
	r.Config.PublicAPIEnabled = false
	if err := r.ensurePublicAPI(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, key, ing); !apierrors.IsNotFound(err) {
		t.Fatal("disabled ingress remains", err)
	}
}
