package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const testSHA = "0123456789abcdef0123456789abcdef01234567"

func gitApp(rev string, ann map[string]string) *App {
	return &App{
		ObjectMeta: metav1.ObjectMeta{Annotations: ann},
		Spec:       AppSpec{Source: &Source{Git: &GitSource{URL: "https://github.com/acme/shop", Revision: rev}}},
	}
}

func TestIsCommit(t *testing.T) {
	for rev, want := range map[string]bool{
		"main": false, "v1.2": false, "release/2026": false, "": false,
		"a1b2c3d": true, "A1B2C3D": true, testSHA: true,
		"a1b2c3": false, "a1b2c3dx": false,
	} {
		if got := IsCommit(rev); got != want {
			t.Errorf("IsCommit(%q) = %v, want %v", rev, got, want)
		}
	}
}

func TestPinnedCommitHoldsOnlyForItsBranch(t *testing.T) {
	app := gitApp("", map[string]string{AnnotationGitCommit: testSHA, AnnotationGitCommitRef: "main"})
	if app.GitRevision() != "main" || !app.WatchesGit() || app.PinnedCommit() != testSHA || app.BuildRevision() != testSHA {
		t.Fatalf("revision %q watched %v pin %q build %q", app.GitRevision(), app.WatchesGit(), app.PinnedCommit(), app.BuildRevision())
	}
	app.Spec.Source.Git.Revision = "release"
	if app.PinnedCommit() != "" || app.BuildRevision() != "release" {
		t.Errorf("a pin made for main must not build release: pin %q build %q", app.PinnedCommit(), app.BuildRevision())
	}
	bad := gitApp("main", map[string]string{AnnotationGitCommit: "not-a-sha", AnnotationGitCommitRef: "main"})
	if bad.PinnedCommit() != "" {
		t.Errorf("a pin that is not a full SHA is no pin: %q", bad.PinnedCommit())
	}
}

func TestACommitRevisionIsBuiltAsGiven(t *testing.T) {
	app := gitApp("a1b2c3d", map[string]string{AnnotationGitCommit: testSHA, AnnotationGitCommitRef: "a1b2c3d"})
	if app.WatchesGit() || app.PinnedCommit() != "" || app.BuildRevision() != "a1b2c3d" {
		t.Errorf("watched %v pin %q build %q", app.WatchesGit(), app.PinnedCommit(), app.BuildRevision())
	}
	blob := &App{Spec: AppSpec{Source: &Source{Blob: &BlobSource{URL: "http://x/a.tgz"}}}}
	if blob.WatchesGit() || blob.GitRevision() != "" || blob.AutoDeploys() {
		t.Error("an archive has no branch to watch")
	}
}

func TestAutoDeploysUnlessSwitchedOff(t *testing.T) {
	app := gitApp("main", nil)
	if !app.AutoDeploys() {
		t.Error("unset means on")
	}
	off := false
	app.Spec.Source.Git.AutoDeploy = &off
	if app.AutoDeploys() {
		t.Error("false means off")
	}
}
