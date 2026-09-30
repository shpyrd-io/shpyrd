package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
)

// REVISION names the code of the image that runs: the source recorded by
// the release that first deployed the image (a rollback pins an older image
// while spec.source names newer code), else the current source; archive
// digests lose their prefix, commits keep their -dirty marker.
func TestSourceOfAndRevisionValue(t *testing.T) {
	app := &shpyrdv1.App{Status: shpyrdv1.AppStatus{Releases: []shpyrdv1.Release{
		{Number: 1, Image: "r/a@sha256:1", Source: "aaaa11112222"},
		{Number: 2, Image: "r/a@sha256:1", Source: "aaaa11112222"},
		{Number: 3, Image: "r/a@sha256:2", Source: "bbbb33334444"},
	}}}
	if got := sourceOf(app, "r/a@sha256:1", "bbbb33334444"); got != "aaaa11112222" {
		t.Errorf("sourceOf(rolled back image) = %q", got)
	}
	if got := sourceOf(app, "r/a@sha256:3", "cccc55556666"); got != "cccc55556666" {
		t.Errorf("sourceOf(new image) = %q", got)
	}
	for in, want := range map[string]string{
		"3f2a9c1b8d7e":         "3f2a9c1b8d7e",
		"3f2a9c1b8d7e-dirty":   "3f2a9c1b8d7e-dirty",
		"archive 9e8d7c6b5a43": "9e8d7c6b5a43",
		"main":                 "main",
		"":                     "",
	} {
		if got := revisionValue(in); got != want {
			t.Errorf("revisionValue(%q) = %q, want %q", in, got, want)
		}
	}
	cfg := Config{Domain: "example.test"}.Defaults()
	web1 := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Name: "web1"}}
	env := cfg.platformEnv(web1, "3f2a9c1b8d7e", "web", 0)
	if !hasEnv(env, "REVISION", "3f2a9c1b8d7e") || !hasEnv(env, "SHPYRD_REVISION", "3f2a9c1b8d7e") || !hasEnv(env, "SHPYRD_PROJECT", "web1") {
		t.Errorf("platformEnv = %v", env)
	}
	for _, e := range cfg.platformEnv(web1, "", "web", 0) {
		if e.Name == "REVISION" || e.Name == "SHPYRD_REVISION" || e.Name == "SHPYRD_PROJECT_REVISION" {
			t.Errorf("a prebuilt image has no revision; got %s=%q", e.Name, e.Value)
		}
	}
}

// Every process is told that it runs here, which project it belongs to
// by id and by name, which process it is and which release it runs.
func TestPlatformEnvTellsAProcessWhatItIs(t *testing.T) {
	cfg := Config{Domain: "example.test"}.Defaults()
	app := &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Name: "shop", Annotations: map[string]string{shpyrdv1.AnnotationDisplayName: "The Shop"}},
		Spec:       shpyrdv1.AppSpec{ID: "01J9ZPX5X9T6R5AKD1H3C0FQ6M", Slug: "shop"},
	}
	env := cfg.platformEnv(app, "3f2a9c1b8d7e", "worker", 5)
	for name, want := range map[string]string{
		"RUNNING_IN_SHPYRD":       "true",
		"SHPYRD_PROJECT":          "shop",
		"SHPYRD_PROJECT_ID":       "01J9ZPX5X9T6R5AKD1H3C0FQ6M",
		"SHPYRD_PROJECT_NAME":     "The Shop",
		"SHPYRD_PROCESS":          "worker",
		"SHPYRD_RELEASE":          "5",
		"SHPYRD_RELEASE_VERSION":  "v5",
		"SHPYRD_PROJECT_REVISION": "3f2a9c1b8d7e",
	} {
		if !hasEnv(env, name, want) {
			t.Errorf("platformEnv lacks %s=%q: %v", name, want, env)
		}
	}
	for _, e := range cfg.platformEnv(app, "", "web", 0) {
		if e.Name == "SHPYRD_RELEASE" || e.Name == "SHPYRD_RELEASE_VERSION" {
			t.Errorf("no release yet, got %s=%q", e.Name, e.Value)
		}
	}
	// The number a rollout belongs to is the one recordRelease will write.
	if n := releaseNumber(app, "r/a@sha256:1", "h1"); n != 1 {
		t.Errorf("first release = %d", n)
	}
	recordRelease(app, "r/a@sha256:1", "h1", "", "", metav1.Now(), "", nil)
	if n := releaseNumber(app, "r/a@sha256:1", "h1"); n != 1 {
		t.Errorf("same image and config = release %d, want 1", n)
	}
	if n := releaseNumber(app, "r/a@sha256:2", "h1"); n != 2 {
		t.Errorf("new image = release %d, want 2", n)
	}
}

// Releases that leave the history hand their images to StaleImages unless a
// kept release (a config-only release shares its predecessor's image) still
// uses them.
func TestUnreferencedImages(t *testing.T) {
	dropped := []shpyrdv1.Release{{Number: 1, Image: "r/a@sha256:1"}, {Number: 2, Image: "r/a@sha256:2"}, {Number: 3, Image: "r/a@sha256:2"}}
	kept := []shpyrdv1.Release{{Number: 4, Image: "r/a@sha256:2"}, {Number: 5, Image: "r/a@sha256:3"}}
	got := unreferencedImages(dropped, kept)
	if len(got) != 1 || got[0] != "r/a@sha256:1" {
		t.Errorf("unreferenced = %v", got)
	}
}
