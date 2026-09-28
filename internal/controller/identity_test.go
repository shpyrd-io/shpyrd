package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/ids"
	"github.com/shpyrd-io/shpyrd/pkg/project"
	"github.com/shpyrd-io/shpyrd/pkg/store"
)

const (
	testProjectID   = "0b1e6c7a-9d6e-4c2f-8a1b-2f3e4d5c6b7a"
	testWorkspaceID = "7f0d9e2c-1111-4222-8333-444455556666"
)

// An App named by its ID (RFC-0076): plain object names inside its own
// namespace, the slug only where people read it (hostname, env, labels),
// identity labels on everything, image paths keyed by ids.
func TestIDNamedProject(t *testing.T) {
	short := ids.Short(testProjectID)
	ns := project.IDNamespace(testProjectID)
	st := store.NewMemory()
	ctx := context.Background()
	acme, err := st.CreateWorkspace(ctx, store.Workspace{ID: testWorkspaceID, Slug: "acme", Name: "Acme", Address: "acme.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{
			Name: short, Namespace: ns, Generation: 1,
			Labels: project.NamespaceLabelsFor("acme", acme.ID, testProjectID, "shop", short),
		},
		Spec: shpyrdv1.AppSpec{
			ID: testProjectID, Slug: "shop",
			Image:     "ghcr.io/example/shop@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Processes: map[string]shpyrdv1.Process{"web": {Replicas: ptr.To[int32](1)}, "worker": {}},
			Access:    shpyrdv1.AccessIdentified,
		},
	}
	r, c := newTestReconciler(t, app, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns, Labels: map[string]string{shpyrdv1.LabelManagedBy: "shpyrd"}}})
	r.Config.WorkspaceDomain = func(slug string) string { return "acme.example.test" }
	r.Config.WorkspaceID = func(slug string) string {
		if slug == "acme" {
			return acme.ID
		}
		return ""
	}
	r.Config.Projects = st
	got := runReconcile(t, r, app)

	if got.Status.URL != "https://shop.acme.example.test:8443" {
		t.Errorf("url = %q: the hostname is the slug, never the id", got.Status.URL)
	}
	for _, name := range []string{"web", "worker"} {
		d := &appsv1.Deployment{}
		if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, d); err != nil {
			t.Fatalf("deployment %s: %v", name, err)
		}
		if d.Spec.Selector.MatchLabels[shpyrdv1.LabelApp] != short || d.Spec.Template.Labels[shpyrdv1.LabelProjectID] != short ||
			d.Spec.Template.Labels[shpyrdv1.LabelProject] != "shop" || d.Spec.Template.Labels[shpyrdv1.LabelWorkspaceID] != ids.Short(acme.ID) {
			t.Errorf("%s labels: selector=%v template=%v", name, d.Spec.Selector.MatchLabels, d.Spec.Template.Labels)
		}
		if name == "web" {
			var envProject, envID string
			for _, e := range d.Spec.Template.Spec.Containers[0].Env {
				switch e.Name {
				case "SHPYRD_PROJECT":
					envProject = e.Value
				case "SHPYRD_PROJECT_ID":
					envID = e.Value
				}
			}
			if envProject != "shop" || envID != testProjectID {
				t.Errorf("env SHPYRD_PROJECT=%q SHPYRD_PROJECT_ID=%q", envProject, envID)
			}
		}
	}
	svc := &corev1.Service{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: "web"}, svc); err != nil {
		t.Fatalf("service web: %v", err)
	}
	ing := &networkingv1.Ingress{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: "app"}, ing); err != nil {
		t.Fatalf("ingress app: %v", err)
	}
	if ing.Spec.Rules[0].Host != "shop.acme.example.test" || ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name != "web" {
		t.Errorf("ingress host=%q backend=%q", ing.Spec.Rules[0].Host, ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name)
	}
	edge := &networkingv1.Ingress{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: "app-edge"}, edge); err != nil {
		t.Fatalf("ingress app-edge: %v", err)
	}
	if auth := ing.Annotations["nginx.ingress.kubernetes.io/auth-url"]; auth == "" || !strings.Contains(auth, "project=shop&") || !strings.Contains(auth, "workspace=acme") {
		t.Errorf("edge auth url names the slug and workspace: %q", auth)
	}
	nsObj := &corev1.Namespace{}
	if err := c.Get(ctx, types.NamespacedName{Name: ns}, nsObj); err != nil {
		t.Fatal(err)
	}
	if nsObj.Labels[shpyrdv1.LabelProjectID] != short || nsObj.Labels[shpyrdv1.LabelWorkspaceID] != ids.Short(acme.ID) || nsObj.Labels[shpyrdv1.LabelProject] != "shop" || nsObj.Labels[shpyrdv1.LabelWorkspace] != "acme" {
		t.Errorf("namespace labels = %v", nsObj.Labels)
	}
	// The isolation policy selects the workspace by id.
	np := &networkingv1.NetworkPolicy{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: IsolationPolicyName}, np); err != nil {
		t.Fatal(err)
	}
	if sel := np.Spec.Egress[0].To[len(np.Spec.Egress[0].To)-1].NamespaceSelector.MatchLabels; sel[shpyrdv1.LabelWorkspaceID] != ids.Short(acme.ID) {
		t.Errorf("egress workspace selector = %v", sel)
	}
	// Mirrored into the store.
	pr, err := st.ProjectBySlug(ctx, "acme", "shop")
	if err != nil || pr.ID != testProjectID || pr.Namespace != ns || pr.Name != "shop" {
		t.Fatalf("store project: %+v %v", pr, err)
	}
	// The finalizer is on; deleting marks the store row.
	if !containsString(got.Finalizers, ProjectFinalizer) {
		t.Fatalf("finalizers = %v", got.Finalizers)
	}
	if err := c.Delete(ctx, got); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: short}}); err != nil {
		t.Fatal(err)
	}
	if live, _ := st.ListProjects(ctx, "acme", false); len(live) != 0 {
		t.Errorf("project still live in the store after delete: %+v", live)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: short}, &shpyrdv1.App{}); err == nil {
		t.Error("the App should be gone once the finalizer is removed")
	}
}

