package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/edge"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/ext/all"
	"github.com/shpyrd-io/shpyrd/pkg/ids"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"github.com/shpyrd-io/shpyrd/pkg/tenancy"
)

// newTenantServer is a server with two workspaces resolved by host: the
// implicit one at example.test and acme at acme.shpyrd.test (RFC-0033
// phase 6). Each has a project called shop.
func newTenantServer(t *testing.T) (*Server, client.Client, store.Store) {
	t.Helper()
	ctx := context.Background()
	st := store.NewMemory()
	// The operator's default workspace answers at the platform domain
	// (RFC-0080, open-source layout); the console at shpyrd.example.test.
	if _, err := st.UpdateWorkspaceAddress(ctx, store.DefaultWorkspace, "example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "acme", Name: "Acme", Address: "acme.shpyrd.test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "closed", Name: "Closed", Address: "closed.shpyrd.test", Status: store.WorkspaceSuspended}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: strings.Repeat("w", 24), Name: "Long", Address: "long.shpyrd.test"}); err != nil {
		t.Fatal(err)
	}
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	defaultShop := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"}, Status: shpyrdv1.AppStatus{URL: "https://shop.example.test"}}
	acmeShop := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-acme-shop", Labels: project.NamespaceLabels("acme", "shop")},
		Spec:       shpyrdv1.AppSpec{Access: shpyrdv1.AccessAuthenticated, Image: "ghcr.io/acme/shop:1"},
		Status:     shpyrdv1.AppStatus{URL: "https://shop.acme.shpyrd.test"},
	}
	acmeOnly := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "wiki", Namespace: "app-acme-wiki", Labels: project.NamespaceLabels("acme", "wiki")}, Spec: shpyrdv1.AppSpec{Image: "ghcr.io/acme/wiki:1"}}
	cr := crfake.NewClientBuilder().WithScheme(scheme).WithObjects(defaultShop, acmeShop, acmeOnly).WithStatusSubresource(&shpyrdv1.App{}).Build()
	k := &kube.Client{Kube: kubefake.NewSimpleClientset(), Namespace: "shpyrd-system"}
	public := PublicConfig{Domain: "example.test", DashboardURL: "https://shpyrd.example.test"}
	s, err := newServer(k, Options{
		Token: testToken, Apps: cr, Store: st, Public: public,
		Tenancy:      &tenancy.ByAddress{Store: st, Domain: public.Domain, ConsoleHost: "shpyrd.example.test"},
		Capabilities: []string{"workspaces"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.authz.TTL = 1
	return s, cr, st
}

// at performs a request at a host with the admin token.
func at(t *testing.T, s *Server, host, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "https://"+host+path, strings.NewReader(body))
	req.Host = host
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func slugsOf(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	var apps []AppSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &apps); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	var out []string
	for _, a := range apps {
		out = append(out, a.Slug)
	}
	return out
}

func TestTenancyByHost(t *testing.T) {
	s, cr, _ := newTenantServer(t)

	// Each host sees its own projects only. The console is not a workspace:
	// project routes there are not found (RFC-0080).
	if rec := at(t, s, "shpyrd.example.test", "GET", "/api/projects", ""); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "console is not a workspace") {
		t.Errorf("projects at the console = %d %s", rec.Code, rec.Body.String())
	}
	if got := slugsOf(t, at(t, s, "example.test", "GET", "/api/projects", "")); len(got) != 1 || got[0] != "shop" {
		t.Errorf("default projects = %v", got)
	}
	if got := slugsOf(t, at(t, s, "acme.shpyrd.test", "GET", "/api/projects", "")); len(got) != 2 || got[0] != "shop" || got[1] != "wiki" {
		t.Errorf("acme projects = %v", got)
	}
	// The same slug at two hosts is two different Apps.
	def := at(t, s, "example.test", "GET", "/api/projects/shop", "")
	acme := at(t, s, "acme.shpyrd.test", "GET", "/api/projects/shop", "")
	if def.Code != 200 || acme.Code != 200 {
		t.Fatalf("get shop: %d %d", def.Code, acme.Code)
	}
	if !strings.Contains(def.Body.String(), "shop.example.test") || !strings.Contains(acme.Body.String(), "shop.acme.shpyrd.test") {
		t.Errorf("wrong app for host: default=%s acme=%s", def.Body.String(), acme.Body.String())
	}
	// A project of one workspace is not found from another.
	if rec := at(t, s, "example.test", "GET", "/api/projects/wiki", ""); rec.Code != http.StatusNotFound {
		t.Errorf("wiki from default = %d", rec.Code)
	}
	// Internal hosts are the operator's door with the default workspace as
	// tenant: project commands over a kubeconfig keep working.
	if got := slugsOf(t, at(t, s, "localhost:8080", "GET", "/api/projects", "")); len(got) != 1 || got[0] != "shop" {
		t.Errorf("localhost projects = %v", got)
	}

	// Hosts nobody claims answer 404, suspended workspaces 403, on JSON and HTML alike.
	if rec := at(t, s, "nobody.example.org", "GET", "/api/projects", ""); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "no workspace answers at nobody.example.org") {
		t.Errorf("unknown host = %d %s", rec.Code, rec.Body.String())
	}
	if rec := at(t, s, "closed.shpyrd.test", "GET", "/api/projects", ""); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "suspended") {
		t.Errorf("suspended workspace = %d %s", rec.Code, rec.Body.String())
	}
	if rec := at(t, s, "shop.closed.shpyrd.test", "GET", "/.shpyrd/signin?rd=/", ""); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Workspace suspended") {
		t.Errorf("suspended app host = %d", rec.Code)
	}
	// The workspace view names the host it answers at.
	var view WorkspaceView
	rec := at(t, s, "acme.shpyrd.test", "GET", "/api/workspace", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &view)
	if view.Slug != "acme" || view.OwnedByOperator || view.Address != "acme.shpyrd.test" || view.URL != "https://acme.shpyrd.test" || view.Domain != "acme.shpyrd.test" {
		t.Errorf("acme view = %+v", view)
	}
	// The default workspace is the operator's, at its own address.
	rec = at(t, s, "example.test", "GET", "/api/workspace", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &view)
	if !view.OwnedByOperator || view.URL != "https://example.test" || view.Domain != "example.test" || view.Address != "example.test" {
		t.Errorf("default view = %+v", view)
	}
	// The console has no workspace view.
	if rec := at(t, s, "shpyrd.example.test", "GET", "/api/workspace", ""); rec.Code != http.StatusNotFound {
		t.Errorf("workspace view at the console = %d", rec.Code)
	}
	// The admin token is never offered at a workspace host, only at the
	// console; /api/config names the door.
	if rec := at(t, s, "acme.shpyrd.test", "GET", "/api/config", ""); !strings.Contains(rec.Body.String(), `"token":false`) || !strings.Contains(rec.Body.String(), `"door":"workspace"`) {
		t.Errorf("acme config: %s", rec.Body.String())
	}
	if rec := at(t, s, "shpyrd.example.test", "GET", "/api/config", ""); !strings.Contains(rec.Body.String(), `"token":true`) || !strings.Contains(rec.Body.String(), `"door":"console"`) || strings.Contains(rec.Body.String(), `"workspace":{`) {
		t.Errorf("console config: %s", rec.Body.String())
	}
	if rec := at(t, s, "example.test", "GET", "/api/config", ""); !strings.Contains(rec.Body.String(), `"ownedByOperator":true`) || !strings.Contains(rec.Body.String(), `"consoleUrl":"https://shpyrd.example.test"`) {
		t.Errorf("default workspace config: %s", rec.Body.String())
	}
	// Capabilities reach the dashboard.
	if rec := at(t, s, "acme.shpyrd.test", "GET", "/api/config", ""); !strings.Contains(rec.Body.String(), `"capabilities":["workspaces"]`) {
		t.Errorf("config = %s", rec.Body.String())
	}

	// Creating a project at acme: the App is named by its id (RFC-0076),
	// labelled with the workspace and the slug; the same slug in another
	// workspace is a different project.
	rec = at(t, s, "acme.shpyrd.test", "POST", "/api/projects", `{"name":"Billing"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create at acme = %d %s", rec.Code, rec.Body.String())
	}
	var created AppSummary
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.Namespace != project.IDNamespace(created.ID) || created.Slug != "billing" {
		t.Fatalf("created = %+v", created)
	}
	ns := &corev1.Namespace{}
	if err := cr.Get(context.Background(), types.NamespacedName{Name: created.Namespace}, ns); err != nil || ns.Labels[shpyrdv1.LabelWorkspace] != "acme" || ns.Labels[shpyrdv1.LabelProject] != "billing" || ns.Labels[shpyrdv1.LabelWorkspaceID] == "" {
		t.Errorf("namespace %s: %v %v", created.Namespace, err, ns.Labels)
	}
	app := &shpyrdv1.App{}
	if err := cr.Get(context.Background(), types.NamespacedName{Namespace: created.Namespace, Name: ids.Short(created.ID)}, app); err != nil || app.Labels[shpyrdv1.LabelWorkspace] != "acme" || app.Spec.Slug != "billing" {
		t.Errorf("app billing: %v %v", err, app.Labels)
	}
	// The path resolves the slug within the workspace.
	if rec := at(t, s, "acme.shpyrd.test", "GET", "/api/projects/billing", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"slug":"billing"`) {
		t.Errorf("get billing at acme = %d %s", rec.Code, rec.Body.String())
	}
	if rec := at(t, s, "example.test", "GET", "/api/projects/billing", ""); rec.Code != http.StatusNotFound {
		t.Errorf("billing must not resolve in the default workspace: %d", rec.Code)
	}
	if rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects", `{"name":"Billing"}`); rec.Code != http.StatusConflict {
		t.Errorf("duplicate slug in the workspace = %d %s", rec.Code, rec.Body.String())
	}
	if rec := at(t, s, "long.shpyrd.test", "POST", "/api/projects", `{"name":"Billing"}`); rec.Code != http.StatusCreated {
		t.Errorf("same slug in another workspace = %d %s", rec.Code, rec.Body.String())
	}
	// Reserved names are refused.
	if rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects", `{"name":"login"}`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "reserved") {
		t.Errorf("reserved slug = %d %s", rec.Code, rec.Body.String())
	}
	// The namespace no longer carries the slugs, so the workspace's length
	// does not bound the project's: 40 characters everywhere.
	if rec := at(t, s, "long.shpyrd.test", "POST", "/api/projects", `{"name":"`+strings.Repeat("a", 40)+`"}`); rec.Code != http.StatusCreated {
		t.Errorf("long slug in explicit workspace = %d %s", rec.Code, rec.Body.String())
	}
	if rec := at(t, s, "example.test", "POST", "/api/projects", `{"name":"`+strings.Repeat("a", 40)+`"}`); rec.Code != http.StatusCreated {
		t.Errorf("long slug in the default workspace = %d %s", rec.Code, rec.Body.String())
	}
}

