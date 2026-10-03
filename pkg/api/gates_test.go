package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
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

// Unused in this task; the flow tests of Task 3 use them.
var _ = httptest.NewRecorder
var _ = http.StatusOK
var _ = gin.New
var _ client.Object