// A legacy App (named by its slug in app-<slug>) receives an id and the
// identity labels, keeps every name it had, and its ledger rows move from
// the slug to the id once.
func TestLegacyProjectGetsIdentity(t *testing.T) {
	st := store.NewMemory()
	ctx := context.Background()
	def, err := st.Workspace(ctx, store.DefaultWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	qty := 42.0
	if err := st.WriteBuckets(ctx, []store.UsageBucket{{WorkspaceID: store.DefaultWorkspace, Project: "shop", Component: "web", Metric: store.MetricCPUUsed,
		PeriodStart: metav1.Now().Time.Add(-time.Hour), PeriodEnd: metav1.Now().Time, Quantity: &qty, Unit: store.UnitCoreSeconds, Quality: store.QualityComplete, Revision: 1}}); err != nil {
		t.Fatal(err)
	}
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop", Generation: 1, Labels: project.NamespaceLabels(project.DefaultWorkspace, "shop")},
		Spec: shpyrdv1.AppSpec{
			Image:     "ghcr.io/example/shop@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Processes: map[string]shpyrdv1.Process{"web": {}},
		},
	}
	r, c := newTestReconciler(t, app, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "app-shop", Labels: map[string]string{shpyrdv1.LabelManagedBy: "shpyrd"}}})
	r.Config.WorkspaceID = func(slug string) string { return def.ID }
	r.Config.Projects = st
	got := runReconcile(t, r, app)

	if got.Spec.ID == "" || got.Spec.Slug != "" || project.IDNamed(got) {
		t.Fatalf("legacy app after reconcile: id=%q slug=%q idNamed=%v", got.Spec.ID, got.Spec.Slug, project.IDNamed(got))
	}
	short := ids.Short(got.Spec.ID)
	if got.Labels[shpyrdv1.LabelProjectID] != short || got.Labels[shpyrdv1.LabelWorkspaceID] != ids.Short(def.ID) || got.Labels[shpyrdv1.LabelProject] != "shop" {
		t.Errorf("app labels = %v", got.Labels)
	}
	// Names unchanged: the slug-prefixed Deployment and Ingress.
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-shop", Name: "shop-web"}, &appsv1.Deployment{}); err != nil {
		t.Errorf("legacy deployment shop-web: %v", err)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-shop", Name: "shop"}, &networkingv1.Ingress{}); err != nil {
		t.Errorf("legacy ingress shop: %v", err)
	}
	if got.Status.URL != "https://shop.example.test:8443" {
		t.Errorf("url = %q", got.Status.URL)
	}
	nsObj := &corev1.Namespace{}
	if err := c.Get(ctx, types.NamespacedName{Name: "app-shop"}, nsObj); err != nil {
		t.Fatal(err)
	}
	if nsObj.Labels[shpyrdv1.LabelProjectID] != short || nsObj.Labels[shpyrdv1.LabelProject] != "shop" {
		t.Errorf("namespace labels = %v", nsObj.Labels)
	}
	// Store: mirrored, and the ledger re-keyed.
	pr, err := st.ProjectBySlug(ctx, store.DefaultWorkspace, "shop")
	if err != nil || pr.ID != got.Spec.ID || pr.Namespace != "app-shop" {
		t.Fatalf("store project: %+v %v", pr, err)
	}
	now := metav1.Now().Time
	if rows, _ := st.QueryBuckets(ctx, store.DefaultWorkspace, short, now.Add(-2*time.Hour), now.Add(time.Hour)); len(rows) != 1 {
		t.Errorf("ledger rows under the id: %d", len(rows))
	}
	if rows, _ := st.QueryBuckets(ctx, store.DefaultWorkspace, "shop", now.Add(-2*time.Hour), now.Add(time.Hour)); len(rows) != 0 {
		t.Errorf("ledger rows still under the slug: %d", len(rows))
	}
	// A second reconcile writes nothing new to the App.
	again := runReconcile(t, r, got)
	if again.Spec.ID != got.Spec.ID {
		t.Error("id must never change")
	}
}

