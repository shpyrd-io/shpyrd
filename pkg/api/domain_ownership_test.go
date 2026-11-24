package api

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/store"
	"github.com/shpyrd-io/shpyrd/pkg/tenancy"
)

// The ownership rule for the hosts a project serves, on every path that
// writes spec.domains: create, deploy and the domains endpoint. Workspace
// alpha has verified example.com, a moved address that has not expired and
// a project serving taken.example.org; workspace beta has verified
// example.net.

// flakyStore fails the lookups the ownership rule makes while down is set.
type flakyStore struct {
	store.Store
	down atomic.Bool
}

var errStoreDown = errors.New("store down")

func (f *flakyStore) WorkspaceByAddress(ctx context.Context, address string) (*store.Workspace, error) {
	if f.down.Load() {
		return nil, errStoreDown
	}
	return f.Store.WorkspaceByAddress(ctx, address)
}

func (f *flakyStore) WorkspaceByHost(ctx context.Context, host string) (*store.Workspace, *store.WorkspaceHost, error) {
	if f.down.Load() {
		return nil, nil, errStoreDown
	}
	return f.Store.WorkspaceByHost(ctx, host)
}

type ownershipFixture struct {
	s  *Server
	cr client.Client
	st *flakyStore
}

const (
	alphaAt = "alpha.ws.example.org"
	betaAt  = "beta.ws.example.org"
	// alphaMoved is the address alpha moved from; its redirect has not expired.
	alphaMoved = "oldalpha.ws.example.org"
)

