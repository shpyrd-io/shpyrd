package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/shpyrd-io/shpyrd/internal/controller"
)

func TestARedirectIngressSendsTheVisitorOnWithTheirPath(t *testing.T) {
	redirect := func(name, host, to string, labelled bool) *networkingv1.Ingress {
		ing := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "app-shop",
			Annotations: map[string]string{controller.AnnotationRedirectTo: to}},
			Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{Host: host}}}}
		if labelled {
			ing.Labels = map[string]string{controller.LabelRedirect: "true"}
		}
		return ing
	}
	s, _ := newTestServer(t, nil, []client.Object{
		redirect("shop-domain-redirect", "acme-shop.shpyrd.app", "https://www.acme.com", true),
		redirect("shop-moved-1", "old.shpyrd.app", "https://www.acme.com:8443", true),
		// Not the platform's: an annotation alone sends no one anywhere.
		redirect("someone-else", "mine.shpyrd.app", "https://evil.test", false),
	})
	// What nginx asks after its rewrite: the visitor's path after
	// RedirectPath, their query as it was.
	get := func(host, uri string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, controller.RedirectPath+uri, nil)
		req.Host = host
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}
	for _, c := range []struct{ host, uri, want string }{
		{"acme-shop.shpyrd.app", "/orders/7?tab=items", "https://www.acme.com/orders/7?tab=items"},
		{"Acme-Shop.shpyrd.app:8443", "/", "https://www.acme.com/"},
		{"old.shpyrd.app", "", "https://www.acme.com:8443/"},
		{"old.shpyrd.app", "/a%20b/caf%C3%A9", "https://www.acme.com:8443/a%20b/caf%C3%A9"},
		// The host stays the platform's choice, whatever the path.
		{"old.shpyrd.app", "//evil.test/x", "https://www.acme.com:8443//evil.test/x"},
		{"old.shpyrd.app", "/@evil.test", "https://www.acme.com:8443/@evil.test"},
	} {
		rec := get(c.host, c.uri)
		if rec.Code != http.StatusPermanentRedirect || rec.Header().Get("Location") != c.want {
			t.Errorf("%s %s: %d %q, want %q", c.host, c.uri, rec.Code, rec.Header().Get("Location"), c.want)
		}
	}
	for _, host := range []string{"mine.shpyrd.app", "example.test", "nowhere.test"} {
		if rec := get(host, "/x"); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d %q", host, rec.Code, rec.Header().Get("Location"))
		}
	}
}
