package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/edge"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// A workspace with three projects: crm lets expenses in, and nobody else.
func internalNamesServer(t *testing.T) *Server {
	t.Helper()
	crm := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "crm", Namespace: "app-crm"}, Spec: shpyrdv1.AppSpec{Allow: []shpyrdv1.AllowEntry{{Project: "expenses"}, {Platform: "mcp"}}}}
	expenses := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "expenses", Namespace: "app-expenses"}}
	shop := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop"}}
	s, _ := newTestServer(t, nil, []client.Object{crm, expenses, shop})
	s.callers = callerMap{
		"10.1.0.5": {Workspace: store.DefaultWorkspace, Project: "expenses"},
		"10.1.0.9": {Workspace: store.DefaultWorkspace, Project: "shop"},
		"10.1.0.7": {Workspace: "other", Project: "expenses"},
		"10.2.0.1": {Platform: true},
	}
	return s
}

// serviceAuthRequest is the auth subrequest of crm's internal Ingress for a
// call from ip, as nginx sends it.
func serviceAuthRequest(t *testing.T, s *Server, project, ip string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/edge/auth?project="+project+"&workspace=default&realm=service", nil)
	req.Header.Set("X-Real-IP", ip)
	req.Header.Set("X-Original-URL", "http://"+project+".default.internal/api/customers")
	req.Header.Set("X-Shpyrd-User", "forged@acme.test") // a client's own headers never survive
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestInternalNameAuthAdmitsTheProjectsLetIn(t *testing.T) {
	s := internalNamesServer(t)
	ctx := context.Background()

	// expenses calls crm: in, as svc:default/expenses, with a service JWT.
	rec := serviceAuthRequest(t, s, "crm", "10.1.0.5", nil)
	if rec.Code != http.StatusOK || rec.Header().Get(headerService) != "svc:default/expenses" || rec.Header().Get(headerCallerProject) != "expenses" {
		t.Fatalf("expenses → crm = %d %v %s", rec.Code, rec.Header(), rec.Body.String())
	}
	if rec.Header().Get("X-Shpyrd-User") != "" || rec.Header().Get("X-Shpyrd-Email") != "" {
		t.Errorf("a service call carries no person: %v", rec.Header())
	}
	var claims edge.Claims
	tok := strings.TrimPrefix(rec.Header().Get("Authorization"), "Bearer ")
	if err := s.edgeKeys.Verify(tok, "JWT", &claims); err != nil {
		t.Fatal(err)
	}
	ws, _ := s.store.Workspace(ctx, store.DefaultWorkspace)
	if claims.Realm != "service" || claims.Subject != "svc:default/expenses" || claims.Audience != "crm" || claims.Project != "crm" || claims.Workspace != "default" || claims.WorkspaceID != ws.ID || claims.Issuer != s.dashboardURLOf(ws) || claims.Email != "" {
		t.Errorf("claims = %+v", claims)
	}
	if claims.ExpiresAt-claims.IssuedAt != int64(edge.TokenTTL.Seconds()) || len(claims.Roles) != 0 || len(claims.Teams) != 0 {
		t.Errorf("a service token lives five minutes and holds no roles or teams: %+v", claims)
	}
	if got := testutil.ToFloat64(serviceCalls.WithLabelValues("default", "crm", "expenses")); got != 1 {
		t.Errorf("calls in = %v, want 1", got)
	}

	// shop is not listed: refused, and counted as a service denial.
	before := testutil.ToFloat64(edgeDenials.WithLabelValues("default", "crm", denialService))
	rec = serviceAuthRequest(t, s, "crm", "10.1.0.9", nil)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "crm does not let shop in") || rec.Header().Get(headerService) != "" {
		t.Errorf("shop → crm = %d %s %v", rec.Code, rec.Body.String(), rec.Header())
	}
	if got := testutil.ToFloat64(edgeDenials.WithLabelValues("default", "crm", denialService)); got != before+1 {
		t.Errorf("denials = %v, want %v", got, before+1)
	}
	// An address of no project: refused, named.
	if rec = serviceAuthRequest(t, s, "crm", "10.9.9.9", nil); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "10.9.9.9 is not one of their addresses") {
		t.Errorf("stranger → crm = %d %s", rec.Code, rec.Body.String())
	}
	// No address at all (nothing in front of the server): refused.
	if rec = serviceAuthRequest(t, s, "crm", "", nil); rec.Code != http.StatusForbidden {
		t.Errorf("no address → crm = %d", rec.Code)
	}
	// Another workspace's expenses, at a name of this workspace: never.
	if rec = serviceAuthRequest(t, s, "crm", "10.1.0.7", nil); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "another workspace") {
		t.Errorf("other/expenses → crm = %d %s", rec.Code, rec.Body.String())
	}
	// A project that is not there.
	if rec = serviceAuthRequest(t, s, "billing", "10.1.0.5", nil); rec.Code != http.StatusNotFound {
		t.Errorf("expenses → billing = %d %s", rec.Code, rec.Body.String())
	}
	// The platform's server by itself is not a project; with a ticket it
	// speaks for the project it forwarded, and a forged ticket says nothing.
	if rec = serviceAuthRequest(t, s, "crm", "10.2.0.1", nil); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "qualified name") {
		t.Errorf("platform → crm = %d %s", rec.Code, rec.Body.String())
	}
	ticket, _ := s.edgeKeys.SignCallerTicket(edge.CallerTicket{Workspace: "default", Project: "expenses"})
	if rec = serviceAuthRequest(t, s, "crm", "10.2.0.1", map[string]string{headerCallerTicket: ticket}); rec.Code != http.StatusOK || rec.Header().Get(headerService) != "svc:default/expenses" {
		t.Errorf("platform with expenses' ticket → crm = %d %s", rec.Code, rec.Body.String())
	}
	if rec = serviceAuthRequest(t, s, "crm", "10.2.0.1", map[string]string{headerCallerTicket: "eyJ.forged.x"}); rec.Code != http.StatusForbidden {
		t.Errorf("platform with a forged ticket → crm = %d", rec.Code)
	}
	// A ticket from a project's own pod changes nothing: shop stays shop.
	if rec = serviceAuthRequest(t, s, "crm", "10.1.0.9", map[string]string{headerCallerTicket: ticket}); rec.Code != http.StatusForbidden {
		t.Errorf("shop with expenses' ticket → crm = %d", rec.Code)
	}
}

