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
	env := cfg.platformEnv(web1, "3f2a9c1b8d7e")
	if !hasEnv(env, "REVISION", "3f2a9c1b8d7e") || !hasEnv(env, "SHPYRD_REVISION", "3f2a9c1b8d7e") || !hasEnv(env, "SHPYRD_PROJECT", "web1") {
		t.Errorf("platformEnv = %v", env)
	}
	for _, e := range cfg.platformEnv(web1, "") {
		if e.Name == "REVISION" || e.Name == "SHPYRD_REVISION" {
			t.Errorf("a prebuilt image has no revision; got %s=%q", e.Name, e.Value)
		}
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