func TestTenancyIsolation(t *testing.T) {
	s, _, st := newTenantServer(t)
	ctx := context.Background()
	// Maria runs the implicit workspace; Ana runs acme. Neither is anyone
	// in the other's workspace.
	if _, _, err := st.PutTeam(ctx, store.DefaultWorkspace, store.Team{Name: "ops", Members: []string{"maria@example.test"}, PlatformRole: shpyrdv1.RolePlatformAdmin}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.PutTeam(ctx, "acme", store.Team{Name: "ops", Members: []string{"ana@acme.test"}, PlatformRole: shpyrdv1.RolePlatformAdmin}); err != nil {
		t.Fatal(err)
	}
	for ws, email := range map[string]string{store.DefaultWorkspace: "maria@example.test", "acme": "ana@acme.test"} {
		if _, err := st.TouchIdentity(ctx, ws, store.Identity{Email: email, Provider: "local"}); err != nil {
			t.Fatal(err)
		}
	}
	s.authz.Invalidate()
	// Maria's console session (RFC-0080); a workspace session of hers at the
	// default workspace's own host; Ana's at acme.
	mariaSID, _ := s.rp.sessions.create(ctx, store.RealmConsole, "", ext.Identity{Email: "maria@example.test", Provider: "local"}, "")
	mariaWS, _ := s.rp.sessions.create(ctx, store.RealmWorkspace, store.DefaultWorkspace, ext.Identity{Email: "maria@example.test", Provider: "local"}, "")
	anaSID, _ := s.rp.sessions.create(ctx, store.RealmWorkspace, "acme", ext.Identity{Email: "ana@acme.test", Provider: "local"}, "")

	me := func(host, sid string) (int, string) {
		req := httptest.NewRequest("GET", "https://"+host+"/api/me", nil)
		req.Host = host
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	// Sessions work at their own door...
	if code, body := me("shpyrd.example.test", mariaSID.ID); code != 200 || !strings.Contains(body, "platform-admin") || !strings.Contains(body, `"console":true`) {
		t.Errorf("maria at the console = %d %s", code, body)
	}
	// ...a platform admin owns the operator's workspace without a membership...
	if code, body := me("example.test", mariaWS.ID); code != 200 || !strings.Contains(body, `"workspace":"owner"`) || !strings.Contains(body, `"console":true`) {
		t.Errorf("maria at the default workspace = %d %s", code, body)
	}
	if code, body := me("acme.shpyrd.test", anaSID.ID); code != 200 || !strings.Contains(body, "platform-admin") || strings.Contains(body, `"console":true`) {
		t.Errorf("ana at home = %d %s", code, body)
	}
	// ...and are no session at all at another door: a replayed cookie is anonymous.
	if code, _ := me("acme.shpyrd.test", mariaSID.ID); code != http.StatusUnauthorized {
		t.Errorf("maria's console cookie at acme = %d, want 401", code)
	}
	if code, _ := me("example.test", mariaSID.ID); code != http.StatusUnauthorized {
		t.Errorf("maria's console cookie at her own workspace = %d, want 401 (realms do not share sessions)", code)
	}
	if code, _ := me("shpyrd.example.test", mariaWS.ID); code != http.StatusUnauthorized {
		t.Errorf("maria's workspace cookie at the console = %d, want 401", code)
	}
	if code, _ := me("shpyrd.example.test", anaSID.ID); code != http.StatusUnauthorized {
		t.Errorf("ana's cookie at the console = %d, want 401", code)
	}
	// Teams are per workspace: acme's ops team is invisible from the default workspace.
	if rec := at(t, s, "example.test", "GET", "/api/teams", ""); strings.Contains(rec.Body.String(), "ana@acme.test") {
		t.Errorf("acme's team leaked into default: %s", rec.Body.String())
	}

	// The edge issues acme's JWT for acme's app: issuer and workspace claim
	// follow the app host nginx reports in X-Original-URL.
	cookie, err := s.edgeKeys.SignCookie(edge.CookieClaims{SessionID: anaSID.ID, Project: "shop"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "http://shpyrd-server.shpyrd-system.svc.cluster.local/edge/auth?project=shop&mode=authenticated", nil)
	req.Host = "shpyrd-server.shpyrd-system.svc.cluster.local"
	req.Header.Set("X-Original-URL", "https://shop.acme.shpyrd.test/orders")
	req.AddCookie(&http.Cookie{Name: s.edgeCookieName(), Value: cookie})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("edge auth at acme = %d %s", rec.Code, rec.Body.String())
	}
	raw := strings.TrimPrefix(rec.Header().Get("Authorization"), "Bearer ")
	var claims edge.Claims
	if err := s.edgeKeys.Verify(raw, "JWT", &claims); err != nil {
		t.Fatalf("verify jwt: %v", err)
	}
	if claims.Issuer != "https://acme.shpyrd.test" || claims.Workspace != "acme" || claims.Email != "ana@acme.test" {
		t.Errorf("acme claims = %+v", claims)
	}
	// Maria's session, carried by a cookie, opens nothing at acme's app.
	foreign, _ := s.edgeKeys.SignCookie(edge.CookieClaims{SessionID: mariaSID.ID, Project: "shop"})
	req2 := httptest.NewRequest("GET", "http://shpyrd-server.shpyrd-system.svc.cluster.local/edge/auth?project=shop&mode=authenticated", nil)
	req2.Header.Set("X-Original-URL", "https://shop.acme.shpyrd.test/")
	req2.Header.Set("Accept", "text/html") // a browser: asked to sign in
	req2.AddCookie(&http.Cookie{Name: s.edgeCookieName(), Value: foreign})
	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Errorf("foreign session at acme's app = %d, want 401 (anonymous)", rec2.Code)
	}
}

// TestPerHostSignIn walks a workspace's sign-in (RFC-0080): its host starts
// OpenID Connect naming its own callback, the issuer returns there, the
// workspace's admission runs and its session is minted at its host. The
// console is not involved and its methods are its own.
func TestPerHostSignIn(t *testing.T) {
	issuer := newFakeIssuer(t)
	s, _, st := newTenantServer(t)
	// A platform default (offered to workspaces) and the console's own.
	if err := s.rp.AddOIDC(context.Background(), ext.OIDCProvider{ID: "test", Label: "Test login", Issuer: issuer.srv.URL, ClientID: "shpyrd", ClientSecret: "sekret", Realm: ext.RealmPlatform}); err != nil {
		t.Fatal(err)
	}
	if err := s.rp.AddOIDC(context.Background(), ext.OIDCProvider{ID: "console-test", Label: "Operators", Issuer: issuer.srv.URL, ClientID: "shpyrd", ClientSecret: "sekret", Realm: ext.RealmConsole}); err != nil {
		t.Fatal(err)
	}
	get := func(host, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "https://"+host+path, nil)
		req.Host = host
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}
	signInAt := func(host, provider string) (*httptest.ResponseRecorder, string) {
		rec := get(host, "/api/auth/login?provider="+provider+"&next=/projects")
		authURL, _ := url.Parse(rec.Header().Get("Location"))
		if rec.Code != http.StatusFound || !strings.HasPrefix(authURL.String(), issuer.srv.URL+"/auth?") {
			t.Fatalf("%s login -> %d %s %s", host, rec.Code, rec.Header().Get("Location"), rec.Body.String())
		}
		// The authorization request names this host's own callback.
		if got := authURL.Query().Get("redirect_uri"); got != "https://"+host+"/api/auth/callback" {
			t.Fatalf("%s redirect_uri = %q", host, got)
		}
		resp, err := http.DefaultTransport.RoundTrip(mustRequest(t, "GET", authURL.String()))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		back, _ := url.Parse(resp.Header.Get("Location"))
		if back.Host != host {
			t.Fatalf("issuer sent the browser to %s, want %s", back.Host, host)
		}
		rec = get(host, back.RequestURI())
		return rec, cookieValue(rec, sessionCookie)
	}

	// 1. At acme: the platform default is offered, the console's is not.
	if rec := get("acme.shpyrd.test", "/api/auth/login?provider=console-test"); rec.Code != http.StatusBadRequest {
		t.Errorf("console method at acme = %d %s", rec.Code, rec.Body.String())
	}
	rec, sid := signInAt("acme.shpyrd.test", "test")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/projects" || sid == "" {
		t.Fatalf("acme callback -> %d %s cookies=%v", rec.Code, rec.Header().Get("Location"), rec.Result().Cookies())
	}
	me := func(host string, sid string) (int, string) {
		req := httptest.NewRequest("GET", "https://"+host+"/api/me", nil)
		req.Host = host
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	if code, body := me("acme.shpyrd.test", sid); code != 200 || !strings.Contains(body, "ada@example.test") {
		t.Errorf("me at acme = %d %s", code, body)
	}
	if code, _ := me("shpyrd.example.test", sid); code != http.StatusUnauthorized {
		t.Errorf("acme's session at the console = %d, want 401", code)
	}
	// A fresh workspace is enforced from birth: Ada, its first person, is
	// nobody there until the workspace's creator names owners.
	if code, body := me("acme.shpyrd.test", sid); code != 200 || strings.Contains(body, "platform-admin") || !strings.Contains(body, `"enforced":true`) {
		t.Errorf("first person in a fresh workspace = %d %s (bootstrap mode must not apply)", code, body)
	}
	// Ada is a person of acme now, and of no other workspace.
	if people, _ := st.ListIdentities(context.Background(), "acme"); len(people) != 1 || people[0].Email != "ada@example.test" {
		t.Errorf("acme people = %+v", people)
	}
	if people, _ := st.ListIdentities(context.Background(), store.DefaultWorkspace); len(people) != 0 {
		t.Errorf("operator people = %+v (a workspace sign-in must not reach the console's people)", people)
	}

	// 1b. On a fresh cluster the operator's workspace follows the console's
	// bootstrap: the first person through its door is its owner, until the
	// first role is written. A customer workspace (acme) never does.
	if _, err := st.CreateWorkspace(context.Background(), store.Workspace{Slug: "platform", Name: "Platform", Address: "platform.shpyrd.test", Owner: store.WorkspaceOwnerOperator}); err != nil {
		t.Fatal(err)
	}
	s.workspacesChanged()
	rec, osid := signInAt("platform.shpyrd.test", "test")
	if rec.Code != http.StatusFound || osid == "" {
		t.Fatalf("platform callback -> %d %s", rec.Code, rec.Body.String())
	}
	if code, body := me("platform.shpyrd.test", osid); code != 200 || !strings.Contains(body, `"workspace":"owner"`) || !strings.Contains(body, `"enforced":false`) || !strings.Contains(body, `"console":true`) {
		t.Errorf("first person at the operator's workspace on a fresh cluster = %d %s (bootstrap owner)", code, body)
	}

	// 1c. A personal token minted there carries the same owner role: the
	// CLI can create and deploy projects in the operator's workspace.
	var osess *session
	if v, ok := s.rp.sessions.get(osid); ok {
		osess = v
	}
	if osess == nil {
		t.Fatal("no session for the operator's workspace sign-in")
	}
	// Tokens are least privilege (RFC-0031): the CLI's asks for the owner's
	// platform role, which the door grants this person without a membership.
	mintReq := httptest.NewRequest("POST", "https://platform.shpyrd.test/api/tokens", strings.NewReader(`{"name":"laptop","platformRole":"platform-admin"}`))
	mintReq.Host = "platform.shpyrd.test"
	mintReq.Header.Set("Content-Type", "application/json")
	mintReq.Header.Set(csrfHeader, osess.CSRF)
	mintReq.AddCookie(&http.Cookie{Name: sessionCookie, Value: osid})
	mintRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(mintRec, mintReq)
	if mintRec.Code != http.StatusCreated {
		t.Fatalf("mint token at the operator's workspace = %d %s", mintRec.Code, mintRec.Body.String())
	}
	var minted TokenCreateView
	_ = json.Unmarshal(mintRec.Body.Bytes(), &minted)
	createReq := httptest.NewRequest("POST", "https://platform.shpyrd.test/api/projects", strings.NewReader(`{"name":"handbook"}`))
	createReq.Host = "platform.shpyrd.test"
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+minted.Token)
	createRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Errorf("create a project with the operator's personal token = %d %s", createRec.Code, createRec.Body.String())
	}

	// 2. At the console: the platform default is not offered; the console's
	// own is, and the first person through it is the platform admin
	// (bootstrap applies to the console realm alone).
	if rec := get("shpyrd.example.test", "/api/auth/login?provider=test"); rec.Code != http.StatusBadRequest {
		t.Errorf("platform default at the console = %d %s", rec.Code, rec.Body.String())
	}
	rec, csid := signInAt("shpyrd.example.test", "console-test")
	if rec.Code != http.StatusFound || csid == "" {
		t.Fatalf("console callback -> %d %s", rec.Code, rec.Body.String())
	}
	if code, body := me("shpyrd.example.test", csid); code != 200 || !strings.Contains(body, "platform-admin") || !strings.Contains(body, `"console":true`) {
		t.Errorf("me at the console = %d %s", code, body)
	}
	if code, _ := me("acme.shpyrd.test", csid); code != http.StatusUnauthorized {
		t.Errorf("console session at acme = %d, want 401", code)
	}
	if code, _ := me("example.test", csid); code != http.StatusUnauthorized {
		t.Errorf("console session at the operator's workspace host = %d, want 401", code)
	}

	// 3. A listed-only workspace refuses strangers at its own callback and
	// sends them to its own login page.
	if _, err := st.UpdateWorkspaceSettings(context.Background(), "acme", store.WorkspaceSettings{JoinPolicy: store.JoinListed}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteIdentity(context.Background(), "acme", "ada@example.test"); err != nil {
		t.Fatal(err)
	}
	rec, sid = signInAt("acme.shpyrd.test", "test")
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/?login_error=") || sid != "" {
		t.Errorf("stranger at a listed-only workspace = %d %s (must land on acme's login page, no session)", rec.Code, rec.Header().Get("Location"))
	}
	// A suspended workspace cannot be signed into at all.
	if rec := get("closed.shpyrd.test", "/api/auth/login?provider=test"); rec.Code != http.StatusForbidden {
		t.Errorf("login at a suspended workspace = %d %s", rec.Code, rec.Body.String())
	}
}

