package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/kube"
)

// #135, end to end through a reconcile: a workspace with a sleep default
// puts a project without a policy of its own to sleep (ScaledObject, the
// Ingress on the interceptor). An explicit "off" on the process, which is
// what the API now stores for "off", opts out: no ScaledObject, the Ingress
// back on the app's Service. Clearing the policy (the API's "default")
// brings the workspace's default back.
func TestSleepOffOptsOutOfTheWorkspaceDefault(t *testing.T) {
	ctx := context.Background()
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop", Labels: map[string]string{shpyrdv1.LabelWorkspace: "acme"}},
		Spec: shpyrdv1.AppSpec{
			Image:     "ghcr.io/acme/shop:1",
			Access:    shpyrdv1.AccessPublic,
			Processes: map[string]shpyrdv1.Process{"web": {Port: ptr.To[int32](8080)}},
		},
	}
	scheme, err := kube.Scheme()
	if err != nil {
		t.Fatal(err)
	}
	// The fake client's REST mapper must know the KEDA kinds, as the
	// cluster with the add-on would.
	mapper := meta.NewDefaultRESTMapper(nil)
	for _, gvk := range []schema.GroupVersionKind{InterceptorRouteGVK, ScaledObjectGVK} {
		scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
		scheme.AddKnownTypeWithName(gvk.GroupVersion().WithKind(gvk.Kind+"List"), &unstructured.UnstructuredList{})
		mapper.Add(gvk, meta.RESTScopeNamespace)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithRESTMapper(mapper).WithObjects(app,
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "app-shop", Labels: map[string]string{shpyrdv1.LabelManagedBy: "shpyrd"}}}).
		WithStatusSubresource(&shpyrdv1.App{}).Build()
	r := &AppReconciler{
		ProcessTypes: func(context.Context, string) []string { return nil },
		Client:       c, APIReader: c, Scheme: scheme, Recorder: record.NewFakeRecorder(100),
		Config: Config{Domain: "example.test", HTTPSPort: "8443", RegistryHost: "10.96.0.50:5000", RegistryInsecure: true, SystemNamespace: "shpyrd-system", SleepAllowed: always}.Defaults(),
	}
	r.Config.WorkspaceSleepDefault = func(slug string) (string, string) {
		if slug == "acme" {
			return "15m", "page"
		}
		return "", ""
	}

	scaledObjectExists := func() bool {
		t.Helper()
		so := &unstructured.Unstructured{}
		so.SetGroupVersionKind(ScaledObjectGVK)
		err := c.Get(ctx, types.NamespacedName{Namespace: "app-shop", Name: "shop-sleep"}, so)
		if err != nil && !apierrors.IsNotFound(err) {
			t.Fatal(err)
		}
		return err == nil
	}
	backend := func() string {
		t.Helper()
		ing := &networkingv1.Ingress{}
		if err := c.Get(ctx, types.NamespacedName{Namespace: "app-shop", Name: "shop"}, ing); err != nil {
			t.Fatal(err)
		}
		return ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name
	}
	setSleep := func(sp *shpyrdv1.SleepSpec) {
		t.Helper()
		cur := &shpyrdv1.App{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(app), cur); err != nil {
			t.Fatal(err)
		}
		p := cur.Spec.Processes["web"]
		p.Sleep = sp
		cur.Spec.Processes["web"] = p
		if err := c.Update(ctx, cur); err != nil {
			t.Fatal(err)
		}
	}

	// No policy of its own: the workspace's default applies.
	got := runReconcile(t, r, app)
	if !scaledObjectExists() || backend() != webSleepServiceName {
		t.Fatalf("workspace default: scaledobject=%v backend=%s, want sleep on", scaledObjectExists(), backend())
	}
	if st := got.Status.Processes["web"].Sleep; st == nil || st.Message == "" {
		t.Errorf("status must say the workspace's default applies, got %+v", st)
	}

	// An explicit off (what the API stores for "off") opts out: the sleep
	// objects go, the Ingress routes to the app again.
	setSleep(&shpyrdv1.SleepSpec{After: "off"})
	got = runReconcile(t, r, app)
	if scaledObjectExists() {
		t.Error("off must leave no ScaledObject while the workspace has a default")
	}
	if b := backend(); b == webSleepServiceName {
		t.Errorf("off must route to the app's own Service, got %s", b)
	}
	if st := got.Status.Processes["web"].Sleep; st != nil {
		t.Errorf("off must report no sleep state, got %+v", st)
	}

	// Cleared again (the API's "default"): the workspace's default is back.
	setSleep(nil)
	runReconcile(t, r, app)
	if !scaledObjectExists() || backend() != webSleepServiceName {
		t.Errorf("default: scaledobject=%v backend=%s, want sleep on again", scaledObjectExists(), backend())
	}
}
