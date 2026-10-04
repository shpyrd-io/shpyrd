package api

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

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

// gateExt declares two gates in front of projects of the operator's
// workspace "platform": "billing", in front of the project billing, and
// "reports", in front of the project reports. Owners of any other workspace
// come in by their rule, which counts how often it is asked. It adds one
// link, shown when billing would admit.
type gateExt struct {
	deps       ext.Deps
	linkCalls  int
	admitCalls int
}

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
func (g *gateExt) Gates(ext.Deps) []ext.Gate {
	admit := func(_ context.Context, v ext.Visitor) bool {
		g.admitCalls++
		return v.WorkspaceRole == store.WorkspaceRoleOwner
	}
	return []ext.Gate{
		{Name: "billing", Host: "billing.example.test", Workspace: "platform", Project: "billing", Admit: admit},
		{Name: "reports", Host: "reports.example.test", Workspace: "platform", Project: "reports", Admit: admit},
	}
}
func (g *gateExt) Links(ctx context.Context, v ext.Visitor) []ext.Link {
	g.linkCalls++
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
	ext                                 *gateExt
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
	gateExtInstance := &gateExt{}
	s, err := newServer(k, Options{
		Token: testToken, Apps: cr, Store: st, Public: public,
		Tenancy:      &tenancy.ByAddress{Store: st, Domain: public.Domain, ConsoleHost: "shpyrd.example.test"},
		Capabilities: []string{"workspaces"},
		Extensions:   []ext.Extension{gateExtInstance},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.authz.TTL = 1
	w.s = s
	w.ext = gateExtInstance
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

// wayIn walks the first three hops of the gate's way in: the workspace's
// host sends the browser to the gate's host to begin, which sets a nonce
// and sends it back, and the workspace's host then vouches with a code. It
// returns the callback's path and the nonce cookie the browser holds.
func (w *gateWorld) wayIn(t *testing.T, wsHost, sid string) (string, *http.Cookie) {
	t.Helper()
	rec := w.get(t, wsHost, "/.shpyrd/gate?name=billing", sid)
	begin := rec.Header().Get("Location")
	if rec.Code != http.StatusFound || !strings.HasPrefix(begin, "https://billing.example.test/.shpyrd/begin?t=") {
		t.Fatalf("gate open = %d %s", rec.Code, begin)
	}
	bq, _ := url.ParseQuery(strings.TrimPrefix(begin, "https://billing.example.test/.shpyrd/begin?"))
	home, err := w.st.WorkspaceByAddress(context.Background(), wsHost)
	if err != nil {
		t.Fatal(err)
	}
	if ticket, err := w.s.edgeKeys.VerifyGateBegin(bq.Get("t"), "billing"); err != nil || ticket.Workspace != home.Slug || ticket.WorkspaceID != home.ID || len(bq) != 1 {
		t.Fatalf("begin ticket: %v %+v %v", err, ticket, bq)
	}
	rec = w.get(t, "billing.example.test", strings.TrimPrefix(begin, "https://billing.example.test"), "")
	back := "https://" + wsHost + "/.shpyrd/gate?"
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), back) {
		t.Fatalf("gate begin = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	var nonce *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == w.s.gateNonceCookieName() {
			nonce = c
		}
	}
	if nonce == nil || !nonce.HttpOnly || !nonce.Secure || nonce.SameSite != http.SameSiteLaxMode || nonce.Path != "/" || nonce.MaxAge != 300 {
		t.Fatalf("nonce cookie: %+v", nonce)
	}
	q, _ := url.ParseQuery(strings.TrimPrefix(rec.Header().Get("Location"), back))
	if q.Get("name") != "billing" || q.Get("nonce") != nonce.Value {
		t.Fatalf("gate begin sent back %s", rec.Header().Get("Location"))
	}
	rec = w.get(t, wsHost, strings.TrimPrefix(rec.Header().Get("Location"), "https://"+wsHost), sid)
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "https://billing.example.test/.shpyrd/callback?code=") {
		t.Fatalf("gate open with a nonce = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	return strings.TrimPrefix(rec.Header().Get("Location"), "https://billing.example.test"), &http.Cookie{Name: nonce.Name, Value: nonce.Value}
}

// enter walks the gate's way in from a workspace's host and returns the gate
// cookie the gate's host set.
func (w *gateWorld) enter(t *testing.T, wsHost, sid string) *http.Cookie {
	t.Helper()
	cb, nonce := w.wayIn(t, wsHost, sid)
	rec := w.get(t, "billing.example.test", cb, "", nonce)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Fatalf("gate callback = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	var gate *http.Cookie
	for _, c := range rec.Result().Cookies() {
		switch c.Name {
		case w.s.gateCookieName():
			if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode {
				t.Errorf("gate cookie attributes: %+v", c)
			}
			gate = c
		case w.s.gateNonceCookieName():
			if c.MaxAge >= 0 {
				t.Errorf("the nonce cookie was not cleared: %+v", c)
			}
		}
	}
	if gate == nil {
		t.Fatal("no gate cookie set")
	}
	return gate
}

// setsCookie says whether a response sets a cookie of that name.
func setsCookie(rec *httptest.ResponseRecorder, name string) bool {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return true
		}
	}
	return false
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
	if claims["aud"] != "gate:billing" || claims["ws"] != "acme" || claims["project"] != "billing" || claims["realm"] != "workspace" || claims["operator"] != nil {
		t.Errorf("claims = %v", claims)
	}
	if roles := claims["roles"].([]any); len(roles) == 0 || roles[0] != "owner" {
		t.Errorf("roles = %v", claims["roles"])
	}
	if rec.Header().Get("X-Shpyrd-Email") != w.ana.Email {
		t.Errorf("X-Shpyrd-Email = %q", rec.Header().Get("X-Shpyrd-Email"))
	}
	cameFrom(t, rec, "acme", "false")
}

// cameFrom checks the headers that say where a visitor came from: an app
// behind a gate reads them before it trusts roles or teams, which mean
// something only in that workspace.
func cameFrom(t *testing.T, rec *httptest.ResponseRecorder, ws, operator string) {
	t.Helper()
	if got := rec.Header().Get("X-Shpyrd-Workspace"); got != ws {
		t.Errorf("X-Shpyrd-Workspace = %q, want %q", got, ws)
	}
	if got := rec.Header().Get("X-Shpyrd-Operator"); got != operator {
		t.Errorf("X-Shpyrd-Operator = %q, want %q", got, operator)
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
	cameFrom(t, rec, "platform", "true")
	// Dora, an owner there, enters by the same access and says the same.
	cookie = w.enter(t, "platform.shpyrd.test", w.session(t, "platform", w.dora))
	rec, claims = w.auth(t, "billing", "GET", cookie)
	if rec.Code != http.StatusOK || claims["operator"] != true || claims["ws"] != "platform" {
		t.Fatalf("auth as the owner = %d %v", rec.Code, claims)
	}
	cameFrom(t, rec, "platform", "true")
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
	cb, nonce := w.wayIn(t, "acme.shpyrd.test", w.session(t, "acme", w.ana))
	if rec := w.get(t, "billing.example.test:8443", cb, "", nonce); rec.Code != http.StatusFound {
		t.Fatalf("callback at the gate's host with a port = %d", rec.Code)
	}
	if rec := w.get(t, "billing.example.test", cb, "", nonce); rec.Code != http.StatusBadRequest {
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
	// A pass naming acme's slug with another workspace's ID opens nothing.
	acme, _ := w.st.Workspace(ctx, "acme")
	pass, err := w.s.edgeCodes.MintPass(ctx, "billing.example.test", edge.GateClaims{SessionID: w.session(t, "acme", w.ana), Workspace: "acme", WorkspaceID: acme.ID + "-old", Gate: "billing"})
	if err != nil {
		t.Fatal(err)
	}
	forged, _ := w.s.edgeKeys.SignGateCookie(edge.GateCookie{Pass: pass, Gate: "billing"})
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

// The gate's cookie reaches the app behind the gate, so it holds a pass,
// never the dashboard session it stands for.
func TestTheGateCookieDoesNotRevealTheSession(t *testing.T) {
	w := newGateWorld(t)
	sid := w.session(t, "acme", w.ana)
	cookie := w.enter(t, "acme.shpyrd.test", sid)
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 3 {
		t.Fatalf("gate cookie is not a signed token: %q", cookie.Value)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), sid) || strings.Contains(cookie.Value, sid) {
		t.Errorf("the gate cookie carries the session: %s", payload)
	}
	if rec, _ := w.auth(t, "billing", "GET", cookie); rec.Code != http.StatusOK {
		t.Errorf("auth = %d", rec.Code)
	}
}

// A callback link is bound to the browser that began the way in: handed to
// another browser, without the nonce cookie or with another one, it sets no
// cookie (login CSRF); a nonce that cannot be one is refused at the
// workspace's host.
func TestACallbackLinkWorksOnlyInTheBrowserThatBeganIt(t *testing.T) {
	w := newGateWorld(t)
	sid := w.session(t, "acme", w.ana)
	cb, nonce := w.wayIn(t, "acme.shpyrd.test", sid)
	rec := w.get(t, "billing.example.test", cb, "")
	if rec.Code != http.StatusBadRequest || setsCookie(rec, w.s.gateCookieName()) {
		t.Errorf("callback without the nonce cookie = %d %q", rec.Code, rec.Header().Values("Set-Cookie"))
	}
	cb, _ = w.wayIn(t, "acme.shpyrd.test", sid)
	other := &http.Cookie{Name: nonce.Name, Value: base64.RawURLEncoding.EncodeToString(make([]byte, 24))}
	rec = w.get(t, "billing.example.test", cb, "", other)
	if rec.Code != http.StatusBadRequest || setsCookie(rec, w.s.gateCookieName()) {
		t.Errorf("callback with another nonce cookie = %d %q", rec.Code, rec.Header().Values("Set-Cookie"))
	}
	for _, bad := range []string{"short", strings.Repeat("a", 33), strings.Repeat("!", 32)} {
		rec := w.get(t, "acme.shpyrd.test", "/.shpyrd/gate?"+url.Values{"name": {"billing"}, "nonce": {bad}}.Encode(), sid)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("gate open with nonce %q = %d %s", bad, rec.Code, rec.Header().Get("Location"))
		}
	}
}

// begin starts only from a ticket a workspace signed for this gate, and
// sends the browser back to that workspace's platform address, looked up
// then: no ticket, a forged, stale or other gate's ticket, or one for a
// workspace re-created under its slug, gets no nonce. At a host that is no
// gate's there is no begin.
func TestBeginStartsOnlyFromASignedTicketBackToTheWorkspacesAddress(t *testing.T) {
	w := newGateWorld(t)
	acme, err := w.st.Workspace(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	sign := func(b edge.GateBegin) string {
		v, err := w.s.edgeKeys.SignGateBegin(b)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	refused := map[string]string{
		"no ticket":                "",
		"a garbage ticket":         "not.a.ticket",
		"another gate's":           sign(edge.GateBegin{Workspace: "acme", WorkspaceID: acme.ID, Gate: "reports"}),
		"an expired ticket":        sign(edge.GateBegin{Workspace: "acme", WorkspaceID: acme.ID, Gate: "billing", ExpiresAt: time.Now().Add(-time.Second).Unix()}),
		"an older acme's":          sign(edge.GateBegin{Workspace: "acme", WorkspaceID: acme.ID + "-old", Gate: "billing"}),
		"a workspace that is none": sign(edge.GateBegin{Workspace: "nope", WorkspaceID: acme.ID, Gate: "billing"}),
	}
	for what, ticket := range refused {
		path := "/.shpyrd/begin"
		if ticket != "" {
			path += "?" + url.Values{"t": {ticket}}.Encode()
		}
		rec := w.get(t, "billing.example.test", path, "")
		if rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" || setsCookie(rec, w.s.gateNonceCookieName()) {
			t.Errorf("begin with %s = %d %s %q", what, rec.Code, rec.Header().Get("Location"), rec.Header().Values("Set-Cookie"))
		}
	}
	// The old unsigned parameter starts nothing.
	if rec := w.get(t, "billing.example.test", "/.shpyrd/begin?from=acme.shpyrd.test", ""); rec.Code != http.StatusBadRequest || setsCookie(rec, w.s.gateNonceCookieName()) {
		t.Errorf("begin with from = %d", rec.Code)
	}
	ok := sign(edge.GateBegin{Workspace: "acme", WorkspaceID: acme.ID, Gate: "billing"})
	rec := w.get(t, "billing.example.test", "/.shpyrd/begin?"+url.Values{"t": {ok}}.Encode(), "")
	back, _ := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusFound || !setsCookie(rec, w.s.gateNonceCookieName()) || back == nil || back.Host != acme.Address || back.Path != "/.shpyrd/gate" {
		t.Errorf("begin with a good ticket = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if rec := w.get(t, "acme.shpyrd.test", "/.shpyrd/begin?"+url.Values{"t": {ok}}.Encode(), ""); rec.Code != http.StatusNotFound || setsCookie(rec, w.s.gateNonceCookieName()) {
		t.Errorf("begin at a workspace's host = %d", rec.Code)
	}
}

// The way in runs only at the workspace's platform address, never at a
// host the request names: from any other host of the workspace the browser
// is sent to the address first, with no ticket signed, and a nonce brought
// to another host is refused.
func TestTheWayInRunsOnlyAtTheWorkspacesAddress(t *testing.T) {
	w := newGateWorld(t)
	sid := w.session(t, "acme", w.ana)
	rec := w.get(t, "x.acme.shpyrd.test", "/.shpyrd/gate?name=billing", sid)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://acme.shpyrd.test/.shpyrd/gate?name=billing" {
		t.Errorf("gate open at another host of acme = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	nonce := base64.RawURLEncoding.EncodeToString(make([]byte, 24))
	rec = w.get(t, "x.acme.shpyrd.test", "/.shpyrd/gate?"+url.Values{"name": {"billing"}, "nonce": {nonce}}.Encode(), sid)
	if rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
		t.Errorf("gate open with a nonce at another host of acme = %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

// The default workspace answers at its address too, and its people enter
// a gate from there like anyone's.
func TestAGateIsEnteredFromTheDefaultWorkspacesAddress(t *testing.T) {
	w := newGateWorld(t)
	owner := ext.Identity{Subject: "u-olga", Email: "olga@example.test", Provider: "local"}
	if _, err := w.st.PutMembership(context.Background(), store.DefaultWorkspace, owner.Email, store.WorkspaceRoleOwner); err != nil {
		t.Fatal(err)
	}
	cookie := w.enter(t, "example.test", w.session(t, store.DefaultWorkspace, owner))
	if rec, _ := w.auth(t, "billing", "GET", cookie); rec.Code != http.StatusOK {
		t.Errorf("auth = %d", rec.Code)
	}
}

// A host to send a browser to is a plain DNS name: lowercase labels of
// letters, digits and inner hyphens, at least two, nothing else.
func TestValidHostnameTakesOnlyPlainNames(t *testing.T) {
	for host, want := range map[string]bool{
		"acme.shpyrd.test":                true,
		"a.b":                             true,
		"x-1.example.test":                true,
		"localhost":                       false,
		"":                                false,
		"Acme.shpyrd.test":                false,
		"-acme.shpyrd.test":               false,
		"acme-.shpyrd.test":               false,
		"acme..shpyrd.test":               false,
		".acme.shpyrd.test":               false,
		"acme.shpyrd.test.":               false,
		"evil%2ecom/.acme.shpyrd.test":    false,
		"evil?.acme.shpyrd.test":          false,
		"evil/.acme.shpyrd.test":          false,
		"evil@acme.shpyrd.test":           false,
		"acme.shpyrd.test:443":            false,
		"acme_x.shpyrd.test":              false,
		strings.Repeat("a", 64) + ".test": false,
		strings.Repeat(strings.Repeat("a", 63)+".", 4) + "a": false,
		strings.Repeat(strings.Repeat("a", 62)+".", 4) + "a": true,
	} {
		if got := validHostname(host); got != want {
			t.Errorf("validHostname(%q) = %v, want %v", host, got, want)
		}
	}
}

// Signing out of a gate ends its pass: the same cookie, kept and replayed,
// opens nothing.
func TestSigningOutOfAGateEndsItsPass(t *testing.T) {
	w := newGateWorld(t)
	cookie := w.enter(t, "acme.shpyrd.test", w.session(t, "acme", w.ana))
	if rec, _ := w.auth(t, "billing", "GET", cookie); rec.Code != http.StatusOK {
		t.Fatalf("auth before signing out = %d", rec.Code)
	}
	if rec := w.get(t, "billing.example.test", "/.shpyrd/logout", "", cookie); rec.Code != http.StatusOK {
		t.Fatalf("logout = %d", rec.Code)
	}
	if rec, _ := w.auth(t, "billing", "GET", cookie); rec.Code != http.StatusUnauthorized {
		t.Errorf("auth with the cookie after signing out = %d", rec.Code)
	}
}

// /api/links is per person: acme's owner sees the extension's link, its
// member does not, and a workspace without a provider gets an empty list.
func TestLinksArePerPerson(t *testing.T) {
	w := newGateWorld(t)
	rec := w.get(t, "acme.shpyrd.test", "/api/links", w.session(t, "acme", w.ana))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"label":"Billing"`) || !strings.Contains(rec.Body.String(), `"section":"Cloud"`) {
		t.Errorf("owner's links = %d %s", rec.Code, rec.Body.String())
	}
	rec = w.get(t, "acme.shpyrd.test", "/api/links", w.session(t, "acme", w.carla))
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("member's links = %d %s", rec.Code, rec.Body.String())
	}
	if rec := w.get(t, "acme.shpyrd.test", "/api/links", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous links = %d", rec.Code)
	}
}

// Links asks no provider without an identity set.
func TestLinksWithoutIdentityAskNoProvider(t *testing.T) {
	w := newGateWorld(t)
	// Set the server to open mode so auth() lets requests through without identity.
	w.s.opts.Token = ""
	// Make a request with no session cookie, so no identity is set.
	rec := w.get(t, "acme.shpyrd.test", "/api/links", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("body = %s", rec.Body.String())
	}
	if w.ext.linkCalls != 0 {
		t.Errorf("linkCalls = %d, want 0", w.ext.linkCalls)
	}
}

// A platform on another HTTPS port keeps it on every hop of the way in:
// the gate's host is sent to with the port, as the workspace's is.
func TestTheWayInKeepsThePlatformsPort(t *testing.T) {
	w := newGateWorld(t)
	w.s.opts.Public.HTTPSPort = "8443"
	sid := w.session(t, "acme", w.ana)
	rec := w.get(t, "acme.shpyrd.test:8443", "/.shpyrd/gate?name=billing", sid)
	begin := rec.Header().Get("Location")
	if rec.Code != http.StatusFound || !strings.HasPrefix(begin, "https://billing.example.test:8443/.shpyrd/begin?t=") {
		t.Fatalf("gate open = %d %s", rec.Code, begin)
	}
	rec = w.get(t, "billing.example.test:8443", strings.TrimPrefix(begin, "https://billing.example.test:8443"), "")
	back := rec.Header().Get("Location")
	if rec.Code != http.StatusFound || !strings.HasPrefix(back, "https://acme.shpyrd.test:8443/.shpyrd/gate?") {
		t.Fatalf("gate begin = %d %s", rec.Code, back)
	}
	rec = w.get(t, "acme.shpyrd.test:8443", strings.TrimPrefix(back, "https://acme.shpyrd.test:8443"), sid)
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "https://billing.example.test:8443/.shpyrd/callback?code=") {
		t.Errorf("gate open with a nonce = %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

// A code of an app's kind is no gate's code, even at the gate's host, in
// the browser that holds a nonce, and even when it names the gate and
// carries that nonce: it sets no gate cookie.
func TestAnAppsCodeDoesNotOpenAGate(t *testing.T) {
	w := newGateWorld(t)
	ctx := context.Background()
	_, nonce := w.wayIn(t, "acme.shpyrd.test", w.session(t, "acme", w.ana))
	plain, err := w.s.edgeCodes.Mint(ctx, "billing.example.test", edge.CookieClaims{SessionID: w.session(t, "acme", w.ana), Project: "billing"})
	if err != nil {
		t.Fatal(err)
	}
	acme, _ := w.st.Workspace(ctx, "acme")
	dressed, err := w.s.edgeCodes.MintJSON(ctx, "billing.example.test", edge.KindEdge, edge.GateClaims{
		SessionID: w.session(t, "acme", w.ana), Workspace: "acme", WorkspaceID: acme.ID, Gate: "billing", Nonce: nonce.Value,
	})
	if err != nil {
		t.Fatal(err)
	}
	for what, code := range map[string]string{"an app's code": plain, "an app's code with a gate's claims": dressed} {
		rec := w.get(t, "billing.example.test", "/.shpyrd/callback?"+url.Values{"code": {code}}.Encode(), "", nonce)
		if rec.Code != http.StatusBadRequest || setsCookie(rec, w.s.gateCookieName()) {
			t.Errorf("%s at the gate = %d %q", what, rec.Code, rec.Header().Values("Set-Cookie"))
		}
	}
}

// One gate's cookie opens only that gate: at another gate it is refused.
// So is each part of one made over: a cookie signed for this gate around
// the other gate's pass, or a cookie signed for the other gate around a
// pass kept for this gate's host, or around one that names this gate.
func TestAGateCookieOpensOnlyItsGate(t *testing.T) {
	w := newGateWorld(t)
	ctx := context.Background()
	sid := w.session(t, "acme", w.ana)
	cookie := w.enter(t, "acme.shpyrd.test", sid)
	if rec, _ := w.auth(t, "billing", "GET", cookie); rec.Code != http.StatusOK {
		t.Fatalf("at its own gate = %d", rec.Code)
	}
	if rec, _ := w.auth(t, "reports", "GET", cookie); rec.Code != http.StatusUnauthorized {
		t.Errorf("billing's cookie at reports = %d", rec.Code)
	}
	acme, _ := w.st.Workspace(ctx, "acme")
	claims := edge.GateClaims{SessionID: sid, Workspace: "acme", WorkspaceID: acme.ID}
	rewrap := func(cookieGate, host, gate string) *http.Cookie {
		c := claims
		c.Gate = gate
		pass, err := w.s.edgeCodes.MintPass(ctx, host, c)
		if err != nil {
			t.Fatal(err)
		}
		v, err := w.s.edgeKeys.SignGateCookie(edge.GateCookie{Pass: pass, Gate: cookieGate})
		if err != nil {
			t.Fatal(err)
		}
		return &http.Cookie{Name: w.s.gateCookieName(), Value: v}
	}
	// A cookie signed for billing, though its pass is reports' own.
	if rec, _ := w.auth(t, "reports", "GET", rewrap("billing", "reports.example.test", "reports")); rec.Code != http.StatusUnauthorized {
		t.Errorf("a cookie of billing at reports = %d", rec.Code)
	}
	// A pass kept for billing's host, though it names reports.
	if rec, _ := w.auth(t, "reports", "GET", rewrap("reports", "billing.example.test", "reports")); rec.Code != http.StatusUnauthorized {
		t.Errorf("a pass of billing's host at reports = %d", rec.Code)
	}
	// A pass kept for reports' host, though it names billing.
	if rec, _ := w.auth(t, "reports", "GET", rewrap("reports", "reports.example.test", "billing")); rec.Code != http.StatusUnauthorized {
		t.Errorf("a pass naming billing at reports = %d", rec.Code)
	}
	// The same cookie, made right, opens reports: the refusals above are
	// the gate's, not Ana's.
	if rec, _ := w.auth(t, "reports", "GET", rewrap("reports", "reports.example.test", "reports")); rec.Code != http.StatusOK {
		t.Errorf("a pass of reports at reports = %d", rec.Code)
	}
}

// Taking access away closes the gate on the next request: Bruno's team
// losing its grant on the project, Ana leaving acme.
func TestTheGateClosesWhenAGrantOrAMembershipGoes(t *testing.T) {
	w := newGateWorld(t)
	ctx := context.Background()
	bruno := w.enter(t, "platform.shpyrd.test", w.session(t, "platform", w.bruno))
	ana := w.enter(t, "acme.shpyrd.test", w.session(t, "acme", w.ana))
	for who, c := range map[string]*http.Cookie{"Bruno": bruno, "Ana": ana} {
		if rec, _ := w.auth(t, "billing", "GET", c); rec.Code != http.StatusOK {
			t.Fatalf("%s before = %d", who, rec.Code)
		}
	}
	grants, err := w.st.ListProjectGrants(ctx, "platform", "billing")
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range grants {
		if err := w.st.DeleteGrant(ctx, "platform", g.ID); err != nil {
			t.Fatal(err)
		}
	}
	w.s.authz.Invalidate()
	if rec, _ := w.auth(t, "billing", "GET", bruno); rec.Code != http.StatusForbidden {
		t.Errorf("Bruno after his team's grant went = %d", rec.Code)
	}
	if err := w.st.DeleteMembership(ctx, "acme", w.ana.Email); err != nil {
		t.Fatal(err)
	}
	w.s.authz.Invalidate()
	if rec, _ := w.auth(t, "billing", "GET", ana); rec.Code != http.StatusForbidden {
		t.Errorf("Ana after leaving acme = %d", rec.Code)
	}
}

// The gate's rule is for people of other workspaces only: people of the
// gate's own workspace enter, and are checked on every request, without it
// ever being asked.
func TestTheGatesRuleIsNeverAskedAtHome(t *testing.T) {
	w := newGateWorld(t)
	for _, id := range []ext.Identity{w.bruno, w.dora} {
		cookie := w.enter(t, "platform.shpyrd.test", w.session(t, "platform", id))
		for range 2 {
			if rec, _ := w.auth(t, "billing", "GET", cookie); rec.Code != http.StatusOK {
				t.Fatalf("%s = %d", id.Email, rec.Code)
			}
		}
	}
	if w.ext.admitCalls != 0 {
		t.Errorf("the rule was asked %d times at home", w.ext.admitCalls)
	}
	// It is asked for someone of another workspace, so the count is real.
	w.enter(t, "acme.shpyrd.test", w.session(t, "acme", w.ana))
	if w.ext.admitCalls == 0 {
		t.Error("the rule was never asked for acme's owner")
	}
}

// A visitor whose workspace is suspended after they entered is refused at
// the gate as suspended.
func TestASuspendedWorkspaceClosesItsGates(t *testing.T) {
	w := newGateWorld(t)
	cookie := w.enter(t, "acme.shpyrd.test", w.session(t, "acme", w.ana))
	if _, err := w.st.SetWorkspaceStatus(context.Background(), "acme", store.WorkspaceSuspended); err != nil {
		t.Fatal(err)
	}
	w.s.authz.Invalidate()
	rec, _ := w.auth(t, "billing", "GET", cookie)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "suspended") {
		t.Errorf("after acme was suspended = %d %s", rec.Code, rec.Body.String())
	}
}