// TestPlanLimits: a workspace with a plan is refused what would exceed it,
// with the number; the implicit workspace, without a plan, is not.
func TestPlanLimits(t *testing.T) {
	s, _, st := newTenantServer(t)
	ctx := context.Background()
	// acme: at most 2 projects, 3 instances, 2 CPU, 10Gi of storage.
	if _, err := st.UpdateWorkspaceSettings(ctx, "acme", store.WorkspaceSettings{Limits: &store.Limits{Projects: 2, Instances: 3, CPU: "2", Storage: "10Gi"}}); err != nil {
		t.Fatal(err)
	}
	// acme already has shop and wiki: a third project is one too many.
	rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects", `{"name":"third"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "allows 2 projects") {
		t.Errorf("third project = %d %s", rec.Code, rec.Body.String())
	}
	// Scaling shop's web to 2 instances is fine (shop 2 + wiki 1 = 3)...
	if rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/scale", `{"process":"web","replicas":2}`); rec.Code != http.StatusOK {
		t.Errorf("scale to 2 = %d %s", rec.Code, rec.Body.String())
	}
	// ...to 3 is one instance over the plan.
	rec = at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/scale", `{"process":"web","replicas":3}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "run 4 instances; the plan allows 3") {
		t.Errorf("scale to 3 = %d %s", rec.Code, rec.Body.String())
	}
	// CPU: shop at shared-xl (2 CPU × 2 instances) + wiki's shared-s (0.5)
	// = 4.5 > 2.
	rec = at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/resize", `{"process":"web","size":"shared-xl"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "CPU; the plan allows 2") {
		t.Errorf("resize over CPU = %d %s", rec.Code, rec.Body.String())
	}
	// Storage: a 20Gi volume exceeds 10Gi.
	rec = at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/volumes", `{"name":"data","size":"20Gi"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "storage; the plan allows 10Gi") {
		t.Errorf("volume over storage = %d %s", rec.Code, rec.Body.String())
	}
	if rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/volumes", `{"name":"data","size":"5Gi"}`); rec.Code != http.StatusCreated {
		t.Errorf("volume within storage = %d %s", rec.Code, rec.Body.String())
	}
	// The workspace view shows the plan and the usage.
	var view WorkspaceView
	rec = at(t, s, "acme.shpyrd.test", "GET", "/api/workspace", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &view)
	if view.Limits == nil || view.Limits.Instances != 3 || view.Usage == nil || view.Usage.Instances != 3 || view.Usage.Projects != 2 || view.Usage.Storage != "5Gi" {
		t.Errorf("view = limits %+v usage %+v", view.Limits, view.Usage)
	}
	// No plan, no ceiling: the operator's workspace scales freely.
	if rec := at(t, s, "example.test", "POST", "/api/projects/shop/scale", `{"process":"web","replicas":50}`); rec.Code != http.StatusOK {
		t.Errorf("default workspace scale = %d %s", rec.Code, rec.Body.String())
	}
}

