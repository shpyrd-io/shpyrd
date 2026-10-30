package buildcatalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestSplitAndPinned(t *testing.T) {
	for ref, want := range map[string][3]string{
		"index.docker.io/paketobuildpacks/nodejs:latest":   {"index.docker.io", "paketobuildpacks/nodejs", "latest"},
		"paketobuildpacks/build-jammy-base":                {"index.docker.io", "paketobuildpacks/build-jammy-base", "latest"},
		"ghcr.io/shpyrd-io/buildpack-build-env:0.1.0":      {"ghcr.io", "shpyrd-io/buildpack-build-env", "0.1.0"},
		"localhost:5000/x/y:1":                             {"localhost:5000", "x/y", "1"},
		"index.docker.io/paketobuildpacks/nodejs@sha256:a": {"index.docker.io", "paketobuildpacks/nodejs", ""},
		"nginx": {"index.docker.io", "library/nginx", "latest"},
	} {
		host, repo, tag := Split(ref)
		if got := [3]string{host, repo, tag}; got != want {
			t.Errorf("Split(%q) = %v, want %v", ref, got, want)
		}
	}
	if got := Pinned("index.docker.io/paketobuildpacks/nodejs:latest", "sha256:abc"); got != "index.docker.io/paketobuildpacks/nodejs@sha256:abc" {
		t.Errorf("Pinned = %q", got)
	}
	if got := Pinned("localhost:5000/x/y:1", "sha256:abc"); got != "localhost:5000/x/y@sha256:abc" {
		t.Errorf("Pinned with a port = %q", got)
	}
	if got := Pinned("index.docker.io/paketobuildpacks/nodejs@sha256:old", "sha256:new"); got != "index.docker.io/paketobuildpacks/nodejs@sha256:new" {
		t.Errorf("Pinned over a digest = %q", got)
	}
}

// A registry that wants an anonymous bearer token, as Docker Hub and
// GitHub's do, and answers the manifest with its digest.
func fakeRegistry(t *testing.T, digests map[string]string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			if r.URL.Query().Get("scope") == "" || r.URL.Query().Get("service") != "registry.test" {
				http.Error(w, "no scope or service", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "anonymous"})
		case strings.HasPrefix(r.URL.Path, "/v2/"):
			if r.Header.Get("Authorization") != "Bearer anonymous" {
				w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="registry.test",scope="repository:x:pull"`, srv.URL))
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if !strings.Contains(r.Header.Get("Accept"), "image.index") {
				http.Error(w, "no index accepted", http.StatusNotAcceptable)
				return
			}
			d, ok := digests[strings.TrimPrefix(r.URL.Path, "/v2/")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Docker-Content-Digest", d)
			w.Header().Set("Content-Type", "application/vnd.oci.image.index.v1+json")
			_, _ = w.Write([]byte(`{"manifests":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRegistryDigest(t *testing.T) {
	srv := fakeRegistry(t, map[string]string{"paketobuildpacks/nodejs/manifests/latest": "sha256:1111"})
	host := strings.TrimPrefix(srv.URL, "http://")
	r := Registry{Scheme: "http"}
	got, err := r.Digest(context.Background(), host+"/paketobuildpacks/nodejs:latest")
	if err != nil || got != "sha256:1111" {
		t.Fatalf("Digest = %q, %v", got, err)
	}
	if _, err := r.Digest(context.Background(), host+"/paketobuildpacks/none:latest"); err == nil {
		t.Error("a missing tag resolved")
	}
}

type fixed map[string]string

func (f fixed) Digest(_ context.Context, ref string) (string, error) {
	if d, ok := f[ref]; ok {
		return d, nil
	}
	if strings.HasPrefix(ref, "10.96.0.50:5000/") {
		return "", ErrCredential
	}
	return "", fmt.Errorf("unknown %s", ref)
}

func catalogObject(kind, name string, spec map[string]interface{}, annotations map[string]string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "kpack.io/v1alpha2", "kind": kind, "metadata": map[string]interface{}{"name": name}, "spec": spec}}
	if annotations != nil {
		u.SetAnnotations(annotations)
	}
	return u
}

func fakeDynamic(objs ...runtime.Object) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	kinds := map[schema.GroupVersionResource]string{}
	for kind, gvr := range Resources {
		kinds[gvr] = kind + "List"
	}
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, kinds, objs...)
}