func TestBareInternalNameIsForwardedToTheCallersWorkspace(t *testing.T) {
	s := internalNamesServer(t)
	type seen struct {
		host, ticket, service, path string
	}
	var got seen
	door := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = seen{host: r.Host, ticket: r.Header.Get(headerCallerTicket), service: r.Header.Get(headerService), path: r.URL.RequestURI()}
		w.Header().Set("X-Answered-By", "crm")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer door.Close()
	u, _ := url.Parse(door.URL)
	s.internalDoorFn = func() string { return u.Host }

	call := func(host, ip string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/customers?page=2", strings.NewReader(`{"name":"ACME"}`))
		req.Host = host
		req.Header.Set("X-Real-IP", ip)
		req.Header.Set(headerService, "svc:default/forged") // dropped on the way
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(notifyingRecorder{rec}, req)
		return rec
	}
	// expenses calls crm.internal: on to crm.default.internal with a ticket
	// naming expenses; the answer comes back as the app gave it.
	rec := call("crm.internal", "10.1.0.5")
	if rec.Code != http.StatusCreated || rec.Header().Get("X-Answered-By") != "crm" || rec.Body.String() != `{"ok":true}` {
		t.Fatalf("forwarded = %d %v %s", rec.Code, rec.Header(), rec.Body.String())
	}
	if got.host != "crm.default.internal" || got.path != "/api/customers?page=2" || got.service != "" {
		t.Errorf("the door saw %+v", got)
	}
	if tk, err := s.edgeKeys.VerifyCallerTicket(got.ticket); err != nil || tk.Project != "expenses" || tk.Workspace != "default" {
		t.Errorf("ticket = %+v, %v", tk, err)
	}
	// Nobody the index knows: refused here, nothing forwarded.
	got = seen{}
	if rec = call("crm.internal", "10.9.9.9"); rec.Code != http.StatusForbidden || got.host != "" || !strings.Contains(rec.Header().Get("Content-Type"), "json") {
		t.Errorf("stranger = %d %s, door saw %+v", rec.Code, rec.Body.String(), got)
	}
	// The platform has no workspace to fill in.
	if rec = call("crm.internal", "10.2.0.1"); rec.Code != http.StatusForbidden || got.host != "" {
		t.Errorf("platform = %d %s", rec.Code, rec.Body.String())
	}
	// A qualified name reaching the server went through no Ingress: no
	// such project. A name that is not one is said so.
	if rec = call("billing.default.internal", "10.1.0.5"); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "no project billing") {
		t.Errorf("billing.default.internal = %d %s", rec.Code, rec.Body.String())
	}
	if rec = call("a.b.c.internal", "10.1.0.5"); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "not an internal name") {
		t.Errorf("a.b.c.internal = %d %s", rec.Code, rec.Body.String())
	}
	// The door not answering is a 502 in JSON, not a hang.
	door.Close()
	if rec = call("crm.internal", "10.1.0.5"); rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "did not answer") {
		t.Errorf("door down = %d %s", rec.Code, rec.Body.String())
	}
}