// An allow list names projects of the caller's workspace and nothing else:
// acme's shop may list acme's wiki; the implicit workspace's shop cannot
// list wiki (there is none there), nor itself, nor a name that is not a
// project (RFC-0033 "cross-workspace allows do not exist").
func TestAllowListStaysInTheWorkspace(t *testing.T) {
	s, _, _ := newTenantServer(t)
	if rec := at(t, s, "acme.shpyrd.test", "PUT", "/api/projects/shop/allow", `[{"project":"wiki"}]`); rec.Code != http.StatusOK {
		t.Fatalf("acme shop allows acme wiki: %d %s", rec.Code, rec.Body.String())
	}
	if rec := at(t, s, "example.test", "PUT", "/api/projects/shop/allow", `[{"project":"wiki"}]`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `in this workspace`) {
		t.Errorf("default shop allowing acme's wiki = %d %s, want 400", rec.Code, rec.Body.String())
	}
	if rec := at(t, s, "acme.shpyrd.test", "PUT", "/api/projects/shop/allow", `[{"project":"shop"}]`); rec.Code != http.StatusBadRequest {
		t.Errorf("a project allowing itself = %d, want 400", rec.Code)
	}
	if rec := at(t, s, "acme.shpyrd.test", "PUT", "/api/projects/shop/allow", `[{"project":"Not A Slug"}]`); rec.Code != http.StatusBadRequest {
		t.Errorf("a non-slug = %d, want 400", rec.Code)
	}
	// The deploy path carries shpyrd.yaml's allow: the same rule applies.
	if rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/deploy", `{"image":"ghcr.io/acme/shop:2","allow":[{"project":"nope"}]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("deploy with an unknown allow = %d %s, want 400", rec.Code, rec.Body.String())
	}
	if rec := at(t, s, "acme.shpyrd.test", "POST", "/api/projects/shop/deploy", `{"image":"ghcr.io/acme/shop:2","allow":[{"project":"wiki"},{"platform":"mcp"}]}`); rec.Code != http.StatusAccepted {
		t.Errorf("deploy with a good allow = %d %s", rec.Code, rec.Body.String())
	}
}