func newOwnershipFixture(t *testing.T) *ownershipFixture {
	t.Helper()
	ctx := context.Background()
	st := &flakyStore{Store: principalFixtureStore()}
	if _, err := st.UpdateWorkspaceAddress(ctx, store.DefaultWorkspace, "platform.ws.example.org"); err != nil {
		t.Fatal(err)
	}
	for _, w := range []store.Workspace{
		{Slug: "alpha", Name: "Alpha", Address: alphaAt},
		{Slug: "beta", Name: "Beta", Address: betaAt},
	} {
		if _, err := st.CreateWorkspace(ctx, w); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	soon := now.Add(24 * time.Hour)
	for ws, h := range map[string]store.WorkspaceHost{
		"alpha": {Host: "example.com", Kind: store.HostCustom, VerifiedAt: &now},
		"beta":  {Host: "example.net", Kind: store.HostCustom, VerifiedAt: &now},
	} {
		if _, err := st.PutWorkspaceHost(ctx, ws, h); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.PutWorkspaceHost(ctx, "alpha", store.WorkspaceHost{Host: alphaMoved, Kind: store.HostMoved, ExpiresAt: &soon}); err != nil {
		t.Fatal(err)
	}
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	app := func(ws, slug string, domains ...string) *shpyrdv1.App {
		return &shpyrdv1.App{
			ObjectMeta: metav1.ObjectMeta{Name: slug, Namespace: "app-" + ws + "-" + slug, Labels: project.NamespaceLabels(ws, slug)},
			Spec:       shpyrdv1.AppSpec{Access: shpyrdv1.AccessAuthenticated, Image: "registry.example.com/" + slug + ":1", Domains: domains},
		}
	}
	routed := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "routed", Namespace: "app-alpha-routed", Labels: map[string]string{shpyrdv1.LabelApp: "routed"}},
		Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{Host: "routed.example.org"}}}}
	cr := crfake.NewClientBuilder().WithScheme(scheme).WithObjects(
		app("alpha", "site", "taken.example.org"),
		app("alpha", "routed"), routed,
		app("beta", "shop", "keep.example.net"),
		app("beta", "other", "other.example.net"),
	).WithStatusSubresource(&shpyrdv1.App{}).Build()
	k := &kube.Client{Kube: kubefake.NewSimpleClientset(), Namespace: "shpyrd-system"}
	layout := tenancy.Layout{WorkspacesDomain: "ws.example.org", AppsDomain: "apps.example.org"}
	public := PublicConfig{Domain: "platform.example.org", DashboardURL: "https://console.example.org"}
	s, err := newServer(k, Options{
		Token: testToken, Apps: cr, Store: st, Public: public, Layout: layout,
		Tenancy:      &tenancy.ByAddress{Store: st, Domain: public.Domain, ConsoleHost: "console.example.org", Layout: layout},
		Capabilities: []string{"workspaces"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.authz.TTL = 1
	return &ownershipFixture{s: s, cr: cr, st: st}
}

// app is the App of a project, found by its workspace and slug whatever
// its name.
func (f *ownershipFixture) app(t *testing.T, ws, slug string) *shpyrdv1.App {
	t.Helper()
	var list shpyrdv1.AppList
	if err := f.cr.List(context.Background(), &list); err != nil {
		t.Fatal(err)
	}
	for i := range list.Items {
		if workspaceOf(&list.Items[i]) == ws && project.SlugOf(&list.Items[i]) == slug {
			return &list.Items[i]
		}
	}
	t.Fatalf("no project %s/%s", ws, slug)
	return nil
}

// beta makes a request at beta's address.
func (f *ownershipFixture) beta(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return at(t, f.s, betaAt, method, path, body)
}

func (f *ownershipFixture) projectCount(t *testing.T) int {
	t.Helper()
	var list shpyrdv1.AppList
	if err := f.cr.List(context.Background(), &list); err != nil {
		t.Fatal(err)
	}
	return len(list.Items)
}

// Hosts beta may not take, each of them alpha's by the ownership rule or
// held by a project of alpha.
var hostsOfAlpha = []string{
	"example.com", "a.example.com", "a.b.example.com", // verified custom host, at any depth
	alphaMoved, "x." + alphaMoved, // moved address, not expired
	alphaAt, "a.b." + alphaAt, // address, two labels under it
	"taken.example.org",  // a custom domain of alpha's project
	"routed.example.org", // an Ingress rule of alpha's project, no domain in its spec
}

func TestDomainOwnershipCreate(t *testing.T) {
	f := newOwnershipFixture(t)
	before := f.projectCount(t)
	for _, host := range hostsOfAlpha {
		rec := f.beta(t, "POST", "/api/projects", `{"name":"fresh","slug":"fresh","domains":["`+host+`"]}`)
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), strings.ToLower(host)) {
			t.Errorf("create with %s = %d %s, want 409 naming the host", host, rec.Code, rec.Body.String())
		}
	}
	if n := f.projectCount(t); n != before {
		t.Errorf("projects = %d, want %d: a refused create leaves nothing behind", n, before)
	}
	var ns corev1.NamespaceList
	if err := f.cr.List(context.Background(), &ns); err != nil || len(ns.Items) != 0 {
		t.Errorf("namespaces = %d, %v, want none", len(ns.Items), err)
	}

	// Beta's own verified domain, below it, and a host nobody owns.
	for _, host := range []string{"example.net", "a.example.net", "a.b.example.net", "free.example.org"} {
		slug := "p" + strings.NewReplacer(".", "-").Replace(host)
		rec := f.beta(t, "POST", "/api/projects", `{"name":"`+slug+`","slug":"`+slug+`","domains":["`+host+`"]}`)
		if rec.Code != http.StatusCreated {
			t.Errorf("create with %s = %d %s, want 201", host, rec.Code, rec.Body.String())
		}
	}

	// Syntax and the platform's names are 400; accepted hosts are stored
	// normalised and once.
	for _, host := range []string{"*.example.net", "not a host", "bad_host.example.net", "console.example.org", "platform.example.org", "auth.platform.example.org", betaAt, "x.apps.example.org"} {
		rec := f.beta(t, "POST", "/api/projects", `{"name":"bad","slug":"bad","domains":["`+host+`"]}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("create with %q = %d %s, want 400", host, rec.Code, rec.Body.String())
		}
	}
	rec := f.beta(t, "POST", "/api/projects", `{"name":"tidy","slug":"tidy","domains":["WWW.Tidy.Example.Net.","www.tidy.example.net","b.tidy.example.net"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create tidy = %d %s", rec.Code, rec.Body.String())
	}
	if got := f.app(t, "beta", "tidy").Spec.Domains; strings.Join(got, ",") != "www.tidy.example.net,b.tidy.example.net" {
		t.Errorf("stored domains = %v", got)
	}
}

func TestDomainOwnershipDeploy(t *testing.T) {
	f := newOwnershipFixture(t)
	const path = "/api/projects/shop/deploy"
	was := f.app(t, "beta", "shop").Spec
	for _, host := range hostsOfAlpha {
		rec := f.beta(t, "POST", path, `{"image":"registry.example.com/shop:2","env":[{"name":"A","value":"b"}],"domains":["keep.example.net","`+host+`"]}`)
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), strings.ToLower(host)) {
			t.Errorf("deploy with %s = %d %s, want 409 naming the host", host, rec.Code, rec.Body.String())
		}
	}
	now := f.app(t, "beta", "shop").Spec
	if now.Image != was.Image || len(now.Env) != 0 || strings.Join(now.Domains, ",") != "keep.example.net" {
		t.Errorf("a refused deploy changed the App: image %q env %v domains %v", now.Image, now.Env, now.Domains)
	}

	// Beta's own domain at any depth, and a free host, beside the one it holds.
	rec := f.beta(t, "POST", path, `{"image":"registry.example.com/shop:3","domains":["keep.example.net","example.net","a.example.net","a.b.example.net","free.example.org"]}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("deploy with beta's hosts = %d %s", rec.Code, rec.Body.String())
	}
	if got := f.app(t, "beta", "shop").Spec.Domains; len(got) != 5 {
		t.Errorf("domains = %v", got)
	}

	// Wildcards, malformed names and the platform's: 400, nothing changes.
	for _, host := range []string{"*.example.net", "not a host", "console.example.org", "platform.example.org"} {
		rec := f.beta(t, "POST", path, `{"image":"registry.example.com/shop:4","domains":["keep.example.net","`+host+`"]}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("deploy with %q = %d %s, want 400", host, rec.Code, rec.Body.String())
		}
	}
	if got := f.app(t, "beta", "shop").Spec.Image; got != "registry.example.com/shop:3" {
		t.Errorf("image = %q after refused deploys", got)
	}

	// Normalised, without duplicates.
	rec = f.beta(t, "POST", path, `{"image":"registry.example.com/shop:6","domains":["Zed.Example.Net.","zed.example.net","keep.example.net"]}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("deploy = %d %s", rec.Code, rec.Body.String())
	}
	if got := f.app(t, "beta", "shop").Spec.Domains; strings.Join(got, ",") != "zed.example.net,keep.example.net" {
		t.Errorf("domains = %v", got)
	}
}

// A deploy that holds the same hosts leaves the list as it is, in whatever
// capitals and order; a deploy without domains leaves it alone.
func TestDomainOwnershipDeployResendsTheList(t *testing.T) {
	f := newOwnershipFixture(t)
	shop := f.app(t, "beta", "shop")
	shop.Spec.Domains = []string{"one.example.net", "two.example.net"}
	if err := f.cr.Update(context.Background(), shop); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"image":"registry.example.com/shop:4","domains":["one.example.net","two.example.net"]}`,
		`{"image":"registry.example.com/shop:4","domains":["TWO.example.net.","One.Example.Net"]}`,
		`{"image":"registry.example.com/shop:5"}`,
	} {
		if rec := f.beta(t, "POST", "/api/projects/shop/deploy", body); rec.Code != http.StatusAccepted {
			t.Fatalf("deploy %s = %d %s", body, rec.Code, rec.Body.String())
		}
		if got := f.app(t, "beta", "shop").Spec.Domains; strings.Join(got, ",") != "one.example.net,two.example.net" {
			t.Errorf("after %s domains = %v, want them as they were", body, got)
		}
	}
}