func TestInternalNameRefusalIsSaidInJSON(t *testing.T) {
	s := internalNamesServer(t)
	errorPage := func(host, code, ip string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/customers", nil)
		req.Host = host
		req.Header.Set("X-Code", code)
		req.Header.Set("X-Format", "*/*")
		req.Header.Set("X-Forwarded-For", ip)
		req.Header.Set("X-Real-IP", "10.1.0.5") // a client's own header: not what nginx recorded
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}
	rec := errorPage("crm.default.internal", "403", "10.1.0.9")
	var body map[string]any
	if rec.Code != http.StatusForbidden || json.Unmarshal(rec.Body.Bytes(), &body) != nil {
		t.Fatalf("403 page = %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(body["error"].(string), "crm does not let shop in") || body["caller"] != "svc:default/shop" || body["project"] != "crm" {
		t.Errorf("403 body = %v", body)
	}
	if rec = errorPage("crm.default.internal", "503", "10.1.0.5"); rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" || !strings.Contains(rec.Body.String(), "crm is not answering") {
		t.Errorf("503 page = %d %v %s", rec.Code, rec.Header(), rec.Body.String())
	}
	// The maintenance page is served on an internal host like on any other.
	req := httptest.NewRequest("GET", maintenancePath, nil)
	req.Host = "crm.default.internal"
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound && strings.Contains(rec.Body.String(), "internal name") {
		t.Errorf("maintenance on an internal host = %d %s", rec.Code, rec.Body.String())
	}
}

func TestPodIndexFollowsTheProjectNamespaces(t *testing.T) {
	ns := func(name string, labels map[string]string) *corev1.Namespace {
		return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
	}
	pod := func(ns, name, ip string, labels map[string]string) *corev1.Pod {
		p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels}}
		p.Status.PodIP = ip
		p.Status.PodIPs = []corev1.PodIP{{IP: ip}}
		return p
	}
	cs := kubefake.NewSimpleClientset(
		ns("app-acme-crm", map[string]string{shpyrdv1.LabelProject: "crm", shpyrdv1.LabelWorkspace: "acme", shpyrdv1.LabelManagedBy: "shpyrd"}),
		ns("app-expenses", map[string]string{shpyrdv1.LabelProject: "expenses"}), // before workspaces: the implicit one
		ns("monitoring", nil),
		pod("app-acme-crm", "web-1", "10.1.0.5", map[string]string{"shpyrd.io/app": "crm"}),
		pod("app-expenses", "worker-1", "10.1.0.6", nil),
		pod("monitoring", "prometheus-0", "10.1.0.8", nil),
		pod("shpyrd-system", "shpyrd-server-abc", "10.2.0.1", map[string]string{"app.kubernetes.io/name": "shpyrd-server"}),
		pod("shpyrd-system", "registry-0", "10.2.0.2", map[string]string{"app.kubernetes.io/name": "registry"}),
	)
	x := newPodIndex(cs, "shpyrd-system")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	x.Run(ctx)

	want := map[string]serviceCaller{
		"10.1.0.5": {Workspace: "acme", Project: "crm"},
		"10.1.0.6": {Workspace: "default", Project: "expenses"},
		"10.2.0.1": {Platform: true},
	}
	for ip, c := range want {
		if got, ok := x.Lookup(ip); !ok || got != c {
			t.Errorf("%s = %+v %v, want %+v", ip, got, ok, c)
		}
	}
	for _, ip := range []string{"10.1.0.8", "10.2.0.2", "10.3.0.1"} {
		if got, ok := x.Lookup(ip); ok {
			t.Errorf("%s = %+v, want nobody: a platform pod that is not the server, or an unknown address", ip, got)
		}
	}
	// A pod that goes takes its address with it; a new one brings its own.
	if err := cs.CoreV1().Pods("app-acme-crm").Delete(ctx, "web-1", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.CoreV1().Pods("app-expenses").Create(ctx, pod("app-expenses", "web-2", "10.1.0.5", nil), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, ok := x.Lookup("10.1.0.5")
		if ok && got.Project == "expenses" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("10.1.0.5 = %+v %v after the pods changed", got, ok)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// notifyingRecorder is a recorder with the close notification gin's writer
// promises the reverse proxy; a real connection has it.
type notifyingRecorder struct{ *httptest.ResponseRecorder }

func (notifyingRecorder) CloseNotify() <-chan bool { return make(chan bool) }
