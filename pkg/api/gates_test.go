package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ctrl "sigs.k8s.io/controller-runtime"
	crfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/edge"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"github.com/shpyrd-io/shpyrd/pkg/tenancy"
)

// gateExt declares one gate, "billing", in front of the project billing of
// the operator's workspace "platform": owners of any other workspace come in
// by its rule. It adds one link, shown when the gate would admit.
type gateExt struct{ deps ext.Deps }

func (*gateExt) Name() string                          { return "gates-test" }
func (*gateExt) Description() string                   { return "test" }
func (*gateExt) Components() []ext.ComponentRef        { return nil }
func (*gateExt) Register(ctrl.Manager, ext.Deps) error { return nil }
func (*gateExt) Types() []ext.ResourceType             { return nil }
func (*gateExt) CLI(ext.CLIGlobals) []*cobra.Command   { return nil }
func (g *gateExt) Routes(_ ext.Router, d ext.Deps) error {
	g.deps = d
	return nil
}
func (*gateExt) Gates(ext.Deps) []ext.Gate {
	return []ext.Gate{{
		Name: "billing", Host: "billing.example.test", Workspace: "platform", Project: "billing",
		Admit: func(_ context.Context, v ext.Visitor) bool { return v.WorkspaceRole == store.WorkspaceRoleOwner },
	}}
}
func (g *gateExt) Links(ctx context.Context, v ext.Visitor) []ext.Link {
	if !g.deps.GateAdmits(ctx, "billing", v) {
		return nil
	}
	return []ext.Link{{Section: "Cloud", Label: "Billing", URL: "/.shpyrd/gate?name=billing", Icon: "credit-card"}}
}

// gateWorld is a server with the gate above and three workspaces:
//   - platform (operator-owned): Bruno in team finance, granted user on
//     billing; Dora, an owner (owners open every project of their
//     workspace); Frank, a member without the grant;
//   - acme: Ana its owner, Carla a member;
//   - beta: an owner of its own, Eve.
type gateWorld struct {
	s                                   *Server
	st                                  store.Store
	ana, carla, bruno, dora, eve, frank ext.Identity
}

