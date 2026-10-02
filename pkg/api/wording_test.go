package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/internal/controller"
)

// Every message a workspace reads (#52) says what happened in words:
// never kubectl, a pod, a namespace, a Job or the CLI's commands. A build
// fails the way it failed in production (kpack's sentence names the pod
// and kubectl); the project, its builds and its releases are read through
// the API and every message-like field is held to the rule. The build's
// own output (its log) is the customer's and is not checked.
func TestCustomerMessagesNameNoInternals(t *testing.T) {
	ctx := context.Background()
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "app-shop", Generation: 1},
		Spec:       shpyrdv1.AppSpec{Source: &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: "https://example.test/shop.git"}}},
	}
	kpackSentence := "Error:  Container build terminated with error ERROR: failed to build: exit status 1: For more info use `kubectl logs -n app-shop shop-build-1-build-pod -c build`"
	build := &unstructured.Unstructured{}
	build.SetGroupVersionKind(controller.KpackBuildGVK)
	build.SetNamespace("app-shop")
	build.SetName("shop-build-1")
	build.SetLabels(map[string]string{"image.kpack.io/image": "shop", "image.kpack.io/buildNumber": "1"})
	_ = unstructured.SetNestedSlice(build.Object, []interface{}{map[string]interface{}{"type": "Succeeded", "status": "False", "message": kpackSentence}}, "status", "conditions")
	// A second failed build the controller has not read yet: the API must
	// not fall back to kpack's sentence.
	unread := build.DeepCopy()
	unread.SetName("shop-build-2")
	unread.SetLabels(map[string]string{"image.kpack.io/image": "shop", "image.kpack.io/buildNumber": "2"})
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "shop-build-1-build-pod", Namespace: "app-shop"},
		Status: corev1.PodStatus{InitContainerStatuses: []corev1.ContainerStatus{
			{Name: "build", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 51}}},
		}},
	}
	s, cr := newTestServer(t, nil, []client.Object{app, build, unread, pod})

	r := &controller.AppReconciler{
		Client: cr, APIReader: cr, Scheme: cr.Scheme(), Recorder: record.NewFakeRecorder(100), Kube: kubefake.NewSimpleClientset(),
		ProcessTypes: func(context.Context, string) []string { return nil },
		Config:       controller.Config{Domain: "example.test", RegistryHost: "10.96.0.50:5000"}.Defaults(),
	}
	reconcile := func() {
		t.Helper()
		if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "app-shop", Name: "shop"}}); err != nil {
			t.Fatal(err)
		}
	}
	reconcile()
	img := &unstructured.Unstructured{}
	img.SetGroupVersionKind(controller.KpackImageGVK)
	if err := cr.Get(ctx, types.NamespacedName{Namespace: "app-shop", Name: "shop"}, img); err != nil {
		t.Fatal(err)
	}
	_ = unstructured.SetNestedField(img.Object, "shop-build-1", "status", "latestBuildRef")
	_ = unstructured.SetNestedField(img.Object, img.GetGeneration(), "status", "observedGeneration")
	_ = unstructured.SetNestedSlice(img.Object, []interface{}{map[string]interface{}{"type": "Ready", "status": "False", "message": "Error: Build 'shop-build-1' in namespace 'app-shop' failed: " + kpackSentence}}, "status", "conditions")
	if err := cr.Update(ctx, img); err != nil {
		t.Fatal(err)
	}
	reconcile()

	for _, path := range []string{"/api/projects/shop", "/api/projects/shop/builds", "/api/projects/shop/releases"} {
		rec := do(t, s, "GET", path, "", true)
		if rec.Code == http.StatusNotFound && path == "/api/projects/shop/releases" {
			continue // releases are part of the project view
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("%s = %d %s", path, rec.Code, rec.Body.String())
		}
		var v interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		checked := 0
		walkMessages(v, func(key, msg string) {
			checked++
			if bad := controller.PlatformWordingFault(msg); bad != "" {
				t.Errorf("%s: %s = %q names %q", path, key, msg, bad)
			}
		})
		if checked == 0 && path != "/api/projects/shop/releases" {
			t.Errorf("%s: no message checked: %s", path, rec.Body.String())
		}
	}
}

// walkMessages calls f with every string under a key that carries a
// message for people.
func walkMessages(v interface{}, f func(key, msg string)) {
	switch x := v.(type) {
	case map[string]interface{}:
		for k, val := range x {
			switch k {
			case "message", "error", "reason", "note", "description", "detail", "summary", "hint":
				if s, ok := val.(string); ok {
					f(k, s)
					continue
				}
			}
			walkMessages(val, f)
		}
	case []interface{}:
		for _, e := range x {
			walkMessages(e, f)
		}
	}
}