func TestDomainOwnershipAddDomain(t *testing.T) {
	f := newOwnershipFixture(t)
	for _, host := range hostsOfAlpha {
		rec := f.beta(t, "POST", "/api/projects/shop/domains", `{"host":"`+host+`"}`)
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), strings.ToLower(host)) {
			t.Errorf("add %s = %d %s, want 409 naming the host", host, rec.Code, rec.Body.String())
		}
	}
	if got := f.app(t, "beta", "shop").Spec.Domains; strings.Join(got, ",") != "keep.example.net" {
		t.Errorf("domains = %v after refused adds", got)
	}
	for _, host := range []string{"example.net", "a.example.net", "a.b.example.net", "free.example.org"} {
		if rec := f.beta(t, "POST", "/api/projects/shop/domains", `{"host":"`+host+`"}`); rec.Code != http.StatusOK {
			t.Errorf("add %s = %d %s, want 200", host, rec.Code, rec.Body.String())
		}
	}
	// Already the project's: the handler's own answer.
	if rec := f.beta(t, "POST", "/api/projects/shop/domains", `{"host":"KEEP.example.net"}`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "already a domain") {
		t.Errorf("add a held host = %d %s", rec.Code, rec.Body.String())
	}
}

// A host the App already holds stays, whatever the rules say now: only a
// write that adds a host is judged.
func TestDomainOwnershipHeldHostsStay(t *testing.T) {
	f := newOwnershipFixture(t)
	shop := f.app(t, "beta", "shop")
	shop.Spec.Domains = []string{"keep.example.net", "legacy.example.com"} // alpha's by the rule
	shop.Spec.Processes = map[string]shpyrdv1.Process{"web": {}}
	if err := f.cr.Update(context.Background(), shop); err != nil {
		t.Fatal(err)
	}
	if rec := f.beta(t, "POST", "/api/projects/shop/deploy", `{"image":"registry.example.com/shop:2","domains":["legacy.example.com","keep.example.net"]}`); rec.Code != http.StatusAccepted {
		t.Errorf("a deploy re-sending the list = %d %s", rec.Code, rec.Body.String())
	}
	if got := f.app(t, "beta", "shop").Spec.Domains; strings.Join(got, ",") != "keep.example.net,legacy.example.com" {
		t.Errorf("domains = %v", got)
	}
	if rec := f.beta(t, "POST", "/api/projects/shop/deploy", `{"image":"registry.example.com/shop:2","domains":["legacy.example.com","keep.example.net","a.example.com"]}`); rec.Code != http.StatusConflict {
		t.Errorf("a deploy adding another host of alpha = %d %s, want 409", rec.Code, rec.Body.String())
	}
	if rec := f.beta(t, "POST", "/api/projects/shop/scale", `{"process":"web","replicas":2}`); rec.Code != http.StatusOK {
		t.Errorf("scale = %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.beta(t, "PUT", "/api/projects/shop/secrets", `{"set":{"A":"b"},"kind":"variable"}`); rec.Code != http.StatusOK {
		t.Errorf("set a variable = %d %s", rec.Code, rec.Body.String())
	}
	if got := f.app(t, "beta", "shop").Spec.Domains; len(got) != 2 {
		t.Errorf("domains = %v after scale and variable", got)
	}
	if rec := f.beta(t, "DELETE", "/api/projects/shop/domains/legacy.example.com", ""); rec.Code != http.StatusOK {
		t.Errorf("remove = %d %s", rec.Code, rec.Body.String())
	}
	if got := f.app(t, "beta", "shop").Spec.Domains; strings.Join(got, ",") != "keep.example.net" {
		t.Errorf("domains = %v after remove", got)
	}
}

// A host in the spec of another project, or in an Ingress of one, is held:
// refused at once, naming the project only when it is in the workspace.
func TestDomainOwnershipHeldByAnotherProject(t *testing.T) {
	f := newOwnershipFixture(t)
	// In another workspace: no slug, and the Ingress of a project with no
	// domain in its spec the same.
	for _, host := range []string{"taken.example.org", "routed.example.org"} {
		rec := f.beta(t, "POST", "/api/projects/shop/domains", `{"host":"`+host+`"}`)
		if rec.Code != http.StatusConflict {
			t.Fatalf("add %s = %d %s", host, rec.Code, rec.Body.String())
		}
		for _, leak := range []string{"site", "routed", "alpha"} {
			if strings.Contains(strings.ReplaceAll(rec.Body.String(), host, ""), leak) {
				t.Errorf("the refusal for %s names %q: %s", host, leak, rec.Body.String())
			}
		}
	}
	// In the same workspace: the project is named.
	rec := f.beta(t, "POST", "/api/projects/shop/domains", `{"host":"other.example.net"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "project other") {
		t.Errorf("add a host of beta's other project = %d %s", rec.Code, rec.Body.String())
	}
	// The project's own Ingress is not a conflict with itself.
	own := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-beta-shop"},
		Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{Host: "mine.example.org"}}}}
	if err := f.cr.Create(context.Background(), own); err != nil {
		t.Fatal(err)
	}
	if rec := f.beta(t, "POST", "/api/projects/shop/domains", `{"host":"mine.example.org"}`); rec.Code != http.StatusOK {
		t.Errorf("add a host of the project's own Ingress = %d %s", rec.Code, rec.Body.String())
	}
	// Create is judged the same way.
	if rec := f.beta(t, "POST", "/api/projects", `{"name":"n","slug":"n","domains":["other.example.net"]}`); rec.Code != http.StatusConflict {
		t.Errorf("create with a host held by another project = %d %s", rec.Code, rec.Body.String())
	}
}

// With the store failing, a new host is neither accepted nor written.
func TestDomainOwnershipStoreFailure(t *testing.T) {
	f := newOwnershipFixture(t)
	before := f.projectCount(t)
	f.st.down.Store(true)
	// The door itself needs the store: resolve the workspace first, then
	// fail only the rule's lookups.
	f.s.tenancy = &tenancy.ByAddress{Store: f.st.Store, Domain: "platform.example.org", ConsoleHost: "console.example.org", Layout: f.s.opts.Layout}
	for name, call := range map[string]func() *httptest.ResponseRecorder{
		"create": func() *httptest.ResponseRecorder {
			return f.beta(t, "POST", "/api/projects", `{"name":"late","slug":"late","domains":["new.example.org"]}`)
		},
		"deploy": func() *httptest.ResponseRecorder {
			return f.beta(t, "POST", "/api/projects/shop/deploy", `{"image":"registry.example.com/shop:9","domains":["keep.example.net","new.example.org"]}`)
		},
		"add": func() *httptest.ResponseRecorder {
			return f.beta(t, "POST", "/api/projects/shop/domains", `{"host":"new.example.org"}`)
		},
	} {
		if rec := call(); rec.Code < 500 || rec.Code > 599 {
			t.Errorf("%s with the store down = %d %s, want 5xx", name, rec.Code, rec.Body.String())
		}
	}
	if n := f.projectCount(t); n != before {
		t.Errorf("projects = %d, want %d", n, before)
	}
	shop := f.app(t, "beta", "shop")
	if strings.Join(shop.Spec.Domains, ",") != "keep.example.net" || shop.Spec.Image != "registry.example.com/shop:1" {
		t.Errorf("shop changed with the store down: %+v", shop.Spec)
	}
}

// The edge's index: a host that belongs to a workspace is its app's, a
// project's listing of it changes nothing, and a name under a workspace's
// domain is not served for another workspace's app.
func TestDomainOwnershipEdgeIndex(t *testing.T) {
	f := newOwnershipFixture(t)
	ctx := context.Background()
	lookup := func(host string) *shpyrdv1.App {
		f.s.hostCache.at = time.Time{}
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("GET", "https://"+host+"/", nil).WithContext(ctx)
		app, err := f.s.appByHost(c, host)
		if err != nil {
			return nil
		}
		return app
	}
	// alpha's app "site" answers at site.example.com (alpha's verified domain).
	if app := lookup("site.example.com"); app == nil || workspaceOf(app) != "alpha" {
		t.Fatalf("site.example.com = %v, want alpha's site", app)
	}
	shop := f.app(t, "beta", "shop")
	shop.Spec.Domains = append(shop.Spec.Domains, "site.example.com", "other.example.com", "alpha-site.apps.example.org")
	if err := f.cr.Update(ctx, shop); err != nil {
		t.Fatal(err)
	}
	if app := lookup("site.example.com"); app == nil || workspaceOf(app) != "alpha" {
		t.Errorf("site.example.com with a beta project listing it = %v, want alpha's site", app)
	}
	if app := lookup("other.example.com"); app != nil {
		t.Errorf("other.example.com, under alpha's domain, listed by beta = %v, want no app", app)
	}
	// A project's listing never takes an address that belongs to a project
	// of the same workspace either.
	routed := f.app(t, "alpha", "routed")
	routed.Spec.Domains = []string{"site.example.com"}
	if err := f.cr.Update(ctx, routed); err != nil {
		t.Fatal(err)
	}
	if app := lookup("site.example.com"); app == nil || project.SlugOf(app) != "site" {
		t.Errorf("site.example.com listed by another alpha project = %v, want alpha's site", app)
	}
	// Beta's own custom domain is still served.
	if app := lookup("keep.example.net"); app == nil || app.Name != "shop" {
		t.Errorf("keep.example.net = %v, want beta's shop", app)
	}
	// A request two labels under alpha's domain resolves to no workspace.
	if rec := at(t, f.s, "a.b.example.com", "GET", "/api/projects", ""); rec.Code != http.StatusNotFound {
		t.Errorf("a.b.example.com = %d, want 404", rec.Code)
	}
	// A failed lookup is not remembered as an answer.
	f.s.hostCache.at = time.Time{}
	f.st.down.Store(true)
	lookup("keep.example.net")
	f.st.down.Store(false)
	f.s.hostCache.mu.Lock()
	cached := !f.s.hostCache.at.IsZero()
	f.s.hostCache.mu.Unlock()
	if cached {
		t.Error("an index built while the store failed was remembered")
	}
}

// Only the funnel (domain_ownership.go) assigns spec.domains; the one other
// place is the archive restore, which writes back the value the App holds.
// A new assignment elsewhere would skip the ownership rule.
func TestDomainOwnershipSpecDomainsAssignedOnlyInTheFunnel(t *testing.T) {
	allowed := map[string]string{"domain_ownership.go": "", "project_archive_restore.go": "applyArchiveConfiguration"}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		for _, decl := range parsed.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			if only, ok := allowed[file]; ok && (only == "" || isFunc && fn.Name.Name == only) {
				continue
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.AssignStmt:
					for _, lhs := range n.Lhs {
						if assignsSpecDomains(lhs) {
							t.Errorf("%s: Spec.Domains is assigned outside the funnel: use setSpecDomains, appendSpecDomain or dropSpecDomain", fset.Position(lhs.Pos()))
						}
					}
				case *ast.CompositeLit:
					if isAppSpec(n.Type) {
						for _, el := range n.Elts {
							if kv, ok := el.(*ast.KeyValueExpr); ok {
								if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "Domains" {
									t.Errorf("%s: an AppSpec is built with Domains outside the funnel", fset.Position(kv.Pos()))
								}
							}
						}
					}
				}
				return true
			})
		}
	}
	if checked < 50 {
		t.Fatalf("only %d files read: the guard is not looking at pkg/api", checked)
	}
}

// assignsSpecDomains says an assignment target is <something>.Domains (or
// an element of it) that is not a Status field.
func assignsSpecDomains(e ast.Expr) bool {
	if ix, ok := e.(*ast.IndexExpr); ok {
		e = ix.X
	}
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Domains" {
		return false
	}
	if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "Status" {
		return false
	}
	return true
}

func isAppSpec(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.SelectorExpr:
		return t.Sel.Name == "AppSpec"
	case *ast.Ident:
		return t.Name == "AppSpec"
	}
	return false
}

// The guard catches the shapes an assignment takes.
func TestDomainOwnershipGuardRecognisesAssignments(t *testing.T) {
	for src, want := range map[string]bool{
		"a.Spec.Domains = x":       true,
		"a.Spec.Domains[0] = x":    true,
		"spec.Domains = x":         true,
		"app.Status.Domains = x":   false,
		"a.Spec.Domain = x":        false,
		"a.Spec.Domains, b = x, y": true,
		"req.Domains = []string{}": true,
		"x := a.Spec.Domains":      false,
	} {
		stmt, err := parser.ParseExpr("func(){ " + src + " }")
		if err != nil {
			t.Fatal(err)
		}
		got := false
		ast.Inspect(stmt, func(n ast.Node) bool {
			if as, ok := n.(*ast.AssignStmt); ok {
				for _, lhs := range as.Lhs {
					got = got || assignsSpecDomains(lhs)
				}
			}
			return true
		})
		if got != want {
			t.Errorf("%q: assignsSpecDomains = %v, want %v", src, got, want)
		}
	}
}
