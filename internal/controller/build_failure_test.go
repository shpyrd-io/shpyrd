package controller

import (
	"context"
	"os"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// What a failed build says (#52), from the end of real build logs (kind,
// kpack 0.18, Paketo): which script failed and why, in words.
func TestDescribeBuildFailure(t *testing.T) {
	read := func(name string) string {
		b, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	for _, c := range []struct{ step, out, want string }{
		{"build", read("build-missing-variable.log"), "The build failed while running `npm run build`: the app reads the variable DATABASE_URL while building, and it is not set."},
		{"detect", read("build-nothing-detected.log"), "No buildpack recognised the source"},
		{"build", "Paketo Buildpack for Node Run Script 2.3.54\n    Running 'npm run build'\n      Error: Failed to collect page data for /\n      Error: DATABASE_URL não está definida\nexit status 1\n", "the app reads the variable DATABASE_URL while building"},
		{"build", "Paketo Buildpack for Node Run Script 2.3.54\n    Running 'npm run build'\n      Error: Cannot find module 'tailwindcss'\nexit status 1\n", "while running `npm run build`: the module tailwindcss cannot be found."},
		{"build", "Paketo Buildpack for Node Run Script 2.3.54\n    Running 'npm run build'\n      Type error: x is not assignable\nexit status 1\n", "The build failed while running `npm run build` (exit status 1)."},
		{"build", "Paketo Buildpack for Go Build 4.1.0\n  Executing build process\n    something went wrong\n", "The build failed in the Go Build buildpack."},
		{"prepare", "", "The build could not fetch the source."},
		{"export", "", "its image could not be saved"},
		{"", "", "The build failed. The build's log has the full output."},
	} {
		got := describeBuildFailure(c.step, c.out)
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: %q, want %q", c.step, got, c.want)
		}
		if bad := PlatformWordingFault(got); bad != "" {
			t.Errorf("%s: %q names %q", c.step, got, bad)
		}
	}
}

// A failed kpack build: the app says what failed, read once from the
// failed step's output and kept on the Build; kpack's sentence (a pod, a
// namespace, kubectl) is never shown.
func TestKpackBuildFailureInWords(t *testing.T) {
	ctx := context.Background()
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "src", Namespace: "app-src", Generation: 1},
		Spec:       shpyrdv1.AppSpec{Source: &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: "https://github.com/example/repo"}}},
	}
	kpackSentence := "Error:  Container build terminated with error ERROR: failed to build: exit status 1: For more info use `kubectl logs -n app-src src-build-1-build-pod -c build`"
	build := &unstructured.Unstructured{}
	build.SetGroupVersionKind(KpackBuildGVK)
	build.SetNamespace("app-src")
	build.SetName("src-build-1")
	_ = unstructured.SetNestedSlice(build.Object, []interface{}{map[string]interface{}{"type": "Succeeded", "status": "False", "message": kpackSentence}}, "status", "conditions")
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "src-build-1-build-pod", Namespace: "app-src"},
		Status: corev1.PodStatus{InitContainerStatuses: []corev1.ContainerStatus{
			{Name: "detect", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}},
			{Name: "build", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 51}}},
		}},
	}
	r, c := newTestReconciler(t, app, build, pod)
	r.Kube = kubefake.NewSimpleClientset() // its logs answer "fake logs"
	runReconcile(t, r, app)
	img := &unstructured.Unstructured{}
	img.SetGroupVersionKind(KpackImageGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: "app-src", Name: "src"}, img); err != nil {
		t.Fatal(err)
	}
	_ = unstructured.SetNestedField(img.Object, "src-build-1", "status", "latestBuildRef")
	_ = unstructured.SetNestedField(img.Object, img.GetGeneration(), "status", "observedGeneration")
	_ = unstructured.SetNestedSlice(img.Object, []interface{}{map[string]interface{}{"type": "Ready", "status": "False", "message": "Error: Build 'src-build-1' in namespace 'app-src' failed: " + kpackSentence}}, "status", "conditions")
	if err := c.Update(ctx, img); err != nil {
		t.Fatal(err)
	}
	got := runReconcile(t, r, app)
	if got.Status.Phase != shpyrdv1.PhaseFailed || !strings.HasPrefix(got.Status.Message, "The build failed") {
		t.Fatalf("status = %s %q", got.Status.Phase, got.Status.Message)
	}
	for _, s := range []string{got.Status.Message, conditionMessage(got, shpyrdv1.ConditionBuilt), conditionMessage(got, shpyrdv1.ConditionReady)} {
		if bad := PlatformWordingFault(s); bad != "" {
			t.Errorf("%q names %q", s, bad)
		}
	}
	_ = c.Get(ctx, types.NamespacedName{Namespace: "app-src", Name: "src-build-1"}, build)
	if build.GetAnnotations()[shpyrdv1.AnnotationBuildFailure] != got.Status.Message {
		t.Errorf("annotation = %q", build.GetAnnotations()[shpyrdv1.AnnotationBuildFailure])
	}
	if KpackFailure(build) != got.Status.Message {
		t.Errorf("KpackFailure = %q", KpackFailure(build))
	}
}

func conditionMessage(app *shpyrdv1.App, typ string) string {
	for _, c := range app.Status.Conditions {
		if c.Type == typ {
			return c.Message
		}
	}
	return ""
}
