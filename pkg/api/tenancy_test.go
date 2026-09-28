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
		Tenancy:      &tenancy.ByAddress{Store: st, Domain: public.Domain, DashboardHost: "shpyrd.example.test"},
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

	// Each host sees its own projects only.
	if got := slugsOf(t, at(t, s, "shpyrd.example.test", "GET", "/api/projects", "")); len(got) != 1 || got[0] != "shop" {
		t.Errorf("default projects = %v", got)
	}
	if got := slugsOf(t, at(t, s, "acme.shpyrd.test", "GET", "/api/projects", "")); len(got) != 2 || got[0] != "shop" || got[1] != "wiki" {
		t.Errorf("acme projects = %v", got)
	}
	// The same slug at two hosts is two different Apps.
	def := at(t, s, "shpyrd.example.test", "GET", "/api/projects/shop", "")
	acme := at(t, s, "acme.shpyrd.test", "GET", "/api/projects/shop", "")
	if def.Code != 200 || acme.Code != 200 {
		t.Fatalf("get shop: %d %d", def.Code, acme.Code)
	}
	if !strings.Contains(def.Body.String(), "shop.example.test") || !strings.Contains(acme.Body.String(), "shop.acme.shpyrd.test") {
		t.Errorf("wrong app for host: default=%s acme=%s", def.Body.String(), acme.Body.String())
	}
	// A project of one workspace is not found from another.
	if rec := at(t, s, "shpyrd.example.test", "GET", "/api/projects/wiki", ""); rec.Code != http.StatusNotFound {
		t.Errorf("wiki from default = %d", rec.Code)
	}
	// Internal hosts are the operator's: the implicit workspace.
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
	if view.Slug != "acme" || view.Implicit || view.Address != "acme.shpyrd.test" || view.URL != "https://acme.shpyrd.test" || view.Domain != "acme.shpyrd.test" {
		t.Errorf("acme view = %+v", view)
	}
	rec = at(t, s, "shpyrd.example.test", "GET", "/api/workspace", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &view)
	if !view.Implicit || view.URL != "https://shpyrd.example.test" || view.Domain != "example.test" {
		t.Errorf("default view = %+v", view)
	}
	// The admin token is never offered at a workspace host, only at the console.
	if rec := at(t, s, "acme.shpyrd.test", "GET", "/api/config", ""); !strings.Contains(rec.Body.String(), `"token":false`) {
		t.Errorf("acme config offers the admin token: %s", rec.Body.String())
	}
	if rec := at(t, s, "shpyrd.example.test", "GET", "/api/config", ""); !strings.Contains(rec.Body.String(), `"token":true`) {
		t.Errorf("console config must offer the admin token: %s", rec.Body.String())
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
	if rec := at(t, s, "shpyrd.example.test", "GET", "/api/projects/billing", ""); rec.Code != http.StatusNotFound {
		t.Errorf("billing must not resolve in the implicit workspace: %d", rec.Code)
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
	if rec := at(t, s, "shpyrd.example.test", "POST", "/api/projects", `{"name":"`+strings.Repeat("a", 40)+`"}`); rec.Code != http.StatusCreated {
		t.Errorf("long slug in implicit workspace = %d %s", rec.Code, rec.Body.String())
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
	mariaSID, _ := s.rp.sessions.create(ctx, store.DefaultWorkspace, ext.Identity{Email: "maria@example.test", Provider: "local"}, "")
	anaSID, _ := s.rp.sessions.create(ctx, "acme", ext.Identity{Email: "ana@acme.test", Provider: "local"}, "")

	me := func(host, sid string) (int, string) {
		req := httptest.NewRequest("GET", "https://"+host+"/api/me", nil)
		req.Host = host
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	// Sessions work at their own workspace's host...
	if code, body := me("shpyrd.example.test", mariaSID.ID); code != 200 || !strings.Contains(body, "platform-admin") {
		t.Errorf("maria at home = %d %s", code, body)
	}
	if code, body := me("acme.shpyrd.test", anaSID.ID); code != 200 || !strings.Contains(body, "platform-admin") {
		t.Errorf("ana at home = %d %s", code, body)
	}
	// ...and are no session at all at another's: a replayed cookie is anonymous.
	if code, _ := me("acme.shpyrd.test", mariaSID.ID); code != http.StatusUnauthorized {
		t.Errorf("maria's cookie at acme = %d, want 401", code)
	}
	if code, _ := me("shpyrd.example.test", anaSID.ID); code != http.StatusUnauthorized {
		t.Errorf("ana's cookie at default = %d, want 401", code)
	}
	// Teams are per workspace: acme's ops team is invisible from the platform host.
	if rec := at(t, s, "shpyrd.example.test", "GET", "/api/teams", ""); strings.Contains(rec.Body.String(), "ana@acme.test") {
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

// TestConsoleHandoff walks the sign-in of an explicit workspace: its host
// sends the browser to the console, the console completes OpenID Connect,
// applies the workspace's admission and hands a one-time code back; the
// workspace host mints its own session. The console keeps no session.
func TestConsoleHandoff(t *testing.T) {
	issuer := newFakeIssuer(t)
	s, _, st := newTenantServer(t)
	if err := s.rp.AddOIDC(context.Background(), ext.OIDCProvider{ID: "test", Label: "Test login", Issuer: issuer.srv.URL, ClientID: "shpyrd", ClientSecret: "sekret"}); err != nil {
		t.Fatal(err)
	}
	get := func(host, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "https://"+host+path, nil)
		req.Host = host
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}

	// 1. At acme, login goes to the console naming the workspace.
	rec := get("acme.shpyrd.test", "/api/auth/login?provider=test&next=/projects")
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusFound || loc.Host != "shpyrd.example.test" || loc.Path != "/api/auth/login" || loc.Query().Get("workspace") != "acme" || loc.Query().Get("next") != "/projects" {
		t.Fatalf("acme login -> %d %s", rec.Code, rec.Header().Get("Location"))
	}
	// 2. The console starts the flow with the issuer.
	rec = get("shpyrd.example.test", loc.RequestURI())
	authURL, _ := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusFound || !strings.HasPrefix(authURL.String(), issuer.srv.URL+"/auth?") {
		t.Fatalf("console login -> %d %s", rec.Code, rec.Header().Get("Location"))
	}
	resp, err := http.DefaultTransport.RoundTrip(mustRequest(t, "GET", authURL.String()))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	back, _ := url.Parse(resp.Header.Get("Location"))
	// 3. The console's callback hands off to acme with a code; no console cookie.
	rec = get("shpyrd.example.test", back.RequestURI())
	hand, _ := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusFound || hand.Host != "acme.shpyrd.test" || hand.Path != "/.shpyrd/session" || hand.Query().Get("code") == "" || hand.Query().Get("rd") != "/projects" {
		t.Fatalf("callback -> %d %s %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if cookieValue(rec, sessionCookie) != "" {
		t.Error("the console must not open a session for a workspace sign-in")
	}
	// 4. acme redeems the code into its own session, for its own host only.
	if rec := get("shpyrd.example.test", hand.RequestURI()); rec.Code != http.StatusNotFound {
		t.Errorf("session code at the console = %d", rec.Code)
	}
	rec = get("acme.shpyrd.test", hand.RequestURI())
	sid := cookieValue(rec, sessionCookie)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/projects" || sid == "" {
		t.Fatalf("session handoff -> %d %s cookies=%v", rec.Code, rec.Header().Get("Location"), rec.Result().Cookies())
	}
	me := func(host string) (int, string) {
		req := httptest.NewRequest("GET", "https://"+host+"/api/me", nil)
		req.Host = host
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	if code, body := me("acme.shpyrd.test"); code != 200 || !strings.Contains(body, "ada@example.test") {
		t.Errorf("me at acme = %d %s", code, body)
	}
	if code, _ := me("shpyrd.example.test"); code != http.StatusUnauthorized {
		t.Errorf("acme's session at the console = %d, want 401", code)
	}
	// A fresh explicit workspace is enforced from birth: Ada, its first
	// person, is nobody there until the workspace's creator names owners.
	if code, body := me("acme.shpyrd.test"); code != 200 || strings.Contains(body, "platform-admin") || !strings.Contains(body, `"enforced":true`) {
		t.Errorf("first person in a fresh explicit workspace = %d %s (bootstrap mode must not apply)", code, body)
	}
	// Ada is a person of acme now, and of no other workspace.
	if people, _ := st.ListIdentities(context.Background(), "acme"); len(people) != 1 || people[0].Email != "ada@example.test" {
		t.Errorf("acme people = %+v", people)
	}
	if people, _ := st.ListIdentities(context.Background(), store.DefaultWorkspace); len(people) != 0 {
		t.Errorf("console people = %+v (the console must not record workspace sign-ins)", people)
	}
	// The code was single use.
	if rec := get("acme.shpyrd.test", hand.RequestURI()); rec.Code != http.StatusBadRequest {
		t.Errorf("replayed session code = %d", rec.Code)
	}

	// 5. The workspace's admission applies at the console: a listed-only
	// workspace refuses strangers before any code is minted.
	if _, err := st.UpdateWorkspaceSettings(context.Background(), "acme", store.WorkspaceSettings{JoinPolicy: store.JoinListed}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteIdentity(context.Background(), "acme", "ada@example.test"); err != nil {
		t.Fatal(err)
	}
	rec = get("shpyrd.example.test", "/api/auth/login?provider=test&workspace=acme&next=/")
	authURL, _ = url.Parse(rec.Header().Get("Location"))
	resp, _ = http.DefaultTransport.RoundTrip(mustRequest(t, "GET", authURL.String()))
	resp.Body.Close()
	back, _ = url.Parse(resp.Header.Get("Location"))
	rec = get("shpyrd.example.test", back.RequestURI())
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "https://acme.shpyrd.test/?login_error=") {
		t.Errorf("stranger at a listed-only workspace = %d %s (must land on acme's login page)", rec.Code, rec.Header().Get("Location"))
	}
	// A suspended workspace cannot be signed into at all.
	if rec := get("shpyrd.example.test", "/api/auth/login?provider=test&workspace=closed"); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "suspended") {
		t.Errorf("login for a suspended workspace = %d %s", rec.Code, rec.Body.String())
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
	// No plan, no ceiling: the implicit workspace scales freely.
	if rec := at(t, s, "shpyrd.example.test", "POST", "/api/projects/shop/scale", `{"process":"web","replicas":50}`); rec.Code != http.StatusOK {
		t.Errorf("implicit scale = %d %s", rec.Code, rec.Body.String())
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
	if rec := at(t, s, "shpyrd.example.test", "PUT", "/api/projects/shop/allow", `[{"project":"wiki"}]`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `in this workspace`) {
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

// Per-workspace SSO (RFC-0033): a workspace's own login methods show on
// its login page only; the platform's show everywhere until a workspace
// hides them, which it may do only once it has a method of its own.
func TestPerWorkspaceLoginMethods(t *testing.T) {
	issuer := newFakeIssuer(t)
	s, _, _ := newTenantServer(t)
	ctx := context.Background()
	add := func(id, ws string) {
		if err := s.rp.AddOIDC(ctx, ext.OIDCProvider{ID: id, Label: id, Issuer: issuer.srv.URL, ClientID: "shpyrd", ClientSecret: "sekret", Workspace: ws}); err != nil {
			t.Fatal(err)
		}
	}
	add("github", "")
	add("ws-acme-okta", "acme")
	add("ws-closed-google", "closed")
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
	if got := providers("shpyrd.example.test"); strings.Join(got, ",") != "github,ws-acme-okta,ws-closed-google" {
		t.Errorf("console shows everything: %v", got)
	}
	if got := providers("acme.shpyrd.test"); strings.Join(got, ",") != "github,ws-acme-okta" {
		t.Errorf("acme shows the platform's and its own: %v", got)
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
	if rec := wsToken("shpyrd.example.test", `{"ownMethodsOnly":true}`); rec.Code != http.StatusBadRequest {
		t.Errorf("the console cannot hide: %d", rec.Code)
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
