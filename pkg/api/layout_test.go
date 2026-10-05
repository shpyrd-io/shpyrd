package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	crfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"github.com/shpyrd-io/shpyrd/pkg/tenancy"
)

// newLayoutServer is the cloud's shape (RFC-0033 names, amended
// 2026-10-04): workspaces at <label>.shpyrd.cloud, apps at
// <label>-<app>.shpyrd.app. acme moved there from acme.shpyrd.app;
// older has not moved yet.
func newLayoutServer(t *testing.T) (*Server, store.Store) {
	t.Helper()
	ctx := context.Background()
	st := store.NewMemory()
	if _, err := st.UpdateWorkspaceAddress(ctx, store.DefaultWorkspace, "platform.shpyrd.cloud"); err != nil {
		t.Fatal(err)
	}
	for _, w := range []store.Workspace{
		{Slug: "acme", Name: "Acme", Address: "acme.shpyrd.cloud"},
		{Slug: "older", Name: "Older", Address: "older.shpyrd.app"},
	} {
		if _, err := st.CreateWorkspace(ctx, w); err != nil {
			t.Fatal(err)
		}
	}
	exp := time.Now().Add(24 * time.Hour)
	if _, err := st.PutWorkspaceHost(ctx, "acme", store.WorkspaceHost{Host: "acme.shpyrd.app", Kind: store.HostMoved, ExpiresAt: &exp}); err != nil {
		t.Fatal(err)
	}
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	shop := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-acme-shop", Labels: project.NamespaceLabels("acme", "shop")},
		Spec:       shpyrdv1.AppSpec{Access: shpyrdv1.AccessAuthenticated, Image: "ghcr.io/acme/shop:1"},
	}
	daily := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "lessons", Namespace: "app-older-lessons", Labels: project.NamespaceLabels("older", "lessons")},
		Spec:       shpyrdv1.AppSpec{Access: shpyrdv1.AccessAuthenticated, Image: "ghcr.io/older/lessons:1"},
	}
	cr := crfake.NewClientBuilder().WithScheme(scheme).WithObjects(shop, daily).WithStatusSubresource(&shpyrdv1.App{}).Build()
	k := &kube.Client{Kube: kubefake.NewSimpleClientset(), Namespace: "shpyrd-system"}
	layout := tenancy.Layout{WorkspacesDomain: "shpyrd.cloud", AppsDomain: "shpyrd.app"}
	public := PublicConfig{Domain: "operator.shpyrd.io", DashboardURL: "https://operator.shpyrd.io"}
	s, err := newServer(k, Options{
		Token: testToken, Apps: cr, Store: st, Public: public, Layout: layout,
		Tenancy:      &tenancy.ByAddress{Store: st, Domain: public.Domain, ConsoleHost: "operator.shpyrd.io", Layout: layout},
		Capabilities: []string{"workspaces"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.authz.TTL = 1
	return s, st
}

func TestLayoutAppHosts(t *testing.T) {
	s, _ := newLayoutServer(t)

	// An app's host is <label>-<app>: the edge finds it and sends sign-in
	// to its workspace's host on shpyrd.cloud.
	rec := at(t, s, "acme-shop.shpyrd.app", "GET", "/.shpyrd/signin?rd=/orders", "")
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "https://acme.shpyrd.cloud/.shpyrd/start?") || !strings.Contains(rec.Header().Get("Location"), "app=acme-shop.shpyrd.app") {
		t.Errorf("sign-in at the app host: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	for _, host := range []string{"acme-nope.shpyrd.app", "nobody-shop.shpyrd.app"} {
		if rec := at(t, s, host, "GET", "/.shpyrd/signin?rd=/", ""); rec.Code == http.StatusFound {
			t.Errorf("%s: no such app, got %d %s", host, rec.Code, rec.Header().Get("Location"))
		}
	}
	// A workspace not moved yet keeps its apps one label under its address.
	rec = at(t, s, "lessons.older.shpyrd.app", "GET", "/.shpyrd/signin?rd=/", "")
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "https://older.shpyrd.app/.shpyrd/start?") {
		t.Errorf("sign-in at an app not moved: %d %s", rec.Code, rec.Header().Get("Location"))
	}

	// The dashboard writes app addresses from the pattern it is given.
	if rec := at(t, s, "acme.shpyrd.cloud", "GET", "/api/config", ""); !strings.Contains(rec.Body.String(), `"appHost":{"prefix":"acme-","suffix":".shpyrd.app"}`) {
		t.Errorf("config at acme: %s", rec.Body.String())
	}
	if rec := at(t, s, "older.shpyrd.app", "GET", "/api/config", ""); !strings.Contains(rec.Body.String(), `"appHost":{"prefix":"","suffix":".older.shpyrd.app"}`) {
		t.Errorf("config at older: %s", rec.Body.String())
	}
}

func TestLayoutOldNamesRedirect(t *testing.T) {
	s, st := newLayoutServer(t)
	// The address from before the layout: pages and apps redirect, a
	// change keeps its method, programs are served where they are.
	for _, c := range []struct{ method, host, path, location string }{
		{"GET", "acme.shpyrd.app", "/projects", "https://acme.shpyrd.cloud/projects"},
		{"GET", "shop.acme.shpyrd.app", "/orders?x=1", "https://acme-shop.shpyrd.app/orders?x=1"},
		{"POST", "shop.acme.shpyrd.app", "/hooks", "https://acme-shop.shpyrd.app/hooks"},
	} {
		rec := at(t, s, c.host, c.method, c.path, "")
		want := http.StatusMovedPermanently
		if c.method == "POST" {
			want = http.StatusPermanentRedirect
		}
		if rec.Code != want || rec.Header().Get("Location") != c.location {
			t.Errorf("%s %s%s = %d %s, want %d %s", c.method, c.host, c.path, rec.Code, rec.Header().Get("Location"), want, c.location)
		}
	}
	if rec := at(t, s, "acme.shpyrd.app", "GET", "/api/config", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"dashboardUrl":"https://acme.shpyrd.cloud"`) {
		t.Errorf("the API at the old address: %d %s", rec.Code, rec.Body.String())
	}

	// A new name inside the layout: no hyphen; the old label redirects,
	// its apps too, and nobody else may take it.
	if rec := at(t, s, "acme.shpyrd.cloud", "PATCH", "/api/workspace", `{"address":"acme-co"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a hyphen in the label: %d %s", rec.Code, rec.Body.String())
	}
	if rec := at(t, s, "acme.shpyrd.cloud", "PATCH", "/api/workspace", `{"address":"acmeco"}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"address":"acmeco.shpyrd.cloud"`) {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body.String())
	}
	if rec := at(t, s, "acme-shop.shpyrd.app", "GET", "/orders", ""); rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "https://acmeco-shop.shpyrd.app/orders" {
		t.Errorf("old label's app: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if rec := at(t, s, "acmeco-shop.shpyrd.app", "GET", "/.shpyrd/signin?rd=/", ""); rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "https://acmeco.shpyrd.cloud/.shpyrd/start?") {
		t.Errorf("new label's app: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if _, err := st.CreateWorkspace(context.Background(), store.Workspace{Slug: "squat", Name: "S", Address: "acme.shpyrd.cloud"}); err == nil {
		t.Error("the old label was given to another workspace")
	}
}

func TestLayoutRefusesPlatformNamesAsCustomDomains(t *testing.T) {
	s, _ := newLayoutServer(t)
	for _, host := range []string{"foo.shpyrd.app", "shpyrd.app", "x.shpyrd.cloud"} {
		if rec := at(t, s, "acme.shpyrd.cloud", "POST", "/api/workspace/domains", `{"host":"`+host+`"}`); rec.Code != http.StatusBadRequest {
			t.Errorf("workspace domain %s: %d %s", host, rec.Code, rec.Body.String())
		}
	}
	// A name of the apps domain would be another app's address, or an
	// unclaimed one a certificate could be issued for.
	for _, host := range []string{"evil-x.shpyrd.app", "login.shpyrd.app", "acme-shop.shpyrd.app"} {
		if rec := at(t, s, "acme.shpyrd.cloud", "POST", "/api/projects/shop/domains", `{"host":"`+host+`"}`); rec.Code != http.StatusBadRequest {
			t.Errorf("project domain %s: %d %s", host, rec.Code, rec.Body.String())
		}
	}
}

// An owner who renames a workspace from before the layout moves it into
// the layout: its dashboard to shpyrd.cloud, its apps to <label>-<app>.
func TestLayoutOwnerMovesIntoTheLayout(t *testing.T) {
	s, _ := newLayoutServer(t)
	rec := at(t, s, "older.shpyrd.app", "PATCH", "/api/workspace", `{"address":"older"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"address":"older.shpyrd.cloud"`) {
		t.Fatalf("move: %d %s", rec.Code, rec.Body.String())
	}
	for _, c := range []struct{ host, path, location string }{
		{"older.shpyrd.app", "/", "https://older.shpyrd.cloud/"},
		{"lessons.older.shpyrd.app", "/lesson/3", "https://older-lessons.shpyrd.app/lesson/3"},
	} {
		if rec := at(t, s, c.host, "GET", c.path, ""); rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != c.location {
			t.Errorf("%s%s = %d %s, want %s", c.host, c.path, rec.Code, rec.Header().Get("Location"), c.location)
		}
	}
	if rec := at(t, s, "older-lessons.shpyrd.app", "GET", "/.shpyrd/signin?rd=/", ""); rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "https://older.shpyrd.cloud/.shpyrd/start?") {
		t.Errorf("the app at its new host: %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

// In the shared layout every workspace signs in through one callback, at
// the workspaces domain itself: the identity provider lists one address,
// not one per workspace. The callback hands the browser back to the host
// the login started at, which completes it as before.
func TestLayoutSignInThroughOneCallback(t *testing.T) {
	issuer := newFakeIssuer(t)
	s, _ := newLayoutServer(t)
	if err := s.rp.AddOIDC(context.Background(), ext.OIDCProvider{ID: "test", Label: "Test login", Issuer: issuer.srv.URL, ClientID: "shpyrd", ClientSecret: "sekret", Realm: ext.RealmPlatform}); err != nil {
		t.Fatal(err)
	}
	get := func(host, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "https://"+host+path, nil)
		req.Host = host
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}
	rec := get("acme.shpyrd.cloud", "/api/auth/login?provider=test&next=/projects")
	authURL, _ := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusFound || authURL.Query().Get("redirect_uri") != "https://shpyrd.cloud/api/auth/callback" {
		t.Fatalf("login -> %d %s", rec.Code, rec.Header().Get("Location"))
	}
	resp, err := http.DefaultTransport.RoundTrip(mustRequest(t, "GET", authURL.String()))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	back, _ := url.Parse(resp.Header.Get("Location"))
	if back.Host != "shpyrd.cloud" {
		t.Fatalf("issuer sent the browser to %s", back.Host)
	}
	// Taken at another workspace's host, the answer mints nothing there.
	if rec := get("older.shpyrd.app", "/api/auth/callback?"+back.RawQuery); cookieValue(rec, sessionCookie) != "" {
		t.Fatal("a login of acme completed at older")
	}
	relayed := get("shpyrd.cloud", back.RequestURI())
	next, _ := url.Parse(relayed.Header().Get("Location"))
	if relayed.Code != http.StatusFound || next.Host != "acme.shpyrd.cloud" || next.Path != "/api/auth/callback" || next.Query().Get("state") != back.Query().Get("state") {
		t.Fatalf("relay -> %d %s", relayed.Code, relayed.Header().Get("Location"))
	}
	done := get("acme.shpyrd.cloud", next.RequestURI())
	if done.Code != http.StatusFound || done.Header().Get("Location") != "/projects" || cookieValue(done, sessionCookie) == "" {
		t.Fatalf("callback at acme -> %d %s", done.Code, done.Header().Get("Location"))
	}
	// The shared callback holds nothing else, and forgets a used state.
	if rec := get("shpyrd.cloud", "/"); rec.Code != http.StatusNotFound {
		t.Errorf("the relay host's root = %d", rec.Code)
	}
	if rec := get("shpyrd.cloud", back.RequestURI()); rec.Code == http.StatusFound {
		t.Errorf("a used state was relayed again: %s", rec.Header().Get("Location"))
	}
	// The identity provider is told of the one callback, whatever the
	// number of workspaces.
	uris, err := s.redirectURIs(context.Background())
	if err != nil || len(uris) != 2 || uris[1] != "https://shpyrd.cloud/api/auth/callback" {
		t.Errorf("redirect URIs = %v %v", uris, err)
	}
}