// A backup carries every explicit workspace's dump; the console restores
// one into its workspace, recreating the workspace from the dump when it is
// gone (a fresh cluster), and refuses the dump at the workspace's own host
// and when the platform has one workspace.
func TestImportExplicitWorkspaceAtConsole(t *testing.T) {
	s, _, st := newTenantServer(t)
	ctx := context.Background()
	dump := `{"version":1,"workspace":{"slug":"beta","name":"Beta Labs","address":"beta.shpyrd.test","status":"suspended","settings":{"joinPolicy":"listed"}},"teams":[{"name":"owners","members":["bea@beta.test"],"platformRole":"platform-admin"}],"grants":[{"project":"shop","role":"user","team":"owners"}]}`
	if _, err := st.Workspace(ctx, "beta"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("beta must not exist yet: %v", err)
	}
	// From an explicit workspace's host: not there.
	if rec := at(t, s, "acme.shpyrd.test", "POST", "/api/workspace/import?workspace=beta", dump); rec.Code != http.StatusNotFound {
		t.Errorf("import of another workspace at a workspace host = %d, want 404", rec.Code)
	}
	rec := at(t, s, "shpyrd.example.test", "POST", "/api/workspace/import?workspace=beta", dump)
	if rec.Code != http.StatusOK {
		t.Fatalf("import at the console = %d %s", rec.Code, rec.Body.String())
	}
	ws, err := st.Workspace(ctx, "beta")
	if err != nil || ws.Name != "Beta Labs" || ws.Address != "beta.shpyrd.test" || ws.Status != store.WorkspaceSuspended || ws.Settings.JoinPolicy != "listed" {
		t.Fatalf("recreated workspace = %+v %v", ws, err)
	}
	teams, _ := st.ListTeams(ctx, "beta")
	grants, _ := st.ListGrants(ctx, "beta")
	if len(teams) < 1 || len(grants) != 1 {
		t.Errorf("beta teams=%d grants=%d", len(teams), len(grants))
	}
	// The dump's slug must be the one named.
	if rec := at(t, s, "shpyrd.example.test", "POST", "/api/workspace/import?workspace=gamma", dump); rec.Code != http.StatusBadRequest {
		t.Errorf("mismatched slug = %d, want 400", rec.Code)
	}
	// Without the workspaces capability the platform has one workspace.
	s.opts.Public.Capabilities = nil
	if rec := at(t, s, "shpyrd.example.test", "POST", "/api/workspace/import?workspace=delta", strings.Replace(dump, `"beta"`, `"delta"`, 1)); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "one workspace") {
		t.Errorf("import on a single-workspace platform = %d %s", rec.Code, rec.Body.String())
	}
}