// The refresh pins every tag of the catalog to its digest and records the
// tag it came from; a second pass with onlyUnpinned leaves the pins; a
// full pass moves a pin whose tag moved on; a reference nobody can resolve
// is reported and left.
func TestRefreshPinsTheCatalog(t *testing.T) {
	ctx := context.Background()
	dyn := fakeDynamic(
		catalogObject("ClusterBuildpack", "paketo-nodejs", map[string]interface{}{"image": "index.docker.io/paketobuildpacks/nodejs:latest"}, nil),
		catalogObject("ClusterBuildpack", "heroku-deb-packages", map[string]interface{}{"image": "index.docker.io/heroku/buildpack-deb-packages:1.0.1"}, nil),
		catalogObject("ClusterBuildpack", "by-hand", map[string]interface{}{"image": "index.docker.io/x/y@sha256:fixed"}, nil),
		catalogObject("ClusterBuildpack", "shpyrd-build-env", map[string]interface{}{"image": "10.96.0.50:5000/shpyrd/buildpacks/build-env:0.1.0"}, nil),
		catalogObject("ClusterStack", "jammy", map[string]interface{}{"id": "io.buildpacks.stacks.jammy",
			"buildImage": map[string]interface{}{"image": "index.docker.io/paketobuildpacks/build-jammy-base:latest"},
			"runImage":   map[string]interface{}{"image": "index.docker.io/paketobuildpacks/run-jammy-base:latest"}}, nil),
	)
	digests := fixed{
		"index.docker.io/paketobuildpacks/nodejs:latest":           "sha256:node1",
		"index.docker.io/paketobuildpacks/build-jammy-base:latest": "sha256:build1",
		"index.docker.io/paketobuildpacks/run-jammy-base:latest":   "sha256:run1",
	}
	res, err := Refresh(ctx, dyn, digests, false)
	if err == nil || !strings.Contains(err.Error(), "heroku-deb-packages") || strings.Contains(err.Error(), "build-env") {
		t.Errorf("the unresolved reference is not reported, or the credentialed one is: %v", err)
	}
	changes := res.Changes
	if len(changes) != 3 {
		t.Fatalf("changes = %v", changes)
	}
	if len(res.Left) != 1 || res.Left[0].Name != "shpyrd-build-env" {
		t.Errorf("left = %v, want the platform's own registry", res.Left)
	}
	get := func(kind, name string) *unstructured.Unstructured {
		u, err := dyn.Resource(Resources[kind]).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	node := get("ClusterBuildpack", "paketo-nodejs")
	if img, _, _ := unstructured.NestedString(node.Object, "spec", "image"); img != "index.docker.io/paketobuildpacks/nodejs@sha256:node1" {
		t.Errorf("nodejs image = %q", img)
	}
	if from := node.GetAnnotations()["shpyrd.io/pinned-image"]; from != "index.docker.io/paketobuildpacks/nodejs:latest" {
		t.Errorf("nodejs pinned from %q", from)
	}
	stack := get("ClusterStack", "jammy")
	if run, _, _ := unstructured.NestedString(stack.Object, "spec", "runImage", "image"); run != "index.docker.io/paketobuildpacks/run-jammy-base@sha256:run1" {
		t.Errorf("run image = %q", run)
	}
	if id, _, _ := unstructured.NestedString(stack.Object, "spec", "id"); id != "io.buildpacks.stacks.jammy" {
		t.Errorf("the patch lost the stack id: %q", id)
	}
	if img, _, _ := unstructured.NestedString(get("ClusterBuildpack", "heroku-deb-packages").Object, "spec", "image"); img != "index.docker.io/heroku/buildpack-deb-packages:1.0.1" {
		t.Errorf("the unresolved field moved: %q", img)
	}
	if img, _, _ := unstructured.NestedString(get("ClusterBuildpack", "by-hand").Object, "spec", "image"); img != "index.docker.io/x/y@sha256:fixed" {
		t.Errorf("a digest named by hand moved: %q", img)
	}

	// The tags move on; the start-up pass leaves the pins.
	digests["index.docker.io/paketobuildpacks/nodejs:latest"] = "sha256:node2"
	digests["index.docker.io/heroku/buildpack-deb-packages:1.0.1"] = "sha256:deb1"
	res, err = Refresh(ctx, dyn, digests, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) != 1 || res.Changes[0].Name != "heroku-deb-packages" {
		t.Errorf("start-up pass changes = %v, want the unpinned one only", res.Changes)
	}
	// The weekly pass moves them.
	res, err = Refresh(ctx, dyn, digests, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) != 1 || res.Changes[0].To != "index.docker.io/paketobuildpacks/nodejs@sha256:node2" {
		t.Errorf("weekly pass changes = %v", res.Changes)
	}
	// Nothing moved: nothing to do.
	if res, err = Refresh(ctx, dyn, digests, false); err != nil || len(res.Changes) != 0 {
		t.Errorf("a still catalog changed: %v, %v", res.Changes, err)
	}
}

// An upgrade applies the manifests again: a pin from the tag the manifest
// names is kept; a manifest naming a new tag, or a digest, is applied as
// written; a live digest with no record of its tag is applied over.
func TestKeepPins(t *testing.T) {
	live := catalogObject("ClusterStack", "jammy", map[string]interface{}{
		"buildImage": map[string]interface{}{"image": "index.docker.io/paketobuildpacks/build-jammy-base@sha256:build1"},
		"runImage":   map[string]interface{}{"image": "index.docker.io/paketobuildpacks/run-jammy-base@sha256:run1"},
	}, map[string]string{
		"shpyrd.io/pinned-build-image": "index.docker.io/paketobuildpacks/build-jammy-base:latest",
		"shpyrd.io/pinned-run-image":   "index.docker.io/paketobuildpacks/run-jammy-base:latest",
	})
	desired := catalogObject("ClusterStack", "jammy", map[string]interface{}{
		"buildImage": map[string]interface{}{"image": "index.docker.io/paketobuildpacks/build-jammy-base:latest"},
		"runImage":   map[string]interface{}{"image": "index.docker.io/paketobuildpacks/run-jammy-full:latest"},
	}, nil)
	if kept := KeepPins(desired, live); kept != 1 {
		t.Errorf("kept %d pins, want 1", kept)
	}
	if img, _, _ := unstructured.NestedString(desired.Object, "spec", "buildImage", "image"); img != "index.docker.io/paketobuildpacks/build-jammy-base@sha256:build1" {
		t.Errorf("build image = %q, the pin was not kept", img)
	}
	if img, _, _ := unstructured.NestedString(desired.Object, "spec", "runImage", "image"); img != "index.docker.io/paketobuildpacks/run-jammy-full:latest" {
		t.Errorf("run image = %q, the new tag was not applied", img)
	}
	if KeepPins(catalogObject("ClusterBuildpack", "x", map[string]interface{}{"image": "a/b:1"}, nil), nil) != 0 {
		t.Error("nothing live, nothing kept")
	}
	unrecorded := catalogObject("ClusterBuildpack", "x", map[string]interface{}{"image": "index.docker.io/a/b@sha256:1"}, nil)
	if KeepPins(catalogObject("ClusterBuildpack", "x", map[string]interface{}{"image": "index.docker.io/a/b:1"}, nil), unrecorded) != 0 {
		t.Error("a digest with no record of its tag was kept")
	}
}
