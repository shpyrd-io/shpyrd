package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	"github.com/shpyrd-io/shpyrd/pkg/gitauth"
)

// A build asks for its place on a machine (build_resources.go): CPU, memory
// and disk, the same on a buildpack build's steps and on a Dockerfile
// build, and no ceiling on the buildpack build.
func TestBuildPodsAskForTheirPlace(t *testing.T) {
	r, _ := newTestReconciler(t)
	want := map[string]string{"cpu": buildRequestCPU, "memory": buildRequestMemory, "ephemeral-storage": buildRequestStorage}

	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "web1", Namespace: "app-web1"},
		Spec:       shpyrdv1.AppSpec{Source: &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: "https://example.test/x.git", Revision: "main"}}},
	}
	img, err := r.Config.desiredKpackImage(app, gitauth.Build{})
	if err != nil {
		t.Fatal(err)
	}
	got, found, _ := unstructured.NestedStringMap(img.Object, "spec", "build", "resources", "requests")
	if !found {
		t.Fatalf("the Image asks for nothing: %v", img.Object["spec"])
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("Image requests %s = %q, want %q", k, got[k], v)
		}
	}
	if _, found, _ := unstructured.NestedMap(img.Object, "spec", "build", "resources", "limits"); found {
		t.Error("a buildpack build has a ceiling; the stack's tools decide")
	}

	job, err := r.Config.desiredBuildJob(dockerfileApp(), 1, "k", gitauth.Build{})
	if err != nil {
		t.Fatal(err)
	}
	req := job.Spec.Template.Spec.Containers[0].Resources.Requests
	for k, v := range map[corev1.ResourceName]string{corev1.ResourceCPU: want["cpu"], corev1.ResourceMemory: want["memory"], corev1.ResourceEphemeralStorage: want["ephemeral-storage"]} {
		if q := req[k]; q.String() != v {
			t.Errorf("Dockerfile build requests %s = %q, want %q", k, q.String(), v)
		}
	}
}