func newGateWorld(t *testing.T) *gateWorld {
	t.Helper()
	ctx := context.Background()
	st := store.NewMemory()
	if _, err := st.UpdateWorkspaceAddress(ctx, store.DefaultWorkspace, "example.test"); err != nil {
		t.Fatal(err)
	}
	for _, w := range []store.Workspace{
		{Slug: "platform", Name: "Platform", Address: "platform.shpyrd.test", Owner: store.WorkspaceOwnerOperator},
		{Slug: "acme", Name: "Acme", Address: "acme.shpyrd.test"},
		{Slug: "beta", Name: "Beta", Address: "beta.shpyrd.test"},
	} {
		if _, err := st.CreateWorkspace(ctx, w); err != nil {
			t.Fatal(err)
		}
	}
	w := &gateWorld{
		st:    st,
		ana:   ext.Identity{Subject: "u-ana", Email: "ana@acme.test", Name: "Ana", Provider: "local"},
		carla: ext.Identity{Subject: "u-carla", Email: "carla@acme.test", Provider: "local"},
		bruno: ext.Identity{Subject: "u-bruno", Email: "bruno@shpyrd.test", Provider: "local"},
		dora:  ext.Identity{Subject: "u-dora", Email: "dora@shpyrd.test", Provider: "local"},
		eve:   ext.Identity{Subject: "u-eve", Email: "eve@beta.test", Provider: "local"},
		frank: ext.Identity{Subject: "u-frank", Email: "frank@shpyrd.test", Provider: "local"},
	}
	for _, m := range []struct{ ws, email, role string }{
		{"acme", w.ana.Email, store.WorkspaceRoleOwner}, {"acme", w.carla.Email, store.WorkspaceRoleMember},
		{"platform", w.bruno.Email, store.WorkspaceRoleMember}, {"platform", w.dora.Email, store.WorkspaceRoleOwner},
		{"beta", w.eve.Email, store.WorkspaceRoleOwner},
		{"platform", w.frank.Email, store.WorkspaceRoleMember},
	} {
		if _, err := st.PutMembership(ctx, m.ws, m.email, m.role); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := st.PutTeam(ctx, "platform", store.Team{Name: "finance", Members: []string{w.bruno.Email}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddGrant(ctx, "platform", store.Grant{Project: "billing", Role: shpyrdv1.RoleUser, Team: "finance"}); err != nil {
		t.Fatal(err)
	}
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	billing := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "billing", Namespace: project.NamespaceIn("platform", "billing"), Labels: project.NamespaceLabels("platform", "billing")},
		Spec:       shpyrdv1.AppSpec{Access: shpyrdv1.AccessAuthenticated, Image: "ghcr.io/shpyrd/billing:1"},
	}
	cr := crfake.NewClientBuilder().WithScheme(scheme).WithObjects(billing).WithStatusSubresource(&shpyrdv1.App{}).Build()
	k := &kube.Client{Kube: kubefake.NewSimpleClientset(), Namespace: "shpyrd-system"}
	public := PublicConfig{Domain: "example.test", DashboardURL: "https://shpyrd.example.test"}
	s, err := newServer(k, Options{
		Token: testToken, Apps: cr, Store: st, Public: public,
		Tenancy:      &tenancy.ByAddress{Store: st, Domain: public.Domain, ConsoleHost: "shpyrd.example.test"},
		Capabilities: []string{"workspaces"},
		Extensions:   []ext.Extension{&gateExt{}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.authz.TTL = 1
	w.s = s
	return w
}

// visitor is someone as the gate sees them, in a workspace.
func (w *gateWorld) visitor(t *testing.T, ws string, id ext.Identity) (ext.Visitor, bool) {
	t.Helper()
	ctx := context.Background()
	wsp, err := w.st.Workspace(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	v, roles, err := w.s.visitorIn(ctx, wsp, id)
	if err != nil {
		t.Fatal(err)
	}
	g, _ := w.s.gateByName("billing")
	_, ok := w.s.gateAdmits(ctx, g, v, roles)
	return v, ok
}

// People of the gate's own workspace come in by the project's access, never
// by the gate's rule; people of any other workspace by the rule alone.
func TestAGateAdmitsByProjectAccessAtHomeAndByItsRuleElsewhere(t *testing.T) {
	w := newGateWorld(t)
	if v, ok := w.visitor(t, "acme", w.ana); !ok || v.WorkspaceRole != store.WorkspaceRoleOwner {
		t.Errorf("acme's owner refused: %+v", v)
	}
	if _, ok := w.visitor(t, "acme", w.carla); ok {
		t.Error("acme's member admitted")
	}
	if _, ok := w.visitor(t, "beta", w.eve); !ok {
		t.Error("another customer's owner refused")
	}
	if v, ok := w.visitor(t, "platform", w.bruno); !ok || !slices.Contains(v.Teams, "finance") {
		t.Errorf("a person granted the project refused: %+v", v)
	}
	// An owner of the gate's workspace opens it as she opens every project
	// there; the rule is not asked at home.
	if _, ok := w.visitor(t, "platform", w.dora); !ok {
		t.Error("platform's owner refused")
	}
	// A member of the gate's workspace without the grant is refused; the rule
	// is not asked at home.
	if _, ok := w.visitor(t, "platform", w.frank); ok {
		t.Error("platform's member without the grant admitted")
	}
}

// An extension asks the same question through its deps: whether a gate
// would admit someone, to show its link.
func TestDepsGateAdmitsAnswersAsTheGate(t *testing.T) {
	w := newGateWorld(t)
	ctx := context.Background()
	v, _ := w.visitor(t, "acme", w.ana)
	if !w.s.deps().GateAdmits(ctx, "billing", v) {
		t.Error("GateAdmits refused acme's owner")
	}
	v, _ = w.visitor(t, "acme", w.carla)
	if w.s.deps().GateAdmits(ctx, "billing", v) {
		t.Error("GateAdmits admitted acme's member")
	}
	if w.s.deps().GateAdmits(ctx, "nope", v) {
		t.Error("GateAdmits admitted at a gate that does not exist")
	}
}

// Two extensions declaring the same gate name, or the same host, are a
// configuration error the server refuses at start.
func TestGatesWithTheSameNameOrHostAreRefused(t *testing.T) {
	s := &Server{}
	g := ext.Gate{Name: "billing", Host: "billing.example.test", Workspace: "platform", Project: "billing"}
	if err := s.addGates([]ext.Gate{g, g}); err == nil || !strings.Contains(err.Error(), "billing") {
		t.Errorf("duplicate gate accepted: %v", err)
	}
	s = &Server{}
	other := g
	other.Name = "reports"
	if err := s.addGates([]ext.Gate{g, other}); err == nil {
		t.Error("two gates on one host accepted")
	}
	s = &Server{}
	bad := g
	bad.Name = "Billing!"
	if err := s.addGates([]ext.Gate{bad}); err == nil {
		t.Error("a gate with an invalid name accepted")
	}
}

// at a host, with a session cookie (or none) and extra cookies.
func (w *gateWorld) get(t *testing.T, host, path, sid string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "https://"+host+path, nil)
	req.Host = host
	req.Header.Set("Accept", "text/html")
	if sid != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sid})
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	w.s.Handler().ServeHTTP(rec, req)
	return rec
}

// session opens a workspace session for someone.
func (w *gateWorld) session(t *testing.T, ws string, id ext.Identity) string {
	t.Helper()
	sess, err := w.s.rp.sessions.create(context.Background(), store.RealmWorkspace, ws, id, "")
	if err != nil {
		t.Fatal(err)
	}
	return sess.ID
}

// enter walks the gate's way in from a workspace's host and returns the gate
// cookie the gate's host set.
func (w *gateWorld) enter(t *testing.T, wsHost, sid string) *http.Cookie {
	t.Helper()
	rec := w.get(t, wsHost, "/.shpyrd/gate?name=billing", sid)
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "https://billing.example.test/.shpyrd/callback?code=") {
		t.Fatalf("gate open = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	cb := strings.TrimPrefix(rec.Header().Get("Location"), "https://billing.example.test")
	rec = w.get(t, "billing.example.test", cb, "")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Fatalf("gate callback = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == w.s.gateCookieName() {
			if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode {
				t.Errorf("gate cookie attributes: %+v", c)
			}
			return c
		}
	}
	t.Fatal("no gate cookie set")
	return nil
}

// auth asks /edge/auth about a gate, as nginx does.
func (w *gateWorld) auth(t *testing.T, gate, method string, cookie *http.Cookie) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest("GET", "/edge/auth?gate="+gate, nil)
	req.Host = "shpyrd-server.shpyrd-system.svc.cluster.local"
	req.Header.Set("X-Original-Method", method)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	w.s.Handler().ServeHTTP(rec, req)
	var claims map[string]any
	if bearer := strings.TrimPrefix(rec.Header().Get("Authorization"), "Bearer "); bearer != "" {
		if err := w.s.edgeKeys.Verify(bearer, "JWT", &claims); err != nil {
			t.Fatalf("the gate's JWT does not verify: %v", err)
		}
	}
	return rec, claims
}

// Ana, owner of acme, walks in from acme's host: a code, a cookie at the
// gate's host, and on each request a JWT for billing that says she came
// from acme, a customer workspace, as its owner.
func TestAnOwnerOfAnotherWorkspaceEntersTheGate(t *testing.T) {
	w := newGateWorld(t)
	cookie := w.enter(t, "acme.shpyrd.test", w.session(t, "acme", w.ana))
	rec, claims := w.auth(t, "billing", "GET", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth = %d %s", rec.Code, rec.Body.String())
	}
	if claims["aud"] != "billing" || claims["ws"] != "acme" || claims["project"] != "billing" || claims["realm"] != "workspace" || claims["operator"] != nil {
		t.Errorf("claims = %v", claims)
	}
	if roles := claims["roles"].([]any); len(roles) == 0 || roles[0] != "owner" {
		t.Errorf("roles = %v", claims["roles"])
	}
	if rec.Header().Get("X-Shpyrd-Email") != w.ana.Email {
		t.Errorf("X-Shpyrd-Email = %q", rec.Header().Get("X-Shpyrd-Email"))
	}
}

// Bruno, granted the project in the gate's own workspace, enters by that
// access: his JWT carries operator, his project role and his teams.
func TestAPersonGrantedTheProjectEntersFromItsOwnWorkspace(t *testing.T) {
	w := newGateWorld(t)
	cookie := w.enter(t, "platform.shpyrd.test", w.session(t, "platform", w.bruno))
	rec, claims := w.auth(t, "billing", "GET", cookie)
	if rec.Code != http.StatusOK || claims["operator"] != true || claims["ws"] != "platform" {
		t.Fatalf("auth = %d %v", rec.Code, claims)
	}
	if roles := claims["roles"].([]any); len(roles) == 0 || roles[0] != shpyrdv1.RoleUser {
		t.Errorf("roles = %v", claims["roles"])
	}
	teams, _ := claims["teams"].([]any)
	if !slices.Contains(teams, any("finance")) {
		t.Errorf("teams = %v", claims["teams"])
	}
}

// Refusals on the way in: nobody signed in goes to the sign-in page and
// back (Review Focus 5: a path, never another host); a member of acme and
// a member of platform without the grant get the no-access page; an
// unknown gate is nothing.
func TestTheWayInRefusesWhoTheGateWouldNotAdmit(t *testing.T) {
	w := newGateWorld(t)
	rec := w.get(t, "acme.shpyrd.test", "/.shpyrd/gate?name=billing", "")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/?next="+url.QueryEscape("/.shpyrd/gate?name=billing") {
		t.Errorf("anonymous = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if rec := w.get(t, "acme.shpyrd.test", "/.shpyrd/gate?name=billing", w.session(t, "acme", w.carla)); rec.Code != http.StatusForbidden {
		t.Errorf("acme's member = %d", rec.Code)
	}
	if rec := w.get(t, "platform.shpyrd.test", "/.shpyrd/gate?name=billing", w.session(t, "platform", w.frank)); rec.Code != http.StatusForbidden {
		t.Errorf("a member of platform without the grant = %d", rec.Code)
	}
	if rec := w.get(t, "acme.shpyrd.test", "/.shpyrd/gate?name=nope", w.session(t, "acme", w.ana)); rec.Code != http.StatusNotFound {
		t.Errorf("unknown gate = %d", rec.Code)
	}
}

// A code works once, at its host; a gate's host with a port is still the
// gate (Review Focus 1); a cookie of an app does not open a gate.
func TestCodesAndCookiesOpenOnlyWhatTheyWereMadeFor(t *testing.T) {
	w := newGateWorld(t)
	rec := w.get(t, "acme.shpyrd.test", "/.shpyrd/gate?name=billing", w.session(t, "acme", w.ana))
	cb := strings.TrimPrefix(rec.Header().Get("Location"), "https://billing.example.test")
	if rec := w.get(t, "billing.example.test:8443", cb, ""); rec.Code != http.StatusFound {
		t.Fatalf("callback at the gate's host with a port = %d", rec.Code)
	}
	if rec := w.get(t, "billing.example.test", cb, ""); rec.Code != http.StatusBadRequest {
		t.Errorf("a code used twice = %d", rec.Code)
	}
	app, _ := w.s.edgeKeys.SignCookie(edge.CookieClaims{SessionID: "x", Project: "billing"})
	if rec, _ := w.auth(t, "billing", "GET", &http.Cookie{Name: w.s.gateCookieName(), Value: app}); rec.Code != http.StatusUnauthorized {
		t.Errorf("an app cookie at the gate = %d", rec.Code)
	}
	if rec, _ := w.auth(t, "billing", "GET", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("no cookie = %d", rec.Code)
	}
}

// Everything is checked again on each request: signing out, losing the
// role, or a workspace re-created under the same slug (Review Focus 2)
// closes the gate.
func TestTheGateClosesWhenAccessEnds(t *testing.T) {
	w := newGateWorld(t)
	ctx := context.Background()
	sid := w.session(t, "acme", w.ana)
	cookie := w.enter(t, "acme.shpyrd.test", sid)
	if _, err := w.st.PutMembership(ctx, "acme", w.ana.Email, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	w.s.authz.Invalidate()
	if rec, _ := w.auth(t, "billing", "GET", cookie); rec.Code != http.StatusForbidden {
		t.Errorf("after losing the owner role = %d", rec.Code)
	}
	if _, err := w.st.PutMembership(ctx, "acme", w.ana.Email, store.WorkspaceRoleOwner); err != nil {
		t.Fatal(err)
	}
	w.s.authz.Invalidate()
	if rec, _ := w.auth(t, "billing", "GET", cookie); rec.Code != http.StatusOK {
		t.Fatalf("owner again = %d", rec.Code)
	}
	w.s.rp.sessions.delete(ctx, sid)
	if rec, _ := w.auth(t, "billing", "GET", cookie); rec.Code != http.StatusUnauthorized {
		t.Errorf("after signing out = %d", rec.Code)
	}
	// A cookie naming acme's slug with another workspace's ID opens nothing.
	acme, _ := w.st.Workspace(ctx, "acme")
	forged, _ := w.s.edgeKeys.SignGateCookie(edge.GateClaims{SessionID: w.session(t, "acme", w.ana), Workspace: "acme", WorkspaceID: acme.ID + "-old", Gate: "billing"})
	if rec, _ := w.auth(t, "billing", "GET", &http.Cookie{Name: w.s.gateCookieName(), Value: forged}); rec.Code != http.StatusUnauthorized {
		t.Errorf("a cookie of an older acme = %d", rec.Code)
	}
}

// A read-only project role passes a gate to look, never to change
// (Review Focus 4), as at an app.
func TestAReaderAtAGateLooksAndDoesNotTouch(t *testing.T) {
	w := newGateWorld(t)
	ctx := context.Background()
	if _, err := w.st.AddGrant(ctx, "platform", store.Grant{Project: "billing", Role: shpyrdv1.RoleReader, User: w.frank.Email}); err != nil {
		t.Fatal(err)
	}
	w.s.authz.Invalidate()
	cookie := w.enter(t, "platform.shpyrd.test", w.session(t, "platform", w.frank))
	if rec, _ := w.auth(t, "billing", "GET", cookie); rec.Code != http.StatusOK {
		t.Errorf("reader GET = %d", rec.Code)
	}
	if rec, _ := w.auth(t, "billing", "POST", cookie); rec.Code != http.StatusForbidden {
		t.Errorf("reader POST = %d", rec.Code)
	}
}

// nginx hands a gate's 401 and 403 to the server's pages: no sign-in to
// send anyone to, a page that says where to open it from.
func TestAGatesHostAnswersItsErrorsWithPages(t *testing.T) {
	w := newGateWorld(t)
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "billing.example.test"
	req.Header.Set("X-Code", "401")
	rec := httptest.NewRecorder()
	w.s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "from your workspace") {
		t.Errorf("401 page = %d %s", rec.Code, rec.Body.String()[:min(200, len(rec.Body.String()))])
	}
	req.Header.Set("X-Code", "403")
	rec = httptest.NewRecorder()
	w.s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("403 page = %d", rec.Code)
	}
	if rec := w.get(t, "billing.example.test", "/.shpyrd/logout", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Set-Cookie"), w.s.gateCookieName()+"=;") {
		t.Errorf("logout = %d %q", rec.Code, rec.Header().Get("Set-Cookie"))
	}
}
