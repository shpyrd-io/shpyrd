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

func catalogApp(commit string) *shpyrdv1.App {
	return &shpyrdv1.App{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			shpyrdv1.AnnotationCatalogEntry:    "fluxyr",
			shpyrdv1.AnnotationCatalogDigest:   "sha256:b",
			shpyrdv1.AnnotationCatalogCommit:   commit,
			shpyrdv1.AnnotationCatalogRevision: "v2",
		}},
		Spec: shpyrdv1.AppSpec{Source: &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: "https://example.test/r", Revision: commit}}},
	}
}

func recordCatalog(app *shpyrdv1.App, image, hash, source string) *shpyrdv1.Release {
	recordRelease(app, image, hash, "", source, metav1.Now(), "", nil)
	stampCatalogVersion(app)
	return app.CurrentRelease()
}

func TestAReleaseBuiltFromTheVersionTheAppIsSetToCarriesIt(t *testing.T) {
	commit := "0123456789abcdef0123456789abcdef01234567"
	app := catalogApp(commit)
	rel := recordCatalog(app, "r/a@sha256:1", "h1", short(commit))
	want := shpyrdv1.CatalogVersion{Entry: "fluxyr", Digest: "sha256:b", Commit: commit, Revision: "v2"}
	if rel.Catalog == nil || *rel.Catalog != want {
		t.Fatalf("catalog = %+v", rel.Catalog)
	}
}

func TestAReleaseNotBuiltFromThatCommitCarriesNoVersion(t *testing.T) {
	commit := "0123456789abcdef0123456789abcdef01234567"
	// Another commit built, the same code from a working tree, an
	// archive, an image: none is the catalog's version.
	for name, source := range map[string]string{"commit": "ffffffffffff", "dirty": short(commit) + "-dirty", "archive": "archive abc", "none": ""} {
		if rel := recordCatalog(catalogApp(commit), "r/a@sha256:1", "h1", source); rel.Catalog != nil {
			t.Errorf("%s: catalog = %+v", name, rel.Catalog)
		}
	}
	app := catalogApp(commit)
	app.Spec.Source.Git.Revision = "main"
	if rel := recordCatalog(app, "r/a@sha256:1", "h1", short(commit)); rel.Catalog != nil {
		t.Errorf("a branch deployed by a person: %+v", rel.Catalog)
	}
	app = catalogApp(commit)
	app.Spec.Image = "r/a@sha256:1"
	if rel := recordCatalog(app, "r/a@sha256:1", "h1", short(commit)); rel.Catalog != nil {
		t.Errorf("an image deploy: %+v", rel.Catalog)
	}
	plain := &shpyrdv1.App{}
	if rel := recordCatalog(plain, "r/a@sha256:1", "h1", short(commit)); rel.Catalog != nil {
		t.Errorf("a project not from the catalog: %+v", rel.Catalog)
	}
}

func TestReleasesThatReuseABuildKeepItsVersion(t *testing.T) {
	a, b := "0123456789abcdef0123456789abcdef01234567", "89abcdef0123456789abcdef0123456789abcdef"
	app := catalogApp(a)
	app.Annotations[shpyrdv1.AnnotationCatalogDigest] = "sha256:a"
	recordCatalog(app, "r/a@sha256:1", "h1", short(a))
	// A change of config vars alone keeps the version.
	if rel := recordCatalog(app, "r/a@sha256:1", "h2", short(a)); rel.Catalog == nil || rel.Catalog.Commit != a {
		t.Fatalf("config change: %+v", rel.Catalog)
	}
	// The update to B.
	app = withTarget(app, b, "sha256:b")
	if rel := recordCatalog(app, "r/b@sha256:2", "h3", short(b)); rel.Catalog == nil || rel.Catalog.Commit != b {
		t.Fatalf("update: %+v", rel.Catalog)
	}
	// A rollback pins A's image: the release carries A, not what the App is set to.
	app.Spec.Image = "r/a@sha256:1"
	rel := recordCatalog(app, "r/a@sha256:1", "h1", short(a))
	if rel.Catalog == nil || rel.Catalog.Commit != a || rel.Catalog.Digest != "sha256:a" {
		t.Fatalf("rollback: %+v", rel.Catalog)
	}
	// A config change while rolled back stays on A.
	if rel := recordCatalog(app, "r/a@sha256:1", "h4", short(a)); rel.Catalog == nil || rel.Catalog.Commit != a {
		t.Fatalf("config change after rollback: %+v", rel.Catalog)
	}
}

func TestAnOldProjectsReleasesCarryNoVersionUntilItIsUpdated(t *testing.T) {
	b := "89abcdef0123456789abcdef0123456789abcdef"
	app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
		shpyrdv1.AnnotationCatalogEntry: "fluxyr", shpyrdv1.AnnotationCatalogDigest: "sha256:old",
	}}, Spec: shpyrdv1.AppSpec{Source: &shpyrdv1.Source{Git: &shpyrdv1.GitSource{URL: "https://example.test/r", Revision: "main"}}}}
	if rel := recordCatalog(app, "r/a@sha256:1", "h1", "1111aaaa2222"); rel.Catalog != nil {
		t.Fatalf("installed on a branch: %+v", rel.Catalog)
	}
	if rel := recordCatalog(app, "r/a@sha256:1", "h2", "1111aaaa2222"); rel.Catalog != nil {
		t.Fatalf("config change: %+v", rel.Catalog)
	}
	app = withTarget(app, b, "sha256:b")
	if rel := recordCatalog(app, "r/b@sha256:2", "h3", short(b)); rel.Catalog == nil || rel.Catalog.Commit != b {
		t.Fatalf("update: %+v", rel.Catalog)
	}
}

func TestAConfigChangeAfterTheAppIsSetToANewerVersionOfTheSameCodeRunsIt(t *testing.T) {
	c := "0123456789abcdef0123456789abcdef01234567"
	app := catalogApp(c)
	app.Annotations[shpyrdv1.AnnotationCatalogDigest] = "sha256:1"
	recordCatalog(app, "r/a@sha256:1", "h1", short(c))
	app = withTarget(app, c, "sha256:2") // the entry's env changed; the code did not
	if rel := recordCatalog(app, "r/a@sha256:1", "h2", short(c)); rel.Catalog == nil || rel.Catalog.Digest != "sha256:2" {
		t.Fatalf("%+v", rel.Catalog)
	}
}

func withTarget(app *shpyrdv1.App, commit, digest string) *shpyrdv1.App {
	app.Annotations[shpyrdv1.AnnotationCatalogCommit] = commit
	app.Annotations[shpyrdv1.AnnotationCatalogDigest] = digest
	app.Spec.Source.Git.Revision = commit
	return app
}