// A project's custom domain is a host the tenancy resolver cannot know:
// the app that claims it says whose it is, so the API, sign-in and the
// edge's callbacks at that host belong to the app's workspace.
func TestCustomDomainResolvesToTheAppsWorkspace(t *testing.T) {
	s, cr, _ := newTenantServer(t)
	ctx := context.Background()
	shop := &shpyrdv1.App{}
	if err := cr.Get(ctx, types.NamespacedName{Namespace: "app-acme-shop", Name: "shop"}, shop); err != nil {
		t.Fatal(err)
	}
	shop.Spec.Domains = []string{"shop.acme-corp.example"}
	if err := cr.Update(ctx, shop); err != nil {
		t.Fatal(err)
	}
	s.hostCache.at = time.Time{} // forget the index
	rec := at(t, s, "shop.acme-corp.example", "GET", "/api/projects", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"wiki"`) || strings.Contains(rec.Body.String(), "example.test") {
		t.Errorf("at the custom domain = %d %s, want acme's projects", rec.Code, rec.Body.String())
	}
	if rec := at(t, s, "nobody.example.org", "GET", "/api/projects", ""); rec.Code != http.StatusNotFound {
		t.Errorf("a host nobody claims = %d, want 404", rec.Code)
	}
}

// Per-workspace SSO (RFC-0033, RFC-0080): a workspace's own login methods
// show on its login page only; the platform's defaults show on every
// workspace's until it hides them, which it may do only once it has a
// method of its own; the console's are the console's alone.
func TestPerWorkspaceLoginMethods(t *testing.T) {
	issuer := newFakeIssuer(t)
	s, _, st := newTenantServer(t)
	ctx := context.Background()
	add := func(id, realm, ws string) {
		if err := s.rp.AddOIDC(ctx, ext.OIDCProvider{ID: id, Label: id, Issuer: issuer.srv.URL, ClientID: "shpyrd", ClientSecret: "sekret", Realm: realm, Workspace: ws}); err != nil {
			t.Fatal(err)
		}
	}
	acme, _ := st.Workspace(ctx, "acme")
	closed, _ := st.Workspace(ctx, "closed")
	add("github", ext.RealmPlatform, "")
	add("console-google", ext.RealmConsole, "")
	add("ws-acme-okta", ext.RealmWorkspace, ids.Short(acme.ID))
	add("ws-closed-google", ext.RealmWorkspace, ids.Short(closed.ID))
	providers := func(host string) []string {
		rec := at(t, s, host, "GET", "/api/config", "")
		var cfg struct {
			Auth AuthConfig `json:"auth"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
			t.Fatalf("%s config: %d %s", host, rec.Code, rec.Body.String())
		}
		var ids []string
		for _, p := range cfg.Auth.Providers {
			ids = append(ids, p.ID)
		}
		return ids
	}
	if got := providers("shpyrd.example.test"); strings.Join(got, ",") != "console-google" {
		t.Errorf("console shows its own methods only: %v", got)
	}
	if got := providers("acme.shpyrd.test"); strings.Join(got, ",") != "github,ws-acme-okta" {
		t.Errorf("acme shows the platform's defaults and its own: %v", got)
	}
	if got := providers("example.test"); strings.Join(got, ",") != "github" {
		t.Errorf("the operator's workspace shows the platform's defaults, not the console's: %v", got)
	}
	// The console starts a workspace's own method for that workspace, and
	// refuses another workspace's.
	if rec := at(t, s, "acme.shpyrd.test", "GET", "/api/auth/login?provider=ws-acme-okta", ""); rec.Code != http.StatusFound {
		t.Errorf("acme's own method at acme: %d %s", rec.Code, rec.Body.String())
	}
	if rec := at(t, s, "acme.shpyrd.test", "GET", "/api/auth/login?provider=ws-closed-google", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("another workspace's method at acme: %d %s", rec.Code, rec.Body.String())
	}
	// Hiding the platform's methods: refused without an own method, then
	// allowed, then the page shows the own method only.
	wsToken := func(host, body string) *httptest.ResponseRecorder {
		return at(t, s, host, "PATCH", "/api/workspace", body)
	}
	if rec := wsToken("long.shpyrd.test", `{"ownMethodsOnly":true}`); rec.Code != http.StatusBadRequest {
		t.Errorf("hide without own method: %d %s", rec.Code, rec.Body.String())
	}
	if rec := wsToken("acme.shpyrd.test", `{"ownMethodsOnly":true}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ownMethodsOnly":true`) {
		t.Fatalf("hide at acme: %d %s", rec.Code, rec.Body.String())
	}
	if got := providers("acme.shpyrd.test"); strings.Join(got, ",") != "ws-acme-okta" {
		t.Errorf("acme with own methods only: %v", got)
	}
	if rec := at(t, s, "acme.shpyrd.test", "GET", "/api/auth/login?provider=github", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("hidden platform method at acme: %d", rec.Code)
	}
	if rec := wsToken("shpyrd.example.test", `{"ownMethodsOnly":true}`); rec.Code != http.StatusNotFound {
		t.Errorf("the console has no workspace settings: %d", rec.Code)
	}
}