// The allow list selects a peer project by id once the peer has one.
func TestAllowListSelectsByID(t *testing.T) {
	peerShort := ids.Short(testProjectID)
	peer := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: peerShort, Namespace: project.IDNamespace(testProjectID),
			Labels: project.NamespaceLabelsFor(project.DefaultWorkspace, "", testProjectID, "billing", peerShort)},
		Spec: shpyrdv1.AppSpec{ID: testProjectID, Slug: "billing", Image: "ghcr.io/example/billing@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
	}
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop", Labels: project.NamespaceLabels(project.DefaultWorkspace, "shop")},
		Spec: shpyrdv1.AppSpec{
			Image:  "ghcr.io/example/shop@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Access: shpyrdv1.AccessIdentified,
			Allow:  []shpyrdv1.AllowEntry{{Project: "billing"}, {Project: "unknown"}},
		},
	}
	r, c := newTestReconciler(t, app, peer)
	runReconcile(t, r, app)
	np := &networkingv1.NetworkPolicy{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "app-shop", Name: IsolationPolicyName}, np); err != nil {
		t.Fatal(err)
	}
	var byID, bySlug bool
	for _, from := range np.Spec.Ingress[0].From {
		if from.NamespaceSelector == nil {
			continue
		}
		if from.NamespaceSelector.MatchLabels[shpyrdv1.LabelProjectID] == peerShort {
			byID = true
		}
		if from.NamespaceSelector.MatchLabels[shpyrdv1.LabelProject] == "unknown" {
			bySlug = true
		}
	}
	if !byID || !bySlug {
		t.Errorf("peers: byID=%v bySlug(fallback)=%v: %+v", byID, bySlug, np.Spec.Ingress[0].From)
	}
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