// Names of a workspace (RFC-0033): the address changes under the same
// parent (old one redirects), a custom domain is added, proven and made
// primary, and every host resolves to the workspace.
func TestWorkspaceAddressAndCustomDomains(t *testing.T) {
	s, _, st := newTenantServer(t)
	ctx := context.Background()
	records := map[string][]string{}
	s.lookupTXT = func(_ context.Context, name string) ([]string, error) { return records[name], nil }
	s.lookupCNAME = func(_ context.Context, host string) (string, error) { return "", errors.New("no cname") }
	patch := func(host, body string) *httptest.ResponseRecorder {
		return at(t, s, host, "PATCH", "/api/workspace", body)
	}

	// Refusals: another parent, a reserved label, another workspace's slug, a taken address.
	for body, want := range map[string]int{
		`{"address":"acme.example.com"}`:       http.StatusBadRequest,
		`{"address":"login"}`:                  http.StatusBadRequest,
		`{"address":"closed"}`:                 http.StatusConflict,
		`{"address":"closed.shpyrd.test"}`:     http.StatusConflict,
		`{"address":"Has Spaces.shpyrd.test"}`: http.StatusBadRequest,
	} {
		if rec := patch("acme.shpyrd.test", body); rec.Code != want {
			t.Errorf("%s: %d %s", body, rec.Code, rec.Body.String())
		}
	}
	// The move: label or full host, same result.
	rec := patch("acme.shpyrd.test", `{"address":"Acme-Corp"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"address":"acme-corp.shpyrd.test"`) || !strings.Contains(rec.Body.String(), `"url":"https://acme-corp.shpyrd.test"`) {
		t.Fatalf("move: %d %s", rec.Code, rec.Body.String())
	}
	if ws, _ := st.Workspace(ctx, "acme"); ws.Address != "acme-corp.shpyrd.test" {
		t.Fatalf("address not stored: %+v", ws)
	}
	// The old address, dashboard and app hosts alike, redirects permanently.
	if rec := at(t, s, "acme.shpyrd.test", "GET", "/api/config", ""); rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "https://acme-corp.shpyrd.test/api/config" {
		t.Errorf("old dashboard host: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if rec := at(t, s, "shop.acme.shpyrd.test", "GET", "/orders?x=1", ""); rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "https://shop.acme-corp.shpyrd.test/orders?x=1" {
		t.Errorf("old app host: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if rec := at(t, s, "acme-corp.shpyrd.test", "GET", "/api/config", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"Acme"`) {
		t.Errorf("new address: %d %s", rec.Code, rec.Body.String())
	}
	// Nobody else can take the old address while it redirects.
	if _, err := st.CreateWorkspace(ctx, store.Workspace{Slug: "squat", Name: "S", Address: "acme.shpyrd.test"}); !errors.Is(err, store.ErrConflict) {
		t.Errorf("old address reusable: %v", err)
	}

	// Custom domains: not the platform's names, not a workspace address.
	const host = "acme-corp.shpyrd.test"
	for body, want := range map[string]int{
		`{"host":"not a host"}`:              http.StatusBadRequest,
		`{"host":"x.acme-corp.shpyrd.test"}`: http.StatusBadRequest,
		`{"host":"other.shpyrd.test"}`:       http.StatusBadRequest,
		`{"host":"shop.example.test"}`:       http.StatusBadRequest,
	} {
		if rec := at(t, s, host, "POST", "/api/workspace/domains", body); rec.Code != want {
			t.Errorf("%s: %d %s", body, rec.Code, rec.Body.String())
		}
	}
	rec = at(t, s, host, "POST", "/api/workspace/domains", `{"host":"Intranet.Acme.com"}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"host":"intranet.acme.com"`) || !strings.Contains(rec.Body.String(), `"verified":false`) || !strings.Contains(rec.Body.String(), `"name":"*.intranet.acme.com"`) || !strings.Contains(rec.Body.String(), `_shpyrd-verify.intranet.acme.com`) {
		t.Fatalf("add domain: %d %s", rec.Code, rec.Body.String())
	}
	var view DomainView
	_ = json.Unmarshal(rec.Body.Bytes(), &view)
	token := strings.TrimPrefix(view.Records[2].Value, "shpyrd-verify=")
	// Unverified: not served, not primary-able.
	if rec := at(t, s, "intranet.acme.com", "GET", "/api/config", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unverified domain served: %d", rec.Code)
	}
	if rec := at(t, s, host, "PATCH", "/api/workspace/domains/intranet.acme.com", `{"primary":true}`); rec.Code != http.StatusBadRequest {
		t.Errorf("primary before verify: %d %s", rec.Code, rec.Body.String())
	}
	if rec := at(t, s, host, "POST", "/api/workspace/domains/intranet.acme.com/verify", ""); rec.Code != http.StatusConflict {
		t.Errorf("verify without records: %d %s", rec.Code, rec.Body.String())
	}
	records["_shpyrd-verify.intranet.acme.com"] = []string{"shpyrd-verify=" + token}
	if rec := at(t, s, host, "POST", "/api/workspace/domains/intranet.acme.com/verify", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"verified":true`) {
		t.Fatalf("verify: %d %s", rec.Code, rec.Body.String())
	}
	// Verified: the dashboard answers there, an app host too, and the
	// workspace's URLs still use the address until it is primary.
	if rec := at(t, s, "intranet.acme.com", "GET", "/api/config", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"Acme"`) {
		t.Errorf("custom domain dashboard: %d %s", rec.Code, rec.Body.String())
	}
	if rec := at(t, s, "shop.intranet.acme.com", "GET", "/.shpyrd/signin?rd=/", ""); rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "https://acme-corp.shpyrd.test/.shpyrd/start") {
		t.Errorf("app at the custom domain: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if rec := at(t, s, host, "PATCH", "/api/workspace/domains/intranet.acme.com", `{"primary":true}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"primary":true`) {
		t.Fatalf("primary: %d %s", rec.Code, rec.Body.String())
	}
	if rec := at(t, s, host, "GET", "/api/workspace", ""); !strings.Contains(rec.Body.String(), `"url":"https://intranet.acme.com"`) || !strings.Contains(rec.Body.String(), `"domain":"intranet.acme.com"`) {
		t.Errorf("workspace with a primary domain: %s", rec.Body.String())
	}
	// Sign-in from an app under the primary domain goes to the primary host.
	if rec := at(t, s, "shop.acme-corp.shpyrd.test", "GET", "/.shpyrd/signin?rd=/", ""); rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "https://intranet.acme.com/.shpyrd/start") {
		t.Errorf("sign-in goes to the primary: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if rec := at(t, s, host, "GET", "/api/workspace/domains", ""); !strings.Contains(rec.Body.String(), `"primary":true`) {
		t.Errorf("list: %s", rec.Body.String())
	}
	if rec := at(t, s, host, "DELETE", "/api/workspace/domains/intranet.acme.com", ""); rec.Code != http.StatusNoContent {
		t.Errorf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if rec := at(t, s, host, "GET", "/api/workspace", ""); !strings.Contains(rec.Body.String(), `"url":"https://acme-corp.shpyrd.test"`) {
		t.Errorf("after delete: %s", rec.Body.String())
	}
}

// TestTwoApplicationsByHost: the console host gets the console application,
// a workspace host the workspace one; assets are shared (RFC-0080).
func TestTwoApplicationsByHost(t *testing.T) {
	s, _, _ := newTenantServer(t)
	s.opts.UI = fstest.MapFS{
		"console/index.html":   {Data: []byte("<html>console</html>")},
		"workspace/index.html": {Data: []byte("<html>workspace</html>")},
		"workspace/app.js":     {Data: []byte("js")},
	}
	s.engine.NoRoute(s.serveUI())
	body := func(host, path string) string {
		rec := at(t, s, host, "GET", path, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s%s = %d", host, path, rec.Code)
		}
		return rec.Body.String()
	}
	if got := body("shpyrd.example.test", "/"); got != "<html>console</html>" {
		t.Errorf("console host = %q", got)
	}
	if got := body("shpyrd.example.test", "/workspaces"); got != "<html>console</html>" {
		t.Errorf("console route = %q", got)
	}
	if got := body("acme.shpyrd.test", "/"); got != "<html>workspace</html>" {
		t.Errorf("workspace host = %q", got)
	}
	if got := body("example.test", "/projects/shop"); got != "<html>workspace</html>" {
		t.Errorf("default workspace host = %q", got)
	}
	if got := body("localhost:8080", "/"); got != "<html>console</html>" {
		t.Errorf("internal host = %q (the operator's door)", got)
	}
	if got := body("acme.shpyrd.test", "/app.js"); got != "js" {
		t.Errorf("asset = %q", got)
	}
}

// A database on a small plan (#53): one that names no size gets db-xs when
// the workspace's memory ceiling is under 512Mi, and the answer says so in
// words; a size under the database floor is said to be raised; without a
// plan the database keeps the usual 256Mi.
func TestDatabaseSizeOnASmallPlan(t *testing.T) {
	s, cr, st := newTenantServer(t)
	s.opts.Extensions = all.All()
	ctx := context.Background()
	if _, err := st.UpdateWorkspaceSettings(ctx, "acme", store.WorkspaceSettings{Limits: &store.Limits{Memory: "256Mi"}}); err != nil {
		t.Fatal(err)
	}
	create := func(host, body string) ResourceView {
		t.Helper()
		rec := at(t, s, host, "POST", "/api/projects/shop/resources", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
		}
		var v ResourceView
		_ = json.Unmarshal(rec.Body.Bytes(), &v)
		return v
	}
	v := create("acme.shpyrd.test", `{"kind":"Postgres","name":"db","spec":{"storage":"5Gi"}}`)
	if v.Details["size"] != "db-xs" || !strings.Contains(v.Note, "db-xs: 128Mi") || !strings.Contains(v.Note, "memory ceiling is 256Mi") {
		t.Errorf("small plan = size %q note %q", v.Details["size"], v.Note)
	}
	pg := &shpyrdv1.Postgres{}
	if err := cr.Get(ctx, types.NamespacedName{Namespace: "app-acme-shop", Name: "db"}, pg); err != nil || pg.Spec.Size != "db-xs" {
		t.Errorf("stored size = %q %v", pg.Spec.Size, err)
	}
	v = create("acme.shpyrd.test", `{"kind":"Postgres","name":"tiny","spec":{"size":"shared-s"}}`)
	if v.Details["size"] != "shared-s" || !strings.Contains(v.Note, "at least 128Mi") {
		t.Errorf("shared-s = size %q note %q", v.Details["size"], v.Note)
	}
	v = create("example.test", `{"kind":"Postgres","name":"db","spec":{}}`)
	if v.Details["size"] != "" || !strings.Contains(v.Note, "256Mi") {
		t.Errorf("no plan = size %q note %q", v.Details["size"], v.Note)
	}
}
